package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/ZentaChain/zentalk-api/pkg/api"
	"github.com/gorilla/mux"
)

// HandleCreateChannel creates a new channel
func (s *Server) HandleCreateChannel(w http.ResponseWriter, r *http.Request) {
	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	var req api.CreateChannelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate input
	if req.Name == "" {
		s.SendError(w, "Channel name is required", http.StatusBadRequest)
		return
	}

	if req.Type != "public" && req.Type != "private" {
		s.SendError(w, "Channel type must be 'public' or 'private'", http.StatusBadRequest)
		return
	}

	// Create channel in database
	channel, err := s.MessageDB.CreateChannel(session.Address, req)
	if err != nil {
		log.Printf("❌ Failed to create channel: %v", err)
		// Check if error is due to duplicate channel name
		if strings.Contains(err.Error(), "already taken") {
			s.SendError(w, err.Error(), http.StatusConflict)
			return
		}
		s.SendError(w, "Failed to create channel", http.StatusInternalServerError)
		return
	}

	log.Printf("✅ Channel created: %s by %s", channel.Name, session.Address)

	// Broadcast to owner's WebSocket
	s.BroadcastToUser(session.Address, "channel_created", api.WSChannelCreated{
		Channel: *channel,
	})

	// If there are initial members, broadcast to them as well
	if len(req.InitialMembers) > 0 {
		for _, memberAddr := range req.InitialMembers {
			if memberAddr != session.Address {
				s.BroadcastToUser(memberAddr, "channel_created", api.WSChannelCreated{
					Channel: *channel,
				})
			}
		}
	}

	s.SendJSON(w, api.CreateChannelResponse{
		Success: true,
		Channel: *channel,
		Message: "Channel created successfully",
	})
}

// HandleGetChannels retrieves all channels user is subscribed to
func (s *Server) HandleGetChannels(w http.ResponseWriter, r *http.Request) {
	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	channels, err := s.MessageDB.GetUserChannels(session.Address)
	if err != nil {
		log.Printf("❌ Failed to get channels: %v", err)
		s.SendError(w, "Failed to get channels", http.StatusInternalServerError)
		return
	}

	s.SendJSON(w, api.GetChannelsResponse{
		Success:  true,
		Channels: channels,
		Message:  "Channels retrieved successfully",
	})
}

// HandleGetChannelInfo retrieves information about a specific channel
func (s *Server) HandleGetChannelInfo(w http.ResponseWriter, r *http.Request) {
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
		// For public channels, allow viewing info
		channel, err := s.MessageDB.GetChannel(channelID, session.Address)
		if err != nil {
			s.SendError(w, "Channel not found", http.StatusNotFound)
			return
		}

		if channel.Type != "public" {
			s.SendError(w, "You must be a member to view this private channel", http.StatusForbidden)
			return
		}

		s.SendJSON(w, api.GetChannelInfoResponse{
			Success: true,
			Channel: *channel,
			Message: "Channel info retrieved successfully",
		})
		return
	}

	// User is a member, get full info
	channel, err := s.MessageDB.GetChannel(channelID, session.Address)
	if err != nil {
		s.SendError(w, "Channel not found", http.StatusNotFound)
		return
	}

	s.SendJSON(w, api.GetChannelInfoResponse{
		Success: true,
		Channel: *channel,
		Message: "Channel info retrieved successfully",
	})
}

// HandleUpdateChannel updates channel information
func (s *Server) HandleUpdateChannel(w http.ResponseWriter, r *http.Request) {
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

	var req api.UpdateChannelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	req.ChannelID = channelID

	// Check if user is admin or owner
	isAdminOrOwner, _, err := s.MessageDB.IsAdminOrOwner(channelID, session.Address)
	if err != nil {
		log.Printf("❌ Failed to check permissions: %v", err)
		s.SendError(w, "Failed to check permissions", http.StatusInternalServerError)
		return
	}

	if !isAdminOrOwner {
		s.SendError(w, "Only admins can update channel info", http.StatusForbidden)
		return
	}

	// Update channel
	err = s.MessageDB.UpdateChannel(channelID, req)
	if err != nil {
		log.Printf("❌ Failed to update channel: %v", err)
		s.SendError(w, "Failed to update channel", http.StatusInternalServerError)
		return
	}

	// Get updated channel
	channel, err := s.MessageDB.GetChannel(channelID, session.Address)
	if err != nil {
		s.SendError(w, "Failed to retrieve updated channel", http.StatusInternalServerError)
		return
	}

	log.Printf("✅ Channel %s updated by %s", channelID, session.Address)

	// Broadcast update to all members
	members, err := s.MessageDB.GetChannelMembers(channelID)
	if err == nil {
		updates := make(map[string]interface{})
		if req.Name != "" {
			updates["name"] = req.Name
		}
		if req.Description != "" {
			updates["description"] = req.Description
		}
		if req.AvatarChunkID > 0 {
			updates["avatar_chunk_id"] = req.AvatarChunkID
		}
		if req.Type != "" {
			updates["type"] = req.Type
		}

		for _, member := range members {
			s.BroadcastToUser(member.UserAddress, "channel_updated", api.WSChannelUpdated{
				ChannelID: channelID,
				Updates:   updates,
			})
		}
	}

	s.SendJSON(w, api.UpdateChannelResponse{
		Success: true,
		Channel: *channel,
		Message: "Channel updated successfully",
	})
}

// HandleDeleteChannel deletes a channel (owner only)
func (s *Server) HandleDeleteChannel(w http.ResponseWriter, r *http.Request) {
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

	// Check if user is owner
	isOwner, err := s.MessageDB.IsOwner(channelID, session.Address)
	if err != nil {
		log.Printf("❌ Failed to check ownership: %v", err)
		s.SendError(w, "Failed to check ownership", http.StatusInternalServerError)
		return
	}

	if !isOwner {
		s.SendError(w, "Only the owner can delete the channel", http.StatusForbidden)
		return
	}

	// Get members before deletion to notify them
	members, _ := s.MessageDB.GetChannelMembers(channelID)

	// Delete channel
	err = s.MessageDB.DeleteChannel(channelID)
	if err != nil {
		log.Printf("❌ Failed to delete channel: %v", err)
		s.SendError(w, "Failed to delete channel", http.StatusInternalServerError)
		return
	}

	log.Printf("🗑️  Channel %s deleted by %s", channelID, session.Address)

	// Broadcast deletion to all members
	for _, member := range members {
		s.BroadcastToUser(member.UserAddress, "channel_deleted", api.WSChannelDeleted{
			ChannelID: channelID,
		})
	}

	s.SendJSON(w, api.DeleteChannelResponse{
		Success: true,
		Message: "Channel deleted successfully",
	})
}

// HandleDiscoverChannels allows users to discover public channels
func (s *Server) HandleDiscoverChannels(w http.ResponseWriter, r *http.Request) {
	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	var req api.DiscoverChannelsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// If no body, use query parameters
		req.Query = r.URL.Query().Get("query")
		req.Limit = 50  // Default
		req.Offset = 0
	}

	channels, err := s.MessageDB.DiscoverPublicChannels(req.Query, req.Limit, req.Offset)
	if err != nil {
		log.Printf("❌ Failed to discover channels: %v", err)
		s.SendError(w, "Failed to discover channels", http.StatusInternalServerError)
		return
	}

	log.Printf("🔍 User %s discovered %d channels", session.Address, len(channels))

	s.SendJSON(w, api.DiscoverChannelsResponse{
		Success:  true,
		Channels: channels,
		Message:  "Channels discovered successfully",
	})
}
