package server

import (
	"github.com/zentalk/protocol/pkg/api"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	zentalkcrypto "github.com/zentalk/protocol/pkg/crypto"
	"github.com/zentalk/protocol/pkg/dht"
	"github.com/zentalk/protocol/pkg/network"
	"github.com/zentalk/protocol/pkg/protocol"
)

// verifyEthereumSignature verifies that a signature was created by signing the message with the given wallet address
func verifyEthereumSignature(message, signature, walletAddress string) error {
	// Ethereum signed messages are prefixed with "\x19Ethereum Signed Message:\n" + len(message)
	messageHash := crypto.Keccak256Hash([]byte(fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(message), message)))

	// Decode the hex signature (remove 0x prefix if present)
	sig := signature
	if len(sig) > 2 && sig[:2] == "0x" {
		sig = sig[2:]
	}
	sigBytes, err := hex.DecodeString(sig)
	if err != nil {
		return fmt.Errorf("invalid signature format: %v", err)
	}

	if len(sigBytes) != 65 {
		return fmt.Errorf("signature must be 65 bytes long, got %d", len(sigBytes))
	}

	// Ethereum signatures have v at the end as 27 or 28, but go-ethereum expects 0 or 1
	if sigBytes[64] >= 27 {
		sigBytes[64] -= 27
	}

	// Recover the public key from the signature
	pubKey, err := ethcrypto.SigToPub(messageHash.Bytes(), sigBytes)
	if err != nil {
		return fmt.Errorf("failed to recover public key: %v", err)
	}

	// Get the Ethereum address from the public key
	recoveredAddr := ethcrypto.PubkeyToAddress(*pubKey)

	// Normalize the wallet address (remove 0x prefix, convert to lowercase)
	normalizedWallet := strings.ToLower(strings.TrimPrefix(walletAddress, "0x"))
	recoveredAddrStr := strings.ToLower(recoveredAddr.Hex()[2:]) // Remove 0x prefix from recovered address

	// Compare addresses
	if normalizedWallet != recoveredAddrStr {
		return fmt.Errorf("signature verification failed: recovered address %s does not match claimed address %s",
			recoveredAddrStr, normalizedWallet)
	}

	return nil
}

// findSessionByProtocolAddress finds a session by its protocol.Address and returns the session key
func (s *Server) FindSessionByProtocolAddress(addr protocol.Address) string {
	s.SessionsLock.RLock()
	defer s.SessionsLock.RUnlock()

	for sessionKey, session := range s.Sessions {
		if session.Client != nil && session.Client.Address == addr {
			return sessionKey
		}
	}
	return ""
}

