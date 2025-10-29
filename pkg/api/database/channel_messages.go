package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/ZentaChain/zentalk-api/pkg/api"
)

// SaveChannelMessage saves a message to a channel
func (db *DB) SaveChannelMessage(msg api.ChannelMessage) error {
	// Serialize reactions to JSON
	reactionsJSON, err := json.Marshal(msg.Reactions)
	if err != nil {
		return fmt.Errorf("failed to serialize reactions: %v", err)
	}

	query := `
		INSERT INTO channel_messages
		(id, channel_id, sender_address, content, timestamp, is_edited, is_deleted, is_pinned,
		 pinned_at, pinned_by, media_url, reactions, view_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			content = excluded.content,
			is_edited = excluded.is_edited,
			is_deleted = excluded.is_deleted,
			is_pinned = excluded.is_pinned,
			pinned_at = excluded.pinned_at,
			pinned_by = excluded.pinned_by,
			reactions = excluded.reactions
	`

	var pinnedAt interface{}
	if msg.PinnedAt != "" {
		pinnedAt = msg.PinnedAt
	}

	var pinnedBy interface{}
	if msg.PinnedBy != "" {
		pinnedBy = msg.PinnedBy
	}

	_, err = db.Conn.Exec(query,
		msg.ID,
		msg.ChannelID,
		msg.Sender.Address,
		msg.Content,
		msg.Timestamp,
		msg.IsEdited,
		msg.IsDeleted,
		msg.IsPinned,
		pinnedAt,
		pinnedBy,
		msg.MediaUrl,
		string(reactionsJSON),
		msg.ViewCount,
	)

	if err != nil {
		return fmt.Errorf("failed to save channel message: %v", err)
	}

	// Store sender info for future retrieval
	_, err = db.Conn.Exec(`
		INSERT OR IGNORE INTO users (wallet_address, username, first_name, last_name, avatar_chunk_id)
		VALUES (?, ?, ?, ?, ?)
	`, msg.Sender.Address, msg.Sender.Username, msg.Sender.FirstName, msg.Sender.LastName, msg.Sender.AvatarChunkID)

	return nil
}

