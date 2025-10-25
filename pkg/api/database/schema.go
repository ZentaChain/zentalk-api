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