// handleInitialize initializes the client (multi-tenant)
func (s *Server) HandleInitialize(w http.ResponseWriter, r *http.Request) {
	var req api.InitializeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Convert wallet address to protocol.Address (properly handling hex format with 0x prefix)
	addr, err := api.HexToAddress(req.WalletAddress)
	if err != nil {
		s.SendError(w, fmt.Sprintf("Invalid wallet address format: %v", err), http.StatusBadRequest)
		return
	}

	// Normalize wallet address for consistent session keying (after validation)
	walletAddr := api.NormalizeAddress(req.WalletAddress)

	// Verify signature if provided (for wallet ownership verification)
	if req.Signature != "" && req.Message != "" {
		log.Printf("🔐 [Initialize] Verifying wallet signature for %s...", walletAddr)

		// Verify that the signature was created by signing the message with the claimed wallet address
		if err := verifyEthereumSignature(req.Message, req.Signature, req.WalletAddress); err != nil {
			log.Printf("❌ [Initialize] Signature verification failed for %s: %v", walletAddr, err)
			s.SendError(w, fmt.Sprintf("Signature verification failed: %v", err), http.StatusUnauthorized)
			return
		}

		log.Printf("✅ [Initialize] Signature verified successfully for %s", walletAddr)
	} else if req.Signature != "" && req.Message == "" {
		log.Printf("⚠️  [Initialize] Signature provided but message missing for %s (cannot verify)", walletAddr)
		s.SendError(w, "Signature provided but message missing", http.StatusBadRequest)
		return
	} else {
		log.Printf("⚠️  [Initialize] No signature provided for %s (security warning - wallet ownership not verified)", walletAddr)
	}

	// Handle username: use provided one or load from database
	username := req.Username
	if username == "" {
		// No username provided - try to load from database (returning user)
		if s.MessageDB != nil {
			existingUser, err := s.MessageDB.GetUser(walletAddr)
			if err != nil {
				log.Printf("⚠️  [Initialize] Error checking database for existing user %s: %v", walletAddr, err)
			} else if existingUser != nil {
				username = existingUser.Username
				log.Printf("👤 [Initialize] Loaded existing username '%s' for %s", username, walletAddr)
			}
		}

		// If still no username, return error asking user to provide one
		if username == "" {
			log.Printf("❌ [Initialize] No username provided and no existing user found for %s", walletAddr)
			s.SendError(w, "Username required for new user registration", http.StatusBadRequest)
			return
		}
	} else {
		log.Printf("👤 [Initialize] Using provided username '%s' for %s", username, walletAddr)

		// Check if username is available for NEW users (not returning users)
		if s.MessageDB != nil {
			available, err := s.MessageDB.IsUsernameAvailable(username, walletAddr)
			if err != nil {
				log.Printf("⚠️  [Initialize] Error checking username availability: %v", err)
				s.SendError(w, fmt.Sprintf("Failed to verify username availability: %v", err), http.StatusInternalServerError)
				return
			}

			if !available {
				log.Printf("❌ [Initialize] Username '%s' is already taken by another user", username)
				s.SendError(w, fmt.Sprintf("Username '%s' is already taken. Please choose a different username.", username), http.StatusConflict)
				return
			}

			log.Printf("✅ [Initialize] Username '%s' is available", username)
		}
	}

	// Check if session already exists
	existingSession := s.GetSession(walletAddr)
	if existingSession != nil && existingSession.Client != nil {
		log.Printf("♻️  Session already exists for %s, checking relay connection...", walletAddr)

		// Check if relay is still connected, reconnect if needed
		if !existingSession.Client.IsConnected() {
			log.Printf("⚠️  Relay disconnected for %s, reconnecting...", walletAddr)
			if err := existingSession.Client.ConnectToRelay("localhost:9001"); err != nil {
				log.Printf("❌ Failed to reconnect to relay for %s: %v", walletAddr, err)
				// Clean up broken session and create new one
				s.CleanupSession(walletAddr)
			} else {
				log.Printf("✅ Reconnected to relay for %s", walletAddr)

				s.SendJSON(w, api.InitializeResponse{
					Success: true,
					Address: walletAddr,
					Message: "Session active, relay reconnected",
				})
				return
			}
		} else {
			log.Printf("✅ Session and relay connection active for %s", walletAddr)

			s.SendJSON(w, api.InitializeResponse{
				Success: true,
				Address: walletAddr,
				Message: "Session active",
			})
			return
		}
	}

	// Create new session (using wallet address as session key, username for display)
	session := s.CreateSession(walletAddr, username)

	// Store bidirectional username <-> address mapping
	s.SessionsLock.Lock()
	s.UsernameToAddress[strings.ToLower(username)] = walletAddr
	s.AddressToUsername[walletAddr] = username // Store original case username
	s.SessionsLock.Unlock()
	log.Printf("📝 Username mappings: %s -> %s, %s -> %s", username, walletAddr, walletAddr, username)

	// Generate keypair
	privKey, err := zentalkcrypto.GenerateRSAKeyPair()
	if err != nil {
		s.SendError(w, fmt.Sprintf("Failed to generate keys: %v", err), http.StatusInternalServerError)
		return
	}

	// Create DHT node
	nodeID := dht.NewNodeID([]byte(walletAddr))
	session.DHTNode = dht.NewNode(nodeID, "localhost:0")
	if err := session.DHTNode.Start(); err != nil {
		s.SendError(w, fmt.Sprintf("Failed to start DHT: %v", err), http.StatusInternalServerError)
		return
	}

	// Bootstrap with other DHT nodes
	s.BootstrapDHTNode(session.DHTNode)

	// Create client
	session.Client = network.NewClient(privKey)

	session.Client.Address = addr
	session.Client.AttachDHT(session.DHTNode)

	// Connect to relay server (use localhost:9001 for now)
	if err := session.Client.ConnectToRelay("localhost:9001"); err != nil {
		s.SendError(w, fmt.Sprintf("Failed to connect to relay: %v", err), http.StatusInternalServerError)
		return
	}
	log.Printf("✅ Client connected to relay server at localhost:9001")

	// Initialize X3DH
	if err := session.Client.InitializeX3DH(); err != nil {
		s.SendError(w, fmt.Sprintf("Failed to initialize X3DH: %v", err), http.StatusInternalServerError)
		return
	}

	// Publish key bundle to DHT
	if err := session.Client.PublishKeyBundle(); err != nil {
		log.Printf("Warning: Failed to publish key bundle for %s: %v", walletAddr, err)
	}

	// Setup message callback
	session.Client.OnMessageReceived = func(msg *protocol.DirectMessage) {
		s.OnMessageReceived(walletAddr, msg)
	}

	log.Printf("✅ Client initialized for %s (username: %s, address: %s)", walletAddr, username, api.AddressToHex(addr))

	// Save user to database
	if s.MessageDB != nil {
		// Save user with basic info (public key can be stored later when needed)
		if err := s.MessageDB.SaveUser(walletAddr, username, walletAddr, "", 0, []byte{}, nil); err != nil {
			log.Printf("⚠️  Failed to save user to database: %v", err)
		} else {
			log.Printf("✅ User saved to database: %s", walletAddr)

			// Update profile with first/last name if provided
			if req.FirstName != "" || req.LastName != "" {
				if err := s.MessageDB.UpdateProfile(walletAddr, req.FirstName, req.LastName, "", 0, []byte{}); err != nil {
					log.Printf("⚠️  Failed to update profile with names: %v", err)
				} else {
					log.Printf("✅ Profile updated with name: %s %s", req.FirstName, req.LastName)
				}
			}
		}
	}

	// Relay will automatically deliver any queued messages when client connects
	// No need for API server to handle offline message delivery!

	s.SendJSON(w, api.InitializeResponse{
		Success: true,
		Address: walletAddr, // Return normalized address used for session key
		Message: "Client initialized successfully",
	})
}

