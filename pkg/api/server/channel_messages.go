package server

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/ZentaChain/zentalk-api/pkg/api"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// HandleSendChannelMessage sends a message to a channel (owner/admin only)
func (s *Server) HandleSendChannelMessage(w http.ResponseWriter, r *http.Request) {
	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	vars := mux.Vars(r)
	channelID := vars["channelId"]

	if channelID == "" {
		s.SendError(w, "Channel ID is required", http.StatusBadRequest)
		return
	}

	var req api.SendChannelMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	req.ChannelID = channelID

	// Validate content
	if req.Content == "" && req.MediaID == "" {
		s.SendError(w, "Message content or media is required", http.StatusBadRequest)
		return
	}

	// Check if user is admin or owner
	isAdminOrOwner, _, err := s.MessageDB.IsAdminOrOwner(channelID, session.Address)
	if err != nil {
		log.Printf("❌ Failed to check permissions: %v", err)
		s.SendError(w, "Failed to check permissions", http.StatusInternalServerError)
		return
	}

	if !isAdminOrOwner {
		s.SendError(w, "Only admins can post messages to this channel", http.StatusForbidden)
		return
	}

	// Get sender info
	sender := s.GetOrCreateUserWithProfile(session.Address)

	// Create message
	messageID := uuid.New().String()
	timestamp := time.Now().Format(time.RFC3339)

	var mediaUrl string
	if req.MediaID != "" {
		// TODO: Handle media URL from MeshStorage
		mediaUrl = "/api/media/" + req.MediaID
	}

	message := api.ChannelMessage{
		ID:        messageID,
		ChannelID: channelID,
		Sender:    *sender,
		Content:   req.Content,
		Timestamp: timestamp,
		MediaUrl:  mediaUrl,
		Reactions: []api.Reaction{},
		ViewCount: 0,
	}

	// Save to database
	err = s.MessageDB.SaveChannelMessage(message)
	if err != nil {
		log.Printf("❌ Failed to save channel message: %v", err)
		s.SendError(w, "Failed to send message", http.StatusInternalServerError)
		return
	}

	log.Printf("✅ Message sent to channel %s by %s", channelID, session.Address)

	// Broadcast to all members
	members, err := s.MessageDB.GetChannelMembers(channelID)
	if err != nil {
		log.Printf("❌ Failed to get members for broadcast: %v", err)
	} else {
		log.Printf("📡 Broadcasting to %d members of channel %s", len(members), channelID)
		for _, member := range members {
			// IMPORTANT: Normalize address to match WebSocket connection key format
			normalizedAddr := api.NormalizeAddress(member.UserAddress)
			log.Printf("📤 Sending channel_message to user %s (normalized: %s)", member.UserAddress, normalizedAddr)
			s.BroadcastToUser(normalizedAddr, "channel_message", api.WSChannelMessage{
				ChannelID: channelID,
				Message:   message,
			})
		}
	}

	s.SendJSON(w, api.SendChannelMessageResponse{
		Success:   true,
		MessageID: messageID,
		Timestamp: timestamp,
		Message:   "Message sent successfully",
	})
}

// HandleGetChannelMessages retrieves messages from a channel
func (s *Server) HandleGetChannelMessages(w http.ResponseWriter, r *http.Request) {
	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	vars := mux.Vars(r)
	channelID := vars["channelId"]

	if channelID == "" {
		s.SendError(w, "Channel ID is required", http.StatusBadRequest)
		return
	}

	// Check if user is a member
	isMember, err := s.MessageDB.IsMember(channelID, session.Address)
	if err != nil {
		log.Printf("❌ Failed to check membership: %v", err)
		s.SendError(w, "Failed to check membership", http.StatusInternalServerError)
		return
	}

	if !isMember {
		s.SendError(w, "You must be a member to view messages", http.StatusForbidden)
		return
	}

	// Parse query parameters
	limit := 50  // default
	before := r.URL.Query().Get("before")

	// Get messages
	messages, err := s.MessageDB.LoadChannelMessages(channelID, limit, before)
	if err != nil {
		log.Printf("❌ Failed to get messages: %v", err)
		s.SendError(w, "Failed to get messages", http.StatusInternalServerError)
		return
	}

	s.SendJSON(w, api.GetChannelMessagesResponse{
		Success:  true,
		Messages: messages,
		Message:  "Messages retrieved successfully",
	})
}

