package server

import (
	"github.com/zentalk/protocol/pkg/api"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"github.com/zentalk/protocol/pkg/protocol"
)

// EncryptedOfflineMessage represents an encrypted message for offline storage
// This ensures end-to-end encryption even when recipient is offline
type EncryptedOfflineMessage struct {
	// X3DH Initial Message data (contains keys Alice used)
	SenderAddress       string `json:"sender_address"`
	SenderIdentityKey   string `json:"sender_identity_key"`    // base64
	EphemeralKey        string `json:"ephemeral_key"`          // base64
	UsedSignedPreKeyID  uint32 `json:"used_signed_prekey_id"`
	UsedOneTimePreKeyID uint32 `json:"used_onetime_prekey_id"`

	// Encrypted content (encrypted with X3DH shared secret)
	Ciphertext string `json:"ciphertext"` // base64
	Nonce      string `json:"nonce"`      // base64 (AES-GCM nonce)

	// Metadata
	MessageID string `json:"message_id"`
	Timestamp int64  `json:"timestamp"`
}

// EncryptOfflineMessage encrypts a message for an offline recipient using their prekey bundle
// Returns an encrypted message that can be stored and later decrypted by the recipient
func (s *Server) EncryptOfflineMessage(senderAddr, recipientAddr, content, messageID string, timestamp int64) (*EncryptedOfflineMessage, error) {
	// Get sender's session to access their X3DH identity
	session := s.GetSession(senderAddr)
	if session == nil {
		return nil, errors.New("sender session not found")
	}

	if session.Client == nil {
		return nil, errors.New("sender client not initialized")
	}

	// Get sender's X3DH identity
	senderIdentity := session.Client.GetX3DHIdentity()
	if senderIdentity == nil {
		return nil, errors.New("sender X3DH not initialized")
	}

	// Convert addresses to protocol.Address format
	senderAddress, err := ParseAddress(senderAddr)
	if err != nil {
		return nil, fmt.Errorf("invalid sender address: %w", err)
	}

	recipientAddress, err := ParseAddress(recipientAddr)
	if err != nil {
		return nil, fmt.Errorf("invalid recipient address: %w", err)
	}

	// Get recipient's key bundle from cache (should have been cached during discovery)
	keyBundle, found := session.Client.GetCachedKeyBundle(recipientAddress)
	if !found {
		return nil, errors.New("recipient key bundle not found in cache - user must be discovered first")
	}

	// Perform X3DH as initiator to derive shared secret
	sharedSecret, _, ephemeralPub, initialMsg, err := protocol.X3DHInitiator(
		senderAddress,
		senderIdentity,
		keyBundle,
	)
	if err != nil {
		return nil, fmt.Errorf("X3DH key agreement failed: %w", err)
	}

	log.Printf("🔐 [Offline Encryption] X3DH completed for offline message to %s (OPK: %d)",
		recipientAddr[:8], initialMsg.UsedOneTimePreKeyID)

	// Encrypt message content with AES-GCM using shared secret
	ciphertext, nonce, err := encryptWithAESGCM([]byte(content), sharedSecret)
	if err != nil {
		return nil, fmt.Errorf("AES-GCM encryption failed: %w", err)
	}

	// Create encrypted offline message
	encryptedMsg := &EncryptedOfflineMessage{
		SenderAddress:       senderAddr,
		SenderIdentityKey:   base64.StdEncoding.EncodeToString(senderIdentity.DHPublic[:]),
		EphemeralKey:        base64.StdEncoding.EncodeToString(ephemeralPub[:]),
		UsedSignedPreKeyID:  initialMsg.UsedSignedPreKeyID,
		UsedOneTimePreKeyID: initialMsg.UsedOneTimePreKeyID,
		Ciphertext:          base64.StdEncoding.EncodeToString(ciphertext),
		Nonce:               base64.StdEncoding.EncodeToString(nonce),
		MessageID:           messageID,
		Timestamp:           timestamp,
	}

	log.Printf("✅ [Offline Encryption] Message encrypted for offline storage (sender: %s, recipient: %s)",
		senderAddr[:8], recipientAddr[:8])

	return encryptedMsg, nil
}

