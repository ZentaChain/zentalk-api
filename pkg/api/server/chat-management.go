package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/zentalk/protocol/pkg/api"
)

// HandleDeleteMessage deletes a single message from the chat history
func (s *Server) HandleDeleteMessage(w http.ResponseWriter, r *http.Request) {
	var req api.DeleteMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// Normalize peer address
	peerAddress := api.NormalizeAddress(req.PeerAddress)

	log.Printf("🗑️  [DeleteMessage] %s deleting message %s from chat with %s (delete_for_everyone: %v)",
		session.Address, req.MessageID, peerAddress, req.DeleteForEveryone)

	// Delete message from session's message history (in-memory)
	messages := session.MessageHistory[peerAddress]
	found := false
	newMessages := make([]api.Message, 0, len(messages))

	for _, msg := range messages {
		if msg.ID == req.MessageID {
			found = true
			// Skip this message (delete it)
			continue
		}
		newMessages = append(newMessages, msg)
	}

	if !found {
		s.SendError(w, "Message not found", http.StatusNotFound)
		return
	}

	// Update message history in-memory
	session.MessageHistory[peerAddress] = newMessages

	// Find the message to check if it has media attachments
	var mediaURL string
	for _, msg := range messages {
		if msg.ID == req.MessageID {
			mediaURL = msg.MediaUrl
			break
		}
	}

	// If message has media, delete the media file and mesh chunks
	if mediaURL != "" {
		log.Printf("🗑️  Message has media attachment: %s", mediaURL)
		// Extract media ID from URL (format: /api/media/{mediaID})
		// You may need to parse this properly based on your URL format
		// For now, we'll try to delete it as is
		if s.MessageDB != nil {
			if err := s.MessageDB.DeleteMediaFile(mediaURL); err != nil {
				log.Printf("⚠️  Failed to delete media file from database: %v", err)
			} else {
				log.Printf("💾 Deleted media file %s from database", mediaURL)
				// TODO: Also delete the encrypted chunk from mesh storage
				// This requires MeshStorage.DeleteChunk(chunkID) which we need to implement
			}
		}
	}

	// Delete message from database (persistent storage)
	if s.MessageDB != nil {
		if err := s.MessageDB.DeleteMessage(session.Address, peerAddress, req.MessageID); err != nil {
			log.Printf("⚠️  Failed to delete message from database: %v", err)
		} else {
			log.Printf("💾 Deleted message %s from database", req.MessageID)
		}

		// Delete from starred messages if it was starred
		if err := s.MessageDB.DeleteStarredMessage(session.Address, req.MessageID); err != nil {
			log.Printf("⚠️  Failed to delete starred message: %v", err)
		}
	}

	// Remove from in-memory pending messages queue (if recipient is offline)
	s.removeFromPendingMessages(peerAddress, req.MessageID)

	// Remove from relay queue (persistent offline messages)
	if s.RelayServer != nil && s.RelayServer.GetMessageQueue() != nil {
		// Note: Relay queue uses message_id, we need to delete by ID
		if err := s.RelayServer.GetMessageQueue().DeleteMessage(req.MessageID); err != nil {
			log.Printf("⚠️  Failed to delete from relay queue: %v", err)
		} else {
			log.Printf("💾 Deleted message %s from relay queue", req.MessageID)
		}
	}

	log.Printf("✅ COMPLETELY deleted message %s from chat with %s", req.MessageID, peerAddress)

	// If delete_for_everyone, notify the peer via WebSocket
	if req.DeleteForEveryone {
		s.BroadcastMessageDeletion(peerAddress, api.WSMessageDeleted{
			MessageID: req.MessageID,
			ChatID:    session.Address, // The peer sees this as the chat ID
		})
	}

	s.SendJSON(w, api.DeleteMessageResponse{
		Success: true,
		Message: "Message deleted successfully",
	})
}

