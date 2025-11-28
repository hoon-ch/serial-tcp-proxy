package web

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hoon-ch/serial-tcp-proxy/internal/config"
	"github.com/hoon-ch/serial-tcp-proxy/internal/logger"
	"github.com/hoon-ch/serial-tcp-proxy/internal/proxy"
)

//go:embed static
var staticFS embed.FS

type Server struct {
	config      *config.Config
	proxy       *proxy.Server
	logger      *logger.Logger
	httpServer  *http.Server
	clients     map[chan string]bool
	clientsMu   sync.Mutex
	logBuffer   []string
	logBufferMu sync.Mutex
}

func NewServer(cfg *config.Config, p *proxy.Server, l *logger.Logger) *Server {
	s := &Server{
		config:    cfg,
		proxy:     p,
		logger:    l,
		clients:   make(map[chan string]bool),
		logBuffer: make([]string, 0, 1000),
	}

	// Register log callback
	l.SetLogCallback(s.broadcastLog)

	return s
}

// validateBasicAuth checks if the request has valid basic auth credentials.
// Returns true if auth is disabled or credentials are valid.
// Returns false if auth is enabled but credentials are missing or invalid.
func (s *Server) validateBasicAuth(r *http.Request) bool {
	if !s.config.WebAuthEnabled {
		return true
	}

	username, password, ok := r.BasicAuth()
	if !ok {
		return false
	}

	usernameMatch := subtle.ConstantTimeCompare([]byte(username), []byte(s.config.WebAuthUsername)) == 1
	passwordMatch := subtle.ConstantTimeCompare([]byte(password), []byte(s.config.WebAuthPassword)) == 1

	return usernameMatch && passwordMatch
}

// sendUnauthorized sends a 401 Unauthorized response with WWW-Authenticate header
func (s *Server) sendUnauthorized(w http.ResponseWriter, r *http.Request) {
	s.logger.Warn("Authentication failed: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
	w.Header().Set("WWW-Authenticate", `Basic realm="Serial TCP Proxy"`)
	http.Error(w, "Unauthorized", http.StatusUnauthorized)
}

// authMiddleware wraps a handler with basic authentication
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.validateBasicAuth(r) {
			s.sendUnauthorized(w, r)
			return
		}
		next(w, r)
	}
}

// authHandler wraps an http.Handler with basic authentication
func (s *Server) authHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.validateBasicAuth(r) {
			s.sendUnauthorized(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) Start() error {
	mux := http.NewServeMux()

	// API endpoints
	// /api/health is public for health probes
	mux.HandleFunc("/api/health", s.handleHealth)
	// Protected endpoints require authentication when enabled
	mux.HandleFunc("/api/status", s.authMiddleware(s.handleStatus))
	mux.HandleFunc("/api/config", s.authMiddleware(s.handleConfig))
	mux.HandleFunc("/api/events", s.authMiddleware(s.handleEvents))
	mux.HandleFunc("/api/inject", s.authMiddleware(s.handleInject))

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

	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()

	for clientChan := range s.clients {
		select {
		case clientChan <- msg:
		default:
			// Drop message if client is too slow
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
