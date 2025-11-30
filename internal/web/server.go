package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hoon-ch/serial-tcp-proxy/internal/config"
	"github.com/hoon-ch/serial-tcp-proxy/internal/logger"
	"github.com/hoon-ch/serial-tcp-proxy/internal/proxy"
)

//go:embed static
var staticFS embed.FS

// WebSocket upgrader with permissive origin check for Home Assistant Ingress
var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for Home Assistant Ingress compatibility
	},
}

// wsClient represents a WebSocket client connection
type wsClient struct {
	conn        *websocket.Conn
	send        chan []byte
	server      *Server
	closed      bool
	closedMu    sync.Mutex
	id          string
	addr        string
	connectedAt time.Time
}

// Session represents an authenticated session
type Session struct {
	Token     string
	CreatedAt time.Time
	ExpiresAt time.Time
}

const (
	sessionCookieName = "session_token"
	sessionDuration   = 24 * time.Hour
)

type Server struct {
	config        *config.Config
	proxy         *proxy.Server
	logger        *logger.Logger
	httpServer    *http.Server
	clients       map[chan string]bool
	clientsMu     sync.Mutex
	wsClients     map[*wsClient]bool
	wsClientsMu   sync.Mutex
	wsClientCount uint64
	logBuffer     []string
	logBufferMu   sync.Mutex
	sessions      map[string]*Session
	sessionsMu    sync.RWMutex
}

func NewServer(cfg *config.Config, p *proxy.Server, l *logger.Logger) *Server {
	s := &Server{
		config:    cfg,
		proxy:     p,
		logger:    l,
		clients:   make(map[chan string]bool),
		wsClients: make(map[*wsClient]bool),
		logBuffer: make([]string, 0, 1000),
		sessions:  make(map[string]*Session),
	}

	// Register log callback
	l.SetLogCallback(s.broadcastLog)

	// Start session cleanup goroutine
	go s.cleanupExpiredSessions()

	return s
}

// generateSessionToken generates a secure random session token
func generateSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// createSession creates a new session and returns the token
func (s *Server) createSession() (string, error) {
	token, err := generateSessionToken()
	if err != nil {
		return "", err
	}

	session := &Session{
		Token:     token,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(sessionDuration),
	}

	s.sessionsMu.Lock()
	s.sessions[token] = session
	s.sessionsMu.Unlock()

	return token, nil
}

// validateSession checks if a session token is valid
func (s *Server) validateSession(token string) bool {
	s.sessionsMu.RLock()
	session, exists := s.sessions[token]
	s.sessionsMu.RUnlock()

	if !exists {
		return false
	}

	if time.Now().After(session.ExpiresAt) {
		s.deleteSession(token)
		return false
	}

	return true
}

// deleteSession removes a session
func (s *Server) deleteSession(token string) {
	s.sessionsMu.Lock()
	delete(s.sessions, token)
	s.sessionsMu.Unlock()
}

// cleanupExpiredSessions periodically removes expired sessions
func (s *Server) cleanupExpiredSessions() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		s.sessionsMu.Lock()
		now := time.Now()
		for token, session := range s.sessions {
			if now.After(session.ExpiresAt) {
				delete(s.sessions, token)
			}
		}
		s.sessionsMu.Unlock()
	}
}

// validateCredentials checks if username and password are correct
func (s *Server) validateCredentials(username, password string) bool {
	usernameMatch := subtle.ConstantTimeCompare([]byte(username), []byte(s.config.WebAuthUsername)) == 1
	passwordMatch := subtle.ConstantTimeCompare([]byte(password), []byte(s.config.WebAuthPassword)) == 1
	return usernameMatch && passwordMatch
}

// getSessionFromRequest extracts and validates session from cookie
func (s *Server) getSessionFromRequest(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	return s.validateSession(cookie.Value)
}

// isAuthenticated checks if request is authenticated (via session or Basic Auth fallback)
func (s *Server) isAuthenticated(r *http.Request) bool {
	if !s.config.WebAuthEnabled {
		return true
	}

	// Check session cookie first
	if s.getSessionFromRequest(r) {
		return true
	}

	// Fallback to Basic Auth for API clients (curl, etc.)
	username, password, ok := r.BasicAuth()
	if ok && s.validateCredentials(username, password) {
		return true
	}

	return false
}

