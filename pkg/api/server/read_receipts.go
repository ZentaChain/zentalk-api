package server

import (
	"github.com/ZentaChain/zentalk-api/pkg/api"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// handleMarkAsRead marks a message as read and sends a read receipt
func (s *Server) handleMarkAsRead(w http.ResponseWriter, r *http.Request) {
	// Get wallet address from header
	walletAddr := r.Header.Get("X-Wallet-Address")
	if walletAddr == "" {
		s.SendError(w, "Missing wallet address", http.StatusUnauthorized)
		return
	}

	// Normalize wallet address
	walletAddr = api.NormalizeAddress(walletAddr)

	// Get session
	session := s.GetSession(walletAddr)
	if session == nil {
		s.SendError(w, "Not initialized", http.StatusBadRequest)
		return
	}

	// Parse request
	var req api.MarkAsReadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Normalize peer address
	req.PeerAddress = api.NormalizeAddress(req.PeerAddress)

	walletDisplay, _ := api.CreateDisplayNameFromAddress(walletAddr)
	peerDisplay, _ := api.CreateDisplayNameFromAddress(req.PeerAddress)
	log.Printf("📖 [MarkAsRead] User %s marking message %s from %s as read",
		walletDisplay, req.MessageID, peerDisplay)

	// For MVP: We skip the network-layer read receipt (which requires RSA public keys)
	// and rely solely on WebSocket broadcast. In a full implementation, read receipts
	// should be sent through the Double Ratchet session like regular messages.
	//
	// TODO: Implement read receipts through ratchet session for proper end-to-end encryption

	log.Printf("📖 [MarkAsRead] Marking message as read locally and broadcasting via WebSocket")

	// Save read status to database
	if s.MessageDB != nil {
		if err := s.MessageDB.MarkMessageAsRead(walletAddr, req.PeerAddress, req.MessageID); err != nil {
			log.Printf("⚠️  Failed to save read status to database: %v", err)
		} else {
			log.Printf("💾 Saved read status to database for message %s", req.MessageID)

			// Update in-memory cache to keep it in sync with database
			// Get the messages slice for this peer
			messages, exists := session.MessageHistory[req.PeerAddress]
			if exists {
				// Find and update the specific message
				found := false
				for i := range messages {
					if messages[i].ID == req.MessageID {
						messages[i].Unread = false
						found = true
						log.Printf("✅ Updated in-memory cache for message %s (Unread: false)", req.MessageID)
						break
					}
				}
				if !found {
					log.Printf("⚠️  Message %s not found in in-memory cache for peer %s", req.MessageID, req.PeerAddress)
				}
				// Reassign the slice back to the map to ensure it's updated
				session.MessageHistory[req.PeerAddress] = messages
			} else {
				log.Printf("⚠️  No message history found for peer %s in in-memory cache", req.PeerAddress)
			}
		}
	}

	// Broadcast read receipt to peer via WebSocket (if online)
	s.broadcastReadReceiptToPeer(req.PeerAddress, api.WSReadReceipt{
		From:       walletAddr,
		MessageID:  req.MessageID,
		ReadStatus: "read",
		Timestamp:  time.Now().UnixMilli(),
	})

	log.Printf("✅ Read receipt sent from %s to %s for message %s",
		walletDisplay, peerDisplay, req.MessageID)

	// Send success response
	json.NewEncoder(w).Encode(api.MarkAsReadResponse{
		Success: true,
		Message: "Read receipt sent",
	})
}

// broadcastReadReceiptToPeer sends a read receipt to a specific peer via WebSocket
func (s *Server) broadcastReadReceiptToPeer(peerAddress string, receipt api.WSReadReceipt) {
	s.WsLock.RLock()
	wsConn, exists := s.WsConnections[peerAddress]
	s.WsLock.RUnlock()

	if !exists {
		peerDisplay, _ := api.CreateDisplayNameFromAddress(peerAddress)
		log.Printf("⚠️  Peer %s not connected via WebSocket, skipping read receipt broadcast", peerDisplay)
		return
	}

	msg := api.WSMessage{
		Type:    "read_receipt",
		Payload: receipt,
	}

	peerDisplay, _ := api.CreateDisplayNameFromAddress(peerAddress)

	// Thread-safe write
	wsConn.mutex.Lock()
	err := wsConn.conn.WriteJSON(msg)
	wsConn.mutex.Unlock()

	if err != nil {
		log.Printf("❌ Failed to send read receipt to %s: %v", peerDisplay, err)
	} else {
		log.Printf("📤 Read receipt sent to %s via WebSocket", peerDisplay)
	}
}
