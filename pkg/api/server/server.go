package server

import (
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/ZentaChain/zentalk-api/pkg/api"
	"github.com/ZentaChain/zentalk-api/pkg/api/database"
	"github.com/ZentaChain/zentalk-api/pkg/dht"
	"github.com/ZentaChain/zentalk-api/pkg/network"
)

// wsConnection wraps a WebSocket connection with a mutex for thread-safe writes
type wsConnection struct {
	conn  *websocket.Conn
	mutex sync.Mutex
}

// ClientSession represents a single user's session
// Messages are stored ENCRYPTED on server (Double Ratchet encryption)
// Server cannot decrypt them - only routes and stores encrypted blobs
type ClientSession struct {
	Client         *network.Client
	DHTNode        *dht.Node
	Username       string
	Address        string
	MessageHistory map[string][]api.Message // chatID -> encrypted messages
	ContactCache   map[string]*api.User     // address -> user
}

// PendingMessage represents a message waiting for offline recipient
type PendingMessage struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
	MessageID string `json:"message_id"`
}

// Server represents the API server (multi-tenant)
type Server struct {
	Sessions          map[string]*ClientSession // walletAddress -> session
	SessionsLock      sync.RWMutex
	UsernameToAddress map[string]string // username -> walletAddress (for username lookup)
	AddressToUsername map[string]string // walletAddress -> username (for reverse lookup)
	RelayServer       *network.RelayServer
	Router            *mux.Router
	WsUpgrader        websocket.Upgrader
	WsConnections     map[string]*wsConnection // address -> connection (thread-safe wrapper)
	WsLock            sync.RWMutex
	OnlineUsers       map[string]bool // address -> online status
	OnlineLock        sync.RWMutex
	PendingMessages   map[string][]PendingMessage // recipient_address -> messages
	PendingLock       sync.RWMutex
	MediaStore        map[string]*api.MediaInfo // media_id -> media info (for metadata only)
	MediaLock         sync.RWMutex
	MessageDB         *database.DB          // persistent encrypted message storage
	MeshStorage       *MeshStorageClient    // MeshStorage client for avatar/media uploads
}

// NewServer creates a new API server with database persistence
// Returns nil and error if initialization fails
// meshStorageURL: URL of MeshStorage API (e.g., "http://localhost:8080")
func NewServer(dbPath string, meshStorageURL string) (*Server, error) {
	// Initialize message database
	messageDB, err := database.New(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize message database: %w", err)
	}

	// Initialize MeshStorage client
	// Default to localhost if not provided
	if meshStorageURL == "" {
		meshStorageURL = "http://localhost:8080"
	}
	meshStorage := NewMeshStorageClient(meshStorageURL)

	s := &Server{
		Router: mux.NewRouter(),
		WsUpgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for development
			},
		},
		Sessions:          make(map[string]*ClientSession),
		UsernameToAddress: make(map[string]string),
		AddressToUsername: make(map[string]string),
		WsConnections:     make(map[string]*wsConnection),
		OnlineUsers:       make(map[string]bool),
		PendingMessages:   make(map[string][]PendingMessage),
		MediaStore:        make(map[string]*api.MediaInfo),
		MessageDB:         messageDB,
		MeshStorage:       meshStorage,
	}

	// Setup routes
	s.setupRoutes()

	log.Printf("✅ API Server initialized (MeshStorage: %s)", meshStorageURL)

	return s, nil
}

// GetSession retrieves a user's session by wallet address
func (s *Server) GetSession(address string) *ClientSession {
	s.SessionsLock.RLock()
	defer s.SessionsLock.RUnlock()
	return s.Sessions[address]
}