// authMiddleware wraps a handler with authentication
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.isAuthenticated(r) {
			s.logger.Warn("Authentication failed: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// authHandler wraps an http.Handler with authentication (for static files)
func (s *Server) authHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow login page and its assets without auth
		if r.URL.Path == "/login.html" || r.URL.Path == "/style.css" || r.URL.Path == "/favicon.png" {
			next.ServeHTTP(w, r)
			return
		}

		if !s.isAuthenticated(r) {
			// Redirect to login page for browser requests
			if s.config.WebAuthEnabled {
				http.Redirect(w, r, "/login.html", http.StatusFound)
				return
			}
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) Start() error {
	mux := http.NewServeMux()

	// API endpoints
	// Public endpoints (no auth required)
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/logout", s.handleLogout)
	mux.HandleFunc("/api/auth/check", s.handleAuthCheck)

	// Protected endpoints require authentication when enabled
	mux.HandleFunc("/api/status", s.authMiddleware(s.handleStatus))
	mux.HandleFunc("/api/config", s.authMiddleware(s.handleConfig))
	mux.HandleFunc("/api/events", s.authMiddleware(s.handleEvents)) // Legacy SSE endpoint
	mux.HandleFunc("/api/ws", s.authMiddleware(s.handleWebSocket))  // WebSocket endpoint
	mux.HandleFunc("/api/inject", s.authMiddleware(s.handleInject))
	mux.HandleFunc("/api/clients", s.authMiddleware(s.handleClients))
	mux.HandleFunc("/api/clients/disconnect", s.authMiddleware(s.handleDisconnectClient))

	// Static files (protected)
	staticRoot, err := fs.Sub(staticFS, "static")
	if err != nil {
		return err
	}
	mux.Handle("/", s.authHandler(http.FileServer(http.FS(staticRoot))))

	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.config.WebPort),
		Handler: mux,
	}

	s.logger.Info("Web UI listening on http://localhost:%d", s.config.WebPort)

	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("Web server error: %v", err)
		}
	}()

	return nil
}

func (s *Server) Stop() {
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(ctx); err != nil {
			s.logger.Error("Web server shutdown error: %v", err)
		}
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := s.proxy.GetStatus()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		s.logger.Error("Failed to encode status: %v", err)
	}
}

// HealthStatus represents the overall health status
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
)

// HealthCheckStatus represents individual check status
type HealthCheckStatus string

const (
	CheckHealthy   HealthCheckStatus = "healthy"
	CheckUnhealthy HealthCheckStatus = "unhealthy"
)

// UpstreamCheck represents upstream health check details
type UpstreamCheck struct {
	Status        HealthCheckStatus `json:"status"`
	Connected     bool              `json:"connected"`
	Address       string            `json:"address"`
	LastConnected string            `json:"last_connected,omitempty"`
}

// ClientsCheck represents clients health check details
type ClientsCheck struct {
	Status HealthCheckStatus `json:"status"`
	Count  int               `json:"count"`
	Max    int               `json:"max"`
}

// WebServerCheck represents web server health check details
type WebServerCheck struct {
	Status HealthCheckStatus `json:"status"`
	Port   int               `json:"port"`
}

// HealthChecks contains all health check results
type HealthChecks struct {
	Upstream  UpstreamCheck  `json:"upstream"`
	Clients   ClientsCheck   `json:"clients"`
	WebServer WebServerCheck `json:"web_server"`
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status    HealthStatus `json:"status"`
	Version   string       `json:"version"`
	Uptime    int64        `json:"uptime"`
	Checks    HealthChecks `json:"checks"`
	Timestamp string       `json:"timestamp"`
}

// Version is set at build time via -ldflags
// This should be set to the same value as main.Version
var Version = "dev"

