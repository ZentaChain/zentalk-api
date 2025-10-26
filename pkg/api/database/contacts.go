package database

import (
	"database/sql"
	"fmt"
	"log"
)

// AddContact adds a contact for a user
func (db *DB) AddContact(userAddr, contactAddr, username string) error {
	query := `
		INSERT OR IGNORE INTO contacts (user_address, contact_address, username)
		VALUES (?, ?, ?)
	`

	_, err := db.Conn.Exec(query, userAddr, contactAddr, username)
	if err != nil {
		return fmt.Errorf("failed to add contact: %v", err)
	}

	return nil
}

// BlockContact blocks a contact
func (db *DB) BlockContact(userAddr, contactAddr string) error {
	query := `
		INSERT OR REPLACE INTO contacts (user_address, contact_address, username, is_blocked)
		VALUES (?, ?, '', 1)
	`

	_, err := db.Conn.Exec(query, userAddr, contactAddr)
	if err != nil {
		return fmt.Errorf("failed to block contact: %v", err)
	}

	return nil
}

// UnblockContact unblocks a contact
func (db *DB) UnblockContact(userAddr, contactAddr string) error {
	query := `
		UPDATE contacts
		SET is_blocked = 0
		WHERE user_address = ? AND contact_address = ?
	`

	_, err := db.Conn.Exec(query, userAddr, contactAddr)
	if err != nil {
		return fmt.Errorf("failed to unblock contact: %v", err)
	}

	return nil
}

// FavoriteContact marks a contact as favorite
func (db *DB) FavoriteContact(userAddr, contactAddr string) error {
	query := `
		UPDATE contacts
		SET is_favorite = 1
		WHERE user_address = ? AND contact_address = ?
	`

	_, err := db.Conn.Exec(query, userAddr, contactAddr)
	if err != nil {
		return fmt.Errorf("failed to favorite contact: %v", err)
	}

	return nil
}

// UnfavoriteContact removes favorite status
func (db *DB) UnfavoriteContact(userAddr, contactAddr string) error {
	query := `
		UPDATE contacts
		SET is_favorite = 0
		WHERE user_address = ? AND contact_address = ?
	`

	_, err := db.Conn.Exec(query, userAddr, contactAddr)
	if err != nil {
		return fmt.Errorf("failed to unfavorite contact: %v", err)
	}

	return nil
}

// IsContactBlocked checks if a contact is blocked
func (db *DB) IsContactBlocked(userAddr, contactAddr string) (bool, error) {
	query := `
		SELECT is_blocked
		FROM contacts
		WHERE user_address = ? AND contact_address = ?
	`

	var isBlocked bool
	err := db.Conn.QueryRow(query, userAddr, contactAddr).Scan(&isBlocked)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check if blocked: %v", err)
	}

	return isBlocked, nil
}

// IsBlocked is an alias for IsContactBlocked
func (db *DB) IsBlocked(userAddr, contactAddr string) (bool, error) {
	return db.IsContactBlocked(userAddr, contactAddr)
}

// GetBlockedContacts retrieves all blocked contacts for a user
func (db *DB) GetBlockedContacts(userAddr string) ([]string, error) {
	query := `
		SELECT contact_address
		FROM contacts
		WHERE user_address = ? AND is_blocked = 1
	`

	rows, err := db.Conn.Query(query, userAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to get blocked contacts: %v", err)
	}
	defer rows.Close()

	var blockedContacts []string
	for rows.Next() {
		var contactAddr string
		if err := rows.Scan(&contactAddr); err != nil {
			log.Printf("Error scanning blocked contact: %v", err)
			continue
		}
		blockedContacts = append(blockedContacts, contactAddr)
	}

	return blockedContacts, nil
}

// MuteContact mutes a contact
func (db *DB) MuteContact(userAddr, contactAddr string) error {
	query := `
		INSERT INTO contacts (user_address, contact_address, username, is_muted)
		VALUES (?, ?, '', 1)
		ON CONFLICT(user_address, contact_address) DO UPDATE SET is_muted = 1
	`

	_, err := db.Conn.Exec(query, userAddr, contactAddr)
	if err != nil {
		return fmt.Errorf("failed to mute contact: %v", err)
	}

	return nil
}

// UnmuteContact unmutes a contact
func (db *DB) UnmuteContact(userAddr, contactAddr string) error {
	query := `
		UPDATE contacts
		SET is_muted = 0
		WHERE user_address = ? AND contact_address = ?
	`

	_, err := db.Conn.Exec(query, userAddr, contactAddr)
	if err != nil {
		return fmt.Errorf("failed to unmute contact: %v", err)
	}

	return nil
}

// IsContactMuted checks if a contact is muted
func (db *DB) IsContactMuted(userAddr, contactAddr string) (bool, error) {
	query := `
		SELECT is_muted
		FROM contacts
		WHERE user_address = ? AND contact_address = ?
	`

	var isMuted bool
	err := db.Conn.QueryRow(query, userAddr, contactAddr).Scan(&isMuted)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check if muted: %v", err)
	}

	return isMuted, nil
}

// GetMutedContacts retrieves all muted contacts for a user
func (db *DB) GetMutedContacts(userAddr string) ([]string, error) {
	query := `
		SELECT contact_address
		FROM contacts
		WHERE user_address = ? AND is_muted = 1
	`

	rows, err := db.Conn.Query(query, userAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to get muted contacts: %v", err)
	}
	defer rows.Close()

	var mutedContacts []string
	for rows.Next() {
		var contactAddr string
		if err := rows.Scan(&contactAddr); err != nil {
			log.Printf("Error scanning muted contact: %v", err)
			continue
		}
		mutedContacts = append(mutedContacts, contactAddr)
	}

	return mutedContacts, nil
}
