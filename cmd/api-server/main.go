package main

import (
	"fmt"
	"log"
	"os"

	"github.com/zentalk/protocol/pkg/api/server"
)

func main() {
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║   ZENTALK API SERVER - Frontend Integration  ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Println()

	// Ensure data directory exists
	dataDir := "./data"
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}

	// Get MeshStorage API URL from environment or use default
	meshStorageURL := os.Getenv("MESH_STORAGE_URL")
	if meshStorageURL == "" {
		meshStorageURL = "http://localhost:8080"
	}

	// Create server with database path and MeshStorage URL
	dbPath := "./data/messages.db"
	srv, err := server.NewServer(dbPath, meshStorageURL)
	if err != nil {
		log.Fatalf("Failed to create API server: %v", err)
	}

	// Start server on port 3001 (8080 is used by MeshStorage API)
	fmt.Println("🚀 Starting API server on http://localhost:3001")
	fmt.Println("📡 WebSocket available at ws://localhost:3001/ws")
	fmt.Printf("💾 MeshStorage API: %s\n", meshStorageURL)
	fmt.Println()
	fmt.Println("API Endpoints:")
	fmt.Println("  POST   /api/initialize    - Initialize client")
	fmt.Println("  POST   /api/send          - Send message")
	fmt.Println("  GET    /api/chats         - Get all chats")
	fmt.Println("  GET    /api/messages/:id  - Get messages for chat")
	fmt.Println("  POST   /api/discover      - Discover contact")
	fmt.Println("  GET    /health            - Health check")
	fmt.Println()
	fmt.Println("Press Ctrl+C to stop the server")
	fmt.Println()

	if err := srv.Start(3001); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