// LoadChannelMessages loads messages from a channel
func (db *DB) LoadChannelMessages(channelID string, limit int, before string) ([]api.ChannelMessage, error) {
	if limit == 0 {
		limit = 50 // Default limit
	}

	var query string
	var args []interface{}

	if before != "" {
		// Pagination: load messages before a specific message ID
		query = `
			SELECT cm.id, cm.channel_id, cm.sender_address, cm.content, cm.timestamp,
			       cm.is_edited, cm.is_deleted, cm.is_pinned, cm.pinned_at, cm.pinned_by,
			       cm.media_url, cm.reactions, cm.view_count,
			       u.username, u.first_name, u.last_name, u.avatar_chunk_id, u.bio, u.is_online, u.status
			FROM channel_messages cm
			LEFT JOIN users u ON cm.sender_address = u.wallet_address
			WHERE cm.channel_id = ? AND cm.created_at < (SELECT created_at FROM channel_messages WHERE id = ?)
			ORDER BY cm.created_at DESC
			LIMIT ?
		`
		args = []interface{}{channelID, before, limit}
	} else {
		query = `
			SELECT cm.id, cm.channel_id, cm.sender_address, cm.content, cm.timestamp,
			       cm.is_edited, cm.is_deleted, cm.is_pinned, cm.pinned_at, cm.pinned_by,
			       cm.media_url, cm.reactions, cm.view_count,
			       u.username, u.first_name, u.last_name, u.avatar_chunk_id, u.bio, u.is_online, u.status
			FROM channel_messages cm
			LEFT JOIN users u ON cm.sender_address = u.wallet_address
			WHERE cm.channel_id = ?
			ORDER BY cm.created_at DESC
			LIMIT ?
		`
		args = []interface{}{channelID, limit}
	}

	rows, err := db.Conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query channel messages: %v", err)
	}
	defer rows.Close()

	messages := make([]api.ChannelMessage, 0)

	for rows.Next() {
		var msg api.ChannelMessage
		var sender api.User
		var reactionsJSON string
		var mediaUrl, pinnedAt, pinnedBy sql.NullString
		var isEdited, isDeleted, isPinned sql.NullBool
		var username, firstName, lastName, bio, status sql.NullString
		var avatarChunkID sql.NullInt64
		var isOnline sql.NullBool

		err := rows.Scan(
			&msg.ID,
			&msg.ChannelID,
			&sender.Address,
			&msg.Content,
			&msg.Timestamp,
			&isEdited,
			&isDeleted,
			&isPinned,
			&pinnedAt,
			&pinnedBy,
			&mediaUrl,
			&reactionsJSON,
			&msg.ViewCount,
			&username,
			&firstName,
			&lastName,
			&avatarChunkID,
			&bio,
			&isOnline,
			&status,
		)

		if err != nil {
			log.Printf("Error scanning channel message: %v", err)
			continue
		}

		// Build sender User object
		if username.Valid {
			sender.Username = username.String
		}
		if firstName.Valid {
			sender.FirstName = firstName.String
		}
		if lastName.Valid {
			sender.LastName = lastName.String
		}
		if avatarChunkID.Valid {
			sender.AvatarChunkID = uint64(avatarChunkID.Int64)
		}
		if bio.Valid {
			sender.Bio = bio.String
		}
		if isOnline.Valid {
			sender.Online = isOnline.Bool
		}
		if status.Valid {
			sender.Status = status.String
		}

		// Build display name
		if sender.FirstName != "" && sender.LastName != "" {
			sender.Name = sender.FirstName + " " + sender.LastName
		} else if sender.FirstName != "" {
			sender.Name = sender.FirstName
		} else if sender.Username != "" {
			sender.Name = sender.Username
		} else {
			sender.Name = sender.Address[:8] + "..."
		}

		msg.Sender = sender

		// Deserialize reactions
		if reactionsJSON != "" && reactionsJSON != "null" {
			var reactions []api.Reaction
			if err := json.Unmarshal([]byte(reactionsJSON), &reactions); err == nil {
				msg.Reactions = reactions
			}
		}

		if mediaUrl.Valid {
			msg.MediaUrl = mediaUrl.String
		}

		if isEdited.Valid {
			msg.IsEdited = isEdited.Bool
		}

		if isDeleted.Valid {
			msg.IsDeleted = isDeleted.Bool
		}

		if isPinned.Valid {
			msg.IsPinned = isPinned.Bool
		}

		if pinnedAt.Valid {
			msg.PinnedAt = pinnedAt.String
		}

		if pinnedBy.Valid {
			msg.PinnedBy = pinnedBy.String
		}

		messages = append(messages, msg)
	}

	// Reverse to get chronological order
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	log.Printf("✅ Loaded %d messages for channel %s", len(messages), channelID)
	return messages, nil
}