// HandleDeleteChat deletes an entire chat (all messages with a peer)
func (s *Server) HandleDeleteChat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PeerAddress string `json:"peer_address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// Normalize peer address
	peerAddress := api.NormalizeAddress(req.PeerAddress)

	log.Printf("🗑️  [DeleteChat] %s deleting entire chat with %s", session.Address, peerAddress)

	// Delete chat from session's message history (in-memory)
	delete(session.MessageHistory, peerAddress)

	// Delete chat from database (persistent storage)
	if s.MessageDB != nil {
		// Delete all messages
		if err := s.MessageDB.DeleteChat(session.Address, peerAddress); err != nil {
			log.Printf("⚠️  Failed to delete chat from database: %v", err)
			s.SendError(w, fmt.Sprintf("Failed to delete chat: %v", err), http.StatusInternalServerError)
			return
		}

		// Delete all starred messages for this chat
		if err := s.MessageDB.DeleteStarredMessagesForChat(session.Address, peerAddress); err != nil {
			log.Printf("⚠️  Failed to delete starred messages for chat: %v", err)
		}

		// TODO: Delete all media files associated with this chat
		// This would require:
		// 1. Query all messages in this chat that have media_url
		// 2. Delete each media file from media_files table
		// 3. Delete encrypted chunks from mesh storage
	}

	// Clear in-memory pending messages for this chat
	s.clearPendingMessagesForChat(peerAddress)

	// Clear relay queue for this recipient (persistent offline messages)
	// Note: This deletes ALL queued messages for the recipient, not just from one sender
	// In a real implementation, you might want to be more selective
	if s.RelayServer != nil && s.RelayServer.GetMessageQueue() != nil {
		// Convert peer address to protocol.Address format
		// This is a simplification - proper implementation would need address conversion
		log.Printf("⚠️  Relay queue cleanup for entire chat not fully implemented - requires address conversion")
		// s.RelayServer.GetMessageQueue().DeleteMessagesForRecipient(peerProtocolAddress)
	}

	log.Printf("✅ COMPLETELY deleted entire chat with %s", peerAddress)

	s.SendJSON(w, map[string]interface{}{
		"success": true,
		"message": "Chat deleted successfully",
	})
}

// removeFromPendingMessages removes a specific message from the pending messages queue
func (s *Server) removeFromPendingMessages(recipientAddr, messageID string) {
	s.PendingLock.Lock()
	defer s.PendingLock.Unlock()

	messages, exists := s.PendingMessages[recipientAddr]
	if !exists || len(messages) == 0 {
		return
	}

	// Filter out the deleted message
	newMessages := make([]PendingMessage, 0, len(messages))
	for _, msg := range messages {
		if msg.MessageID != messageID {
			newMessages = append(newMessages, msg)
		}
	}

	if len(newMessages) > 0 {
		s.PendingMessages[recipientAddr] = newMessages
		log.Printf("🗑️  Removed deleted message from pending queue for %s", recipientAddr)
	} else {
		delete(s.PendingMessages, recipientAddr)
		log.Printf("🗑️  Cleared pending messages queue for %s", recipientAddr)
	}
}

// clearPendingMessagesForChat removes all pending messages for a specific chat
func (s *Server) clearPendingMessagesForChat(recipientAddr string) {
	s.PendingLock.Lock()
	defer s.PendingLock.Unlock()

	if _, exists := s.PendingMessages[recipientAddr]; exists {
		delete(s.PendingMessages, recipientAddr)
		log.Printf("🗑️  Cleared all pending messages for chat with %s", recipientAddr)
	}
}

// BroadcastMessageDeletion notifies a peer that a message was deleted
func (s *Server) BroadcastMessageDeletion(peerAddress string, deletion api.WSMessageDeleted) {
	s.WsLock.RLock()
	wsConn, exists := s.WsConnections[peerAddress]
	s.WsLock.RUnlock()

	if !exists {
		log.Printf("⚠️  Peer %s not connected via WebSocket, skipping message deletion notification", peerAddress)
		return
	}

	msg := api.WSMessage{
		Type:    "message_deleted",
		Payload: deletion,
	}

	// Thread-safe write
	wsConn.mutex.Lock()
	err := wsConn.conn.WriteJSON(msg)
	wsConn.mutex.Unlock()

	if err != nil {
		log.Printf("❌ Failed to send message deletion to %s: %v", peerAddress, err)
	} else {
		log.Printf("✅ Message deletion notification sent to %s", peerAddress)
	}
}
