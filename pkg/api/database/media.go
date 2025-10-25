package database

import (
	"database/sql"
	"fmt"
)

// SaveMediaFile saves media file metadata to the database
func (db *DB) SaveMediaFile(id, userAddr, fileName, mimeType string, fileSize int64, meshChunkID uint64, encryptionKey []byte) error {
	query := `
		INSERT INTO media_files (id, user_address, file_name, mime_type, file_size, mesh_chunk_id, encryption_key)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`

	_, err := db.Conn.Exec(query, id, userAddr, fileName, mimeType, fileSize, meshChunkID, encryptionKey)
	if err != nil {
		return fmt.Errorf("failed to save media file: %v", err)
	}

	return nil
}

// GetMediaFile retrieves media file metadata by ID
func (db *DB) GetMediaFile(mediaID string) (map[string]interface{}, error) {
	query := `
		SELECT id, user_address, file_name, mime_type, file_size, mesh_chunk_id, encryption_key, uploaded_at
		FROM media_files
		WHERE id = ?
	`

	var id, userAddr, fileName, mimeType string
	var fileSize int64
	var meshChunkID uint64
	var encryptionKey []byte
	var uploadedAt string

	err := db.Conn.QueryRow(query, mediaID).Scan(
		&id, &userAddr, &fileName, &mimeType, &fileSize, &meshChunkID, &encryptionKey, &uploadedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("media file not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get media file: %v", err)
	}

	mediaFile := map[string]interface{}{
		"id":             id,
		"user_address":   userAddr,
		"file_name":      fileName,
		"mime_type":      mimeType,
		"file_size":      fileSize,
		"mesh_chunk_id":  meshChunkID,
		"encryption_key": encryptionKey,
		"uploaded_at":    uploadedAt,
	}

	return mediaFile, nil
}

// DeleteMediaFile deletes a media file entry
func (db *DB) DeleteMediaFile(mediaID string) error {
	query := `
		DELETE FROM media_files
		WHERE id = ?
	`

	_, err := db.Conn.Exec(query, mediaID)
	if err != nil {
		return fmt.Errorf("failed to delete media file: %v", err)
	}

	return nil
}