// HandleEditChannelMessage edits a channel message (admin/owner only)
func (s *Server) HandleEditChannelMessage(w http.ResponseWriter, r *http.Request) {
	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	vars := mux.Vars(r)
	channelID := vars["channelId"]
	messageID := vars["messageId"]

	if channelID == "" || messageID == "" {
		s.SendError(w, "Channel ID and message ID are required", http.StatusBadRequest)
		return
	}

	var req api.EditChannelMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	req.ChannelID = channelID
	req.MessageID = messageID

	// Validate content
	if req.NewContent == "" {
		s.SendError(w, "New content is required", http.StatusBadRequest)
		return
	}

	// Check if user is admin or owner
	isAdminOrOwner, _, err := s.MessageDB.IsAdminOrOwner(channelID, session.Address)
	if err != nil {
		log.Printf("❌ Failed to check permissions: %v", err)
		s.SendError(w, "Failed to check permissions", http.StatusInternalServerError)
		return
	}

	if !isAdminOrOwner {
		s.SendError(w, "Only admins can edit messages", http.StatusForbidden)
		return
	}

	// Edit message
	err = s.MessageDB.EditChannelMessage(channelID, messageID, req.NewContent)
	if err != nil {
		log.Printf("❌ Failed to edit message: %v", err)
		s.SendError(w, "Failed to edit message", http.StatusInternalServerError)
		return
	}

	log.Printf("✏️  Message %s edited in channel %s by %s", messageID, channelID, session.Address)

	// Broadcast to all members
	members, _ := s.MessageDB.GetChannelMembers(channelID)
	for _, member := range members {
		s.BroadcastToUser(member.UserAddress, "channel_message_edited", api.WSChannelMessageEdited{
			ChannelID:  channelID,
			MessageID:  messageID,
			NewContent: req.NewContent,
		})
	}

	s.SendJSON(w, api.EditChannelMessageResponse{
		Success: true,
		Message: "Message edited successfully",
	})
}

// HandleDeleteChannelMessage deletes a channel message (admin/owner only)
func (s *Server) HandleDeleteChannelMessage(w http.ResponseWriter, r *http.Request) {
	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	vars := mux.Vars(r)
	channelID := vars["channelId"]
	messageID := vars["messageId"]

	if channelID == "" || messageID == "" {
		s.SendError(w, "Channel ID and message ID are required", http.StatusBadRequest)
		return
	}

	// Check if user is admin or owner
	isAdminOrOwner, _, err := s.MessageDB.IsAdminOrOwner(channelID, session.Address)
	if err != nil {
		log.Printf("❌ Failed to check permissions: %v", err)
		s.SendError(w, "Failed to check permissions", http.StatusInternalServerError)
		return
	}

	if !isAdminOrOwner {
		s.SendError(w, "Only admins can delete messages", http.StatusForbidden)
		return
	}

	// Delete message
	err = s.MessageDB.DeleteChannelMessage(channelID, messageID)
	if err != nil {
		log.Printf("❌ Failed to delete message: %v", err)
		s.SendError(w, "Failed to delete message", http.StatusInternalServerError)
		return
	}

	log.Printf("🗑️  Message %s deleted in channel %s by %s", messageID, channelID, session.Address)

	// Broadcast to all members
	members, _ := s.MessageDB.GetChannelMembers(channelID)
	for _, member := range members {
		s.BroadcastToUser(member.UserAddress, "channel_message_deleted", api.WSChannelMessageDeleted{
			ChannelID: channelID,
			MessageID: messageID,
		})
	}

	s.SendJSON(w, api.DeleteChannelMessageResponse{
		Success: true,
		Message: "Message deleted successfully",
	})
}

// HandlePinChannelMessage pins a message in a channel (admin/owner only)
func (s *Server) HandlePinChannelMessage(w http.ResponseWriter, r *http.Request) {
	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	vars := mux.Vars(r)
	channelID := vars["channelId"]
	messageID := vars["messageId"]

	if channelID == "" || messageID == "" {
		s.SendError(w, "Channel ID and message ID are required", http.StatusBadRequest)
		return
	}

	// Check if user is admin or owner
	isAdminOrOwner, _, err := s.MessageDB.IsAdminOrOwner(channelID, session.Address)
	if err != nil {
		log.Printf("❌ Failed to check permissions: %v", err)
		s.SendError(w, "Failed to check permissions", http.StatusInternalServerError)
		return
	}

	if !isAdminOrOwner {
		s.SendError(w, "Only admins can pin messages", http.StatusForbidden)
		return
	}

	// Pin message
	err = s.MessageDB.PinChannelMessage(channelID, messageID, session.Address)
	if err != nil {
		log.Printf("❌ Failed to pin message: %v", err)
		s.SendError(w, "Failed to pin message", http.StatusInternalServerError)
		return
	}

	log.Printf("📌 Message %s pinned in channel %s by %s", messageID, channelID, session.Address)

	// Broadcast to all members
	members, _ := s.MessageDB.GetChannelMembers(channelID)
	for _, member := range members {
		s.BroadcastToUser(member.UserAddress, "channel_message_pinned", api.WSChannelMessagePinned{
			ChannelID: channelID,
			MessageID: messageID,
		})
	}

	s.SendJSON(w, api.PinChannelMessageResponse{
		Success: true,
		Message: "Message pinned successfully",
	})
}

