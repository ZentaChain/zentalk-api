package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// MeshStorageClient provides HTTP client for MeshStorage API
type MeshStorageClient struct {
	apiURL     string
	httpClient *http.Client
}

// NewMeshStorageClient creates a new MeshStorage HTTP client
func NewMeshStorageClient(apiURL string) *MeshStorageClient {
	return &MeshStorageClient{
		apiURL: apiURL,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// UploadRequest represents MeshStorage upload request
type MeshUploadRequest struct {
	UserAddr  string `json:"userAddr"`
	ChunkID   int    `json:"chunkID"`
	Data      string `json:"data"`      // base64 encoded
	Encrypted bool   `json:"encrypted"` // true if already encrypted client-side
}

// UploadResponse represents MeshStorage upload response
type MeshUploadResponse struct {
	Success        bool     `json:"success"`
	UserAddr       string   `json:"userAddr"`
	ChunkID        int      `json:"chunkID"`
	OriginalSize   int      `json:"originalSizeBytes"`
	EncryptedSize  int      `json:"encryptedSizeBytes"`
	ShardCount     int      `json:"shardCount"`
	StorageNodes   []string `json:"storageNodes"`
	Encrypted      bool     `json:"encrypted"`
	EncryptionInfo string   `json:"encryptionInfo"`
}

// DownloadResponse represents MeshStorage download response
type MeshDownloadResponse struct {
	Success        bool   `json:"success"`
	Data           string `json:"data"` // base64 encoded
	OriginalSize   int    `json:"originalSizeBytes"`
	EncryptedSize  int    `json:"encryptedSizeBytes"`
	Encrypted      bool   `json:"encrypted"`
	EncryptionInfo string `json:"encryptionInfo"`
}

// Upload uploads data to MeshStorage and returns chunk ID and encryption key
// The data will be encrypted automatically by MeshStorage using wallet-derived key
func (c *MeshStorageClient) Upload(userAddr string, data []byte) (chunkID uint64, encryptionKey []byte, err error) {
	// Generate chunk ID from timestamp (simple approach)
	chunkID = uint64(time.Now().UnixNano())

	// Base64 encode data
	base64Data := base64.StdEncoding.EncodeToString(data)

	// Ensure address has 0x prefix (MeshStorage requires it)
	formattedAddr := userAddr
	if len(userAddr) > 0 && userAddr[0:2] != "0x" {
		formattedAddr = "0x" + userAddr
	}

	req := MeshUploadRequest{
		UserAddr:  formattedAddr,
		ChunkID:   int(chunkID),
		Data:      base64Data,
		Encrypted: false, // Let MeshStorage encrypt it
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Call MeshStorage API
	resp, err := c.httpClient.Post(
		fmt.Sprintf("%s/api/v1/storage/upload", c.apiURL),
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return 0, nil, fmt.Errorf("upload request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, nil, fmt.Errorf("upload failed with status %d: %s", resp.StatusCode, string(body))
	}

	var uploadResp MeshUploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&uploadResp); err != nil {
		return 0, nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !uploadResp.Success {
		return 0, nil, fmt.Errorf("upload failed")
	}

	// MeshStorage encrypts with wallet-derived key, so we return a derived key indicator
	// The actual encryption key is managed by MeshStorage internally
	// For avatar/media, the encryption key should be returned by MeshStorage in future
	// For now, we use empty key to indicate MeshStorage manages it
	encryptionKey = []byte{} // TODO: MeshStorage should return the encryption key

	return chunkID, encryptionKey, nil
}

// Download downloads and decrypts data from MeshStorage
func (c *MeshStorageClient) Download(userAddr string, chunkID uint64) ([]byte, error) {
	// Ensure address has 0x prefix (MeshStorage requires it)
	formattedAddr := userAddr
	if len(userAddr) > 0 && userAddr[0:2] != "0x" {
		formattedAddr = "0x" + userAddr
	}

	resp, err := c.httpClient.Get(
		fmt.Sprintf("%s/api/v1/storage/download/%s/%d", c.apiURL, formattedAddr, chunkID),
	)
	if err != nil {
		return nil, fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("download failed with status %d: %s", resp.StatusCode, string(body))
	}

	var downloadResp MeshDownloadResponse
	if err := json.NewDecoder(resp.Body).Decode(&downloadResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !downloadResp.Success {
		return nil, fmt.Errorf("download failed")
	}

	// Decode base64 data
	data, err := base64.StdEncoding.DecodeString(downloadResp.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode data: %w", err)
	}

	return data, nil
}