// DecryptOfflineMessage decrypts an offline message when the recipient comes online
func (s *Server) DecryptOfflineMessage(recipientAddr string, encryptedMsg *EncryptedOfflineMessage) (string, error) {
	// Get recipient's session
	session := s.GetSession(recipientAddr)
	if session == nil {
		return "", errors.New("recipient session not found")
	}

	if session.Client == nil {
		return "", errors.New("recipient client not initialized")
	}

	// Get recipient's X3DH components
	recipientIdentity := session.Client.GetX3DHIdentity()
	if recipientIdentity == nil {
		return "", errors.New("recipient X3DH not initialized")
	}

	// Decode keys from base64
	var senderIdentityKey, ephemeralKey [32]byte

	senderIDBytes, err := base64.StdEncoding.DecodeString(encryptedMsg.SenderIdentityKey)
	if err != nil {
		return "", fmt.Errorf("failed to decode sender identity key: %w", err)
	}
	copy(senderIdentityKey[:], senderIDBytes)

	ephemBytes, err := base64.StdEncoding.DecodeString(encryptedMsg.EphemeralKey)
	if err != nil {
		return "", fmt.Errorf("failed to decode ephemeral key: %w", err)
	}
	copy(ephemeralKey[:], ephemBytes)

	// Reconstruct InitialMessage
	senderAddress, err := ParseAddress(encryptedMsg.SenderAddress)
	if err != nil {
		return "", fmt.Errorf("invalid sender address: %w", err)
	}

	initialMsg := &protocol.InitialMessage{
		SenderAddress:       senderAddress,
		IdentityKey:         senderIdentityKey,
		EphemeralKey:        ephemeralKey,
		UsedSignedPreKeyID:  encryptedMsg.UsedSignedPreKeyID,
		UsedOneTimePreKeyID: encryptedMsg.UsedOneTimePreKeyID,
	}

	// Get recipient's signed prekey (we need access to the private part)
	signedPreKey := session.Client.GetSignedPreKey()
	if signedPreKey == nil {
		return "", errors.New("recipient signed prekey not found")
	}

	// Get recipient's one-time prekeys map
	oneTimePreKeys := session.Client.GetOneTimePreKeys()

	// Perform X3DH as responder to derive the same shared secret
	sharedSecret, err := protocol.X3DHResponder(
		recipientIdentity,
		signedPreKey,
		oneTimePreKeys,
		initialMsg,
	)
	if err != nil {
		return "", fmt.Errorf("X3DH responder failed: %w", err)
	}

	log.Printf("🔓 [Offline Decryption] X3DH completed for offline message from %s",
		encryptedMsg.SenderAddress[:8])

	// Decrypt ciphertext with AES-GCM using shared secret
	ciphertextBytes, err := base64.StdEncoding.DecodeString(encryptedMsg.Ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	nonceBytes, err := base64.StdEncoding.DecodeString(encryptedMsg.Nonce)
	if err != nil {
		return "", fmt.Errorf("failed to decode nonce: %w", err)
	}

	plaintext, err := decryptWithAESGCM(ciphertextBytes, nonceBytes, sharedSecret)
	if err != nil {
		return "", fmt.Errorf("AES-GCM decryption failed: %w", err)
	}

	log.Printf("✅ [Offline Decryption] Message decrypted successfully (from: %s)",
		encryptedMsg.SenderAddress[:8])

	return string(plaintext), nil
}

// encryptWithAESGCM encrypts data using AES-256-GCM
func encryptWithAESGCM(plaintext, key []byte) (ciphertext, nonce []byte, err error) {
	// Create AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}

	// Generate random nonce
	nonce = make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}

	// Encrypt
	ciphertext = gcm.Seal(nil, nonce, plaintext, nil)

	return ciphertext, nonce, nil
}

// decryptWithAESGCM decrypts data using AES-256-GCM
func decryptWithAESGCM(ciphertext, nonce, key []byte) (plaintext []byte, err error) {
	// Create AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	// Decrypt
	plaintext, err = gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}

// ParseAddress converts hex string address to protocol.Address (20 bytes)
func ParseAddress(hexAddr string) (protocol.Address, error) {
	// Normalize: remove 0x prefix if present, convert to lowercase
	normalized := api.NormalizeAddress(hexAddr)

	var addr protocol.Address
	if len(normalized) != 40 {
		return addr, fmt.Errorf("invalid address length: %d (expected 40)", len(normalized))
	}

	// Convert hex string to bytes
	for i := 0; i < 20; i++ {
		var b byte
		_, err := fmt.Sscanf(normalized[i*2:i*2+2], "%02x", &b)
		if err != nil {
			return addr, fmt.Errorf("invalid hex character: %w", err)
		}
		addr[i] = b
	}

	return addr, nil
}

// storeEncryptedPendingMessage stores an encrypted pending message
func (s *Server) storeEncryptedPendingMessage(recipientAddr string, encryptedMsg *EncryptedOfflineMessage) {
	s.PendingLock.Lock()
	defer s.PendingLock.Unlock()

	// Serialize encrypted message to JSON
	jsonData, err := json.Marshal(encryptedMsg)
	if err != nil {
		log.Printf("❌ Failed to marshal encrypted message: %v", err)
		return
	}

	// Store as a regular PendingMessage with encrypted JSON as content
	msg := PendingMessage{
		From:      encryptedMsg.SenderAddress,
		To:        recipientAddr,
		Content:   string(jsonData), // Store encrypted message as JSON string
		Timestamp: encryptedMsg.Timestamp,
		MessageID: encryptedMsg.MessageID,
	}

	s.PendingMessages[recipientAddr] = append(s.PendingMessages[recipientAddr], msg)
	log.Printf("📦 Stored ENCRYPTED pending message for offline user %s (from: %s)", recipientAddr[:8], encryptedMsg.SenderAddress[:8])
}
