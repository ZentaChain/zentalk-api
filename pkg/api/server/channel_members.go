package server

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/ZentaChain/zentalk-api/pkg/api"
	"github.com/gorilla/mux"
)

// HandleSubscribeToChannel adds user to a public channel
func (s *Server) HandleSubscribeToChannel(w http.ResponseWriter, r *http.Request) {
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

	var req api.SubscribeToChannelRequest
	json.NewDecoder(r.Body).Decode(&req)
	req.ChannelID = channelID

	// Get channel info
	channel, err := s.MessageDB.GetChannel(channelID, session.Address)
	if err != nil {
		s.SendError(w, "Channel not found", http.StatusNotFound)
		return
	}

	// Check if already a member
	isMember, err := s.MessageDB.IsMember(channelID, session.Address)
	if err != nil {
		log.Printf("❌ Failed to check membership: %v", err)
		s.SendError(w, "Failed to check membership", http.StatusInternalServerError)
		return
	}

	if isMember {
		s.SendError(w, "You are already subscribed to this channel", http.StatusBadRequest)
		return
	}

	// For private channels, require invite code
	if channel.Type == "private" {
		if req.InviteCode == "" {
			s.SendError(w, "Invite code is required for private channels", http.StatusBadRequest)
			return
		}

		// Validate invite code
		inviteChannelID, err := s.MessageDB.ValidateChannelInvite(req.InviteCode)
		if err != nil {
			s.SendError(w, err.Error(), http.StatusBadRequest)
			return
		}

		if inviteChannelID != channelID {
			s.SendError(w, "Invalid invite code for this channel", http.StatusBadRequest)
			return
		}

		// Increment invite uses
		s.MessageDB.IncrementInviteUses(req.InviteCode)
	}

	// Add as subscriber
	err = s.MessageDB.AddChannelMember(channelID, session.Address, "subscriber")
	if err != nil {
		log.Printf("❌ Failed to add member: %v", err)
		s.SendError(w, "Failed to subscribe to channel", http.StatusInternalServerError)
		return
	}

	log.Printf("✅ User %s subscribed to channel %s", session.Address, channelID)

	// Get member info to broadcast
	member, _ := s.MessageDB.GetChannelMember(channelID, session.Address)

	// Broadcast to all members
	members, _ := s.MessageDB.GetChannelMembers(channelID)
	for _, m := range members {
		normalizedAddr := api.NormalizeAddress(m.UserAddress)
		s.BroadcastToUser(normalizedAddr, "channel_member_joined", api.WSChannelMemberJoined{
			ChannelID: channelID,
			Member:    *member,
		})
	}

	s.SendJSON(w, api.SubscribeToChannelResponse{
		Success: true,
		Message: "Successfully subscribed to channel",
	})
}

// HandleUnsubscribeFromChannel removes user from channel
func (s *Server) HandleUnsubscribeFromChannel(w http.ResponseWriter, r *http.Request) {
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

	if isOwner {
		s.SendError(w, "Owner cannot leave the channel. Please transfer ownership or delete the channel.", http.StatusForbidden)
		return
	}

	// Get members before leaving
	members, _ := s.MessageDB.GetChannelMembers(channelID)

	// Remove member
	err = s.MessageDB.RemoveChannelMember(channelID, session.Address)
	if err != nil {
		log.Printf("❌ Failed to remove member: %v", err)
		s.SendError(w, "Failed to unsubscribe from channel", http.StatusInternalServerError)
		return
	}

	log.Printf("✅ User %s unsubscribed from channel %s", session.Address, channelID)

	// Broadcast to all members
	for _, m := range members {
		normalizedAddr := api.NormalizeAddress(m.UserAddress)
		s.BroadcastToUser(normalizedAddr, "channel_member_left", api.WSChannelMemberLeft{
			ChannelID:   channelID,
			UserAddress: session.Address,
		})
	}

	s.SendJSON(w, api.UnsubscribeFromChannelResponse{
		Success: true,
		Message: "Successfully unsubscribed from channel",
	})
}

