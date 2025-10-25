package server

import (
	"github.com/zentalk/protocol/pkg/api"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// storePendingMessage stores a message for an offline recipient
func (s *Server) storePendingMessage(from, to, content, messageID string) {
	s.PendingLock.Lock()
	defer s.PendingLock.Unlock()

	msg := PendingMessage{
		From:      from,
		To:        to,
		Content:   content,
		Timestamp: time.Now().UnixMilli(),
		MessageID: messageID,
	}

	s.PendingMessages[to] = append(s.PendingMessages[to], msg)
	log.Printf("📦 Stored pending message for offline user %s (from: %s)", to[:8], from[:8])
}

// getPendingMessages retrieves and clears pending messages for a user
func (s *Server) getPendingMessages(address string) []PendingMessage {
	s.PendingLock.Lock()
	defer s.PendingLock.Unlock()

	messages := s.PendingMessages[address]
	delete(s.PendingMessages, address)

	if len(messages) > 0 {
		log.Printf("📬 Retrieved %d pending messages for %s", len(messages), address[:8])
	}

	return messages
}

// deliverPendingMessages automatically delivers pending messages when user comes online
func (s *Server) deliverPendingMessages(walletAddr string) {
	pendingMsgs := s.getPendingMessages(walletAddr)

	if len(pendingMsgs) == 0 {
		return
	}

	log.Printf("📨 Delivering %d pending messages to %s", len(pendingMsgs), walletAddr[:8])

	session := s.GetSession(walletAddr)
	if session == nil {
		log.Printf("⚠️  Session not found for %s, cannot deliver pending messages", walletAddr[:8])
		// Put messages back
		s.PendingLock.Lock()
		s.PendingMessages[walletAddr] = pendingMsgs
		s.PendingLock.Unlock()
		return
	}

	// Deliver each pending message
	for _, msg := range pendingMsgs {
		// Normalize sender address
		senderAddr := api.NormalizeAddress(msg.From)

		// Decrypt message content if it's encrypted
		content := msg.Content
		var encryptedMsg EncryptedOfflineMessage
		if err := json.Unmarshal([]byte(msg.Content), &encryptedMsg); err == nil && encryptedMsg.Ciphertext != "" {
			// api.Message is encrypted - decrypt it
			log.Printf("🔓 [Offline Delivery] Decrypting offline message from %s", senderAddr[:8])

			decryptedContent, err := s.DecryptOfflineMessage(walletAddr, &encryptedMsg)
			if err != nil {
				log.Printf("❌ [Offline Delivery] Failed to decrypt message from %s: %v (delivering encrypted)", senderAddr[:8], err)
				// Fallback: deliver encrypted content (user will see encrypted data)
			} else {
				content = decryptedContent
				log.Printf("✅ [Offline Delivery] Successfully decrypted message from %s", senderAddr[:8])
			}
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

		// Store message in history (with decrypted content)
		message := api.Message{
			ID:        msg.MessageID,
			Content:   content,
			Timestamp: api.FormatTimestamp(time.UnixMilli(msg.Timestamp)),
			Sender:    contact,
			Unread:    true,
		}

		session.MessageHistory[senderAddr] = append(session.MessageHistory[senderAddr], message)

		// Broadcast via WebSocket (if online)
		wsMsg := api.WSMessage{
			Type: "message",
			Payload: api.WSIncomingMessage{
				ID:        msg.MessageID,
				From:      senderAddr,
				Content:   content,
				Timestamp: msg.Timestamp / 1000, // Convert to seconds
			},
		}

		if err := s.SendWSMessage(walletAddr, wsMsg); err != nil {
			log.Printf("❌ [WebSocket] Failed to send pending message to %s: %v", walletAddr[:8], err)
		} else {
			log.Printf("✅ [WebSocket] Delivered pending message to %s (from: %s)", walletAddr[:8], senderAddr[:8])
		}
	}
}

// handleGetPendingMessages handles GET /api/pending-messages endpoint
func (s *Server) handleGetPendingMessages(w http.ResponseWriter, r *http.Request) {
	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	pendingMsgs := s.getPendingMessages(session.Address)

	s.SendJSON(w, map[string]interface{}{
		"success":  true,
		"messages": pendingMsgs,
		"count":    len(pendingMsgs),
	})
}
