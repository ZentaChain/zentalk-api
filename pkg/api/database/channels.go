package database

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"log"
	"time"

	"github.com/ZentaChain/zentalk-api/pkg/api"
	"github.com/google/uuid"
)

// IsChannelNameTaken checks if a channel name is already taken
func (db *DB) IsChannelNameTaken(name string) (bool, error) {
	query := "SELECT COUNT(*) FROM channels WHERE LOWER(name) = LOWER(?)"
	var count int
	err := db.Conn.QueryRow(query, name).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check channel name: %v", err)
	}
	return count > 0, nil
}

// CreateChannel creates a new channel
func (db *DB) CreateChannel(ownerAddr string, req api.CreateChannelRequest) (*api.Channel, error) {
	// Check if channel name is already taken (case-insensitive)
	nameTaken, err := db.IsChannelNameTaken(req.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to check channel name availability: %v", err)
	}
	if nameTaken {
		return nil, fmt.Errorf("channel name '%s' is already taken", req.Name)
	}

	channelID := uuid.New().String()
	now := time.Now()

	// Decode avatar key if provided
	var avatarKey []byte
	if req.AvatarKey != "" {
		decoded, err := base64.StdEncoding.DecodeString(req.AvatarKey)
		if err != nil {
			return nil, fmt.Errorf("failed to decode avatar key: %v", err)
		}
		avatarKey = decoded
	}

	query := `
		INSERT INTO channels
		(id, name, description, avatar_chunk_id, avatar_key, owner_address, type, subscriber_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
	`

	_, err = db.Conn.Exec(query,
		channelID,
		req.Name,
		req.Description,
		req.AvatarChunkID,
		avatarKey,
		ownerAddr,
		req.Type,
		now,
		now,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create channel: %v", err)
	}

	// Add owner as member
	memberQuery := `
		INSERT INTO channel_members
		(channel_id, user_address, role, joined_at)
		VALUES (?, ?, 'owner', ?)
	`

	_, err = db.Conn.Exec(memberQuery, channelID, ownerAddr, now)
	if err != nil {
		return nil, fmt.Errorf("failed to add owner as member: %v", err)
	}

	// Add initial members if provided
	if len(req.InitialMembers) > 0 {
		for _, memberAddr := range req.InitialMembers {
			if memberAddr == ownerAddr {
				continue // Skip owner (already added)
			}
			_, err = db.Conn.Exec(memberQuery, channelID, memberAddr, now)
			if err != nil {
				log.Printf("⚠️  Failed to add initial member %s: %v", memberAddr, err)
			}
		}

		// Update subscriber count
		_, err = db.Conn.Exec(
			"UPDATE channels SET subscriber_count = ? WHERE id = ?",
			1+len(req.InitialMembers),
			channelID,
		)
	}

	channel := &api.Channel{
		ID:              channelID,
		Name:            req.Name,
		Description:     req.Description,
		AvatarChunkID:   req.AvatarChunkID,
		AvatarKey:       avatarKey,
		OwnerAddress:    ownerAddr,
		Type:            req.Type,
		IsVerified:      false,
		SubscriberCount: 1 + len(req.InitialMembers),
		CreatedAt:       now.Format(time.RFC3339),
		UpdatedAt:       now.Format(time.RFC3339),
		UserRole:        "owner",
	}

	log.Printf("✅ Channel created: %s (%s) by %s", channel.Name, channelID, ownerAddr)
	return channel, nil
}

// GetChannel retrieves a channel by ID
func (db *DB) GetChannel(channelID, userAddr string) (*api.Channel, error) {
	query := `
		SELECT c.id, c.name, c.description, c.avatar_chunk_id, c.avatar_key, c.owner_address,
		       c.type, c.is_verified, c.subscriber_count, c.created_at, c.updated_at,
		       COALESCE(cm.role, '') as user_role, COALESCE(cm.is_muted, 0) as is_muted
		FROM channels c
		LEFT JOIN channel_members cm ON c.id = cm.channel_id AND cm.user_address = ?
		WHERE c.id = ?
	`

	var channel api.Channel
	var avatarKey sql.NullString
	var createdAt, updatedAt time.Time

	err := db.Conn.QueryRow(query, userAddr, channelID).Scan(
		&channel.ID,
		&channel.Name,
		&channel.Description,
		&channel.AvatarChunkID,
		&avatarKey,
		&channel.OwnerAddress,
		&channel.Type,
		&channel.IsVerified,
		&channel.SubscriberCount,
		&createdAt,
		&updatedAt,
		&channel.UserRole,
		&channel.IsMuted,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("channel not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query channel: %v", err)
	}

	if avatarKey.Valid {
		channel.AvatarKey = []byte(avatarKey.String)
	}

	channel.CreatedAt = createdAt.Format(time.RFC3339)
	channel.UpdatedAt = updatedAt.Format(time.RFC3339)

	return &channel, nil
}