// HandleGetChannelMembers retrieves all members of a channel
func (s *Server) HandleGetChannelMembers(w http.ResponseWriter, r *http.Request) {
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
		s.SendError(w, "You must be a member to view members list", http.StatusForbidden)
		return
	}

	members, err := s.MessageDB.GetChannelMembers(channelID)
	if err != nil {
		log.Printf("❌ Failed to get members: %v", err)
		s.SendError(w, "Failed to get channel members", http.StatusInternalServerError)
		return
	}

	s.SendJSON(w, api.GetChannelMembersResponse{
		Success: true,
		Members: members,
		Message: "Members retrieved successfully",
	})
}

// HandleRemoveChannelMember removes a member from channel (admin/owner only)
func (s *Server) HandleRemoveChannelMember(w http.ResponseWriter, r *http.Request) {
	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	vars := mux.Vars(r)
	channelID := vars["channelId"]
	targetUserAddr := vars["userAddress"]

	if channelID == "" || targetUserAddr == "" {
		s.SendError(w, "Channel ID and user address are required", http.StatusBadRequest)
		return
	}

	// Check if requester is admin or owner
	isAdminOrOwner, _, err := s.MessageDB.IsAdminOrOwner(channelID, session.Address)
	if err != nil {
		log.Printf("❌ Failed to check permissions: %v", err)
		s.SendError(w, "Failed to check permissions", http.StatusInternalServerError)
		return
	}

	if !isAdminOrOwner {
		s.SendError(w, "Only admins can remove members", http.StatusForbidden)
		return
	}

	// Cannot remove owner
	isOwner, _ := s.MessageDB.IsOwner(channelID, targetUserAddr)
	if isOwner {
		s.SendError(w, "Cannot remove the channel owner", http.StatusForbidden)
		return
	}

	// Get members before removal
	members, _ := s.MessageDB.GetChannelMembers(channelID)

	// Remove member
	err = s.MessageDB.RemoveChannelMember(channelID, targetUserAddr)
	if err != nil {
		log.Printf("❌ Failed to remove member: %v", err)
		s.SendError(w, "Failed to remove member", http.StatusInternalServerError)
		return
	}

	log.Printf("🗑️  User %s removed from channel %s by %s", targetUserAddr, channelID, session.Address)

	// Broadcast to all members
	for _, m := range members {
		normalizedAddr := api.NormalizeAddress(m.UserAddress)
		s.BroadcastToUser(normalizedAddr, "channel_member_removed", api.WSChannelMemberRemoved{
			ChannelID:   channelID,
			UserAddress: targetUserAddr,
		})
	}

	s.SendJSON(w, api.RemoveChannelMemberResponse{
		Success: true,
		Message: "Member removed successfully",
	})
}

// HandlePromoteToAdmin promotes a member to admin
func (s *Server) HandlePromoteToAdmin(w http.ResponseWriter, r *http.Request) {
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

	var req api.PromoteToAdminRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	req.ChannelID = channelID

	// Check if requester is admin or owner
	isAdminOrOwner, _, err := s.MessageDB.IsAdminOrOwner(channelID, session.Address)
	if err != nil {
		log.Printf("❌ Failed to check permissions: %v", err)
		s.SendError(w, "Failed to check permissions", http.StatusInternalServerError)
		return
	}

	if !isAdminOrOwner {
		s.SendError(w, "Only admins can promote members", http.StatusForbidden)
		return
	}

	// Promote to admin
	err = s.MessageDB.UpdateChannelMemberRole(channelID, req.UserAddress, "admin")
	if err != nil {
		log.Printf("❌ Failed to promote member: %v", err)
		s.SendError(w, "Failed to promote member", http.StatusInternalServerError)
		return
	}

	log.Printf("⬆️  User %s promoted to admin in channel %s by %s", req.UserAddress, channelID, session.Address)

	// Broadcast to all members
	members, _ := s.MessageDB.GetChannelMembers(channelID)
	for _, m := range members {
		normalizedAddr := api.NormalizeAddress(m.UserAddress)
		s.BroadcastToUser(normalizedAddr, "channel_member_promoted", api.WSChannelMemberPromoted{
			ChannelID:   channelID,
			UserAddress: req.UserAddress,
			Role:        "admin",
		})
	}

	s.SendJSON(w, api.PromoteToAdminResponse{
		Success: true,
		Message: "Member promoted to admin successfully",
	})
}

