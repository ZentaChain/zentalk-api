package server

import (
	"github.com/zentalk/protocol/pkg/api"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

// handleUploadMedia handles media file uploads (images, videos, etc.)
func (s *Server) HandleUploadMedia(w http.ResponseWriter, r *http.Request) {
	var req api.UploadMediaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	walletAddr := api.NormalizeAddress(session.Address)

	// Validate required fields
	if req.Data == "" {
		s.SendError(w, "Media data cannot be empty", http.StatusBadRequest)
		return
	}

	if req.MediaType == "" {
		s.SendError(w, "Media type is required", http.StatusBadRequest)
		return
	}

	// Decode base64 data
	decodedData, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		s.SendError(w, "Invalid base64 data", http.StatusBadRequest)
		return
	}

	log.Printf("📤 [UploadMedia] User %s uploading %s (%d bytes, type: %s)",
		walletAddr, req.FileName, len(decodedData), req.MediaType)

	// Upload to MeshStorage
	chunkID, encryptionKey, err := s.MeshStorage.Upload(walletAddr, decodedData)
	if err != nil {
		log.Printf("❌ Failed to upload to MeshStorage: %v", err)
		s.SendError(w, "Failed to upload media to MeshStorage", http.StatusInternalServerError)
		return
	}

	log.Printf("✅ Media uploaded to MeshStorage: ChunkID=%d, size=%d bytes", chunkID, len(decodedData))

	// Generate media ID for API response
	mediaID := fmt.Sprintf("mesh_%d", chunkID)

	// Save metadata to database
	if s.MessageDB != nil {
		if err := s.MessageDB.SaveMediaFile(
			mediaID,
			walletAddr,
			req.FileName,
			req.MimeType,
			int64(len(decodedData)),
			chunkID,
			encryptionKey,
		); err != nil {
			log.Printf("⚠️  Failed to save media metadata: %v", err)
		}
	}

	// Store in memory cache for quick access (metadata only)
	mediaInfo := &api.MediaInfo{
		ID:        mediaID,
		Type:      req.MediaType,
		URL:       fmt.Sprintf("/api/media/%d", chunkID),
		FileName:  req.FileName,
		MimeType:  req.MimeType,
		Size:      int64(len(decodedData)),
		CreatedAt: api.FormatTimestamp(time.Now()),
	}

	s.MediaLock.Lock()
	s.MediaStore[mediaID] = mediaInfo
	s.MediaLock.Unlock()

	s.SendJSON(w, api.UploadMediaResponse{
		Success: true,
		MediaID: mediaID,
		URL:     mediaInfo.URL,
		Message: "Media uploaded to MeshStorage successfully",
	})
}

// handleGetMedia retrieves uploaded media
func (s *Server) HandleGetMedia(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	mediaID := vars["mediaId"]

	s.MediaLock.RLock()
	mediaInfo, exists := s.MediaStore[mediaID]
	s.MediaLock.RUnlock()

	if !exists {
		s.SendError(w, "Media not found", http.StatusNotFound)
		return
	}

	s.SendJSON(w, map[string]interface{}{
		"success": true,
		"media":   mediaInfo,
	})
}

// HandleDownloadAvatar downloads avatar from MeshStorage and returns as base64
func (s *Server) HandleDownloadAvatar(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	walletAddr := vars["address"]

	// Get user from database to find avatar chunk ID
	user, err := s.MessageDB.GetUser(walletAddr)
	if err != nil {
		s.SendError(w, "User not found", http.StatusNotFound)
		return
	}

	if user.AvatarChunkID == 0 {
		s.SendError(w, "User has no avatar", http.StatusNotFound)
		return
	}

	// Download from MeshStorage
	data, err := s.MeshStorage.Download(walletAddr, user.AvatarChunkID)
	if err != nil {
		log.Printf("❌ Failed to download avatar: %v", err)
		s.SendError(w, "Failed to download avatar from MeshStorage", http.StatusInternalServerError)
		return
	}

	// Encode to base64
	base64Data := base64.StdEncoding.EncodeToString(data)

	s.SendJSON(w, map[string]interface{}{
		"success": true,
		"data":    base64Data,
		"size":    len(data),
	})
}

// HandleDownloadAvatarByChunkID downloads avatar by chunk ID directly
func (s *Server) HandleDownloadAvatarByChunkID(w http.ResponseWriter, r *http.Request) {
	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	vars := mux.Vars(r)
	var chunkID uint64
	_, err = fmt.Sscanf(vars["chunkId"], "%d", &chunkID)
	if err != nil {
		s.SendError(w, "Invalid chunk ID", http.StatusBadRequest)
		return
	}

	walletAddr := api.NormalizeAddress(session.Address)

	// Download from MeshStorage
	data, err := s.MeshStorage.Download(walletAddr, chunkID)
	if err != nil {
		log.Printf("❌ Failed to download avatar chunk %d: %v", chunkID, err)
		s.SendError(w, "Failed to download from MeshStorage", http.StatusInternalServerError)
		return
	}

	log.Printf("✅ Downloaded avatar chunk %d: %d bytes", chunkID, len(data))

	// Encode to base64
	base64Data := base64.StdEncoding.EncodeToString(data)

	s.SendJSON(w, map[string]interface{}{
		"success": true,
		"data":    base64Data,
		"size":    len(data),
	})
}

// HandleDeleteAvatar removes the user's avatar
func (s *Server) HandleDeleteAvatar(w http.ResponseWriter, r *http.Request) {
	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	walletAddr := api.NormalizeAddress(session.Address)

	log.Printf("🗑️  [DeleteAvatar] User %s deleting avatar", walletAddr)

	// Get current user profile to preserve other fields
	user, err := s.MessageDB.GetUser(walletAddr)
	if err != nil {
		log.Printf("❌ Failed to get user profile: %v", err)
		s.SendError(w, "Failed to get user profile", http.StatusInternalServerError)
		return
	}

	// Update user profile to remove avatar (set chunk ID to 0, empty key)
	err = s.MessageDB.UpdateProfile(
		walletAddr,
		user.FirstName,
		user.LastName,
		user.Bio,
		0,        // avatarChunkID = 0 (no avatar)
		[]byte{}, // avatarKey = empty
	)
	if err != nil {
		log.Printf("❌ Failed to delete avatar: %v", err)
		s.SendError(w, "Failed to delete avatar", http.StatusInternalServerError)
		return
	}

	log.Printf("✅ Avatar deleted for user %s", walletAddr)

	s.SendJSON(w, map[string]interface{}{
		"success": true,
		"message": "Avatar deleted successfully",
	})
}
