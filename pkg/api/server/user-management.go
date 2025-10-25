package server

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/ZentaChain/zentalk-api/pkg/api"
)

// HandleMuteUser mutes a user (no notifications from them)
func (s *Server) HandleMuteUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserAddress string `json:"user_address"`
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

	// Normalize address
	targetAddress := api.NormalizeAddress(req.UserAddress)

	log.Printf("🔇 [MuteUser] %s muting %s", session.Address, targetAddress)

	// Mute in database
	if s.MessageDB != nil {
		if err := s.MessageDB.MuteContact(session.Address, targetAddress); err != nil {
			log.Printf("⚠️  Failed to mute user in database: %v", err)
			s.SendError(w, "Failed to mute user", http.StatusInternalServerError)
			return
		}
		log.Printf("💾 Muted %s in database", targetAddress)
	}

	// Broadcast to all current user's devices
	s.BroadcastUserAction(session.Address, api.WSUserAction{
		Action:      "user_muted",
		UserAddress: targetAddress,
	})

	log.Printf("✅ Successfully muted %s", targetAddress)

	s.SendJSON(w, map[string]interface{}{
		"success": true,
		"message": "User muted successfully",
	})
}

// HandleUnmuteUser unmutes a previously muted user
func (s *Server) HandleUnmuteUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserAddress string `json:"user_address"`
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

	// Normalize address
	targetAddress := api.NormalizeAddress(req.UserAddress)

	log.Printf("🔊 [UnmuteUser] %s unmuting %s", session.Address, targetAddress)

	// Unmute in database
	if s.MessageDB != nil {
		if err := s.MessageDB.UnmuteContact(session.Address, targetAddress); err != nil {
			log.Printf("⚠️  Failed to unmute user in database: %v", err)
			s.SendError(w, "Failed to unmute user", http.StatusInternalServerError)
			return
		}
		log.Printf("💾 Unmuted %s in database", targetAddress)
	}

	// Broadcast to all current user's devices
	s.BroadcastUserAction(session.Address, api.WSUserAction{
		Action:      "user_unmuted",
		UserAddress: targetAddress,
	})

	log.Printf("✅ Successfully unmuted %s", targetAddress)

	s.SendJSON(w, map[string]interface{}{
		"success": true,
		"message": "User unmuted successfully",
	})
}

// HandleGetMutedUsers retrieves the list of muted users
func (s *Server) HandleGetMutedUsers(w http.ResponseWriter, r *http.Request) {
	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	mutedUsers := make([]string, 0)

	if s.MessageDB != nil {
		muted, err := s.MessageDB.GetMutedContacts(session.Address)
		if err != nil {
			log.Printf("⚠️  Failed to get muted users: %v", err)
			s.SendError(w, "Failed to get muted users", http.StatusInternalServerError)
			return
		}
		mutedUsers = muted
	}

	log.Printf("📋 [GetMutedUsers] %s has %d muted users", session.Address, len(mutedUsers))

	s.SendJSON(w, map[string]interface{}{
		"success":     true,
		"muted_users": mutedUsers,
	})
}

// HandleClearChat clears all messages in a chat but keeps the contact
func (s *Server) HandleClearChat(w http.ResponseWriter, r *http.Request) {
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

	log.Printf("🧹 [ClearChat] %s clearing chat with %s", session.Address, peerAddress)

	// Clear chat from session's message history (in-memory)
	session.MessageHistory[peerAddress] = []api.Message{}

	// Clear chat from database (but keep contact)
	if s.MessageDB != nil {
		if err := s.MessageDB.DeleteChat(session.Address, peerAddress); err != nil {
			log.Printf("⚠️  Failed to clear chat from database: %v", err)
			s.SendError(w, "Failed to clear chat", http.StatusInternalServerError)
			return
		}
		log.Printf("💾 Cleared chat with %s from database", peerAddress)

		// Delete all starred messages for this chat
		if err := s.MessageDB.DeleteStarredMessagesForChat(session.Address, peerAddress); err != nil {
			log.Printf("⚠️  Failed to delete starred messages for chat: %v", err)
		}
	}

	// Clear in-memory pending messages for this chat
	s.clearPendingMessagesForChat(peerAddress)

	// Broadcast to all current user's devices
	s.BroadcastUserAction(session.Address, api.WSUserAction{
		Action:      "chat_cleared",
		UserAddress: peerAddress,
	})

	log.Printf("✅ Successfully cleared chat with %s", peerAddress)

	s.SendJSON(w, map[string]interface{}{
		"success": true,
		"message": "Chat cleared successfully",
	})
}

// BroadcastUserAction broadcasts a user action to all user's connected devices
func (s *Server) BroadcastUserAction(userAddress string, action api.WSUserAction) {
	s.WsLock.RLock()
	wsConn, exists := s.WsConnections[userAddress]
	s.WsLock.RUnlock()

	if !exists {
		log.Printf("⚠️  User %s not connected via WebSocket, skipping user action broadcast", userAddress)
		return
	}

	msg := api.WSMessage{
		Type:    "user_action",
		Payload: action,
	}

	// Thread-safe write
	wsConn.mutex.Lock()
	err := wsConn.conn.WriteJSON(msg)
	wsConn.mutex.Unlock()

	if err != nil {
		log.Printf("❌ Failed to broadcast user action to %s: %v", userAddress, err)
	} else {
		log.Printf("✅ User action broadcast sent to %s", userAddress)
	}
}
