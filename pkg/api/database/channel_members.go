package database

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"github.com/ZentaChain/zentalk-api/pkg/api"
	"github.com/google/uuid"
)

// AddChannelMember adds a user to a channel
func (db *DB) AddChannelMember(channelID, userAddr, role string) error {
	query := `
		INSERT INTO channel_members
		(channel_id, user_address, role, joined_at)
		VALUES (?, ?, ?, ?)
	`

	_, err := db.Conn.Exec(query, channelID, userAddr, role, time.Now())
	if err != nil {
		return fmt.Errorf("failed to add channel member: %v", err)
	}

	// Update subscriber count
	_, err = db.Conn.Exec(
		"UPDATE channels SET subscriber_count = subscriber_count + 1 WHERE id = ?",
		channelID,
	)
	if err != nil {
		log.Printf("⚠️  Failed to update subscriber count: %v", err)
	}

	log.Printf("✅ User %s added to channel %s as %s", userAddr, channelID, role)
	return nil
}

// RemoveChannelMember removes a user from a channel
func (db *DB) RemoveChannelMember(channelID, userAddr string) error {
	query := "DELETE FROM channel_members WHERE channel_id = ? AND user_address = ?"

	result, err := db.Conn.Exec(query, channelID, userAddr)
	if err != nil {
		return fmt.Errorf("failed to remove channel member: %v", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("member not found in channel")
	}

	// Update subscriber count
	_, err = db.Conn.Exec(
		"UPDATE channels SET subscriber_count = subscriber_count - 1 WHERE id = ? AND subscriber_count > 0",
		channelID,
	)
	if err != nil {
		log.Printf("⚠️  Failed to update subscriber count: %v", err)
	}

	log.Printf("🗑️  User %s removed from channel %s", userAddr, channelID)
	return nil
}

// GetChannelMember retrieves a specific channel member
func (db *DB) GetChannelMember(channelID, userAddr string) (*api.ChannelMember, error) {
	query := `
		SELECT cm.channel_id, cm.user_address, cm.role, cm.joined_at, cm.is_muted, cm.last_read_message_id,
		       u.username, u.first_name, u.last_name, u.avatar_chunk_id
		FROM channel_members cm
		LEFT JOIN users u ON cm.user_address = u.wallet_address
		WHERE cm.channel_id = ? AND cm.user_address = ?
	`

	var member api.ChannelMember
	var joinedAt time.Time
	var username, firstName, lastName sql.NullString
	var avatarChunkID sql.NullInt64

	err := db.Conn.QueryRow(query, channelID, userAddr).Scan(
		&member.ChannelID,
		&member.UserAddress,
		&member.Role,
		&joinedAt,
		&member.IsMuted,
		&member.LastReadMessageID,
		&username,
		&firstName,
		&lastName,
		&avatarChunkID,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("member not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query channel member: %v", err)
	}

	member.JoinedAt = joinedAt.Format(time.RFC3339)

	if username.Valid {
		member.Username = username.String
	}
	if firstName.Valid {
		member.FirstName = firstName.String
	}
	if lastName.Valid {
		member.LastName = lastName.String
	}
	if avatarChunkID.Valid {
		member.Avatar = fmt.Sprintf("%d", avatarChunkID.Int64)
	}

	return &member, nil
}

// GetChannelMembers retrieves all members of a channel
func (db *DB) GetChannelMembers(channelID string) ([]api.ChannelMember, error) {
	query := `
		SELECT cm.channel_id, cm.user_address, cm.role, cm.joined_at, cm.is_muted, cm.last_read_message_id,
		       u.username, u.first_name, u.last_name, u.avatar_chunk_id
		FROM channel_members cm
		LEFT JOIN users u ON cm.user_address = u.wallet_address
		WHERE cm.channel_id = ?
		ORDER BY
			CASE cm.role
				WHEN 'owner' THEN 1
				WHEN 'admin' THEN 2
				WHEN 'subscriber' THEN 3
			END,
			cm.joined_at ASC
	`

	rows, err := db.Conn.Query(query, channelID)
	if err != nil {
		return nil, fmt.Errorf("failed to query channel members: %v", err)
	}
	defer rows.Close()

	members := make([]api.ChannelMember, 0)

	for rows.Next() {
		var member api.ChannelMember
		var joinedAt time.Time
		var username, firstName, lastName sql.NullString
		var avatarChunkID sql.NullInt64
		var lastReadMessageID sql.NullString

		err := rows.Scan(
			&member.ChannelID,
			&member.UserAddress,
			&member.Role,
			&joinedAt,
			&member.IsMuted,
			&lastReadMessageID,
			&username,
			&firstName,
			&lastName,
			&avatarChunkID,
		)

		if err != nil {
			log.Printf("Error scanning channel member: %v", err)
			continue
		}

		member.JoinedAt = joinedAt.Format(time.RFC3339)

		if username.Valid {
			member.Username = username.String
		}
		if firstName.Valid {
			member.FirstName = firstName.String
		}
		if lastName.Valid {
			member.LastName = lastName.String
		}
		if avatarChunkID.Valid {
			member.Avatar = fmt.Sprintf("%d", avatarChunkID.Int64)
		}
		if lastReadMessageID.Valid {
			member.LastReadMessageID = lastReadMessageID.String
		}

		members = append(members, member)
	}

	log.Printf("📊 GetChannelMembers for %s: found %d members", channelID, len(members))
	return members, nil
}

// UpdateChannelMemberRole updates a member's role
func (db *DB) UpdateChannelMemberRole(channelID, userAddr, newRole string) error {
	query := "UPDATE channel_members SET role = ? WHERE channel_id = ? AND user_address = ?"

	result, err := db.Conn.Exec(query, newRole, channelID, userAddr)
	if err != nil {
		return fmt.Errorf("failed to update member role: %v", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("member not found in channel")
	}

	log.Printf("✅ User %s role updated to %s in channel %s", userAddr, newRole, channelID)
	return nil
}

// IsMember checks if a user is a member of a channel
func (db *DB) IsMember(channelID, userAddr string) (bool, error) {
	query := "SELECT COUNT(*) FROM channel_members WHERE channel_id = ? AND user_address = ?"

	var count int
	err := db.Conn.QueryRow(query, channelID, userAddr).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check membership: %v", err)
	}

	return count > 0, nil
}

// IsAdminOrOwner checks if a user is an admin or owner of a channel
func (db *DB) IsAdminOrOwner(channelID, userAddr string) (bool, string, error) {
	query := "SELECT role FROM channel_members WHERE channel_id = ? AND user_address = ?"

	var role string
	err := db.Conn.QueryRow(query, channelID, userAddr).Scan(&role)
	if err == sql.ErrNoRows {
		return false, "", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("failed to check role: %v", err)
	}

	return role == "admin" || role == "owner", role, nil
}

// IsOwner checks if a user is the owner of a channel
func (db *DB) IsOwner(channelID, userAddr string) (bool, error) {
	query := "SELECT COUNT(*) FROM channel_members WHERE channel_id = ? AND user_address = ? AND role = 'owner'"

	var count int
	err := db.Conn.QueryRow(query, channelID, userAddr).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check ownership: %v", err)
	}

	return count > 0, nil
}

