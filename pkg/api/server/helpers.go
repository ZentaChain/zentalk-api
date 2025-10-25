package server

import (
	"encoding/json"
	"github.com/zentalk/protocol/pkg/api"
	"net/http"
)

// SendJSON sends a JSON response
func (s *Server) SendJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

// SendError sends an error response
func (s *Server) SendError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(api.ErrorResponse{
		Success: false,
		Error:   message,
	})
}

// SendSuccess sends a success response
func (s *Server) SendSuccess(w http.ResponseWriter, message string) {
	s.SendJSON(w, map[string]interface{}{
		"success": true,
		"message": message,
	})
}

// SendData sends data with success flag
func (s *Server) SendData(w http.ResponseWriter, data interface{}) {
	s.SendJSON(w, map[string]interface{}{
		"success": true,
		"data":    data,
	})
}
