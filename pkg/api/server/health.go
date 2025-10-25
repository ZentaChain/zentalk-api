package server

import (
	"fmt"
	"net/http"

)

// handleHealth handles health check requests
func (s *Server) HandleHealth(w http.ResponseWriter, r *http.Request) {
	s.SessionsLock.RLock()
	sessionCount := len(s.Sessions)
	s.SessionsLock.RUnlock()

	s.SendJSON(w, map[string]interface{}{
		"status":       "ok",
		"sessions":     sessionCount,
		"multi_tenant": true,
	})
}

// handleDebug provides debug information
func (s *Server) HandleDebug(w http.ResponseWriter, r *http.Request) {
	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	s.OnlineLock.RLock()
	onlineUsers := make([]string, 0, len(s.OnlineUsers))
	for addr := range s.OnlineUsers {
		onlineUsers = append(onlineUsers, addr)
	}
	s.OnlineLock.RUnlock()

	debugInfo := map[string]interface{}{
		"username":        session.Username,
		"address":         session.Address,
		"online_users":    onlineUsers,
		"cached_contacts": len(session.ContactCache),
		"active_chats":    len(session.MessageHistory),
		"dht_node_id":     fmt.Sprintf("%x", session.DHTNode.ID),
	}

	s.SendJSON(w, map[string]interface{}{
		"success": true,
		"debug":   debugInfo,
	})
}