// HandleDemoteAdmin demotes an admin to subscriber
func (s *Server) HandleDemoteAdmin(w http.ResponseWriter, r *http.Request) {
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

	var req api.DemoteAdminRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	req.ChannelID = channelID

	// Check if requester is admin or owner
	isAdminOrOwner, _, err := s.MessageDB.IsAdminOrOwner(channelID, session.Address)
	if err != nil {
		log.Printf("❌ Failed to check permissions: %v", err)
		s.SendError(w, "Failed to check permissions", http.StatusInternalServerError)
		return
	}

	if !isAdminOrOwner {
		s.SendError(w, "Only admins can demote members", http.StatusForbidden)
		return
	}

	// Cannot demote owner
	isOwner, _ := s.MessageDB.IsOwner(channelID, req.UserAddress)
	if isOwner {
		s.SendError(w, "Cannot demote the channel owner", http.StatusForbidden)
		return
	}

	// Demote to subscriber
	err = s.MessageDB.UpdateChannelMemberRole(channelID, req.UserAddress, "subscriber")
	if err != nil {
		log.Printf("❌ Failed to demote admin: %v", err)
		s.SendError(w, "Failed to demote admin", http.StatusInternalServerError)
		return
	}

	log.Printf("⬇️  Admin %s demoted to subscriber in channel %s by %s", req.UserAddress, channelID, session.Address)

	// Broadcast to all members
	members, _ := s.MessageDB.GetChannelMembers(channelID)
	for _, m := range members {
		normalizedAddr := api.NormalizeAddress(m.UserAddress)
		s.BroadcastToUser(normalizedAddr, "channel_member_promoted", api.WSChannelMemberPromoted{
			ChannelID:   channelID,
			UserAddress: req.UserAddress,
			Role:        "subscriber",
		})
	}

	s.SendJSON(w, api.DemoteAdminResponse{
		Success: true,
		Message: "Admin demoted successfully",
	})
}

// HandleTransferOwnership transfers channel ownership
func (s *Server) HandleTransferOwnership(w http.ResponseWriter, r *http.Request) {
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

	var req api.TransferOwnershipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	req.ChannelID = channelID

	// Check if requester is owner
	isOwner, err := s.MessageDB.IsOwner(channelID, session.Address)
	if err != nil {
		log.Printf("❌ Failed to check ownership: %v", err)
		s.SendError(w, "Failed to check ownership", http.StatusInternalServerError)
		return
	}

	if !isOwner {
		s.SendError(w, "Only the owner can transfer ownership", http.StatusForbidden)
		return
	}

	// Check if new owner is a member
	isMember, err := s.MessageDB.IsMember(channelID, req.NewOwnerAddress)
	if err != nil || !isMember {
		s.SendError(w, "New owner must be a member of the channel", http.StatusBadRequest)
		return
	}

	// Transfer ownership: demote current owner to admin, promote new owner
	err = s.MessageDB.UpdateChannelMemberRole(channelID, session.Address, "admin")
	if err != nil {
		log.Printf("❌ Failed to demote current owner: %v", err)
		s.SendError(w, "Failed to transfer ownership", http.StatusInternalServerError)
		return
	}

	err = s.MessageDB.UpdateChannelMemberRole(channelID, req.NewOwnerAddress, "owner")
	if err != nil {
		// Rollback
		s.MessageDB.UpdateChannelMemberRole(channelID, session.Address, "owner")
		log.Printf("❌ Failed to promote new owner: %v", err)
		s.SendError(w, "Failed to transfer ownership", http.StatusInternalServerError)
		return
	}

	// Update owner in channels table
	s.MessageDB.UpdateChannel(channelID, api.UpdateChannelRequest{
		ChannelID: channelID,
	})

	log.Printf("👑 Ownership of channel %s transferred from %s to %s", channelID, session.Address, req.NewOwnerAddress)

	// Broadcast to all members
	members, _ := s.MessageDB.GetChannelMembers(channelID)
	for _, m := range members {
		normalizedAddr := api.NormalizeAddress(m.UserAddress)
		s.BroadcastToUser(normalizedAddr, "channel_member_promoted", api.WSChannelMemberPromoted{
			ChannelID:   channelID,
			UserAddress: req.NewOwnerAddress,
			Role:        "owner",
		})
	}

	s.SendJSON(w, api.TransferOwnershipResponse{
		Success: true,
		Message: "Ownership transferred successfully",
	})
}

