package web

import (
	"context"
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

func (s *Server) Start() error {
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.HandleFunc("/api/inject", s.handleInject)

	// Static files
	staticRoot, err := fs.Sub(staticFS, "static")
	if err != nil {
		return err
	}
	mux.Handle("/", http.FileServer(http.FS(staticRoot)))

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
		s.httpServer.Shutdown(ctx)
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	status := s.proxy.GetStatus()
	json.NewEncoder(w).Encode(status)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	// Return safe config (hide sensitive if any, though none currently)
	json.NewEncoder(w).Encode(s.config)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

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

	// Send initial status
	statusData, _ := json.Marshal(s.proxy.GetStatus())
	fmt.Fprintf(w, "event: status\ndata: %s\n\n", statusData)
	w.(http.Flusher).Flush()

	// Send buffered logs
	s.logBufferMu.Lock()
	for _, msg := range s.logBuffer {
		fmt.Fprintf(w, "event: log\ndata: %s\n\n", msg)
	}
	s.logBufferMu.Unlock()
	w.(http.Flusher).Flush()

	// Periodic status update ticker
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case msg := <-clientChan:
			fmt.Fprintf(w, "event: log\ndata: %s\n\n", msg)
			w.(http.Flusher).Flush()
		case <-ticker.C:
			statusData, _ := json.Marshal(s.proxy.GetStatus())
			fmt.Fprintf(w, "event: status\ndata: %s\n\n", statusData)
			w.(http.Flusher).Flush()
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

	w.WriteHeader(http.StatusOK)
}