// HandleUnpinChannelMessage unpins a message in a channel (admin/owner only)
func (s *Server) HandleUnpinChannelMessage(w http.ResponseWriter, r *http.Request) {
	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	vars := mux.Vars(r)
	channelID := vars["channelId"]
	messageID := vars["messageId"]

	if channelID == "" || messageID == "" {
		s.SendError(w, "Channel ID and message ID are required", http.StatusBadRequest)
		return
	}

	// Check if user is admin or owner
	isAdminOrOwner, _, err := s.MessageDB.IsAdminOrOwner(channelID, session.Address)
	if err != nil {
		log.Printf("❌ Failed to check permissions: %v", err)
		s.SendError(w, "Failed to check permissions", http.StatusInternalServerError)
		return
	}

	if !isAdminOrOwner {
		s.SendError(w, "Only admins can unpin messages", http.StatusForbidden)
		return
	}

	// Unpin message
	err = s.MessageDB.UnpinChannelMessage(channelID, messageID)
	if err != nil {
		log.Printf("❌ Failed to unpin message: %v", err)
		s.SendError(w, "Failed to unpin message", http.StatusInternalServerError)
		return
	}

	log.Printf("📌 Message %s unpinned in channel %s by %s", messageID, channelID, session.Address)

	// Broadcast to all members
	members, _ := s.MessageDB.GetChannelMembers(channelID)
	for _, member := range members {
		s.BroadcastToUser(member.UserAddress, "channel_message_pinned", api.WSChannelMessagePinned{
			ChannelID: channelID,
			MessageID: messageID,
		})
	}

	s.SendJSON(w, api.UnpinChannelMessageResponse{
		Success: true,
		Message: "Message unpinned successfully",
	})
}

// HandleAddChannelMessageReaction adds a reaction to a channel message
func (s *Server) HandleAddChannelMessageReaction(w http.ResponseWriter, r *http.Request) {
	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	vars := mux.Vars(r)
	channelID := vars["channelId"]
	messageID := vars["messageId"]

	if channelID == "" || messageID == "" {
		s.SendError(w, "Channel ID and message ID are required", http.StatusBadRequest)
		return
	}

	var req struct {
		Emoji string `json:"emoji"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Emoji == "" {
		s.SendError(w, "Emoji is required", http.StatusBadRequest)
		return
	}

	// Check if user is a member
	isMember, err := s.MessageDB.IsMember(channelID, session.Address)
	if err != nil || !isMember {
		s.SendError(w, "You must be a member to react to messages", http.StatusForbidden)
		return
	}

	// Add reaction
	err = s.MessageDB.AddChannelMessageReaction(channelID, messageID, req.Emoji, session.Address)
	if err != nil {
		log.Printf("❌ Failed to add reaction: %v", err)
		s.SendError(w, "Failed to add reaction", http.StatusInternalServerError)
		return
	}

	// Broadcast to all members
	members, _ := s.MessageDB.GetChannelMembers(channelID)
	for _, member := range members {
		s.BroadcastToUser(member.UserAddress, "channel_reaction_added", api.WSReactionAdded{
			MessageID: messageID,
			ChatID:    channelID,
			Emoji:     req.Emoji,
			From:      session.Address,
		})
	}

	s.SendJSON(w, map[string]interface{}{
		"success": true,
		"message": "Reaction added successfully",
	})
}

// HandleRemoveChannelMessageReaction removes a reaction from a channel message
func (s *Server) HandleRemoveChannelMessageReaction(w http.ResponseWriter, r *http.Request) {
	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	vars := mux.Vars(r)
	channelID := vars["channelId"]
	messageID := vars["messageId"]

	if channelID == "" || messageID == "" {
		s.SendError(w, "Channel ID and message ID are required", http.StatusBadRequest)
		return
	}

	var req struct {
		Emoji string `json:"emoji"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Emoji == "" {
		s.SendError(w, "Emoji is required", http.StatusBadRequest)
		return
	}

	// Check if user is a member
	isMember, err := s.MessageDB.IsMember(channelID, session.Address)
	if err != nil || !isMember {
		s.SendError(w, "You must be a member to react to messages", http.StatusForbidden)
		return
	}

	// Remove reaction
	err = s.MessageDB.RemoveChannelMessageReaction(channelID, messageID, req.Emoji, session.Address)
	if err != nil {
		log.Printf("❌ Failed to remove reaction: %v", err)
		s.SendError(w, "Failed to remove reaction", http.StatusInternalServerError)
		return
	}

	// Broadcast to all members
	members, _ := s.MessageDB.GetChannelMembers(channelID)
	for _, member := range members {
		s.BroadcastToUser(member.UserAddress, "channel_reaction_removed", api.WSReactionRemoved{
			MessageID: messageID,
			ChatID:    channelID,
			Emoji:     req.Emoji,
			From:      session.Address,
		})
	}

	s.SendJSON(w, map[string]interface{}{
		"success": true,
		"message": "Reaction removed successfully",
	})
}