// GetUserChannels retrieves all channels a user is subscribed to
func (db *DB) GetUserChannels(userAddr string) ([]api.Channel, error) {
	query := `
		SELECT c.id, c.name, c.description, c.avatar_chunk_id, c.avatar_key, c.owner_address,
		       c.type, c.is_verified, c.subscriber_count, c.created_at, c.updated_at,
		       cm.role, cm.is_muted
		FROM channels c
		INNER JOIN channel_members cm ON c.id = cm.channel_id
		WHERE cm.user_address = ?
		ORDER BY c.updated_at DESC
	`

	rows, err := db.Conn.Query(query, userAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to query user channels: %v", err)
	}
	defer rows.Close()

	channels := make([]api.Channel, 0)

	for rows.Next() {
		var channel api.Channel
		var avatarKey sql.NullString
		var createdAt, updatedAt time.Time

		err := rows.Scan(
			&channel.ID,
			&channel.Name,
			&channel.Description,
			&channel.AvatarChunkID,
			&avatarKey,
			&channel.OwnerAddress,
			&channel.Type,
			&channel.IsVerified,
			&channel.SubscriberCount,
			&createdAt,
			&updatedAt,
			&channel.UserRole,
			&channel.IsMuted,
		)

		if err != nil {
			log.Printf("Error scanning channel: %v", err)
			continue
		}

		if avatarKey.Valid {
			channel.AvatarKey = []byte(avatarKey.String)
		}

		channel.CreatedAt = createdAt.Format(time.RFC3339)
		channel.UpdatedAt = updatedAt.Format(time.RFC3339)

		channels = append(channels, channel)
	}

	log.Printf("✅ Loaded %d channels for user %s", len(channels), userAddr)
	return channels, nil
}

// UpdateChannel updates channel information
func (db *DB) UpdateChannel(channelID string, req api.UpdateChannelRequest) error {
	updates := make([]string, 0)
	args := make([]interface{}, 0)

	if req.Name != "" {
		updates = append(updates, "name = ?")
		args = append(args, req.Name)
	}

	if req.Description != "" {
		updates = append(updates, "description = ?")
		args = append(args, req.Description)
	}

	if req.AvatarChunkID > 0 {
		updates = append(updates, "avatar_chunk_id = ?")
		args = append(args, req.AvatarChunkID)
	}

	if req.AvatarKey != "" {
		decoded, err := base64.StdEncoding.DecodeString(req.AvatarKey)
		if err != nil {
			return fmt.Errorf("failed to decode avatar key: %v", err)
		}
		updates = append(updates, "avatar_key = ?")
		args = append(args, decoded)
	}

	if req.Type != "" {
		updates = append(updates, "type = ?")
		args = append(args, req.Type)
	}

	if len(updates) == 0 {
		return fmt.Errorf("no fields to update")
	}

	updates = append(updates, "updated_at = ?")
	args = append(args, time.Now())

	args = append(args, channelID)

	query := fmt.Sprintf("UPDATE channels SET %s WHERE id = ?", joinStrings(updates, ", "))

	result, err := db.Conn.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to update channel: %v", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("channel not found")
	}

	log.Printf("✅ Channel %s updated", channelID)
	return nil
}

// DeleteChannel deletes a channel
func (db *DB) DeleteChannel(channelID string) error {
	query := "DELETE FROM channels WHERE id = ?"

	result, err := db.Conn.Exec(query, channelID)
	if err != nil {
		return fmt.Errorf("failed to delete channel: %v", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("channel not found")
	}

	log.Printf("🗑️  Channel %s deleted", channelID)
	return nil
}

// DiscoverPublicChannels finds public channels (for discovery/search)
func (db *DB) DiscoverPublicChannels(query string, limit, offset int) ([]api.Channel, error) {
	var rows *sql.Rows
	var err error

	if limit == 0 {
		limit = 50 // Default limit
	}

	if query == "" {
		// Get all public channels
		sqlQuery := `
			SELECT id, name, description, avatar_chunk_id, avatar_key, owner_address,
			       type, is_verified, subscriber_count, created_at, updated_at
			FROM channels
			WHERE type = 'public'
			ORDER BY subscriber_count DESC, created_at DESC
			LIMIT ? OFFSET ?
		`
		rows, err = db.Conn.Query(sqlQuery, limit, offset)
	} else {
		// Search public channels
		sqlQuery := `
			SELECT id, name, description, avatar_chunk_id, avatar_key, owner_address,
			       type, is_verified, subscriber_count, created_at, updated_at
			FROM channels
			WHERE type = 'public' AND (name LIKE ? OR description LIKE ?)
			ORDER BY subscriber_count DESC, created_at DESC
			LIMIT ? OFFSET ?
		`
		searchPattern := "%" + query + "%"
		rows, err = db.Conn.Query(sqlQuery, searchPattern, searchPattern, limit, offset)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to discover channels: %v", err)
	}
	defer rows.Close()

	channels := make([]api.Channel, 0)

	for rows.Next() {
		var channel api.Channel
		var avatarKey sql.NullString
		var createdAt, updatedAt time.Time

		err := rows.Scan(
			&channel.ID,
			&channel.Name,
			&channel.Description,
			&channel.AvatarChunkID,
			&avatarKey,
			&channel.OwnerAddress,
			&channel.Type,
			&channel.IsVerified,
			&channel.SubscriberCount,
			&createdAt,
			&updatedAt,
		)

		if err != nil {
			log.Printf("Error scanning channel: %v", err)
			continue
		}

		if avatarKey.Valid {
			channel.AvatarKey = []byte(avatarKey.String)
		}

		channel.CreatedAt = createdAt.Format(time.RFC3339)
		channel.UpdatedAt = updatedAt.Format(time.RFC3339)

		channels = append(channels, channel)
	}

	return channels, nil
}

// Helper function to join strings
func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}
