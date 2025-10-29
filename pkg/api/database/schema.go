package database

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

// DB handles persistent storage of encrypted messages
type DB struct {
	Conn *sql.DB
}

// New creates a new database instance
func New(dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %v", err)
	}

	// Create tables if they don't exist
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		wallet_address TEXT PRIMARY KEY,
		username TEXT NOT NULL UNIQUE COLLATE NOCASE,
		first_name TEXT,
		last_name TEXT,
		zentalk_address TEXT UNIQUE,
		bio TEXT,
		avatar_chunk_id INTEGER DEFAULT 0,
		avatar_key BLOB,
		public_key BLOB,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_online DATETIME,
		is_online BOOLEAN DEFAULT 0,
		status TEXT DEFAULT 'online'
	);

	CREATE TABLE IF NOT EXISTS contacts (
		user_address TEXT NOT NULL,
		contact_address TEXT NOT NULL,
		username TEXT,
		is_blocked BOOLEAN DEFAULT 0,
		is_muted BOOLEAN DEFAULT 0,
		is_favorite BOOLEAN DEFAULT 0,
		added_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (user_address, contact_address),
		FOREIGN KEY (user_address) REFERENCES users(wallet_address) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS starred_messages (
		user_address TEXT NOT NULL,
		message_id TEXT NOT NULL,
		peer_address TEXT NOT NULL,
		starred_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (user_address, message_id),
		FOREIGN KEY (user_address) REFERENCES users(wallet_address) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS media_files (
		id TEXT PRIMARY KEY,
		user_address TEXT NOT NULL,
		file_name TEXT NOT NULL,
		mime_type TEXT NOT NULL,
		file_size INTEGER,
		mesh_chunk_id INTEGER,
		encryption_key BLOB,
		uploaded_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (user_address) REFERENCES users(wallet_address) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS messages (
		id TEXT PRIMARY KEY,
		user_address TEXT NOT NULL,
		peer_address TEXT NOT NULL,
		content TEXT NOT NULL,
		timestamp TEXT NOT NULL,
		sender TEXT NOT NULL,
		media_url TEXT,
		is_edited BOOLEAN DEFAULT 0,
		is_deleted BOOLEAN DEFAULT 0,
		is_read BOOLEAN DEFAULT 0,
		reactions TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(user_address, peer_address, id),
		FOREIGN KEY (user_address) REFERENCES users(wallet_address) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_user_peer ON messages(user_address, peer_address);
	CREATE INDEX IF NOT EXISTS idx_created_at ON messages(created_at);
	CREATE INDEX IF NOT EXISTS idx_contacts_user ON contacts(user_address);
	CREATE INDEX IF NOT EXISTS idx_starred_user ON starred_messages(user_address);
	CREATE INDEX IF NOT EXISTS idx_media_user ON media_files(user_address);

	-- Channels (broadcast channels like Telegram)
	CREATE TABLE IF NOT EXISTS channels (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE COLLATE NOCASE,
		description TEXT,
		avatar_chunk_id INTEGER DEFAULT 0,
		avatar_key BLOB,
		owner_address TEXT NOT NULL,
		type TEXT NOT NULL CHECK(type IN ('public', 'private')),
		is_verified BOOLEAN DEFAULT 0,
		subscriber_count INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(owner_address) REFERENCES users(wallet_address) ON DELETE CASCADE
	);

	-- Channel members (subscribers)
	CREATE TABLE IF NOT EXISTS channel_members (
		channel_id TEXT NOT NULL,
		user_address TEXT NOT NULL,
		role TEXT NOT NULL CHECK(role IN ('owner', 'admin', 'subscriber')),
		joined_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		is_muted BOOLEAN DEFAULT 0,
		last_read_message_id TEXT,
		PRIMARY KEY (channel_id, user_address),
		FOREIGN KEY(channel_id) REFERENCES channels(id) ON DELETE CASCADE,
		FOREIGN KEY(user_address) REFERENCES users(wallet_address) ON DELETE CASCADE
	);

	-- Channel messages (only owner/admin can send)
	CREATE TABLE IF NOT EXISTS channel_messages (
		id TEXT PRIMARY KEY,
		channel_id TEXT NOT NULL,
		sender_address TEXT NOT NULL,
		content TEXT NOT NULL,
		timestamp TEXT NOT NULL,
		is_edited BOOLEAN DEFAULT 0,
		is_deleted BOOLEAN DEFAULT 0,
		is_pinned BOOLEAN DEFAULT 0,
		pinned_at DATETIME,
		pinned_by TEXT,
		media_url TEXT,
		reactions TEXT,
		view_count INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(channel_id) REFERENCES channels(id) ON DELETE CASCADE,
		FOREIGN KEY(sender_address) REFERENCES users(wallet_address) ON DELETE SET NULL
	);

	-- Channel invites (for private channels)
	CREATE TABLE IF NOT EXISTS channel_invites (
		id TEXT PRIMARY KEY,
		channel_id TEXT NOT NULL,
		invited_by TEXT NOT NULL,
		invite_code TEXT UNIQUE NOT NULL,
		max_uses INTEGER DEFAULT 0,
		uses INTEGER DEFAULT 0,
		expires_at DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(channel_id) REFERENCES channels(id) ON DELETE CASCADE,
		FOREIGN KEY(invited_by) REFERENCES users(wallet_address) ON DELETE CASCADE
	);

	-- Indexes for channels
	CREATE INDEX IF NOT EXISTS idx_channels_name ON channels(name COLLATE NOCASE);
	CREATE INDEX IF NOT EXISTS idx_channels_owner ON channels(owner_address);
	CREATE INDEX IF NOT EXISTS idx_channels_type ON channels(type);
	CREATE INDEX IF NOT EXISTS idx_channel_members_user ON channel_members(user_address);
	CREATE INDEX IF NOT EXISTS idx_channel_members_role ON channel_members(channel_id, role);
	CREATE INDEX IF NOT EXISTS idx_channel_messages_channel ON channel_messages(channel_id, created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_channel_messages_pinned ON channel_messages(channel_id, is_pinned);
	CREATE INDEX IF NOT EXISTS idx_channel_invites_code ON channel_invites(invite_code);
	`

	if _, err := conn.Exec(schema); err != nil {
		return nil, fmt.Errorf("failed to create schema: %v", err)
	}

	log.Printf("✅ Message database initialized at %s", dbPath)

	return &DB{Conn: conn}, nil
}

// Close closes the database connection
func (db *DB) Close() error {
	return db.Conn.Close()
}
