package api

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ZentaChain/zentalk-api/pkg/protocol"
)

// Request types

type InitializeRequest struct {
	WalletAddress string `json:"wallet_address"`        // Ethereum wallet address (0x...)
	Username      string `json:"username,omitempty"`    // Display username (optional for returning users)
	FirstName     string `json:"first_name,omitempty"`  // First name (for new users)
	LastName      string `json:"last_name,omitempty"`   // Last name (for new users)
	Signature     string `json:"signature,omitempty"`   // Optional signature for wallet verification
	Message       string `json:"message,omitempty"`     // Message that was signed
}

type SendMessageRequest struct {
	RecipientAddress string `json:"recipient_address"` // Hex-encoded address
	Content          string `json:"content"`
}

type GetMessagesRequest struct {
	ChatID string `json:"chat_id"`
	Limit  int    `json:"limit"`
}

// Response types

type InitializeResponse struct {
	Success bool   `json:"success"`
	Address string `json:"address"` // Hex-encoded address
	Message string `json:"message"`
}

type SendMessageResponse struct {
	Success   bool   `json:"success"`
	MessageID string `json:"message_id"`
	Timestamp int64  `json:"timestamp"`
	Message   string `json:"message"`
}

type ErrorResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
}

// Data types

type User struct {
	Name          string `json:"name"`
	Username      string `json:"username"`
	FirstName     string `json:"first_name,omitempty"`
	LastName      string `json:"last_name,omitempty"`
	Avatar        string `json:"avatar,omitempty"`          // DEPRECATED: Legacy field for backwards compatibility (not used)
	AvatarChunkID uint64 `json:"avatar_chunk_id,omitempty"` // MeshStorage chunk ID
	AvatarKey     []byte `json:"avatar_key,omitempty"`      // Encryption key for avatar
	Bio           string `json:"bio"` // Always send bio field, even if empty
	Online        bool   `json:"online"`
	Status        string `json:"status"` // "online", "away", "busy", "offline" - always send even if empty
	Address       string `json:"address"` // Hex-encoded
}

type Reaction struct {
	Emoji      string   `json:"emoji"`       // Emoji reaction (e.g., "👍", "❤️")
	Count      int      `json:"count"`       // Number of users who reacted
	Users      []User   `json:"users"`       // Users who reacted
	HasReacted bool     `json:"hasReacted"`  // Whether current user reacted
}

type Message struct {
	ID        string      `json:"id"`
	Content   string      `json:"content"`
	Timestamp string      `json:"timestamp"`
	Sender    interface{} `json:"sender"` // User object or "You"
	Unread    bool        `json:"unread,omitempty"`
	Reactions []Reaction  `json:"reactions,omitempty"` // Message reactions
	MediaUrl  string      `json:"mediaUrl,omitempty"`  // URL for media messages
	IsEdited  bool        `json:"isEdited,omitempty"`  // Whether message was edited
	IsDeleted bool        `json:"isDeleted,omitempty"` // Whether message was deleted
}

type Chat struct {
	ID       string    `json:"id"`
	Sender   User      `json:"sender"`
	Messages []Message `json:"messages"`
}

type ChatsResponse struct {
	Success bool   `json:"success"`
	Chats   []Chat `json:"chats"`
}

// WebSocket message types

type WSMessage struct {
	Type    string      `json:"type"` // "message", "ack", "typing", "online"
	Payload interface{} `json:"payload"`
}

type WSIncomingMessage struct {
	ID        string `json:"id"`
	From      string `json:"from"`      // Hex-encoded address
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
}

type WSTypingIndicator struct {
	From   string `json:"from"`
	Typing bool   `json:"typing"`
}

type WSOnlineStatus struct {
	Address string `json:"address"`
	Online  bool   `json:"online"`
}

type WSReadReceipt struct {
	From       string `json:"from"`        // Hex-encoded address who read the message
	MessageID  string `json:"message_id"`  // Message that was read
	ReadStatus string `json:"read_status"` // "delivered", "read", "seen"
	Timestamp  int64  `json:"timestamp"`   // When it was read
}

