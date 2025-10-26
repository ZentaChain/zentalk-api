package server

import (
	"github.com/ZentaChain/zentalk-api/pkg/api"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

)

// handleDiscoverContact discovers a contact by address or username (multi-tenant)
func (s *Server) HandleDiscoverContact(w http.ResponseWriter, r *http.Request) {
	var req api.DiscoverContactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	searchInput := req.Address

	// Try username lookup first if input doesn't look like a wallet address
	// Wallet addresses are typically 40+ hex characters (with or without 0x)
	if len(searchInput) < 40 && !strings.HasPrefix(searchInput, "0x") && !strings.HasPrefix(searchInput, "0X") {
		// Might be a username, try to look up in username map
		s.SessionsLock.RLock()
		walletAddr, found := s.UsernameToAddress[strings.ToLower(searchInput)]
		s.SessionsLock.RUnlock()

		if found {
			log.Printf("🔎 Username '%s' resolved to wallet address: %s", searchInput, walletAddr)
			searchInput = walletAddr // Use the wallet address for discovery
		} else {
			log.Printf("⚠️  Username '%s' not found in map, treating as address", searchInput)
		}
	}

	addr, err := api.HexToAddress(searchInput)
	if err != nil {
		s.SendError(w, fmt.Sprintf("Invalid address or username not found: %s", req.Address), http.StatusBadRequest)
		return
	}

	// Discover key bundle
	bundle, err := session.Client.DiscoverKeyBundle(addr)
	if err != nil {
		log.Printf("❌ Failed to discover key bundle for %s: %v", req.Address, err)
		s.SendJSON(w, api.DiscoverContactResponse{
			Success: false,
			Message: fmt.Sprintf("Contact not found in DHT. They may not have initialized yet. Error: %v", err),
		})
		return
	}

	log.Printf("✅ Successfully discovered key bundle for %s: %+v", req.Address, bundle != nil)

	// Create user object with full profile (including bio from database)
	normalizedAddr := api.NormalizeAddress(searchInput) // Use searchInput which may be resolved address
	user := s.GetOrCreateUserWithProfile(normalizedAddr)

	// Cache contact (use normalized address as key)
	session.ContactCache[normalizedAddr] = user

	s.SendJSON(w, api.DiscoverContactResponse{
		Success: true,
		User:    user,
		Message: "Contact discovered",
	})
}

// handleGetPeerInfo returns detailed information about a peer including encryption status
func (s *Server) HandleGetPeerInfo(w http.ResponseWriter, r *http.Request) {
	var req api.PeerInfoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// Convert peer address
	peerAddr, err := api.HexToAddress(req.PeerAddress)
	if err != nil {
		s.SendError(w, "Invalid peer address", http.StatusBadRequest)
		return
	}

	// Get or create contact info (normalize address for lookup)
	normalizedPeerAddr := api.NormalizeAddress(req.PeerAddress)
	// Always refresh from database to get latest bio and profile data
	contact := s.GetOrCreateUserWithProfile(normalizedPeerAddr)
	contact.Online = s.IsUserOnline(normalizedPeerAddr)
	// Update cache with fresh data
	session.ContactCache[normalizedPeerAddr] = contact

	// Get encryption status
	hasRatchetSession := false
	var identityKeyHex string
	var signedPreKeyID uint32
	var opksAvailable int
	var ratchetInitTime string

	if session.Client != nil {
		// Check if ratchet session exists
		if ratchetSession, exists := session.Client.GetRatchetSession(peerAddr); exists {
			hasRatchetSession = true
			// Get session creation time from session storage
			ratchetInitTime = "Recently" // Could be enhanced to track actual time
			_ = ratchetSession           // Avoid unused variable warning
			log.Printf("✅ [api.PeerInfo] Found ratchet session with %s", req.PeerAddress)
		} else {
			log.Printf("⚠️  [api.PeerInfo] No ratchet session found with %s (looking for address: %x)", req.PeerAddress, peerAddr[:])
		}

		// Try to get key bundle from cache
		if keyBundle, found := session.Client.GetCachedKeyBundle(peerAddr); found {
			// Get identity key fingerprint (first 16 bytes of the 32-byte public key)
			identityKeyHex = fmt.Sprintf("%x", keyBundle.IdentityKey[:16])
			signedPreKeyID = keyBundle.SignedPreKey.KeyID
			opksAvailable = len(keyBundle.OneTimePreKeys)
		}
	}

	// Get connection info
	relayConnected := session.Client != nil && session.Client.IsConnected()
	relayAddress := ""
	if relayConnected {
		relayAddress = session.Client.GetRelayAddress()
	}

	dhtNodeID := ""
	if session.DHTNode != nil {
		dhtNodeID = fmt.Sprintf("%x", session.DHTNode.ID[:16]) // First 16 bytes
	}

	// Check if key bundle is in DHT
	keyBundleInDHT := false
	if session.Client != nil && session.DHTNode != nil {
		_, err := session.Client.DiscoverKeyBundle(peerAddr)
		keyBundleInDHT = (err == nil)
	}

	// Determine trust level based on session and verification
	trustLevel := "unknown"
	if hasRatchetSession {
		trustLevel = "medium" // Has established encrypted session
	}
	if keyBundleInDHT && hasRatchetSession {
		trustLevel = "high" // Fully verified with DHT and session
	}

	// Build peer info
	peerInfo := &api.PeerInfo{
		Address:  req.PeerAddress,
		Name:     contact.Name,
		Username: contact.Username,
		Avatar:   contact.Avatar,
		Bio:      contact.Bio,
		Online:   contact.Online,
		EncryptionStatus: api.EncryptionStatus{
			Protocol:                "X3DH + Double Ratchet",
			HasRatchetSession:       hasRatchetSession,
			RatchetInitialized:      ratchetInitTime,
			ForwardSecrecy:          hasRatchetSession, // Forward secrecy is active when ratchet is active
			IdentityKey:             identityKeyHex,
			SignedPreKeyID:          signedPreKeyID,
			OneTimePreKeysAvailable: opksAvailable,
		},
		ConnectionInfo: api.ConnectionInfo{
			RelayConnected: relayConnected,
			RelayAddress:   relayAddress,
			DHTNodeID:      dhtNodeID,
			KeyBundleInDHT: keyBundleInDHT,
			LastSeen:       api.FormatTimestamp(api.TimeNow()), // Could be enhanced to track actual last seen
		},
		SecurityIndicators: api.SecurityIndicators{
			EndToEndEncrypted: hasRatchetSession,
			OnionRouting:      true, // Always true when using the relay network
			Verified:          keyBundleInDHT && hasRatchetSession,
			TrustLevel:        trustLevel,
		},
	}

	s.SendJSON(w, api.PeerInfoResponse{
		Success: true,
		Peer:    peerInfo,
		Message: "Peer information retrieved",
	})
}

