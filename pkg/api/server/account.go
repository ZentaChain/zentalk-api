package server

import (
	"github.com/zentalk/protocol/pkg/api"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"strings"

)

// handleDeleteAccount permanently deletes a user's account and cleans up all resources
func (s *Server) HandleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	var req api.DeleteAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Normalize wallet address
	walletAddr := api.NormalizeAddress(req.WalletAddress)

	// Get session
	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// Verify the wallet address matches the session (security check)
	if session.Address != walletAddr {
		s.SendError(w, "Wallet address mismatch", http.StatusForbidden)
		return
	}

	log.Printf("🗑️  [DeleteAccount] Deleting account for %s", walletAddr)

	// Close WebSocket connection if exists
	s.WsLock.Lock()
	if wsConn, exists := s.WsConnections[walletAddr]; exists {
		wsConn.conn.Close()
		delete(s.WsConnections, walletAddr)
		log.Printf("🔌 Closed WebSocket for %s", walletAddr)
	}
	s.WsLock.Unlock()

	// Remove from online users
	s.OnlineLock.Lock()
	delete(s.OnlineUsers, walletAddr)
	s.OnlineLock.Unlock()

	// Remove username mappings
	s.SessionsLock.Lock()
	if username, exists := s.AddressToUsername[walletAddr]; exists {
		delete(s.UsernameToAddress, strings.ToLower(username))
		delete(s.AddressToUsername, walletAddr)
		log.Printf("🗑️  Removed username mappings for %s (%s)", walletAddr, username)
	}
	s.SessionsLock.Unlock()

	// Disconnect client (if connected to relay)
	if session.Client != nil {
		if err := session.Client.Disconnect(); err != nil {
			log.Printf("⚠️  Error disconnecting client for %s: %v", walletAddr, err)
		} else {
			log.Printf("🔌 Disconnected client from relay for %s", walletAddr)
		}
	}

	// Delete user data from database
	if s.MessageDB != nil {
		if err := s.MessageDB.DeleteUserData(walletAddr); err != nil {
			log.Printf("⚠️  Failed to delete user data from database: %v", err)
		}
	}

	// Clean up session (stops DHT node and removes from sessions map)
	s.CleanupSession(walletAddr)

	log.Printf("✅ [DeleteAccount] Account deleted successfully for %s", walletAddr)

	s.SendJSON(w, api.DeleteAccountResponse{
		Success: true,
		Message: "Account deleted successfully",
	})
}

// HandleCheckUsername checks if a username is available
func (s *Server) HandleCheckUsername(w http.ResponseWriter, r *http.Request) {
	var req api.CheckUsernameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate username format
	if req.Username == "" || len(req.Username) > 50 {
		s.SendJSON(w, api.CheckUsernameResponse{
			Available: false,
			Message:   "Username must be between 1-50 characters",
		})
		return
	}

	// Check availability in database
	available := true
	var message string

	if s.MessageDB != nil {
		isAvailable, err := s.MessageDB.IsUsernameAvailable(req.Username, "")
		if err != nil {
			log.Printf("❌ Error checking username availability: %v", err)
			s.SendError(w, "Failed to check username availability", http.StatusInternalServerError)
			return
		}

		available = isAvailable
		if available {
			message = "Username is available"
			log.Printf("✅ Username '%s' is available", req.Username)
		} else {
			message = "Username is already taken"
			log.Printf("❌ Username '%s' is already taken", req.Username)
		}
	} else {
		message = "Username availability check not available"
	}

	s.SendJSON(w, api.CheckUsernameResponse{
		Available: available,
		Message:   message,
	})
}

// HandleUpdateUsername updates a user's display username
func (s *Server) HandleUpdateUsername(w http.ResponseWriter, r *http.Request) {
	var req api.UpdateUsernameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate username
	if req.NewUsername == "" || len(req.NewUsername) > 50 {
		s.SendError(w, "Username must be between 1-50 characters", http.StatusBadRequest)
		return
	}

	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	walletAddr := api.NormalizeAddress(session.Address)
	oldUsername := session.Username

	log.Printf("📝 Updating username for %s from '%s' to '%s'", walletAddr, oldUsername, req.NewUsername)

	// Check if new username is available (excluding current user)
	if s.MessageDB != nil {
		available, err := s.MessageDB.IsUsernameAvailable(req.NewUsername, walletAddr)
		if err != nil {
			log.Printf("❌ Error checking username availability: %v", err)
			s.SendError(w, "Failed to verify username availability", http.StatusInternalServerError)
			return
		}

		if !available {
			log.Printf("❌ Username '%s' is already taken by another user", req.NewUsername)
			s.SendError(w, "Username is already taken. Please choose a different username.", http.StatusConflict)
			return
		}
	}

	// Update in database
	if s.MessageDB != nil {
		if err := s.MessageDB.UpdateUsername(walletAddr, req.NewUsername); err != nil {
			log.Printf("❌ Failed to update username in database: %v", err)
			s.SendError(w, "Failed to update username", http.StatusInternalServerError)
			return
		}
	}

	// Update in session
	session.Username = req.NewUsername

	// Update username mappings
	s.SessionsLock.Lock()
	// Remove old username mapping
	if oldUsername != "" {
		delete(s.UsernameToAddress, strings.ToLower(oldUsername))
	}
	// Add new username mapping
	s.UsernameToAddress[strings.ToLower(req.NewUsername)] = walletAddr
	s.AddressToUsername[walletAddr] = req.NewUsername
	s.SessionsLock.Unlock()

	log.Printf("✅ Username updated successfully for %s", walletAddr)

	s.SendJSON(w, api.UpdateUsernameResponse{
		Success: true,
		Message: "Username updated successfully",
	})
}