// Contact discovery

type DiscoverContactRequest struct {
	Address string `json:"address"` // Hex-encoded
}

type DiscoverContactResponse struct {
	Success bool   `json:"success"`
	User    *User  `json:"user,omitempty"`
	Message string `json:"message"`
}

// Helper to convert protocol.Address to hex string (always without 0x prefix)
func AddressToHex(addr protocol.Address) string {
	return fmt.Sprintf("%x", addr[:])
}

// NormalizeAddress ensures all addresses are in consistent format (lowercase, without 0x prefix)
// This is critical for using addresses as map keys
func NormalizeAddress(input string) string {
	// Remove 0x prefix if present
	if len(input) > 2 && (input[:2] == "0x" || input[:2] == "0X") {
		input = input[2:]
	}
	// Convert to lowercase for consistency
	return strings.ToLower(input)
}

// Helper to convert hex string or Ethereum address to protocol.Address
func HexToAddress(input string) (protocol.Address, error) {
	var addr protocol.Address
	hexStr := input

	// If it starts with 0x, remove the prefix
	if len(input) > 2 && (input[:2] == "0x" || input[:2] == "0X") {
		hexStr = input[2:]
	}

	// If it's exactly 40 hex characters, treat as Ethereum address (with or without 0x prefix)
	if len(hexStr) == 40 {
		// Validate and decode hex string to bytes
		bytes := make([]byte, 20)
		for i := 0; i < 20; i++ {
			b, err := strconv.ParseUint(hexStr[i*2:i*2+2], 16, 8)
			if err != nil {
				return addr, fmt.Errorf("invalid hex in address at position %d: %v", i*2, err)
			}
			bytes[i] = byte(b)
		}

		copy(addr[:], bytes)
		return addr, nil
	}

	// Otherwise treat as raw string (legacy username format)
	// Only accept if it doesn't look like a malformed Ethereum address
	if len(hexStr) > 20 {
		return addr, fmt.Errorf("address too long for legacy format (max 20 chars)")
	}
	copy(addr[:], []byte(hexStr))
	return addr, nil
}

// Format timestamp
func FormatTimestamp(t time.Time) string {
	return t.Format("2006-01-02 15:04")
}

// TimeNow returns current time (for easier testing/mocking)
func TimeNow() time.Time {
	return time.Now()
}

// createDisplayNameFromAddress safely creates display name and username from address
func CreateDisplayNameFromAddress(addr string) (displayName string, username string) {
	nameLength := 8
	if len(addr) < nameLength {
		nameLength = len(addr)
	}

	if nameLength == len(addr) {
		displayName = addr
	} else {
		displayName = addr[:nameLength] + "..."
	}

	username = "@" + addr[:nameLength]
	return
}

// Peer information types

type PeerInfoRequest struct {
	PeerAddress string `json:"peer_address"` // Hex-encoded
}

type PeerInfoResponse struct {
	Success bool      `json:"success"`
	Peer    *PeerInfo `json:"peer,omitempty"`
	Message string    `json:"message"`
}

type PeerInfo struct {
	Address           string           `json:"address"`            // Hex-encoded wallet address
	Name              string           `json:"name"`               // Display name
	Username          string           `json:"username"`           // Username
	Avatar            string           `json:"avatar"`             // Avatar URL
	Bio               string           `json:"bio"`                // User bio - always send
	Online            bool             `json:"online"`             // Online status
	EncryptionStatus  EncryptionStatus `json:"encryption_status"`  // Encryption details
	ConnectionInfo    ConnectionInfo   `json:"connection_info"`    // Connection details
	SecurityIndicators SecurityIndicators `json:"security_indicators"` // Security indicators
}

type EncryptionStatus struct {
	Protocol        string `json:"protocol"`         // e.g., "X3DH + Double Ratchet"
	HasRatchetSession bool `json:"has_ratchet_session"` // Whether ratchet session exists
	RatchetInitialized string `json:"ratchet_initialized"` // When session was created
	ForwardSecrecy  bool   `json:"forward_secrecy"`  // Forward secrecy enabled
	IdentityKey     string `json:"identity_key"`     // Identity key fingerprint (hex)
	SignedPreKeyID  uint32 `json:"signed_prekey_id"` // SignedPreKey ID
	OneTimePreKeysAvailable int `json:"onetime_prekeys_available"` // Number of OPKs available
}

