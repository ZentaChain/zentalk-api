package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/ZentaChain/zentalk-api/pkg/api"
)

// SaveMessage saves a message to the database
func (db *DB) SaveMessage(userAddr, peerAddr string, msg api.Message) error {
	// Serialize reactions to JSON
	reactionsJSON, err := json.Marshal(msg.Reactions)
	if err != nil {
		return fmt.Errorf("failed to serialize reactions: %v", err)
	}

	// Determine sender string
	senderStr := ""
	switch s := msg.Sender.(type) {
	case string:
		senderStr = s
	case api.User:
		userData, _ := json.Marshal(s)
		senderStr = string(userData)
	case *api.User:
		userData, _ := json.Marshal(s)
		senderStr = string(userData)
	}

	// Determine is_read value: default to 0 for new messages, preserve existing for updates
	isRead := 0
	if senderStr == "You" {
		isRead = 1 // Messages sent by "You" are always read
	}

	query := `
		INSERT INTO messages
		(id, user_address, peer_address, content, timestamp, sender, media_url, is_edited, is_read, reactions)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_address, peer_address, id) DO UPDATE SET
			content = excluded.content,
			is_edited = excluded.is_edited,
			reactions = excluded.reactions
	`

	_, err = db.Conn.Exec(query,
		msg.ID,
		userAddr,
		peerAddr,
		msg.Content,
		msg.Timestamp,
		senderStr,
		msg.MediaUrl,
		msg.IsEdited,
		isRead,
		string(reactionsJSON),
	)

	if err != nil {
		return fmt.Errorf("failed to save message: %v", err)
	}

	return nil
}

// LoadMessages loads all messages for a user-peer pair
func (db *DB) LoadMessages(userAddr, peerAddr string) ([]api.Message, error) {
	query := `
		SELECT id, content, timestamp, sender, media_url, is_edited, is_read, reactions
		FROM messages
		WHERE user_address = ? AND peer_address = ?
		ORDER BY created_at ASC
	`

	rows, err := db.Conn.Query(query, userAddr, peerAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to query messages: %v", err)
	}
	defer rows.Close()

	messages := make([]api.Message, 0)

	for rows.Next() {
		var msg api.Message
		var senderStr string
		var reactionsJSON string
		var mediaUrl sql.NullString
		var isEdited sql.NullBool
		var isRead sql.NullBool

		err := rows.Scan(
			&msg.ID,
			&msg.Content,
			&msg.Timestamp,
			&senderStr,
			&mediaUrl,
			&isEdited,
			&isRead,
			&reactionsJSON,
		)
		if err != nil {
			log.Printf("Error scanning message: %v", err)
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

		// Set unread status: message is unread if it's not read AND sender is not "You"
		if isRead.Valid && !isRead.Bool && senderStr != "You" {
			msg.Unread = true
			log.Printf("📥 [LoadMessages] Message %s: is_read=%v, sender=%s → Unread=true", msg.ID, isRead.Bool, senderStr)
		} else {
			msg.Unread = false
			if isRead.Valid {
				log.Printf("📥 [LoadMessages] Message %s: is_read=%v, sender=%s → Unread=false", msg.ID, isRead.Bool, senderStr)
			}
		}

		messages = append(messages, msg)
	}

	log.Printf("✅ [LoadMessages] Loaded %d messages for %s -> %s", len(messages), userAddr, peerAddr)
	return messages, nil
}

// LoadAllChats loads all chats for a user
func (db *DB) LoadAllChats(userAddr string) (map[string][]api.Message, error) {
	query := `
		SELECT DISTINCT peer_address
		FROM messages
		WHERE user_address = ?
	`

	rows, err := db.Conn.Query(query, userAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to query chats: %v", err)
	}
	defer rows.Close()

	chats := make(map[string][]api.Message)

	for rows.Next() {
		var peerAddr string
		if err := rows.Scan(&peerAddr); err != nil {
			log.Printf("Error scanning peer address: %v", err)
			continue
		}

		messages, err := db.LoadMessages(userAddr, peerAddr)
		if err != nil {
			log.Printf("Error loading messages for %s -> %s: %v", userAddr, peerAddr, err)
			continue
		}

		chats[peerAddr] = messages
	}

	return chats, nil
}

// DeleteMessage deletes a message from the database
func (db *DB) DeleteMessage(userAddr, peerAddr, messageID string) error {
	query := `
		DELETE FROM messages
		WHERE user_address = ? AND peer_address = ? AND id = ?
	`

	_, err := db.Conn.Exec(query, userAddr, peerAddr, messageID)
	if err != nil {
		return fmt.Errorf("failed to delete message: %v", err)
	}

	return nil
}

// DeleteChat deletes all messages in a chat (between user and peer)
func (db *DB) DeleteChat(userAddr, peerAddr string) error {
	query := `
		DELETE FROM messages
		WHERE user_address = ? AND peer_address = ?
	`

	result, err := db.Conn.Exec(query, userAddr, peerAddr)
	if err != nil {
		return fmt.Errorf("failed to delete chat: %v", err)
	}

	rowsAffected, _ := result.RowsAffected()
	log.Printf("🗑️  Deleted chat for %s <-> %s (%d messages)", userAddr, peerAddr, rowsAffected)

	return nil
}

// CleanupOldMessages deletes messages older than the specified duration
func (db *DB) CleanupOldMessages(olderThan time.Duration) error {
	cutoff := time.Now().Add(-olderThan)

	query := `
		DELETE FROM messages
		WHERE created_at < ?
	`

	result, err := db.Conn.Exec(query, cutoff)
	if err != nil {
		return fmt.Errorf("failed to cleanup old messages: %v", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		log.Printf("🧹 Cleaned up %d old messages", rowsAffected)
	}

	return nil
}

// DeleteUserData deletes all data for a user (for account deletion)
func (db *DB) DeleteUserData(userAddr string) error {
	// Delete messages
	query := `
		DELETE FROM messages
		WHERE user_address = ?
	`

	result, err := db.Conn.Exec(query, userAddr)
	if err != nil {
		return fmt.Errorf("failed to delete user messages: %v", err)
	}

	messagesDeleted, _ := result.RowsAffected()
	log.Printf("🗑️  Deleted %d messages for user %s", messagesDeleted, userAddr)

	// Delete user record
	userQuery := `
		DELETE FROM users
		WHERE wallet_address = ?
	`

	_, err = db.Conn.Exec(userQuery, userAddr)
	if err != nil {
		return fmt.Errorf("failed to delete user record: %v", err)
	}

	log.Printf("🗑️  Deleted user record for %s", userAddr)

	return nil
}

// MarkMessageAsRead marks a message as read in the database
func (db *DB) MarkMessageAsRead(userAddr, peerAddr, messageID string) error {
	query := `
		UPDATE messages
		SET is_read = 1
		WHERE user_address = ? AND peer_address = ? AND id = ?
	`

	_, err := db.Conn.Exec(query, userAddr, peerAddr, messageID)
	if err != nil {
		return fmt.Errorf("failed to mark message as read: %v", err)
	}

	return nil
}
