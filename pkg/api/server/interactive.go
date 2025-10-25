package server

import (
	"github.com/ZentaChain/zentalk-api/pkg/api"
	"encoding/json"
	"log"
	"net/http"

)

// handleTypingIndicator broadcasts typing status to a peer
func (s *Server) HandleTypingIndicator(w http.ResponseWriter, r *http.Request) {
	var req api.TypingIndicatorRequest
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

	log.Printf("💬 [TypingIndicator] %s is %s typing to %s",
		session.Address,
		map[bool]string{true: "now", false: "no longer"}[req.Typing],
		peerAddress)

	// Broadcast typing indicator to peer via WebSocket
	s.BroadcastTypingIndicator(peerAddress, api.WSTypingIndicator{
		From:   session.Address,
		Typing: req.Typing,
	})

	s.SendJSON(w, api.TypingIndicatorResponse{
		Success: true,
		Message: "Typing indicator sent",
	})
}

// broadcastTypingIndicator sends typing status to a specific peer via WebSocket
func (s *Server) BroadcastTypingIndicator(peerAddress string, indicator api.WSTypingIndicator) {
	s.WsLock.RLock()
	wsConn, exists := s.WsConnections[peerAddress]
	s.WsLock.RUnlock()

	if !exists {
		log.Printf("⚠️  Peer %s not connected via WebSocket, skipping typing indicator", peerAddress)
		return
	}

	msg := api.WSMessage{
		Type:    "typing",
		Payload: indicator,
	}

	// Thread-safe write
	wsConn.mutex.Lock()
	err := wsConn.conn.WriteJSON(msg)
	wsConn.mutex.Unlock()

	if err != nil {
		log.Printf("❌ Failed to send typing indicator to %s: %v", peerAddress, err)
	} else {
		log.Printf("✅ Typing indicator sent to %s", peerAddress)
	}
}

// handleEditMessage edits a message in the chat history
func (s *Server) HandleEditMessage(w http.ResponseWriter, r *http.Request) {
	var req api.EditMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// Validate new content
	if req.NewContent == "" {
		s.SendError(w, "New content cannot be empty", http.StatusBadRequest)
		return
	}

	// Normalize peer address
	peerAddress := api.NormalizeAddress(req.PeerAddress)

	log.Printf("✏️  [EditMessage] %s editing message %s in chat with %s",
		session.Address, req.MessageID, peerAddress)

	// Edit message in session's message history
	messages := session.MessageHistory[peerAddress]
	found := false

	for i, msg := range messages {
		if msg.ID == req.MessageID {
			// Verify the message is from the current user (only allow editing own messages)
			if msg.Sender != "You" {
				s.SendError(w, "Cannot edit messages from other users", http.StatusForbidden)
				return
			}

			// Update the message content and mark as edited
			messages[i].Content = req.NewContent
			messages[i].IsEdited = true
			found = true

			// Save updated message to database
			if s.MessageDB != nil {
				if err := s.MessageDB.SaveMessage(session.Address, peerAddress, messages[i]); err != nil {
					log.Printf("⚠️  Failed to save edited message to database: %v", err)
				}
			}

			log.Printf("✅ Edited message %s in chat with %s", req.MessageID, peerAddress)
			break
		}
	}

	if !found {
		s.SendError(w, "api.Message not found", http.StatusNotFound)
		return
	}

	// Notify the peer via WebSocket about the edit
	s.BroadcastMessageEdit(peerAddress, api.WSMessageEdited{
		MessageID:  req.MessageID,
		ChatID:     session.Address, // The peer sees this as the chat ID
		NewContent: req.NewContent,
	})

	s.SendJSON(w, api.EditMessageResponse{
		Success: true,
		Message: "api.Message edited successfully",
	})
}

// broadcastMessageEdit notifies a peer that a message was edited
func (s *Server) BroadcastMessageEdit(peerAddress string, edit api.WSMessageEdited) {
	s.WsLock.RLock()
	wsConn, exists := s.WsConnections[peerAddress]
	s.WsLock.RUnlock()

	if !exists {
		log.Printf("⚠️  Peer %s not connected via WebSocket, skipping message edit notification", peerAddress)
		return
	}

	msg := api.WSMessage{
		Type:    "message_edited",
		Payload: edit,
	}

	// Thread-safe write
	wsConn.mutex.Lock()
	err := wsConn.conn.WriteJSON(msg)
	wsConn.mutex.Unlock()

	if err != nil {
		log.Printf("❌ Failed to send message edit to %s: %v", peerAddress, err)
	} else {
		log.Printf("✅ api.Message edit notification sent to %s", peerAddress)
	}
}