// CreateSession creates a new user's session
// Messages stored encrypted on server - server cannot decrypt (E2EE)
func (s *Server) CreateSession(address, username string) *ClientSession {
	s.SessionsLock.Lock()
	defer s.SessionsLock.Unlock()

	session := &ClientSession{
		Username:       username,
		Address:        address,
		MessageHistory: make(map[string][]api.Message),
		ContactCache:   make(map[string]*api.User),
	}
	s.Sessions[address] = session

	// Load message history from database
	if s.MessageDB != nil {
		chats, err := s.MessageDB.LoadAllChats(address)
		if err != nil {
			log.Printf("⚠️  Failed to load message history for %s: %v", address, err)
		} else if len(chats) > 0 {
			session.MessageHistory = chats
			log.Printf("✅ Loaded %d chat(s) from database for %s", len(chats), address)
		}
	}

	return session
}

// CleanupSession removes a session and stops its resources
func (s *Server) CleanupSession(address string) {
	s.SessionsLock.Lock()
	session, exists := s.Sessions[address]
	if exists {
		delete(s.Sessions, address)
	}
	s.SessionsLock.Unlock()

	if !exists || session == nil {
		return
	}

	// Stop DHT node
	if session.DHTNode != nil {
		if err := session.DHTNode.Stop(); err != nil {
			log.Printf("Error stopping DHT node for %s: %v", address, err)
		} else {
			log.Printf("🛑 Stopped DHT node for %s", address)
		}
	}

	log.Printf("🧹 Cleaned up session for %s", address)
}

// GetOrCreateUserWithProfile fetches user from database with full profile (bio, etc.)
// If not found, creates a basic user object with available information
func (s *Server) GetOrCreateUserWithProfile(normalizedAddr string) *api.User {
	// Try to get from database first
	dbUser, err := s.MessageDB.GetUser(normalizedAddr)
	if err == nil && dbUser != nil {
		// Found in database, construct full name
		if dbUser.FirstName != "" || dbUser.LastName != "" {
			dbUser.Name = dbUser.FirstName
			if dbUser.LastName != "" {
				if dbUser.FirstName != "" {
					dbUser.Name += " " + dbUser.LastName
				} else {
					dbUser.Name = dbUser.LastName
				}
			}
		} else if dbUser.Username != "" {
			dbUser.Name = dbUser.Username
		}

		// Set online status
		dbUser.Online = s.IsUserOnline(normalizedAddr)

		// Ensure status is never empty
		if dbUser.Status == "" {
			dbUser.Status = "online"
		}

		return dbUser
	}

	// Not in database, create from session mappings
	s.SessionsLock.RLock()
	actualUsername, hasUsername := s.AddressToUsername[normalizedAddr]
	s.SessionsLock.RUnlock()

	var displayName, displayUsername string
	if hasUsername {
		displayName = actualUsername
		displayUsername = "@" + actualUsername
	} else {
		displayName, displayUsername = api.CreateDisplayNameFromAddress(normalizedAddr)
	}

	return &api.User{
		Name:     displayName,
		Username: displayUsername,
		Avatar:   "/static/images/avatar/default.jpg",
		Online:   s.IsUserOnline(normalizedAddr),
		Status:   "online", // Default for users not yet in database
		Address:  normalizedAddr,
	}
}

