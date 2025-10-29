package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ZentaChain/zentalk-api/pkg/api"
)

// handleWebSocket handles WebSocket connections
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Get user address from query parameter
	address := r.URL.Query().Get("address")
	if address == "" {
		log.Printf("WebSocket connection rejected: no address provided")
		http.Error(w, "Address required", http.StatusBadRequest)
		return
	}

	// Normalize address for consistent lookups
	address = api.NormalizeAddress(address)

	conn, err := s.WsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	// Register connection with thread-safe wrapper
	wsConn := &wsConnection{conn: conn}
	s.WsLock.Lock()
	s.WsConnections[address] = wsConn
	s.WsLock.Unlock()

	// Mark user as online
	s.OnlineLock.Lock()
	s.OnlineUsers[address] = true
	s.OnlineLock.Unlock()

	log.Printf("WebSocket connected: %s", address)

	// Broadcast online status (excluding users who blocked this user)
	s.broadcastOnlineStatus(address, true)

	// Cleanup on disconnect
	defer func() {
		// Remove connection
		s.WsLock.Lock()
		delete(s.WsConnections, address)
		s.WsLock.Unlock()

		// Mark user as offline
		s.OnlineLock.Lock()
		delete(s.OnlineUsers, address)
		s.OnlineLock.Unlock()

		// Update last_online timestamp in database
		if s.MessageDB != nil {
			normalizedAddr := api.NormalizeAddress(address)
			err := s.MessageDB.UpdateLastOnline(normalizedAddr)
			if err != nil {
				log.Printf("⚠️  Failed to update last_online for %s: %v", address, err)
			}
		}

		// DON'T clean up session here - WebSocket may just be reconnecting
		// Session cleanup should only happen on explicit logout/disconnect
		// The session will remain in memory for future reconnections

		conn.Close()
		log.Printf("WebSocket disconnected: %s", address)

		// Broadcast offline status (excluding users who blocked this user)
		s.broadcastOnlineStatus(address, false)
	}()

	// Keep connection alive and handle incoming messages
	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			break
		}

		// Handle ping/pong for connection keepalive
		if messageType == websocket.PingMessage {
			// Thread-safe write
			wsConn.mutex.Lock()
			err := conn.WriteMessage(websocket.PongMessage, nil)
			wsConn.mutex.Unlock()
			if err != nil {
				log.Printf("Failed to send pong: %v", err)
				break
			}
		}

		// Handle text messages (if needed for future features)
		if messageType == websocket.TextMessage {
			log.Printf("Received WebSocket message from %s: %s", address, string(message))
			// Future: handle typing indicators, read receipts, etc.
		}
	}
}

// broadcastWS broadcasts a message to all WebSocket connections
func (s *Server) broadcastWS(msg api.WSMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal WebSocket message: %v", err)
		return
	}

	s.WsLock.RLock()
	defer s.WsLock.RUnlock()

	for address, wsConn := range s.WsConnections {
		// Thread-safe write
		wsConn.mutex.Lock()
		err := wsConn.conn.WriteMessage(websocket.TextMessage, data)
		wsConn.mutex.Unlock()
		if err != nil {
			log.Printf("Failed to send to WebSocket %s: %v", address, err)
		}
	}
}

