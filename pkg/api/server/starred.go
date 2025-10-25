package server

import (
	"github.com/ZentaChain/zentalk-api/pkg/api"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

// HandleStarMessage stars a message for a user
func (s *Server) HandleStarMessage(w http.ResponseWriter, r *http.Request) {
	var req api.StarMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	normalizedUser := api.NormalizeAddress(session.Address)
	normalizedPeer := api.NormalizeAddress(req.PeerAddress)

	// Star the message in database
	if s.MessageDB != nil {
		if err := s.MessageDB.StarMessage(normalizedUser, req.MessageID, normalizedPeer); err != nil {
			log.Printf("❌ Failed to star message: %v", err)
			s.SendError(w, fmt.Sprintf("Failed to star message: %v", err), http.StatusInternalServerError)
			return
		}
	}

	log.Printf("⭐ api.User %s starred message %s", normalizedUser, req.MessageID)

	s.SendJSON(w, api.StarMessageResponse{
		Success: true,
		Message: "api.Message starred successfully",
	})
}

// HandleUnstarMessage unstars a message for a user
func (s *Server) HandleUnstarMessage(w http.ResponseWriter, r *http.Request) {
	var req api.UnstarMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	normalizedUser := api.NormalizeAddress(session.Address)

	// Unstar the message in database
	if s.MessageDB != nil {
		if err := s.MessageDB.UnstarMessage(normalizedUser, req.MessageID); err != nil {
			log.Printf("❌ Failed to unstar message: %v", err)
			s.SendError(w, fmt.Sprintf("Failed to unstar message: %v", err), http.StatusInternalServerError)
			return
		}
	}

	log.Printf("⭐ api.User %s unstarred message %s", normalizedUser, req.MessageID)

	s.SendJSON(w, api.UnstarMessageResponse{
		Success: true,
		Message: "api.Message unstarred successfully",
	})
}

// HandleGetStarredMessages retrieves all starred messages for a user
func (s *Server) HandleGetStarredMessages(w http.ResponseWriter, r *http.Request) {
	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	normalizedUser := api.NormalizeAddress(session.Address)

	var messages []api.Message
	if s.MessageDB != nil {
		messages, err = s.MessageDB.GetStarredMessages(normalizedUser)
		if err != nil {
			log.Printf("❌ Failed to get starred messages: %v", err)
			s.SendError(w, fmt.Sprintf("Failed to get starred messages: %v", err), http.StatusInternalServerError)
			return
		}
	}

	s.SendJSON(w, api.GetStarredMessagesResponse{
		Success:  true,
		Messages: messages,
		Message:  fmt.Sprintf("Found %d starred messages", len(messages)),
	})
}