// handleAddReaction adds a reaction to a message
func (s *Server) HandleAddReaction(w http.ResponseWriter, r *http.Request) {
	var req api.AddReactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// Validate emoji
	if req.Emoji == "" {
		s.SendError(w, "Emoji cannot be empty", http.StatusBadRequest)
		return
	}

	// Normalize peer address
	peerAddress := api.NormalizeAddress(req.PeerAddress)

	log.Printf("👍 [AddReaction] %s adding reaction %s to message %s in chat with %s",
		session.Address, req.Emoji, req.MessageID, peerAddress)

	// Find and update the message in message history
	messages := session.MessageHistory[peerAddress]
	found := false

	for i, msg := range messages {
		if msg.ID == req.MessageID {
			// Initialize reactions array if nil
			if messages[i].Reactions == nil {
				messages[i].Reactions = []api.Reaction{}
			}

			// Check if this emoji already exists
			reactionFound := false
			for j, reaction := range messages[i].Reactions {
				if reaction.Emoji == req.Emoji {
					// User already reacted, just mark as reacted
					messages[i].Reactions[j].HasReacted = true
					reactionFound = true
					break
				}
			}

			// If emoji doesn't exist, add new reaction
			if !reactionFound {
				// Get current user info
				currentUser := api.User{
					Name:     session.Username,
					Username: "@" + session.Username,
					Address:  session.Address,
				}

				messages[i].Reactions = append(messages[i].Reactions, api.Reaction{
					Emoji:      req.Emoji,
					Count:      1,
					Users:      []api.User{currentUser},
					HasReacted: true,
				})
			}

			found = true

			// Save updated message to database
			if s.MessageDB != nil {
				if err := s.MessageDB.SaveMessage(session.Address, peerAddress, messages[i]); err != nil {
					log.Printf("⚠️  Failed to save message with reaction to database: %v", err)
				}
			}

			log.Printf("✅ Added reaction %s to message %s", req.Emoji, req.MessageID)
			break
		}
	}

	if !found {
		s.SendError(w, "api.Message not found", http.StatusNotFound)
		return
	}

	// Notify the peer via WebSocket about the new reaction
	s.BroadcastReactionAdded(peerAddress, api.WSReactionAdded{
		MessageID: req.MessageID,
		ChatID:    session.Address,
		Emoji:     req.Emoji,
		From:      session.Address,
	})

	s.SendJSON(w, api.AddReactionResponse{
		Success: true,
		Message: "api.Reaction added successfully",
	})
}

// handleRemoveReaction removes a reaction from a message
func (s *Server) HandleRemoveReaction(w http.ResponseWriter, r *http.Request) {
	var req api.RemoveReactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// Validate emoji
	if req.Emoji == "" {
		s.SendError(w, "Emoji cannot be empty", http.StatusBadRequest)
		return
	}

	// Normalize peer address
	peerAddress := api.NormalizeAddress(req.PeerAddress)

	log.Printf("👎 [RemoveReaction] %s removing reaction %s from message %s in chat with %s",
		session.Address, req.Emoji, req.MessageID, peerAddress)

	// Find and update the message in message history
	messages := session.MessageHistory[peerAddress]
	found := false

	for i, msg := range messages {
		if msg.ID == req.MessageID {
			// Find and remove the reaction
			if messages[i].Reactions != nil {
				for j, reaction := range messages[i].Reactions {
					if reaction.Emoji == req.Emoji {
						// Remove this reaction from the array
						messages[i].Reactions = append(messages[i].Reactions[:j], messages[i].Reactions[j+1:]...)
						log.Printf("✅ Removed reaction %s from message %s", req.Emoji, req.MessageID)
						break
					}
				}
			}
			found = true

			// Save updated message to database
			if s.MessageDB != nil {
				if err := s.MessageDB.SaveMessage(session.Address, peerAddress, messages[i]); err != nil {
					log.Printf("⚠️  Failed to save message after removing reaction to database: %v", err)
				}
			}

			break
		}
	}

	if !found {
		s.SendError(w, "api.Message not found", http.StatusNotFound)
		return
	}

	// Notify the peer via WebSocket about the removed reaction
	s.BroadcastReactionRemoved(peerAddress, api.WSReactionRemoved{
		MessageID: req.MessageID,
		ChatID:    session.Address,
		Emoji:     req.Emoji,
		From:      session.Address,
	})

	s.SendJSON(w, api.RemoveReactionResponse{
		Success: true,
		Message: "api.Reaction removed successfully",
	})
}

// broadcastReactionAdded notifies a peer that a reaction was added
func (s *Server) BroadcastReactionAdded(peerAddress string, reaction api.WSReactionAdded) {
	s.WsLock.RLock()
	wsConn, exists := s.WsConnections[peerAddress]
	s.WsLock.RUnlock()

	if !exists {
		log.Printf("⚠️  Peer %s not connected via WebSocket, skipping reaction notification", peerAddress)
		return
	}

	msg := api.WSMessage{
		Type:    "reaction_added",
		Payload: reaction,
	}

	// Thread-safe write
	wsConn.mutex.Lock()
	err := wsConn.conn.WriteJSON(msg)
	wsConn.mutex.Unlock()

	if err != nil {
		log.Printf("❌ Failed to send reaction added to %s: %v", peerAddress, err)
	} else {
		log.Printf("✅ api.Reaction added notification sent to %s", peerAddress)
	}
}

// broadcastReactionRemoved notifies a peer that a reaction was removed
func (s *Server) BroadcastReactionRemoved(peerAddress string, reaction api.WSReactionRemoved) {
	s.WsLock.RLock()
	wsConn, exists := s.WsConnections[peerAddress]
	s.WsLock.RUnlock()

	if !exists {
		log.Printf("⚠️  Peer %s not connected via WebSocket, skipping reaction removal notification", peerAddress)
		return
	}

	msg := api.WSMessage{
		Type:    "reaction_removed",
		Payload: reaction,
	}

	// Thread-safe write
	wsConn.mutex.Lock()
	err := wsConn.conn.WriteJSON(msg)
	wsConn.mutex.Unlock()

	if err != nil {
		log.Printf("❌ Failed to send reaction removed to %s: %v", peerAddress, err)
	} else {
		log.Printf("✅ api.Reaction removed notification sent to %s", peerAddress)
	}
}