type ConnectionInfo struct {
	RelayConnected   bool   `json:"relay_connected"`    // Connected to relay
	RelayAddress     string `json:"relay_address"`      // Relay address
	DHTNodeID        string `json:"dht_node_id"`        // DHT node ID (hex)
	KeyBundleInDHT   bool   `json:"key_bundle_in_dht"`  // Key bundle published in DHT
	LastSeen         string `json:"last_seen"`          // Last seen timestamp
}

type SecurityIndicators struct {
	EndToEndEncrypted bool   `json:"end_to_end_encrypted"`  // E2EE enabled
	OnionRouting      bool   `json:"onion_routing"`         // Onion routing enabled
	Verified          bool   `json:"verified"`              // Peer verified
	TrustLevel        string `json:"trust_level"`           // "unknown", "low", "medium", "high"
}

// Mark as read types

type MarkAsReadRequest struct {
	PeerAddress string `json:"peer_address"` // Hex-encoded address of the peer
	MessageID   string `json:"message_id"`   // Message ID to mark as read
}

type MarkAsReadResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// Delete account types

type DeleteAccountRequest struct {
	WalletAddress string `json:"wallet_address"` // Hex-encoded wallet address
}

type DeleteAccountResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// Typing indicator types

type TypingIndicatorRequest struct {
	PeerAddress string `json:"peer_address"` // Who to send typing indicator to
	Typing      bool   `json:"typing"`       // true = typing, false = stopped typing
}

type TypingIndicatorResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// Message deletion types

type DeleteMessageRequest struct {
	MessageID   string `json:"message_id"`   // ID of the message to delete
	PeerAddress string `json:"peer_address"` // Address of the peer in this chat
	DeleteForEveryone bool `json:"delete_for_everyone"` // true = delete for both, false = delete only for me
}

type DeleteMessageResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type WSMessageDeleted struct {
	MessageID string `json:"message_id"` // ID of the deleted message
	ChatID    string `json:"chat_id"`    // Chat ID (peer address)
}

// Message editing types

type EditMessageRequest struct {
	MessageID   string `json:"message_id"`   // ID of the message to edit
	PeerAddress string `json:"peer_address"` // Address of the peer in this chat
	NewContent  string `json:"new_content"`  // New message content
}

type EditMessageResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type WSMessageEdited struct {
	MessageID  string `json:"message_id"`  // ID of the edited message
	ChatID     string `json:"chat_id"`     // Chat ID (peer address)
	NewContent string `json:"new_content"` // New message content
}

// User action types (block, mute, clear chat, etc.)

type WSUserAction struct {
	Action      string `json:"action"`       // "user_blocked", "user_unblocked", "user_muted", "user_unmuted", "chat_cleared", etc.
	UserAddress string `json:"user_address"` // Address of the user being acted upon
}

// Message reaction types

type AddReactionRequest struct {
	MessageID   string `json:"message_id"`   // ID of the message to react to
	PeerAddress string `json:"peer_address"` // Address of the peer in this chat
	Emoji       string `json:"emoji"`        // Emoji reaction (e.g., "👍", "❤️")
}

type AddReactionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type RemoveReactionRequest struct {
	MessageID   string `json:"message_id"`   // ID of the message
	PeerAddress string `json:"peer_address"` // Address of the peer in this chat
	Emoji       string `json:"emoji"`        // Emoji reaction to remove
}

type RemoveReactionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type WSReactionAdded struct {
	MessageID string `json:"message_id"` // ID of the message
	ChatID    string `json:"chat_id"`    // Chat ID (peer address)
	Emoji     string `json:"emoji"`      // Emoji reaction
	From      string `json:"from"`       // Who added the reaction
}