// HandleMuteChannel mutes a channel for the user
func (s *Server) HandleMuteChannel(w http.ResponseWriter, r *http.Request) {
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

	err = s.MessageDB.MuteChannel(channelID, session.Address)
	if err != nil {
		log.Printf("❌ Failed to mute channel: %v", err)
		s.SendError(w, "Failed to mute channel", http.StatusInternalServerError)
		return
	}

	s.SendJSON(w, api.MuteChannelResponse{
		Success: true,
		Message: "Channel muted successfully",
	})
}

// HandleUnmuteChannel unmutes a channel for the user
func (s *Server) HandleUnmuteChannel(w http.ResponseWriter, r *http.Request) {
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

	err = s.MessageDB.UnmuteChannel(channelID, session.Address)
	if err != nil {
		log.Printf("❌ Failed to unmute channel: %v", err)
		s.SendError(w, "Failed to unmute channel", http.StatusInternalServerError)
		return
	}

	s.SendJSON(w, api.UnmuteChannelResponse{
		Success: true,
		Message: "Channel unmuted successfully",
	})
}

// HandleCreateChannelInvite creates an invite link for private channel
func (s *Server) HandleCreateChannelInvite(w http.ResponseWriter, r *http.Request) {
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

	var req api.CreateChannelInviteRequest
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
		s.SendError(w, "Only admins can create invites", http.StatusForbidden)
		return
	}

	// Parse expiration time if provided
	var expiresAt *time.Time
	if req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err != nil {
			s.SendError(w, "Invalid expiration time format", http.StatusBadRequest)
			return
		}
		expiresAt = &t
	}

	// Create invite
	invite, err := s.MessageDB.CreateChannelInvite(channelID, session.Address, req.MaxUses, expiresAt)
	if err != nil {
		log.Printf("❌ Failed to create invite: %v", err)
		s.SendError(w, "Failed to create invite", http.StatusInternalServerError)
		return
	}

	s.SendJSON(w, api.CreateChannelInviteResponse{
		Success: true,
		Invite:  *invite,
		Message: "Invite created successfully",
	})
}

// HandleGetChannelInvites retrieves all invites for a channel
func (s *Server) HandleGetChannelInvites(w http.ResponseWriter, r *http.Request) {
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

	// Check if user is admin or owner
	isAdminOrOwner, _, err := s.MessageDB.IsAdminOrOwner(channelID, session.Address)
	if err != nil {
		log.Printf("❌ Failed to check permissions: %v", err)
		s.SendError(w, "Failed to check permissions", http.StatusInternalServerError)
		return
	}

	if !isAdminOrOwner {
		s.SendError(w, "Only admins can view invites", http.StatusForbidden)
		return
	}

	invites, err := s.MessageDB.GetChannelInvites(channelID)
	if err != nil {
		log.Printf("❌ Failed to get invites: %v", err)
		s.SendError(w, "Failed to get invites", http.StatusInternalServerError)
		return
	}

	s.SendJSON(w, api.GetChannelInvitesResponse{
		Success: true,
		Invites: invites,
		Message: "Invites retrieved successfully",
	})
}

// HandleRevokeChannelInvite revokes an invite
func (s *Server) HandleRevokeChannelInvite(w http.ResponseWriter, r *http.Request) {
	_, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	vars := mux.Vars(r)
	inviteID := vars["inviteId"]

	if inviteID == "" {
		s.SendError(w, "Invite ID is required", http.StatusBadRequest)
		return
	}

	// TODO: Check if user has permission to revoke (admin/owner of the channel)
	// Will need to use session.WalletAddress to verify ownership
	// For now, allowing any authenticated user

	err = s.MessageDB.RevokeChannelInvite(inviteID)
	if err != nil {
		log.Printf("❌ Failed to revoke invite: %v", err)
		s.SendError(w, "Failed to revoke invite", http.StatusInternalServerError)
		return
	}

	s.SendJSON(w, api.RevokeChannelInviteResponse{
		Success: true,
		Message: "Invite revoked successfully",
	})
}
