package database

import (
	"database/sql"
	"fmt"

	"github.com/ZentaChain/zentalk-api/pkg/api"
)

// SaveUser saves or updates a user in the database
func (db *DB) SaveUser(walletAddr, username, zentalkAddr string, bio string, avatarChunkID uint64, avatarKey []byte, publicKey []byte) error {
	query := `
		INSERT INTO users (wallet_address, username, zentalk_address, bio, avatar_chunk_id, avatar_key, public_key, last_online)
		VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(wallet_address) DO UPDATE SET
			username = excluded.username,
			bio = excluded.bio,
			avatar_chunk_id = excluded.avatar_chunk_id,
			avatar_key = excluded.avatar_key,
			public_key = excluded.public_key,
			last_online = CURRENT_TIMESTAMP
	`

	_, err := db.Conn.Exec(query, walletAddr, username, zentalkAddr, bio, avatarChunkID, avatarKey, publicKey)
	if err != nil {
		return fmt.Errorf("failed to save user: %v", err)
	}

	return nil
}

// UpdateProfile updates user's profile information (first_name, last_name, bio, avatar)
func (db *DB) UpdateProfile(walletAddr, firstName, lastName, bio string, avatarChunkID uint64, avatarKey []byte) error {
	query := `
		UPDATE users
		SET first_name = ?, last_name = ?, bio = ?, avatar_chunk_id = ?, avatar_key = ?
		WHERE wallet_address = ?
	`

	_, err := db.Conn.Exec(query, firstName, lastName, bio, avatarChunkID, avatarKey, walletAddr)
	if err != nil {
		return fmt.Errorf("failed to update profile: %v", err)
	}

	return nil
}

// GetUser retrieves a user by wallet address
func (db *DB) GetUser(walletAddr string) (*api.User, error) {
	query := `
		SELECT wallet_address, username, first_name, last_name, zentalk_address, bio, avatar_chunk_id, avatar_key, public_key, created_at, last_online, is_online, status
		FROM users
		WHERE wallet_address = ?
	`

	var user api.User
	var firstName, lastName, bio, status sql.NullString
	var avatarChunkID sql.NullInt64
	var avatarKey []byte
	var publicKey sql.NullString
	var createdAt, lastOnline sql.NullString
	var isOnline sql.NullBool
	var zentalkAddr sql.NullString

	err := db.Conn.QueryRow(query, walletAddr).Scan(
		&user.Address,
		&user.Username,
		&firstName,
		&lastName,
		&zentalkAddr,
		&bio,
		&avatarChunkID,
		&avatarKey,
		&publicKey,
		&createdAt,
		&lastOnline,
		&isOnline,
		&status,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %v", err)
	}

	if firstName.Valid {
		user.FirstName = firstName.String
	}
	if lastName.Valid {
		user.LastName = lastName.String
	}
	if bio.Valid {
		user.Bio = bio.String
	}
	if avatarChunkID.Valid {
		user.AvatarChunkID = uint64(avatarChunkID.Int64)
	}
	user.AvatarKey = avatarKey
	if isOnline.Valid {
		user.Online = isOnline.Bool
	}
	if status.Valid {
		user.Status = status.String
	}

	return &user, nil
}

// UpdateUserOnlineStatus updates the user's online status
func (db *DB) UpdateUserOnlineStatus(walletAddr string, online bool) error {
	query := `
		UPDATE users
		SET is_online = ?, last_online = CURRENT_TIMESTAMP
		WHERE wallet_address = ?
	`

	_, err := db.Conn.Exec(query, online, walletAddr)
	if err != nil {
		return fmt.Errorf("failed to update online status: %v", err)
	}

	return nil
}

// UpdateUserStatus updates the user's status (online, away, busy, offline)
func (db *DB) UpdateUserStatus(walletAddr, status string) error {
	query := `
		UPDATE users
		SET status = ?, last_online = CURRENT_TIMESTAMP
		WHERE wallet_address = ?
	`

	_, err := db.Conn.Exec(query, status, walletAddr)
	if err != nil {
		return fmt.Errorf("failed to update status: %v", err)
	}

	return nil
}

// UpdateLastOnline updates the last_online timestamp for a user
func (db *DB) UpdateLastOnline(walletAddr string) error {
	query := `
		UPDATE users
		SET last_online = CURRENT_TIMESTAMP, is_online = 0
		WHERE wallet_address = ?
	`

	_, err := db.Conn.Exec(query, walletAddr)
	if err != nil {
		return fmt.Errorf("failed to update last_online: %v", err)
	}

	return nil
}

// UpdateUsername updates the user's username
func (db *DB) UpdateUsername(walletAddr, newUsername string) error {
	query := `
		UPDATE users
		SET username = ?
		WHERE wallet_address = ?
	`

	_, err := db.Conn.Exec(query, newUsername, walletAddr)
	if err != nil {
		return fmt.Errorf("failed to update username: %v", err)
	}

	return nil
}

// GetUserByUsername retrieves a user by their username (case-insensitive)
func (db *DB) GetUserByUsername(username string) (*api.User, error) {
	query := `
		SELECT wallet_address, username, first_name, last_name, zentalk_address, bio, avatar_chunk_id, avatar_key, public_key, created_at, last_online, is_online, status
		FROM users
		WHERE LOWER(username) = LOWER(?)
	`

	var user api.User
	var firstName, lastName, bio, status sql.NullString
	var avatarChunkID sql.NullInt64
	var avatarKey []byte
	var publicKey sql.NullString
	var createdAt, lastOnline sql.NullString
	var isOnline sql.NullBool
	var zentalkAddr sql.NullString

	err := db.Conn.QueryRow(query, username).Scan(
		&user.Address,
		&user.Username,
		&firstName,
		&lastName,
		&zentalkAddr,
		&bio,
		&avatarChunkID,
		&avatarKey,
		&publicKey,
		&createdAt,
		&lastOnline,
		&isOnline,
		&status,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user by username: %v", err)
	}

	if firstName.Valid {
		user.FirstName = firstName.String
	}
	if lastName.Valid {
		user.LastName = lastName.String
	}
	if bio.Valid {
		user.Bio = bio.String
	}
	if avatarChunkID.Valid {
		user.AvatarChunkID = uint64(avatarChunkID.Int64)
	}
	user.AvatarKey = avatarKey
	if isOnline.Valid {
		user.Online = isOnline.Bool
	}
	if status.Valid {
		user.Status = status.String
	}

	return &user, nil
}

// IsUsernameAvailable checks if a username is available (not taken by another user)
// Returns true if available, false if taken
func (db *DB) IsUsernameAvailable(username string, excludeWallet string) (bool, error) {
	query := `
		SELECT COUNT(*)
		FROM users
		WHERE LOWER(username) = LOWER(?) AND wallet_address != ?
	`

	var count int
	err := db.Conn.QueryRow(query, username, excludeWallet).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check username availability: %v", err)
	}

	return count == 0, nil
}
