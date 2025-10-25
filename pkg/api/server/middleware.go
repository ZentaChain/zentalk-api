package server

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"net/http"
	"regexp"
	"sync"
	"time"
)

// corsMiddleware adds CORS headers to allow cross-origin requests
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Wallet-Address")
		w.Header().Set("Access-Control-Max-Age", "3600")

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware logs HTTP requests
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response writer wrapper to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		log.Printf(
			"%s %s %d %s",
			r.Method,
			r.RequestURI,
			wrapped.statusCode,
			time.Since(start),
		)
	})
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Hijack implements http.Hijacker for WebSocket support
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := rw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not implement http.Hijacker")
	}
	return h.Hijack()
}

// Rate limiting state
var (
	rateLimitMap  = make(map[string]*rateLimiter)
	rateLimitLock sync.RWMutex
)

// rateLimiter tracks requests for a single client
type rateLimiter struct {
	tokens     float64
	lastUpdate time.Time
	mu         sync.Mutex
}

// Simple token bucket rate limiter
const (
	rateLimitBurst      = 100   // Maximum burst size
	rateLimitRefillRate = 100.0 // Tokens per minute
)

// rateLimitMiddleware implements token bucket rate limiting per IP
func rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get client IP
		clientIP := getClientIP(r)

		// Get or create rate limiter for this IP
		rateLimitLock.Lock()
		limiter, exists := rateLimitMap[clientIP]
		if !exists {
			limiter = &rateLimiter{
				tokens:     rateLimitBurst,
				lastUpdate: time.Now(),
			}
			rateLimitMap[clientIP] = limiter
		}
		rateLimitLock.Unlock()

		// Check if request is allowed
		limiter.mu.Lock()
		now := time.Now()
		elapsed := now.Sub(limiter.lastUpdate).Minutes()

		// Refill tokens based on time elapsed
		limiter.tokens += elapsed * rateLimitRefillRate
		if limiter.tokens > rateLimitBurst {
			limiter.tokens = rateLimitBurst
		}
		limiter.lastUpdate = now

		// Check if we have tokens available
		if limiter.tokens >= 1.0 {
			limiter.tokens -= 1.0
			limiter.mu.Unlock()
			next.ServeHTTP(w, r)
		} else {
			limiter.mu.Unlock()
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
		}
	})
}

// getClientIP extracts the client IP from the request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	// Fallback to RemoteAddr
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return ip
}

// Ethereum address validation regex (0x followed by 40 hex characters)
var ethereumAddressRegex = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)

// authMiddleware validates wallet address authentication
// Validates the X-Wallet-Address header contains a valid Ethereum address
// For production, add signature validation to prove wallet ownership
func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for health check and OPTIONS requests
		if r.URL.Path == "/health" || r.Method == "OPTIONS" {
			next.ServeHTTP(w, r)
			return
		}

		// Get wallet address from header
		walletAddr := r.Header.Get("X-Wallet-Address")
		if walletAddr == "" {
			http.Error(w, "Missing X-Wallet-Address header", http.StatusUnauthorized)
			return
		}

		// Validate Ethereum address format
		if !ethereumAddressRegex.MatchString(walletAddr) {
			http.Error(w, "Invalid Ethereum address format", http.StatusUnauthorized)
			return
		}

		// TODO: For production, add signature validation
		// 1. Get nonce or challenge from database
		// 2. Verify signature in X-Wallet-Signature header
		// 3. Ensure signature matches wallet address

		next.ServeHTTP(w, r)
	})
}
