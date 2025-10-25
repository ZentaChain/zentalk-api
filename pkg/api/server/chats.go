package server

import (
	"log"
	"net/http"

	"github.com/zentalk/protocol/pkg/api"
)

// handleGetChats returns all chats (multi-tenant)
func (s *Server) HandleGetChats(w http.ResponseWriter, r *http.Request) {
	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	chats := make([]api.Chat, 0)

	log.Printf("📚 [GetChats] Loading chats for session. Total chats in memory: %d", len(session.MessageHistory))

	// Convert message history to chats
	for chatID, messages := range session.MessageHistory {
		// Count unread messages for logging
		unreadCount := 0
		for _, msg := range messages {
			if msg.Unread {
				unreadCount++
			}
		}
		log.Printf("📊 [GetChats] Chat %s: %d messages, %d unread", chatID, len(messages), unreadCount)

		// chatID is already normalized when stored
		contact := session.ContactCache[chatID]
		if contact == nil {
			// Get user with full profile (including bio from database)
			contact = s.GetOrCreateUserWithProfile(chatID)
			session.ContactCache[chatID] = contact
		} else {
			contact.Online = s.IsUserOnline(chatID)
		}

		chats = append(chats, api.Chat{
			ID:       chatID,
			Sender:   *contact,
			Messages: messages,
		})
	}

	s.SendJSON(w, api.ChatsResponse{
		Success: true,
		Chats:   chats,
	})
}