// HandleBlockContact blocks a contact
func (s *Server) HandleBlockContact(w http.ResponseWriter, r *http.Request) {
	var req api.BlockContactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// Normalize contact address
	normalizedContact := api.NormalizeAddress(req.ContactAddress)
	normalizedUser := api.NormalizeAddress(session.Address)

	// Block contact in database
	if s.MessageDB != nil {
		if err := s.MessageDB.BlockContact(normalizedUser, normalizedContact); err != nil {
			log.Printf("❌ Failed to block contact: %v", err)
			s.SendError(w, fmt.Sprintf("Failed to block contact: %v", err), http.StatusInternalServerError)
			return
		}
	}

	log.Printf("🚫 User %s blocked %s", normalizedUser, normalizedContact)

	// Notify the blocked user via WebSocket
	s.BroadcastUserAction(normalizedContact, api.WSUserAction{
		Action:      "you_were_blocked",
		UserAddress: normalizedUser, // Who blocked them
	})

	s.SendJSON(w, api.BlockContactResponse{
		Success: true,
		Message: "Contact blocked successfully",
	})
}

// HandleUnblockContact unblocks a contact
func (s *Server) HandleUnblockContact(w http.ResponseWriter, r *http.Request) {
	var req api.UnblockContactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// Normalize contact address
	normalizedContact := api.NormalizeAddress(req.ContactAddress)
	normalizedUser := api.NormalizeAddress(session.Address)

	// Unblock contact in database
	if s.MessageDB != nil {
		if err := s.MessageDB.UnblockContact(normalizedUser, normalizedContact); err != nil {
			log.Printf("❌ Failed to unblock contact: %v", err)
			s.SendError(w, fmt.Sprintf("Failed to unblock contact: %v", err), http.StatusInternalServerError)
			return
		}
	}

	log.Printf("✅ User %s unblocked %s", normalizedUser, normalizedContact)

	// Notify the unblocked user via WebSocket
	s.BroadcastUserAction(normalizedContact, api.WSUserAction{
		Action:      "you_were_unblocked",
		UserAddress: normalizedUser, // Who unblocked them
	})

	s.SendJSON(w, api.UnblockContactResponse{
		Success: true,
		Message: "Contact unblocked successfully",
	})
}

// HandleGetBlockedContacts retrieves all blocked contacts
func (s *Server) HandleGetBlockedContacts(w http.ResponseWriter, r *http.Request) {
	session, err := s.GetUserSession(r)
	if err != nil {
		s.SendError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	normalizedUser := api.NormalizeAddress(session.Address)

	var blockedAddresses []string
	if s.MessageDB != nil {
		blockedAddresses, err = s.MessageDB.GetBlockedContacts(normalizedUser)
		if err != nil {
			log.Printf("❌ Failed to get blocked contacts: %v", err)
			s.SendError(w, fmt.Sprintf("Failed to get blocked contacts: %v", err), http.StatusInternalServerError)
			return
		}
	}

	s.SendJSON(w, api.GetBlockedContactsResponse{
		Success:          true,
		BlockedAddresses: blockedAddresses,
		Message:          fmt.Sprintf("Found %d blocked contacts", len(blockedAddresses)),
	})
}
