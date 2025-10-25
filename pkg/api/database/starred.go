package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"

	"github.com/ZentaChain/zentalk-api/pkg/api"
)

// StarMessage adds a message to starred messages
func (db *DB) StarMessage(userAddr, messageID, peerAddr string) error {
	query := `
		INSERT OR IGNORE INTO starred_messages (user_address, message_id, peer_address)
		VALUES (?, ?, ?)
	`

	_, err := db.Conn.Exec(query, userAddr, messageID, peerAddr)
	if err != nil {
		return fmt.Errorf("failed to star message: %v", err)
	}

	return nil
}

// UnstarMessage removes a message from starred messages
func (db *DB) UnstarMessage(userAddr, messageID string) error {
	query := `
		DELETE FROM starred_messages
		WHERE user_address = ? AND message_id = ?
	`

	_, err := db.Conn.Exec(query, userAddr, messageID)
	if err != nil {
		return fmt.Errorf("failed to unstar message: %v", err)
	}

	return nil
}

// DeleteStarredMessage removes a message from starred messages (cleanup on delete)
func (db *DB) DeleteStarredMessage(userAddr, messageID string) error {
	query := `
		DELETE FROM starred_messages
		WHERE user_address = ? AND message_id = ?
	`

	result, err := db.Conn.Exec(query, userAddr, messageID)
	if err != nil {
		return fmt.Errorf("failed to delete starred message: %v", err)
	}

	rows, _ := result.RowsAffected()
	if rows > 0 {
		log.Printf("🗑️  Removed message %s from starred", messageID)
	}

	return nil
}

// DeleteStarredMessagesForChat removes all starred messages for a specific chat
func (db *DB) DeleteStarredMessagesForChat(userAddr, peerAddr string) error {
	query := `
		DELETE FROM starred_messages
		WHERE user_address = ? AND peer_address = ?
	`

	result, err := db.Conn.Exec(query, userAddr, peerAddr)
	if err != nil {
		return fmt.Errorf("failed to delete starred messages for chat: %v", err)
	}

	rows, _ := result.RowsAffected()
	if rows > 0 {
		log.Printf("🗑️  Removed %d starred messages from chat %s", rows, peerAddr)
	}

	return nil
}

// GetStarredMessages retrieves all starred messages for a user
func (db *DB) GetStarredMessages(userAddr string) ([]api.Message, error) {
	query := `
		SELECT m.id, m.content, m.timestamp, m.sender, m.media_url, m.is_edited, m.reactions
		FROM messages m
		INNER JOIN starred_messages s ON m.id = s.message_id AND m.user_address = s.user_address
		WHERE s.user_address = ?
		ORDER BY s.starred_at DESC
	`

	rows, err := db.Conn.Query(query, userAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to query starred messages: %v", err)
	}
	defer rows.Close()

	messages := make([]api.Message, 0)

	for rows.Next() {
		var msg api.Message
		var senderStr string
		var reactionsJSON string
		var mediaUrl sql.NullString
		var isEdited sql.NullBool

		err := rows.Scan(
			&msg.ID,
			&msg.Content,
			&msg.Timestamp,
			&senderStr,
			&mediaUrl,
			&isEdited,
			&reactionsJSON,
		)
		if err != nil {
			log.Printf("Error scanning starred message: %v", err)
			continue
		}

		// Deserialize sender
		if senderStr == "You" {
			msg.Sender = "You"
		} else {
			var user api.User
			if err := json.Unmarshal([]byte(senderStr), &user); err == nil {
				msg.Sender = user
			} else {
				msg.Sender = senderStr
			}
		}

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

		messages = append(messages, msg)
	}

	return messages, nil
}

// IsMessageStarred checks if a message is starred
func (db *DB) IsMessageStarred(userAddr, messageID string) (bool, error) {
	query := `
		SELECT COUNT(*) FROM starred_messages
		WHERE user_address = ? AND message_id = ?
	`

	var count int
	err := db.Conn.QueryRow(query, userAddr, messageID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check if message starred: %v", err)
	}

	return count > 0, nil
}
