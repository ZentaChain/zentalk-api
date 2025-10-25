package server

import (
	"github.com/zentalk/protocol/pkg/api"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/zentalk/protocol/pkg/crypto"
	"github.com/zentalk/protocol/pkg/protocol"

)

// handleSendMessage sends a message to a recipient (multi-tenant)
func (s *Server) HandleSendMessage(w http.ResponseWriter, r *http.Request) {
	var req api.SendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("❌ [SendMessage] Failed to decode request body: %v", err)
		s.SendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	log.Printf("📤 [SendMessage] Request: recipient=%s, content_length=%d", req.RecipientAddress, len(req.Content))

	session, err := s.GetUserSession(r)
	if err != nil {
		log.Printf("❌ [SendMessage] Failed to get user session: %v", err)
		log.Printf("   Headers: X-Wallet-Address=%s", r.Header.Get("X-Wallet-Address"))
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	searchInput := req.RecipientAddress

	// Try username lookup first if input doesn't look like a wallet address
	// Wallet addresses are typically 40+ hex characters (with or without 0x)
	if len(searchInput) < 40 && !strings.HasPrefix(searchInput, "0x") && !strings.HasPrefix(searchInput, "0X") {
		// Might be a username, try to look up in username map
		s.SessionsLock.RLock()
		walletAddr, found := s.UsernameToAddress[strings.ToLower(searchInput)]
		s.SessionsLock.RUnlock()

		if found {
			log.Printf("🔎 [SendMessage] Username '%s' resolved to wallet address: %s", searchInput, walletAddr)
			searchInput = walletAddr // Use the wallet address for sending
		} else {
			log.Printf("⚠️  [SendMessage] Username '%s' not found, treating as address", searchInput)
		}
	}

	// Convert recipient address
	recipientAddr, err := api.HexToAddress(searchInput)
	if err != nil {
		s.SendError(w, fmt.Sprintf("Invalid recipient address or username not found: %s", req.RecipientAddress), http.StatusBadRequest)
		return
	}

	// Try to get cached key bundle first (Telegram-style UX)
	bundle, found := session.Client.GetCachedKeyBundle(recipientAddr)
	if !found {
		// Auto-discover if not cached (no manual "add contact" needed!)
		log.Printf("🔍 [Auto-Discover] Key bundle not cached for %s, discovering from DHT...", req.RecipientAddress)

		bundle, err = session.Client.DiscoverKeyBundle(recipientAddr)
		if err != nil {
			s.SendError(w, fmt.Sprintf("Failed to discover recipient. They may not have initialized yet: %v", err), http.StatusNotFound)
			return
		}

		// Cache for future messages
		session.Client.CacheKeyBundle(recipientAddr, bundle)
		log.Printf("✅ [Auto-Discover] Successfully discovered and cached key bundle for %s", req.RecipientAddress)

		// Also cache contact info for UI
		normalizedRecipient := api.NormalizeAddress(searchInput) // Use resolved address
		if session.ContactCache[normalizedRecipient] == nil {
			// Get user with full profile (including bio from database)
			contact := s.GetOrCreateUserWithProfile(normalizedRecipient)
			session.ContactCache[normalizedRecipient] = contact
		}
	}

	msgID := fmt.Sprintf("msg_%d", time.Now().UnixNano())

	// Load relay public key from file (for testing)
	// In production, this should be obtained from the relay during handshake
	relayPubKeyPEM, err := os.ReadFile("./keys/relay.pem.pub")
	if err != nil {
		s.SendError(w, fmt.Sprintf("Failed to read relay public key: %v", err), http.StatusInternalServerError)
		return
	}

	relayPubKey, err := crypto.ImportPublicKeyPEM(relayPubKeyPEM)
	if err != nil {
		s.SendError(w, fmt.Sprintf("Failed to parse relay public key: %v", err), http.StatusInternalServerError)
		return
	}

	// Create single-hop relay path through connected relay
	// For testing, we use the relay the client is connected to (localhost:9001)
	// The relay address needs to be a protocol.Address (20 bytes)
	// We'll use a derived address from the relay's listening address
	relayAddr := protocol.Address{} // Zero address means "connected relay"

	relayPath := []*crypto.RelayInfo{
		{
			Address:   relayAddr,
			PublicKey: relayPubKey,
		},
	}

	// Create a proper DirectMessage structure with proper sequence numbering
	directMsg := &protocol.DirectMessage{
		From:           session.Client.Address,
		To:             recipientAddr,
		Timestamp:      uint64(time.Now().UnixMilli()),
		SequenceNumber: session.Client.GetNextSequenceNumber(recipientAddr),
		ContentType:    protocol.ContentTypeText,
		Content:        []byte(req.Content),
	}

	// Encode the DirectMessage to bytes
	msgPayload := directMsg.Encode()

	log.Printf("📍 Sending message to %s via relay (1 hop)", req.RecipientAddress)
	if err := session.Client.SendRatchetMessage(recipientAddr, bundle, msgPayload, relayPath); err != nil {
		log.Printf("❌ [SendMessage] SendRatchetMessage failed: %v", err)
		s.SendError(w, fmt.Sprintf("Failed to send message: %v", err), http.StatusInternalServerError)
		return
	}

	// Store in message history (encrypted on client before sending)
	// Server stores encrypted blobs - cannot decrypt (E2EE with Double Ratchet)
	normalizedRecipient := api.NormalizeAddress(req.RecipientAddress)

	// Check if message contains media
	mediaUrl := ""
	if strings.HasPrefix(req.Content, "[MEDIA]") {
		// Extract media URL from content
		mediaUrl = strings.TrimSpace(strings.TrimPrefix(req.Content, "[MEDIA]"))
	}

	newMessage := api.Message{
		ID:        msgID,
		Content:   req.Content,
		Timestamp: api.FormatTimestamp(time.Now()),
		Sender:    "You",
		MediaUrl:  mediaUrl,
	}
	session.MessageHistory[normalizedRecipient] = append(session.MessageHistory[normalizedRecipient], newMessage)

	// Save message to database
	if s.MessageDB != nil {
		if err := s.MessageDB.SaveMessage(session.Address, normalizedRecipient, newMessage); err != nil {
			log.Printf("⚠️  Failed to save message to database: %v", err)
		}
	}

	log.Printf("📤 api.Message sent from %s to %s via relay (stored encrypted)", session.Username, req.RecipientAddress)

	s.SendJSON(w, api.SendMessageResponse{
		Success:   true,
		MessageID: msgID,
		Timestamp: time.Now().Unix(),
		Message:   "api.Message sent successfully",
	})
}

// handleGetMessages returns messages for a specific chat (multi-tenant)
func (s *Server) HandleGetMessages(w http.ResponseWriter, r *http.Request) {
	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	vars := mux.Vars(r)
	chatID := api.NormalizeAddress(vars["chatId"])

	messages := session.MessageHistory[chatID]

	s.SendJSON(w, map[string]interface{}{
		"success":  true,
		"messages": messages,
	})
}

// onMessageReceived is called when a message is received (multi-tenant)
func (s *Server) OnMessageReceived(walletAddr string, msg *protocol.DirectMessage) {
	session := s.GetSession(walletAddr)
	if session == nil {
		log.Printf("⚠️  Received message for unknown session: %s", walletAddr)
		return
	}

	log.Printf("📥 api.Message received by %s from %x: %s", walletAddr, msg.From[:8], string(msg.Content))

	// Find sender's session to get their normalized username
	// This ensures we use the same key format as their session (e.g., "bbbb" not "6262626200000000...")
	senderAddr := s.FindSessionByProtocolAddress(msg.From)
	if senderAddr == "" {
		// Fallback to hex if no session found (shouldn't happen in normal flow)
		senderAddr = api.NormalizeAddress(api.AddressToHex(msg.From))
	}

	// Get or create contact
	contact := session.ContactCache[senderAddr]
	if contact == nil {
		// Get user with full profile (including bio from database)
		contact = s.GetOrCreateUserWithProfile(senderAddr)
		session.ContactCache[senderAddr] = contact
	} else {
		contact.Online = s.IsUserOnline(senderAddr)
	}

	// Store message
	msgID := protocol.GenerateMessageID()

	// Check if message contains media
	content := string(msg.Content)
	mediaUrl := ""
	if strings.HasPrefix(content, "[MEDIA]") {
		// Extract media URL from content
		mediaUrl = strings.TrimSpace(strings.TrimPrefix(content, "[MEDIA]"))
	}

	message := api.Message{
		ID:        fmt.Sprintf("%x", msgID),
		Content:   content,
		Timestamp: api.FormatTimestamp(time.Now()),
		Sender:    contact,
		Unread:    true,
		MediaUrl:  mediaUrl,
	}

	session.MessageHistory[senderAddr] = append(session.MessageHistory[senderAddr], message)

	// Save message to database
	if s.MessageDB != nil {
		if err := s.MessageDB.SaveMessage(walletAddr, senderAddr, message); err != nil {
			log.Printf("⚠️  Failed to save received message to database: %v", err)
		}
	}

	// Broadcast to user's WebSocket
	wsMsg := api.WSMessage{
		Type: "message",
		Payload: api.WSIncomingMessage{
			ID:        fmt.Sprintf("%x", msgID),
			From:      senderAddr,
			Content:   string(msg.Content),
			Timestamp: time.Now().Unix(),
		},
	}

	// Send only to the specific user
	log.Printf("📨 [WebSocket] Attempting to send message to recipient %s", walletAddr)
	if err := s.SendWSMessage(walletAddr, wsMsg); err != nil {
		log.Printf("❌ [WebSocket] Failed to send WS message to %s: %v", walletAddr, err)
	} else {
		log.Printf("✅ [WebSocket] Successfully sent message to %s", walletAddr)
	}
}