type WSReactionRemoved struct {
	MessageID string `json:"message_id"` // ID of the message
	ChatID    string `json:"chat_id"`    // Chat ID (peer address)
	Emoji     string `json:"emoji"`      // Emoji reaction
	From      string `json:"from"`       // Who removed the reaction
}

// Media message types

type UploadMediaRequest struct {
	MediaType string `json:"media_type"` // "image", "video", "audio", "file"
	FileName  string `json:"file_name"`  // Original file name
	MimeType  string `json:"mime_type"`  // MIME type (e.g., "image/jpeg", "image/png")
	Data      string `json:"data"`       // Base64 encoded file data
}

type UploadMediaResponse struct {
	Success bool   `json:"success"`
	MediaID string `json:"media_id"` // Unique ID for the uploaded media
	URL     string `json:"url"`      // URL to access the media
	Message string `json:"message"`
}

type SendMediaMessageRequest struct {
	RecipientAddress string `json:"recipient_address"` // Hex-encoded address
	MediaID          string `json:"media_id"`          // ID of the uploaded media
	Caption          string `json:"caption"`           // Optional caption
}

type MediaInfo struct {
	ID        string `json:"id"`
	Type      string `json:"type"`      // "image", "video", "audio", "file"
	URL       string `json:"url"`       // URL to access the media
	FileName  string `json:"file_name"` // Original file name
	MimeType  string `json:"mime_type"` // MIME type
	Size      int64  `json:"size"`      // File size in bytes
	CreatedAt string `json:"created_at"`
}

// Contact blocking types

type BlockContactRequest struct {
	ContactAddress string `json:"contact_address"` // Address to block
}

type BlockContactResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type UnblockContactRequest struct {
	ContactAddress string `json:"contact_address"` // Address to unblock
}

type UnblockContactResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type GetBlockedContactsResponse struct {
	Success          bool     `json:"success"`
	BlockedAddresses []string `json:"blocked_addresses"` // List of blocked addresses
	Message          string   `json:"message"`
}

// Starred messages types

type StarMessageRequest struct {
	MessageID   string `json:"message_id"`   // Message ID to star
	PeerAddress string `json:"peer_address"` // Peer address in this chat
}

type StarMessageResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type UnstarMessageRequest struct {
	MessageID string `json:"message_id"` // Message ID to unstar
}

type UnstarMessageResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type GetStarredMessagesResponse struct {
	Success  bool      `json:"success"`
	Messages []Message `json:"messages"` // List of starred messages
	Message  string    `json:"message"`
}

// Update username types

type UpdateUsernameRequest struct {
	NewUsername string `json:"new_username"` // New username
}

type UpdateUsernameResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// Check username availability types

type CheckUsernameRequest struct {
	Username string `json:"username"` // Username to check
}

type CheckUsernameResponse struct {
	Available bool   `json:"available"` // Whether username is available
	Message   string `json:"message"`   // Descriptive message
}

// Update profile types

type UpdateProfileRequest struct {
	FirstName     string `json:"first_name"`      // First name
	LastName      string `json:"last_name"`       // Last name
	Bio           string `json:"bio"`             // Bio/status
	AvatarChunkID uint64 `json:"avatar_chunk_id"` // MeshStorage chunk ID
	AvatarKey     string `json:"avatar_key"`      // Base64-encoded encryption key
}

type UpdateProfileResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type GetProfileResponse struct {
	Success       bool   `json:"success"`
	FirstName     string `json:"first_name"`
	LastName      string `json:"last_name"`
	Username      string `json:"username"`
	Bio           string `json:"bio"`
	AvatarChunkID uint64 `json:"avatar_chunk_id"`
	AvatarKey     string `json:"avatar_key"` // Base64-encoded encryption key
	Address       string `json:"address"`
	Status        string `json:"status"` // "online", "away", "busy", "offline"
	Message       string `json:"message"`
}

// Update status types

type UpdateStatusRequest struct {
	Status string `json:"status"` // "online", "away", "busy", "offline"
}

type UpdateStatusResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type WSStatusUpdate struct {
	Address string `json:"address"`
	Status  string `json:"status"`
}