// broadcastOnlineStatus broadcasts online status but excludes users who have blocked the sender
func (s *Server) broadcastOnlineStatus(senderAddress string, online bool) {
	msg := api.WSMessage{
		Type: "online",
		Payload: api.WSOnlineStatus{
			Address: senderAddress,
			Online:  online,
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal WebSocket message: %v", err)
		return
	}

	normalizedSender := api.NormalizeAddress(senderAddress)

	s.WsLock.RLock()
	defer s.WsLock.RUnlock()

	for recipientAddress, wsConn := range s.WsConnections {
		normalizedRecipient := api.NormalizeAddress(recipientAddress)

		// Check if recipient has blocked the sender
		if s.MessageDB != nil {
			isBlocked, err := s.MessageDB.IsContactBlocked(normalizedRecipient, normalizedSender)
			if err != nil {
				log.Printf("Error checking block status for %s -> %s: %v", normalizedRecipient, normalizedSender, err)
				continue
			}

			// Don't send online status to users who have blocked the sender
			if isBlocked {
				continue
			}
		}

		// Thread-safe write
		wsConn.mutex.Lock()
		err := wsConn.conn.WriteMessage(websocket.TextMessage, data)
		wsConn.mutex.Unlock()
		if err != nil {
			log.Printf("Failed to send online status to WebSocket %s: %v", recipientAddress, err)
		}
	}
}

// broadcastStatusUpdate broadcasts status update but excludes users who have blocked the sender
func (s *Server) broadcastStatusUpdate(senderAddress string, status string) {
	msg := api.WSMessage{
		Type: "status_update",
		Payload: api.WSStatusUpdate{
			Address: senderAddress,
			Status:  status,
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal WebSocket message: %v", err)
		return
	}

	normalizedSender := api.NormalizeAddress(senderAddress)

	s.WsLock.RLock()
	defer s.WsLock.RUnlock()

	for recipientAddress, wsConn := range s.WsConnections {
		normalizedRecipient := api.NormalizeAddress(recipientAddress)

		// Check if recipient has blocked the sender
		if s.MessageDB != nil {
			isBlocked, err := s.MessageDB.IsContactBlocked(normalizedRecipient, normalizedSender)
			if err != nil {
				log.Printf("Error checking block status for %s -> %s: %v", normalizedRecipient, normalizedSender, err)
				continue
			}

			// Don't send status update to users who have blocked the sender
			if isBlocked {
				continue
			}
		}

		// Thread-safe write
		wsConn.mutex.Lock()
		err := wsConn.conn.WriteMessage(websocket.TextMessage, data)
		wsConn.mutex.Unlock()
		if err != nil {
			log.Printf("Failed to send status update to WebSocket %s: %v", recipientAddress, err)
		}
	}
}

// broadcastProfileUpdate broadcasts profile update but excludes users who have blocked the sender
func (s *Server) broadcastProfileUpdate(senderAddress, firstName, lastName, bio string, avatarChunkID uint64) {
	msg := api.WSMessage{
		Type: "profile_update",
		Payload: api.WSProfileUpdate{
			Address:       senderAddress,
			FirstName:     firstName,
			LastName:      lastName,
			Bio:           bio,
			AvatarChunkID: avatarChunkID,
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal WebSocket message: %v", err)
		return
	}

	normalizedSender := api.NormalizeAddress(senderAddress)

	s.WsLock.RLock()
	defer s.WsLock.RUnlock()

	log.Printf("📢 Broadcasting profile update for %s to %d connected users", senderAddress, len(s.WsConnections))

	for recipientAddress, wsConn := range s.WsConnections {
		normalizedRecipient := api.NormalizeAddress(recipientAddress)

		// Check if recipient has blocked the sender
		if s.MessageDB != nil {
			isBlocked, err := s.MessageDB.IsContactBlocked(normalizedRecipient, normalizedSender)
			if err != nil {
				log.Printf("Error checking block status for %s -> %s: %v", normalizedRecipient, normalizedSender, err)
				continue
			}

			// Don't send profile update to users who have blocked the sender
			if isBlocked {
				continue
			}
		}

		// Thread-safe write
		wsConn.mutex.Lock()
		err := wsConn.conn.WriteMessage(websocket.TextMessage, data)
		wsConn.mutex.Unlock()
		if err != nil {
			log.Printf("Failed to send profile update to WebSocket %s: %v", recipientAddress, err)
		} else {
			log.Printf("✅ Sent profile update to %s", recipientAddress)
		}
	}
}

// SendWSMessage sends a message to a specific WebSocket connection
func (s *Server) SendWSMessage(address string, msg api.WSMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %v", err)
	}

	s.WsLock.RLock()
	wsConn, exists := s.WsConnections[address]
	s.WsLock.RUnlock()

	if !exists {
		return fmt.Errorf("no connection for address: %s", address)
	}

	// Thread-safe write
	wsConn.mutex.Lock()
	defer wsConn.mutex.Unlock()

	if err := wsConn.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("failed to write message: %v", err)
	}

	return nil
}

// IsUserOnline checks if a user is currently online
func (s *Server) IsUserOnline(address string) bool {
	s.OnlineLock.RLock()
	defer s.OnlineLock.RUnlock()
	return s.OnlineUsers[address]
}

// BroadcastToUser sends a WebSocket message to a specific user
func (s *Server) BroadcastToUser(address string, eventType string, payload interface{}) {
	msg := api.WSMessage{
		Type:    eventType,
		Payload: payload,
	}

	if err := s.SendWSMessage(address, msg); err != nil {
		// User might be offline, log but don't fail
		log.Printf("⚠️  Failed to broadcast %s to %s: %v", eventType, address, err)
	}
}

// getOnlineUsers returns a list of all currently online user addresses
func (s *Server) getOnlineUsers() []string {
	s.OnlineLock.RLock()
	defer s.OnlineLock.RUnlock()

	users := make([]string, 0, len(s.OnlineUsers))
	for address := range s.OnlineUsers {
		users = append(users, address)
	}

	return users
}

// pingClients sends periodic ping messages to keep connections alive
func (s *Server) startPingRoutine() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		s.WsLock.RLock()
		connections := make(map[string]*wsConnection)
		for addr, wsConn := range s.WsConnections {
			connections[addr] = wsConn
		}
		s.WsLock.RUnlock()

		for address, wsConn := range connections {
			// Thread-safe write
			wsConn.mutex.Lock()
			err := wsConn.conn.WriteMessage(websocket.PingMessage, nil)
			wsConn.mutex.Unlock()
			if err != nil {
				log.Printf("Failed to ping %s: %v", address, err)
			}
		}
	}
}