// SetVersion allows setting the version from main package
func SetVersion(v string) {
	Version = v
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	isListening := s.proxy.IsListening()
	isUpstreamConnected := s.proxy.IsUpstreamConnected()

	// Determine upstream check status
	upstreamStatus := CheckUnhealthy
	if isUpstreamConnected {
		upstreamStatus = CheckHealthy
	}

	// Get last connected time
	lastConnected := s.proxy.GetUpstreamLastConnected()
	lastConnectedStr := ""
	if !lastConnected.IsZero() {
		lastConnectedStr = lastConnected.Format(time.RFC3339)
	}

	// Determine overall health status
	var overallStatus HealthStatus
	if !isListening {
		overallStatus = HealthStatusUnhealthy
	} else if isUpstreamConnected {
		overallStatus = HealthStatusHealthy
	} else {
		overallStatus = HealthStatusDegraded
	}

	// Calculate uptime in seconds
	uptime := int64(time.Since(s.proxy.GetStartTime()).Seconds())

	response := HealthResponse{
		Status:  overallStatus,
		Version: Version,
		Uptime:  uptime,
		Checks: HealthChecks{
			Upstream: UpstreamCheck{
				Status:        upstreamStatus,
				Connected:     isUpstreamConnected,
				Address:       s.proxy.GetUpstreamAddr(),
				LastConnected: lastConnectedStr,
			},
			Clients: ClientsCheck{
				Status: CheckHealthy,
				Count:  s.proxy.GetClientCount(),
				Max:    s.proxy.GetMaxClients(),
			},
			WebServer: WebServerCheck{
				Status: CheckHealthy,
				Port:   s.config.WebPort,
			},
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	// Set HTTP status code based on health
	httpStatus := http.StatusOK
	if overallStatus == HealthStatusUnhealthy {
		httpStatus = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("Failed to encode health response: %v", err)
	}
}

// PublicConfig contains only non-sensitive configuration fields for API exposure
type PublicConfig struct {
	UpstreamHost string `json:"upstream_host"`
	UpstreamPort int    `json:"upstream_port"`
	ListenPort   int    `json:"listen_port"`
	MaxClients   int    `json:"max_clients"`
	LogPackets   bool   `json:"log_packets"`
	WebPort      int    `json:"web_port"`
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	publicConfig := PublicConfig{
		UpstreamHost: s.config.UpstreamHost,
		UpstreamPort: s.config.UpstreamPort,
		ListenPort:   s.config.ListenPort,
		MaxClients:   s.config.MaxClients,
		LogPackets:   s.config.LogPackets,
		WebPort:      s.config.WebPort,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(publicConfig); err != nil {
		s.logger.Error("Failed to encode config: %v", err)
	}
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	// Check if Flusher is supported
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Register as web client (counts toward maxClients)
	if err := s.proxy.AddWebClient(); err != nil {
		http.Error(w, "Max clients reached", http.StatusServiceUnavailable)
		return
	}

	// Set headers for SSE - critical for proxy compatibility
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	// Disable buffering for various proxies
	w.Header().Set("X-Accel-Buffering", "no")           // nginx
	w.Header().Set("X-Content-Type-Options", "nosniff") // Prevent content sniffing

	// Explicitly send headers and flush immediately
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Create a channel for this client
	clientChan := make(chan string, 10)

	// Register client
	s.clientsMu.Lock()
	s.clients[clientChan] = true
	s.clientsMu.Unlock()

	// Ensure client is removed when connection closes
	defer func() {
		s.clientsMu.Lock()
		delete(s.clients, clientChan)
		s.clientsMu.Unlock()
		close(clientChan)
		s.proxy.RemoveWebClient()
	}()

	// Helper function to write and flush SSE event
	writeEvent := func(event, data string) {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
		flusher.Flush()
	}

	// Send initial status
	if statusData, err := json.Marshal(s.proxy.GetStatus()); err == nil {
		writeEvent("status", string(statusData))
	}

	// Send buffered logs
	s.logBufferMu.Lock()
	for _, msg := range s.logBuffer {
		writeEvent("log", msg)
	}
	s.logBufferMu.Unlock()

	// Periodic status update ticker (2 seconds)
	statusTicker := time.NewTicker(2 * time.Second)
	defer statusTicker.Stop()

	// Heartbeat ticker to keep connection alive through proxies (15 seconds)
	heartbeatTicker := time.NewTicker(15 * time.Second)
	defer heartbeatTicker.Stop()

	for {
		select {
		case msg := <-clientChan:
			writeEvent("log", msg)
		case <-statusTicker.C:
			if statusData, err := json.Marshal(s.proxy.GetStatus()); err == nil {
				writeEvent("status", string(statusData))
			}
		case <-heartbeatTicker.C:
			// Send comment as heartbeat to keep connection alive
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) broadcastLog(msg string) {
	// Add to buffer
	s.logBufferMu.Lock()
	s.logBuffer = append(s.logBuffer, msg)
	if len(s.logBuffer) > 1000 {
		s.logBuffer = s.logBuffer[1:]
	}
	s.logBufferMu.Unlock()

	// Broadcast to SSE clients
	s.clientsMu.Lock()
	for clientChan := range s.clients {
		select {
		case clientChan <- msg:
		default:
			// Drop message if client is too slow
		}
	}
	s.clientsMu.Unlock()

	// Broadcast to WebSocket clients
	s.broadcastToWebSocket("log", msg)
}

// WebSocket message types
type wsMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// handleWebSocket handles WebSocket connections for real-time events
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Register as web client (counts toward maxClients)
	if err := s.proxy.AddWebClient(); err != nil {
		http.Error(w, "Max clients reached", http.StatusServiceUnavailable)
		return
	}

	// Set response headers for proxy compatibility (Home Assistant Ingress)
	responseHeader := http.Header{}
	responseHeader.Set("X-Accel-Buffering", "no") // Disable nginx buffering

	conn, err := wsUpgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		s.logger.Error("WebSocket upgrade failed: %v", err)
		s.proxy.RemoveWebClient()
		return
	}

	// Generate unique ID for web client
	s.wsClientsMu.Lock()
	s.wsClientCount++
	clientID := fmt.Sprintf("web#%d", s.wsClientCount)
	s.wsClientsMu.Unlock()

	client := &wsClient{
		conn:        conn,
		send:        make(chan []byte, 256),
		server:      s,
		id:          clientID,
		addr:        r.RemoteAddr,
		connectedAt: time.Now(),
	}

	// Register client
	s.wsClientsMu.Lock()
	s.wsClients[client] = true
	s.wsClientsMu.Unlock()

	// Send initial status
	if statusData, err := json.Marshal(s.proxy.GetStatus()); err == nil {
		msg := wsMessage{Type: "status", Data: json.RawMessage(statusData)}
		if data, err := json.Marshal(msg); err == nil {
			client.send <- data
		}
	}

	// Send buffered logs (copy buffer to avoid holding lock during channel sends)
	s.logBufferMu.Lock()
	bufferedLogs := make([]string, len(s.logBuffer))
	copy(bufferedLogs, s.logBuffer)
	s.logBufferMu.Unlock()

	for _, logMsg := range bufferedLogs {
		msg := wsMessage{Type: "log", Data: logMsg}
		if data, err := json.Marshal(msg); err == nil {
			select {
			case client.send <- data:
			default:
				// Channel full, skip remaining buffered logs
				break
			}
		}
	}

	// Start goroutines for reading and writing
	go client.writePump()
	go client.readPump()
}

// close safely closes the client and cleans up resources
func (c *wsClient) close() {
	c.closedMu.Lock()
	if c.closed {
		c.closedMu.Unlock()
		return
	}
	c.closed = true
	c.closedMu.Unlock()

	// Remove from server's client list
	c.server.wsClientsMu.Lock()
	delete(c.server.wsClients, c)
	c.server.wsClientsMu.Unlock()

	// Decrement web client count
	c.server.proxy.RemoveWebClient()

	// Close connection
	c.conn.Close()
}

// writePump pumps messages from the send channel to the WebSocket connection
func (c *wsClient) writePump() {
	ticker := time.NewTicker(2 * time.Second) // Status update interval
	pingTicker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		pingTicker.Stop()
		c.close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			if err := c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
				return
			}
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			// Send periodic status update
			if statusData, err := json.Marshal(c.server.proxy.GetStatus()); err == nil {
				msg := wsMessage{Type: "status", Data: json.RawMessage(statusData)}
				if data, err := json.Marshal(msg); err == nil {
					if err := c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
						return
					}
					if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
						return
					}
				}
			}
		case <-pingTicker.C:
			if err := c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
				return
			}
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// readPump pumps messages from the WebSocket connection (handles pongs and close)
func (c *wsClient) readPump() {
	defer func() {
		// Safely close client and cleanup resources
		c.close()
	}()

	c.conn.SetReadLimit(512)
	if err := c.conn.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
		return
	}
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.server.logger.Error("WebSocket error: %v", err)
			}
			break
		}
	}
}