// getUserSession gets session from request context or header
func (s *Server) GetUserSession(r *http.Request) (*ClientSession, error) {
	// Try to get wallet address from header
	walletAddr := r.Header.Get("X-Wallet-Address")
	if walletAddr == "" {
		return nil, fmt.Errorf("wallet address required in X-Wallet-Address header")
	}

	// Normalize wallet address before lookup (remove 0x prefix, lowercase)
	normalizedAddr := api.NormalizeAddress(walletAddr)

	session := s.GetSession(normalizedAddr)
	if session == nil || session.Client == nil {
		return nil, fmt.Errorf("session not initialized for %s", walletAddr)
	}

	return session, nil
}

// bootstrapDHTNode connects a DHT node to existing nodes for discovery
func (s *Server) BootstrapDHTNode(newNode *dht.Node) {
	s.SessionsLock.RLock()
	bootstrapped := false
	for _, session := range s.Sessions {
		if session.DHTNode != nil && session.DHTNode != newNode {
			// Create contact from existing node
			contact := &dht.Contact{
				ID:      session.DHTNode.ID,
				Address: session.DHTNode.Address,
			}
			// Connect the new node to this existing node
			if err := newNode.Bootstrap(contact); err != nil {
				log.Printf("Failed to bootstrap with node: %v", err)
			} else {
				log.Printf("✅ Bootstrapped DHT node with existing node %x", contact.ID)
				bootstrapped = true
				break // One successful bootstrap is enough
			}
		}
	}
	s.SessionsLock.RUnlock()

	// If bootstrap succeeded, trigger all existing sessions to republish their key bundles
	// This ensures that nodes that initialized first (when no other nodes existed)
	// can now publish their key bundles to the newly joined node
	if bootstrapped {
		s.SessionsLock.RLock()
		for _, session := range s.Sessions {
			if session.Client != nil && session.DHTNode != newNode {
				// Republish in a goroutine to avoid blocking
				// Add delay to allow DHT routing tables to update
				go func(c *network.Client, addr string) {
					// Wait 2 seconds for DHT network to stabilize
					time.Sleep(2 * time.Second)

					if err := c.PublishKeyBundle(); err != nil {
						log.Printf("Failed to republish key bundle for %s: %v", addr, err)
					} else {
						log.Printf("✅ Republished key bundle for %s to new DHT node", addr)
					}
				}(session.Client, session.Username)
			}
		}
		s.SessionsLock.RUnlock()
	}
}