func (s *Server) setupRoutes() {
	// Enable CORS first
	s.Router.Use(corsMiddleware)

	// Enable logging middleware to see all requests
	s.Router.Use(loggingMiddleware)

	// API routes
	api := s.Router.PathPrefix("/api").Subrouter()
	api.HandleFunc("/initialize", s.HandleInitialize).Methods("POST", "OPTIONS")
	api.HandleFunc("/send", s.HandleSendMessage).Methods("POST", "OPTIONS")
	api.HandleFunc("/chats", s.HandleGetChats).Methods("GET", "OPTIONS")
	api.HandleFunc("/discover", s.HandleDiscoverContact).Methods("POST", "OPTIONS")
	api.HandleFunc("/peer-info", s.HandleGetPeerInfo).Methods("POST", "OPTIONS")
	api.HandleFunc("/messages/{chatId}", s.HandleGetMessages).Methods("GET", "OPTIONS")
	api.HandleFunc("/mark-as-read", s.handleMarkAsRead).Methods("POST", "OPTIONS")
	api.HandleFunc("/delete-account", s.HandleDeleteAccount).Methods("POST", "OPTIONS")
	api.HandleFunc("/typing", s.HandleTypingIndicator).Methods("POST", "OPTIONS")
	api.HandleFunc("/delete-message", s.HandleDeleteMessage).Methods("POST", "OPTIONS")
	api.HandleFunc("/delete-chat", s.HandleDeleteChat).Methods("POST", "OPTIONS")
	api.HandleFunc("/edit-message", s.HandleEditMessage).Methods("POST", "OPTIONS")
	api.HandleFunc("/add-reaction", s.HandleAddReaction).Methods("POST", "OPTIONS")
	api.HandleFunc("/remove-reaction", s.HandleRemoveReaction).Methods("POST", "OPTIONS")
	api.HandleFunc("/upload-media", s.HandleUploadMedia).Methods("POST", "OPTIONS")
	api.HandleFunc("/media/{mediaId}", s.HandleGetMedia).Methods("GET", "OPTIONS")
	api.HandleFunc("/avatar/{address}", s.HandleDownloadAvatar).Methods("GET", "OPTIONS")
	api.HandleFunc("/avatar/chunk/{chunkId}", s.HandleDownloadAvatarByChunkID).Methods("GET", "OPTIONS")
	api.HandleFunc("/avatar", s.HandleDeleteAvatar).Methods("DELETE", "OPTIONS")
	api.HandleFunc("/block-contact", s.HandleBlockContact).Methods("POST", "OPTIONS")
	api.HandleFunc("/unblock-contact", s.HandleUnblockContact).Methods("POST", "OPTIONS")
	api.HandleFunc("/blocked-contacts", s.HandleGetBlockedContacts).Methods("GET", "OPTIONS")
	api.HandleFunc("/mute-user", s.HandleMuteUser).Methods("POST", "OPTIONS")
	api.HandleFunc("/unmute-user", s.HandleUnmuteUser).Methods("POST", "OPTIONS")
	api.HandleFunc("/muted-users", s.HandleGetMutedUsers).Methods("GET", "OPTIONS")
	api.HandleFunc("/clear-chat", s.HandleClearChat).Methods("POST", "OPTIONS")
	api.HandleFunc("/star-message", s.HandleStarMessage).Methods("POST", "OPTIONS")
	api.HandleFunc("/unstar-message", s.HandleUnstarMessage).Methods("POST", "OPTIONS")
	api.HandleFunc("/starred-messages", s.HandleGetStarredMessages).Methods("GET", "OPTIONS")
	api.HandleFunc("/update-username", s.HandleUpdateUsername).Methods("POST", "OPTIONS")
	api.HandleFunc("/check-username", s.HandleCheckUsername).Methods("POST", "OPTIONS")
	api.HandleFunc("/update-profile", s.HandleUpdateProfile).Methods("POST", "OPTIONS")
	api.HandleFunc("/get-profile", s.HandleGetProfile).Methods("GET", "OPTIONS")
	api.HandleFunc("/update-status", s.HandleUpdateStatus).Methods("POST", "OPTIONS")
	api.HandleFunc("/debug", s.HandleDebug).Methods("GET", "OPTIONS")

	// WebSocket route
	s.Router.HandleFunc("/ws", s.handleWebSocket)

	// Health check
	s.Router.HandleFunc("/health", s.HandleHealth).Methods("GET")

	log.Println("✅ All routes registered successfully")
}

// Start starts the HTTP server
func (s *Server) Start(port int) error {
	addr := fmt.Sprintf(":%d", port)
	log.Printf("Starting API server on %s", addr)
	return http.ListenAndServe(addr, s.Router)
}