// broadcastToWebSocket sends a message to all WebSocket clients
func (s *Server) broadcastToWebSocket(msgType string, data interface{}) {
	msg := wsMessage{Type: msgType, Data: data}
	jsonData, err := json.Marshal(msg)
	if err != nil {
		return
	}

	s.wsClientsMu.Lock()
	clients := make([]*wsClient, 0, len(s.wsClients))
	for client := range s.wsClients {
		clients = append(clients, client)
	}
	s.wsClientsMu.Unlock()

	for _, client := range clients {
		// Check if client is already closed before sending
		client.closedMu.Lock()
		if client.closed {
			client.closedMu.Unlock()
			continue
		}
		client.closedMu.Unlock()

		select {
		case client.send <- jsonData:
		default:
			// Client too slow, close connection
			go client.close()
		}
	}
}

type InjectRequest struct {
	Target string `json:"target"` // "upstream" or "downstream"
	Format string `json:"format"` // "hex" or "ascii"
	Data   string `json:"data"`
}

func (s *Server) handleInject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req InjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	var data []byte
	if req.Format == "hex" {
		// Clean hex string: remove spaces, newlines, 0x prefix
		hexStr := strings.ReplaceAll(req.Data, " ", "")
		hexStr = strings.ReplaceAll(hexStr, "\n", "")
		hexStr = strings.ReplaceAll(hexStr, "\r", "")
		hexStr = strings.TrimPrefix(hexStr, "0x")

		var err error
		data, err = hex.DecodeString(hexStr)
		if err != nil {
			http.Error(w, fmt.Sprintf("Invalid Hex: %v", err), http.StatusBadRequest)
			return
		}
	} else {
		data = []byte(req.Data)
	}

	if err := s.proxy.InjectPacket(req.Target, data); err != nil {
		http.Error(w, fmt.Sprintf("Injection failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]bool{"success": true}); err != nil {
		s.logger.Error("Failed to encode inject response: %v", err)
	}
}

// ClientsResponse represents the response for the clients endpoint
type ClientsResponse struct {
	Clients    []proxy.ClientInfo `json:"clients"`
	TCPCount   int                `json:"tcp_count"`
	WebCount   int                `json:"web_count"`
	TotalCount int                `json:"total_count"`
	MaxClients int                `json:"max_clients"`
}

func (s *Server) handleClients(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get TCP clients
	clients := s.proxy.GetClients()

	// Add web clients
	s.wsClientsMu.Lock()
	for client := range s.wsClients {
		clients = append(clients, proxy.ClientInfo{
			ID:          client.id,
			Addr:        client.addr,
			ConnectedAt: client.connectedAt.Format(time.RFC3339),
			Type:        "web",
		})
	}
	s.wsClientsMu.Unlock()

	response := ClientsResponse{
		Clients:    clients,
		TCPCount:   s.proxy.GetTCPClientCount(),
		WebCount:   s.proxy.GetWebClientCount(),
		TotalCount: s.proxy.GetClientCount(),
		MaxClients: s.proxy.GetMaxClients(),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("Failed to encode clients response: %v", err)
	}
}

type DisconnectRequest struct {
	ClientID string `json:"client_id"`
}

func (s *Server) handleDisconnectClient(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req DisconnectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.ClientID == "" {
		http.Error(w, "client_id is required", http.StatusBadRequest)
		return
	}

	// Check if it's a web client
	if strings.HasPrefix(req.ClientID, "web#") {
		success := s.disconnectWebClient(req.ClientID)
		if !success {
			http.Error(w, "Client not found", http.StatusNotFound)
			return
		}
	} else {
		// TCP client
		success := s.proxy.DisconnectClient(req.ClientID)
		if !success {
			http.Error(w, "Client not found", http.StatusNotFound)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]bool{"success": true}); err != nil {
		s.logger.Error("Failed to encode disconnect response: %v", err)
	}
}

// disconnectWebClient disconnects a web client by ID
func (s *Server) disconnectWebClient(id string) bool {
	s.wsClientsMu.Lock()
	var target *wsClient
	for client := range s.wsClients {
		if client.id == id {
			target = client
			break
		}
	}
	s.wsClientsMu.Unlock()

	if target == nil {
		return false
	}

	target.close()
	return true
}

// LoginRequest represents the login request body
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// handleLogin handles POST /api/login
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// If auth is disabled, just return success
	if !s.config.WebAuthEnabled {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	if !s.validateCredentials(req.Username, req.Password) {
		s.logger.Warn("Login failed for user '%s' from %s", req.Username, r.RemoteAddr)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid username or password"})
		return
	}

	// Create session
	token, err := s.createSession()
	if err != nil {
		s.logger.Error("Failed to create session: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to create session"})
		return
	}

	// Set session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionDuration.Seconds()),
	})

	s.logger.Info("User '%s' logged in from %s", req.Username, r.RemoteAddr)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// handleLogout handles POST /api/logout
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Delete session if exists
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		s.deleteSession(cookie.Value)
	}

	// Clear cookie
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// handleAuthCheck handles GET /api/auth/check
func (s *Server) handleAuthCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// If auth is disabled, always authenticated
	if !s.config.WebAuthEnabled {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"authenticated": true,
			"auth_enabled":  false,
		})
		return
	}

	authenticated := s.getSessionFromRequest(r)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"authenticated": authenticated,
		"auth_enabled":  true,
	})
}