// EditChannelMessage updates a channel message
func (db *DB) EditChannelMessage(channelID, messageID, newContent string) error {
	query := `
		UPDATE channel_messages
		SET content = ?, is_edited = 1
		WHERE id = ? AND channel_id = ?
	`

	result, err := db.Conn.Exec(query, newContent, messageID, channelID)
	if err != nil {
		return fmt.Errorf("failed to edit message: %v", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("message not found")
	}

	log.Printf("✏️  Channel message %s edited", messageID)
	return nil
}

// DeleteChannelMessage deletes a channel message (soft delete)
func (db *DB) DeleteChannelMessage(channelID, messageID string) error {
	query := `
		UPDATE channel_messages
		SET is_deleted = 1, content = 'This message was deleted', media_url = ''
		WHERE id = ? AND channel_id = ?
	`

	result, err := db.Conn.Exec(query, messageID, channelID)
	if err != nil {
		return fmt.Errorf("failed to delete message: %v", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("message not found")
	}

	log.Printf("🗑️  Channel message %s deleted", messageID)
	return nil
}

// PinChannelMessage pins a message in a channel
func (db *DB) PinChannelMessage(channelID, messageID, pinnedBy string) error {
	query := `
		UPDATE channel_messages
		SET is_pinned = 1, pinned_at = ?, pinned_by = ?
		WHERE id = ? AND channel_id = ?
	`

	result, err := db.Conn.Exec(query, time.Now(), pinnedBy, messageID, channelID)
	if err != nil {
		return fmt.Errorf("failed to pin message: %v", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("message not found")
	}

	log.Printf("📌 Channel message %s pinned by %s", messageID, pinnedBy)
	return nil
}

// UnpinChannelMessage unpins a message in a channel
func (db *DB) UnpinChannelMessage(channelID, messageID string) error {
	query := `
		UPDATE channel_messages
		SET is_pinned = 0, pinned_at = NULL, pinned_by = NULL
		WHERE id = ? AND channel_id = ?
	`

	result, err := db.Conn.Exec(query, messageID, channelID)
	if err != nil {
		return fmt.Errorf("failed to unpin message: %v", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("message not found")
	}

	log.Printf("📌 Channel message %s unpinned", messageID)
	return nil
}

// GetPinnedMessages retrieves all pinned messages in a channel
func (db *DB) GetPinnedMessages(channelID string) ([]api.ChannelMessage, error) {
	query := `
		SELECT cm.id, cm.channel_id, cm.sender_address, cm.content, cm.timestamp,
		       cm.is_edited, cm.is_deleted, cm.is_pinned, cm.pinned_at, cm.pinned_by,
		       cm.media_url, cm.reactions, cm.view_count,
		       u.username, u.first_name, u.last_name, u.avatar_chunk_id, u.bio, u.is_online, u.status
		FROM channel_messages cm
		LEFT JOIN users u ON cm.sender_address = u.wallet_address
		WHERE cm.channel_id = ? AND cm.is_pinned = 1
		ORDER BY cm.pinned_at DESC
	`

	rows, err := db.Conn.Query(query, channelID)
	if err != nil {
		return nil, fmt.Errorf("failed to query pinned messages: %v", err)
	}
	defer rows.Close()

	messages := make([]api.ChannelMessage, 0)

	for rows.Next() {
		var msg api.ChannelMessage
		var sender api.User
		var reactionsJSON string
		var mediaUrl, pinnedAt, pinnedBy sql.NullString
		var isEdited, isDeleted, isPinned sql.NullBool
		var username, firstName, lastName, bio, status sql.NullString
		var avatarChunkID sql.NullInt64
		var isOnline sql.NullBool

		err := rows.Scan(
			&msg.ID,
			&msg.ChannelID,
			&sender.Address,
			&msg.Content,
			&msg.Timestamp,
			&isEdited,
			&isDeleted,
			&isPinned,
			&pinnedAt,
			&pinnedBy,
			&mediaUrl,
			&reactionsJSON,
			&msg.ViewCount,
			&username,
			&firstName,
			&lastName,
			&avatarChunkID,
			&bio,
			&isOnline,
			&status,
		)

		if err != nil {
			log.Printf("Error scanning pinned message: %v", err)
			continue
		}

		// Build sender User object
		if username.Valid {
			sender.Username = username.String
		}
		if firstName.Valid {
			sender.FirstName = firstName.String
		}
		if lastName.Valid {
			sender.LastName = lastName.String
		}
		if avatarChunkID.Valid {
			sender.AvatarChunkID = uint64(avatarChunkID.Int64)
		}
		if bio.Valid {
			sender.Bio = bio.String
		}
		if isOnline.Valid {
			sender.Online = isOnline.Bool
		}
		if status.Valid {
			sender.Status = status.String
		}

		// Build display name
		if sender.FirstName != "" && sender.LastName != "" {
			sender.Name = sender.FirstName + " " + sender.LastName
		} else if sender.FirstName != "" {
			sender.Name = sender.FirstName
		} else if sender.Username != "" {
			sender.Name = sender.Username
		} else {
			sender.Name = sender.Address[:8] + "..."
		}

		msg.Sender = sender

		// Deserialize reactions
		if reactionsJSON != "" && reactionsJSON != "null" {
			var reactions []api.Reaction
			if err := json.Unmarshal([]byte(reactionsJSON), &reactions); err == nil {
				msg.Reactions = reactions
			}
		}

		if mediaUrl.Valid {
			msg.MediaUrl = mediaUrl.String
		}
		if isEdited.Valid {
			msg.IsEdited = isEdited.Bool
		}
		if isDeleted.Valid {
			msg.IsDeleted = isDeleted.Bool
		}
		if isPinned.Valid {
			msg.IsPinned = isPinned.Bool
		}
		if pinnedAt.Valid {
			msg.PinnedAt = pinnedAt.String
		}
		if pinnedBy.Valid {
			msg.PinnedBy = pinnedBy.String
		}

		messages = append(messages, msg)
	}

	return messages, nil
}

// AddChannelMessageReaction adds a reaction to a channel message
func (db *DB) AddChannelMessageReaction(channelID, messageID, emoji, userAddr string) error {
	// This is similar to DM reactions - load, modify, save
	// For simplicity, we'll implement a basic version
	query := "SELECT reactions FROM channel_messages WHERE id = ? AND channel_id = ?"

	var reactionsJSON string
	err := db.Conn.QueryRow(query, messageID, channelID).Scan(&reactionsJSON)
	if err != nil {
		return fmt.Errorf("failed to get message: %v", err)
	}

	var reactions []api.Reaction
	if reactionsJSON != "" && reactionsJSON != "null" {
		json.Unmarshal([]byte(reactionsJSON), &reactions)
	}

	// Find or create reaction
	found := false
	for i := range reactions {
		if reactions[i].Emoji == emoji {
			// Check if user already reacted
			alreadyReacted := false
			for _, u := range reactions[i].Users {
				if u.Address == userAddr {
					alreadyReacted = true
					break
				}
			}

			if !alreadyReacted {
				reactions[i].Count++
				reactions[i].Users = append(reactions[i].Users, api.User{Address: userAddr})
			}
			found = true
			break
		}
	}

	if !found {
		reactions = append(reactions, api.Reaction{
			Emoji: emoji,
			Count: 1,
			Users: []api.User{{Address: userAddr}},
		})
	}

	// Save back
	newReactionsJSON, _ := json.Marshal(reactions)
	_, err = db.Conn.Exec(
		"UPDATE channel_messages SET reactions = ? WHERE id = ? AND channel_id = ?",
		string(newReactionsJSON), messageID, channelID,
	)

	return err
}

// RemoveChannelMessageReaction removes a reaction from a channel message
func (db *DB) RemoveChannelMessageReaction(channelID, messageID, emoji, userAddr string) error {
	query := "SELECT reactions FROM channel_messages WHERE id = ? AND channel_id = ?"

	var reactionsJSON string
	err := db.Conn.QueryRow(query, messageID, channelID).Scan(&reactionsJSON)
	if err != nil {
		return fmt.Errorf("failed to get message: %v", err)
	}

	var reactions []api.Reaction
	if reactionsJSON != "" && reactionsJSON != "null" {
		json.Unmarshal([]byte(reactionsJSON), &reactions)
	}

	// Remove user from reaction
	for i := range reactions {
		if reactions[i].Emoji == emoji {
			newUsers := make([]api.User, 0)
			for _, u := range reactions[i].Users {
				if u.Address != userAddr {
					newUsers = append(newUsers, u)
				}
			}
			reactions[i].Users = newUsers
			reactions[i].Count = len(newUsers)

			// Remove reaction if no users left
			if reactions[i].Count == 0 {
				reactions = append(reactions[:i], reactions[i+1:]...)
			}
			break
		}
	}

	// Save back
	newReactionsJSON, _ := json.Marshal(reactions)
	_, err = db.Conn.Exec(
		"UPDATE channel_messages SET reactions = ? WHERE id = ? AND channel_id = ?",
		string(newReactionsJSON), messageID, channelID,
	)

	return err
}