// MuteChannel mutes a channel for a user
func (db *DB) MuteChannel(channelID, userAddr string) error {
	query := "UPDATE channel_members SET is_muted = 1 WHERE channel_id = ? AND user_address = ?"

	result, err := db.Conn.Exec(query, channelID, userAddr)
	if err != nil {
		return fmt.Errorf("failed to mute channel: %v", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("member not found in channel")
	}

	log.Printf("🔕 User %s muted channel %s", userAddr, channelID)
	return nil
}

// UnmuteChannel unmutes a channel for a user
func (db *DB) UnmuteChannel(channelID, userAddr string) error {
	query := "UPDATE channel_members SET is_muted = 0 WHERE channel_id = ? AND user_address = ?"

	result, err := db.Conn.Exec(query, channelID, userAddr)
	if err != nil {
		return fmt.Errorf("failed to unmute channel: %v", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("member not found in channel")
	}

	log.Printf("🔔 User %s unmuted channel %s", userAddr, channelID)
	return nil
}

// CreateChannelInvite creates an invite link for a private channel
func (db *DB) CreateChannelInvite(channelID, invitedBy string, maxUses int, expiresAt *time.Time) (*api.ChannelInvite, error) {
	inviteID := uuid.New().String()

	// Generate random invite code
	codeBytes := make([]byte, 16)
	if _, err := rand.Read(codeBytes); err != nil {
		return nil, fmt.Errorf("failed to generate invite code: %v", err)
	}
	inviteCode := hex.EncodeToString(codeBytes)

	query := `
		INSERT INTO channel_invites
		(id, channel_id, invited_by, invite_code, max_uses, uses, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?)
	`

	now := time.Now()
	_, err := db.Conn.Exec(query, inviteID, channelID, invitedBy, inviteCode, maxUses, expiresAt, now)
	if err != nil {
		return nil, fmt.Errorf("failed to create invite: %v", err)
	}

	invite := &api.ChannelInvite{
		ID:         inviteID,
		ChannelID:  channelID,
		InvitedBy:  invitedBy,
		InviteCode: inviteCode,
		MaxUses:    maxUses,
		Uses:       0,
		CreatedAt:  now.Format(time.RFC3339),
	}

	if expiresAt != nil {
		invite.ExpiresAt = expiresAt.Format(time.RFC3339)
	}

	log.Printf("✅ Invite created for channel %s: %s", channelID, inviteCode)
	return invite, nil
}

// GetChannelInvites retrieves all invites for a channel
func (db *DB) GetChannelInvites(channelID string) ([]api.ChannelInvite, error) {
	query := `
		SELECT id, channel_id, invited_by, invite_code, max_uses, uses, expires_at, created_at
		FROM channel_invites
		WHERE channel_id = ?
		ORDER BY created_at DESC
	`

	rows, err := db.Conn.Query(query, channelID)
	if err != nil {
		return nil, fmt.Errorf("failed to query invites: %v", err)
	}
	defer rows.Close()

	invites := make([]api.ChannelInvite, 0)

	for rows.Next() {
		var invite api.ChannelInvite
		var expiresAt sql.NullTime
		var createdAt time.Time

		err := rows.Scan(
			&invite.ID,
			&invite.ChannelID,
			&invite.InvitedBy,
			&invite.InviteCode,
			&invite.MaxUses,
			&invite.Uses,
			&expiresAt,
			&createdAt,
		)

		if err != nil {
			log.Printf("Error scanning invite: %v", err)
			continue
		}

		invite.CreatedAt = createdAt.Format(time.RFC3339)
		if expiresAt.Valid {
			invite.ExpiresAt = expiresAt.Time.Format(time.RFC3339)
		}

		invites = append(invites, invite)
	}

	return invites, nil
}

// ValidateChannelInvite checks if an invite is valid and returns channel ID
func (db *DB) ValidateChannelInvite(inviteCode string) (string, error) {
	query := `
		SELECT channel_id, max_uses, uses, expires_at
		FROM channel_invites
		WHERE invite_code = ?
	`

	var channelID string
	var maxUses, uses int
	var expiresAt sql.NullTime

	err := db.Conn.QueryRow(query, inviteCode).Scan(&channelID, &maxUses, &uses, &expiresAt)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("invalid invite code")
	}
	if err != nil {
		return "", fmt.Errorf("failed to validate invite: %v", err)
	}

	// Check if expired
	if expiresAt.Valid && time.Now().After(expiresAt.Time) {
		return "", fmt.Errorf("invite has expired")
	}

	// Check if max uses reached
	if maxUses > 0 && uses >= maxUses {
		return "", fmt.Errorf("invite has reached maximum uses")
	}

	return channelID, nil
}

// IncrementInviteUses increments the use count of an invite
func (db *DB) IncrementInviteUses(inviteCode string) error {
	query := "UPDATE channel_invites SET uses = uses + 1 WHERE invite_code = ?"

	_, err := db.Conn.Exec(query, inviteCode)
	if err != nil {
		return fmt.Errorf("failed to increment invite uses: %v", err)
	}

	return nil
}

// RevokeChannelInvite deletes an invite
func (db *DB) RevokeChannelInvite(inviteID string) error {
	query := "DELETE FROM channel_invites WHERE id = ?"

	result, err := db.Conn.Exec(query, inviteID)
	if err != nil {
		return fmt.Errorf("failed to revoke invite: %v", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("invite not found")
	}

	log.Printf("🗑️  Invite %s revoked", inviteID)
	return nil
}