// HandleUpdateProfile updates user's profile (first name, last name, bio, avatar)
func (s *Server) HandleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	var req api.UpdateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	walletAddr := api.NormalizeAddress(session.Address)

	log.Printf("📝 Updating profile for %s (avatar chunk: %d)", walletAddr, req.AvatarChunkID)

	// Decode base64 avatar key if provided
	var avatarKey []byte
	if req.AvatarKey != "" {
		avatarKey, err = base64.StdEncoding.DecodeString(req.AvatarKey)
		if err != nil {
			s.SendError(w, "Invalid avatar key encoding", http.StatusBadRequest)
			return
		}
	}

	// Update in database
	if s.MessageDB != nil {
		if err := s.MessageDB.UpdateProfile(walletAddr, req.FirstName, req.LastName, req.Bio, req.AvatarChunkID, avatarKey); err != nil {
			log.Printf("❌ Failed to update profile in database: %v", err)
			s.SendError(w, "Failed to update profile", http.StatusInternalServerError)
			return
		}
	}

	log.Printf("✅ Profile updated successfully for %s", walletAddr)

	s.SendJSON(w, api.UpdateProfileResponse{
		Success: true,
		Message: "Profile updated successfully",
	})
}

// HandleGetProfile retrieves user's profile information
func (s *Server) HandleGetProfile(w http.ResponseWriter, r *http.Request) {
	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	walletAddr := api.NormalizeAddress(session.Address)

	// Get from database
	var user *api.User
	if s.MessageDB != nil {
		user, err = s.MessageDB.GetUser(walletAddr)
		if err != nil {
			log.Printf("❌ Failed to get user from database: %v", err)
			s.SendError(w, "Failed to get profile", http.StatusInternalServerError)
			return
		}
	}

	if user == nil {
		s.SendError(w, "User not found", http.StatusNotFound)
		return
	}

	// Ensure status is never empty
	status := user.Status
	if status == "" {
		status = "online"
	}

	s.SendJSON(w, api.GetProfileResponse{
		Success:       true,
		FirstName:     user.FirstName,
		LastName:      user.LastName,
		Username:      user.Username,
		Bio:           user.Bio,
		AvatarChunkID: user.AvatarChunkID,
		AvatarKey:     base64.StdEncoding.EncodeToString(user.AvatarKey),
		Address:       user.Address,
		Status:        status,
		Message:       "Profile retrieved successfully",
	})
}

// HandleUpdateStatus updates user's status (online, away, busy, offline)
func (s *Server) HandleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	var req api.UpdateStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	walletAddr := api.NormalizeAddress(session.Address)

	// Validate status
	validStatuses := map[string]bool{
		"online":  true,
		"away":    true,
		"busy":    true,
		"offline": true,
	}
	if !validStatuses[req.Status] {
		s.SendError(w, "Invalid status. Must be: online, away, busy, or offline", http.StatusBadRequest)
		return
	}

	// Update status in database
	if s.MessageDB != nil {
		if err := s.MessageDB.UpdateUserStatus(walletAddr, req.Status); err != nil {
			log.Printf("❌ Failed to update status in database: %v", err)
			s.SendError(w, "Failed to update status", http.StatusInternalServerError)
			return
		}
	}

	// Broadcast status update via WebSocket
	s.broadcastWS(api.WSMessage{
		Type: "status_update",
		Payload: api.WSStatusUpdate{
			Address: walletAddr,
			Status:  req.Status,
		},
	})

	log.Printf("✅ Status updated for %s: %s", walletAddr, req.Status)

	s.SendJSON(w, api.UpdateStatusResponse{
		Success: true,
		Message: "Status updated successfully",
	})
}
