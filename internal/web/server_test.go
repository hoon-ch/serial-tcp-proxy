package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hoon-ch/serial-tcp-proxy/internal/config"
	"github.com/hoon-ch/serial-tcp-proxy/internal/logger"
	"github.com/hoon-ch/serial-tcp-proxy/internal/proxy"
)

func newTestLogger() *logger.Logger {
	log, _ := logger.New(false, "")
	log.SetOutput(io.Discard)
	return log
}

func TestHealthEndpoint_Degraded(t *testing.T) {
	// Create config with unreachable upstream (proxy will be degraded)
	cfg := &config.Config{
		UpstreamHost: "192.168.255.255",
		UpstreamPort: 9999,
		ListenPort:   0,
		MaxClients:   10,
		LogPackets:   false,
		WebPort:      18080,
	}

	// Get a free port for proxy
	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}
	cfg.ListenPort = proxyListener.Addr().(*net.TCPAddr).Port
	proxyListener.Close()

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)

	err = p.Start()
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer p.Stop()

	// Create web server
	webServer := NewServer(cfg, p, log)

	// Test health endpoint
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()

	webServer.handleHealth(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	// Should return 200 (degraded is still 200)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var health HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Check degraded status (upstream not connected but proxy listening)
	if health.Status != HealthStatusDegraded {
		t.Errorf("Expected status 'degraded', got '%s'", health.Status)
	}

	if health.Checks.Upstream.Connected {
		t.Error("Expected upstream to be disconnected")
	}

	if health.Checks.Upstream.Status != CheckUnhealthy {
		t.Errorf("Expected upstream status 'unhealthy', got '%s'", health.Checks.Upstream.Status)
	}

	if health.Checks.Clients.Status != CheckHealthy {
		t.Errorf("Expected clients status 'healthy', got '%s'", health.Checks.Clients.Status)
	}

	if health.Checks.WebServer.Status != CheckHealthy {
		t.Errorf("Expected web_server status 'healthy', got '%s'", health.Checks.WebServer.Status)
	}

	if health.Uptime < 0 {
		t.Error("Expected positive uptime")
	}

	if health.Timestamp == "" {
		t.Error("Expected timestamp to be set")
	}
}

func TestHealthEndpoint_Healthy(t *testing.T) {
	// Start a mock upstream server
	upstreamListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start mock upstream: %v", err)
	}
	defer upstreamListener.Close()

	// Accept connection in background
	go func() {
		conn, err := upstreamListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Keep connection alive
		time.Sleep(5 * time.Second)
	}()

	// Create config
	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: upstreamListener.Addr().(*net.TCPAddr).Port,
		ListenPort:   0,
		MaxClients:   10,
		LogPackets:   false,
		WebPort:      18080,
	}

	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}
	cfg.ListenPort = proxyListener.Addr().(*net.TCPAddr).Port
	proxyListener.Close()

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)

	err = p.Start()
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer p.Stop()

	// Wait for upstream connection
	time.Sleep(200 * time.Millisecond)

	webServer := NewServer(cfg, p, log)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()

	webServer.handleHealth(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var health HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if health.Status != HealthStatusHealthy {
		t.Errorf("Expected status 'healthy', got '%s'", health.Status)
	}

	if !health.Checks.Upstream.Connected {
		t.Error("Expected upstream to be connected")
	}

	if health.Checks.Upstream.Status != CheckHealthy {
		t.Errorf("Expected upstream status 'healthy', got '%s'", health.Checks.Upstream.Status)
	}

	if health.Checks.Upstream.LastConnected == "" {
		t.Error("Expected last_connected to be set")
	}
}

func TestHealthEndpoint_MethodNotAllowed(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: 8899,
		ListenPort:   18899,
		MaxClients:   10,
		LogPackets:   false,
		WebPort:      18080,
	}

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)

	webServer := NewServer(cfg, p, log)

	req := httptest.NewRequest(http.MethodPost, "/api/health", nil)
	w := httptest.NewRecorder()

	webServer.handleHealth(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", resp.StatusCode)
	}
}

func TestHealthEndpoint_ResponseFormat(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost: "192.168.50.143",
		UpstreamPort: 8899,
		ListenPort:   0,
		MaxClients:   10,
		LogPackets:   false,
		WebPort:      18080,
	}

	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}
	cfg.ListenPort = proxyListener.Addr().(*net.TCPAddr).Port
	proxyListener.Close()

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)

	err = p.Start()
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer p.Stop()

	webServer := NewServer(cfg, p, log)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()

	webServer.handleHealth(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	// Check content type
	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", contentType)
	}

	// Verify JSON structure
	var health map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Check required fields exist
	requiredFields := []string{"status", "version", "uptime", "checks", "timestamp"}
	for _, field := range requiredFields {
		if _, ok := health[field]; !ok {
			t.Errorf("Missing required field: %s", field)
		}
	}

	// Check checks structure
	checks, ok := health["checks"].(map[string]interface{})
	if !ok {
		t.Fatal("checks field is not an object")
	}

	checkFields := []string{"upstream", "clients", "web_server"}
	for _, field := range checkFields {
		if _, ok := checks[field]; !ok {
			t.Errorf("Missing check field: %s", field)
		}
	}

	// Verify upstream check structure
	upstream, ok := checks["upstream"].(map[string]interface{})
	if !ok {
		t.Fatal("upstream check is not an object")
	}

	upstreamFields := []string{"status", "connected", "address"}
	for _, field := range upstreamFields {
		if _, ok := upstream[field]; !ok {
			t.Errorf("Missing upstream field: %s", field)
		}
	}

	// Verify address format
	if upstream["address"] != "192.168.50.143:8899" {
		t.Errorf("Unexpected upstream address: %v", upstream["address"])
	}
}

func TestHealthEndpoint_ClientCount(t *testing.T) {
	// Start mock upstream
	upstreamListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start mock upstream: %v", err)
	}
	defer upstreamListener.Close()

	go func() {
		for {
			conn, err := upstreamListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				time.Sleep(5 * time.Second)
			}(conn)
		}
	}()

	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: upstreamListener.Addr().(*net.TCPAddr).Port,
		ListenPort:   0,
		MaxClients:   10,
		LogPackets:   false,
		WebPort:      18080,
	}

	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}
	proxyAddr := proxyListener.Addr().String()
	cfg.ListenPort = proxyListener.Addr().(*net.TCPAddr).Port
	proxyListener.Close()

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)

	err = p.Start()
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer p.Stop()

	time.Sleep(200 * time.Millisecond)

	// Connect a client
	client, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("Failed to connect client: %v", err)
	}
	defer client.Close()

	time.Sleep(100 * time.Millisecond)

	webServer := NewServer(cfg, p, log)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()

	webServer.handleHealth(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	var health HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if health.Checks.Clients.Count != 1 {
		t.Errorf("Expected 1 client, got %d", health.Checks.Clients.Count)
	}

	if health.Checks.Clients.Max != 10 {
		t.Errorf("Expected max 10 clients, got %d", health.Checks.Clients.Max)
	}
}

func TestAuthMiddleware_Disabled(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost:    "127.0.0.1",
		UpstreamPort:    8899,
		ListenPort:      18899,
		MaxClients:      10,
		WebPort:         18080,
		WebAuthEnabled:  false,
		WebAuthUsername: "",
		WebAuthPassword: "",
	}

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)
	webServer := NewServer(cfg, p, log)

	// Create a test handler
	handler := webServer.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 when auth disabled, got %d", resp.StatusCode)
	}
}

func TestAuthMiddleware_Enabled_NoCredentials(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost:    "127.0.0.1",
		UpstreamPort:    8899,
		ListenPort:      18899,
		MaxClients:      10,
		WebPort:         18080,
		WebAuthEnabled:  true,
		WebAuthUsername: "admin",
		WebAuthPassword: "secret",
	}

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)
	webServer := NewServer(cfg, p, log)

	handler := webServer.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected status 401 without credentials, got %d", resp.StatusCode)
	}
}

func TestAuthMiddleware_Enabled_WrongCredentials(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost:    "127.0.0.1",
		UpstreamPort:    8899,
		ListenPort:      18899,
		MaxClients:      10,
		WebPort:         18080,
		WebAuthEnabled:  true,
		WebAuthUsername: "admin",
		WebAuthPassword: "secret",
	}

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)
	webServer := NewServer(cfg, p, log)

	handler := webServer.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.SetBasicAuth("admin", "wrongpassword")
	w := httptest.NewRecorder()

	handler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected status 401 with wrong credentials, got %d", resp.StatusCode)
	}
}

func TestAuthMiddleware_Enabled_WrongUsername(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost:    "127.0.0.1",
		UpstreamPort:    8899,
		ListenPort:      18899,
		MaxClients:      10,
		WebPort:         18080,
		WebAuthEnabled:  true,
		WebAuthUsername: "admin",
		WebAuthPassword: "secret",
	}

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)
	webServer := NewServer(cfg, p, log)

	handler := webServer.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.SetBasicAuth("wronguser", "secret")
	w := httptest.NewRecorder()

	handler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected status 401 with wrong username, got %d", resp.StatusCode)
	}
}

func TestAuthMiddleware_Enabled_ValidCredentials(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost:    "127.0.0.1",
		UpstreamPort:    8899,
		ListenPort:      18899,
		MaxClients:      10,
		WebPort:         18080,
		WebAuthEnabled:  true,
		WebAuthUsername: "admin",
		WebAuthPassword: "secret",
	}

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)
	webServer := NewServer(cfg, p, log)

	handler := webServer.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()

	handler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 with valid credentials, got %d", resp.StatusCode)
	}
}

func TestAuthHandler_Enabled_ValidCredentials(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost:    "127.0.0.1",
		UpstreamPort:    8899,
		ListenPort:      18899,
		MaxClients:      10,
		WebPort:         18080,
		WebAuthEnabled:  true,
		WebAuthUsername: "admin",
		WebAuthPassword: "secret",
	}

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)
	webServer := NewServer(cfg, p, log)

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	handler := webServer.authHandler(innerHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 with valid credentials, got %d", resp.StatusCode)
	}
}

func TestAuthHandler_Enabled_NoCredentials(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost:    "127.0.0.1",
		UpstreamPort:    8899,
		ListenPort:      18899,
		MaxClients:      10,
		WebPort:         18080,
		WebAuthEnabled:  true,
		WebAuthUsername: "admin",
		WebAuthPassword: "secret",
	}

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)
	webServer := NewServer(cfg, p, log)

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	handler := webServer.authHandler(innerHandler)

	// Test with a non-login page path that should redirect
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	// With session-based auth, unauthenticated requests to protected paths get redirected to login
	if resp.StatusCode != http.StatusFound {
		t.Errorf("Expected status 302 (redirect to login) without credentials, got %d", resp.StatusCode)
	}

	// Check redirect location
	location := resp.Header.Get("Location")
	if location != "/login.html" {
		t.Errorf("Expected redirect to /login.html, got %s", location)
	}
}

func TestHealthEndpoint_NoAuthRequired(t *testing.T) {
	// Start a mock upstream server
	upstreamListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start mock upstream: %v", err)
	}
	defer upstreamListener.Close()

	go func() {
		conn, err := upstreamListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		time.Sleep(5 * time.Second)
	}()

	cfg := &config.Config{
		UpstreamHost:    "127.0.0.1",
		UpstreamPort:    upstreamListener.Addr().(*net.TCPAddr).Port,
		ListenPort:      0,
		MaxClients:      10,
		WebPort:         18080,
		WebAuthEnabled:  true,
		WebAuthUsername: "admin",
		WebAuthPassword: "secret",
	}

	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}
	cfg.ListenPort = proxyListener.Addr().(*net.TCPAddr).Port
	proxyListener.Close()

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)

	err = p.Start()
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer p.Stop()

	time.Sleep(200 * time.Millisecond)

	webServer := NewServer(cfg, p, log)

	// Health endpoint should work without auth even when auth is enabled
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()

	webServer.handleHealth(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	// Should return 200 (health endpoint is public)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 for health endpoint without auth, got %d", resp.StatusCode)
	}
}

func TestHandleStatus_Success(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost: "192.168.255.255",
		UpstreamPort: 9999,
		ListenPort:   0,
		MaxClients:   10,
		LogPackets:   false,
		WebPort:      18080,
	}

	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}
	cfg.ListenPort = proxyListener.Addr().(*net.TCPAddr).Port
	proxyListener.Close()

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)

	err = p.Start()
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer p.Stop()

	webServer := NewServer(cfg, p, log)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()

	webServer.handleStatus(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", contentType)
	}

	var status map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
}

func TestHandleStatus_MethodNotAllowed(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: 8899,
		ListenPort:   18899,
		MaxClients:   10,
		WebPort:      18080,
	}

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)
	webServer := NewServer(cfg, p, log)

	req := httptest.NewRequest(http.MethodPost, "/api/status", nil)
	w := httptest.NewRecorder()

	webServer.handleStatus(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", resp.StatusCode)
	}
}

func TestHandleConfig_Success(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost: "192.168.1.100",
		UpstreamPort: 5000,
		ListenPort:   6000,
		MaxClients:   20,
		LogPackets:   true,
		WebPort:      8080,
	}

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)
	webServer := NewServer(cfg, p, log)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()

	webServer.handleConfig(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", contentType)
	}

	var pubCfg PublicConfig
	if err := json.NewDecoder(resp.Body).Decode(&pubCfg); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if pubCfg.UpstreamHost != "192.168.1.100" {
		t.Errorf("Expected upstream_host '192.168.1.100', got '%s'", pubCfg.UpstreamHost)
	}
	if pubCfg.UpstreamPort != 5000 {
		t.Errorf("Expected upstream_port 5000, got %d", pubCfg.UpstreamPort)
	}
	if pubCfg.ListenPort != 6000 {
		t.Errorf("Expected listen_port 6000, got %d", pubCfg.ListenPort)
	}
	if pubCfg.MaxClients != 20 {
		t.Errorf("Expected max_clients 20, got %d", pubCfg.MaxClients)
	}
	if !pubCfg.LogPackets {
		t.Error("Expected log_packets true")
	}
	if pubCfg.WebPort != 8080 {
		t.Errorf("Expected web_port 8080, got %d", pubCfg.WebPort)
	}
}

func TestHandleConfig_MethodNotAllowed(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: 8899,
		ListenPort:   18899,
		MaxClients:   10,
		WebPort:      18080,
	}

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)
	webServer := NewServer(cfg, p, log)

	req := httptest.NewRequest(http.MethodPost, "/api/config", nil)
	w := httptest.NewRecorder()

	webServer.handleConfig(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", resp.StatusCode)
	}
}

func TestHandleInject_MethodNotAllowed(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: 8899,
		ListenPort:   18899,
		MaxClients:   10,
		WebPort:      18080,
	}

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)
	webServer := NewServer(cfg, p, log)

	req := httptest.NewRequest(http.MethodGet, "/api/inject", nil)
	w := httptest.NewRecorder()

	webServer.handleInject(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", resp.StatusCode)
	}
}

func TestHandleInject_InvalidJSON(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: 8899,
		ListenPort:   18899,
		MaxClients:   10,
		WebPort:      18080,
	}

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)
	webServer := NewServer(cfg, p, log)

	req := httptest.NewRequest(http.MethodPost, "/api/inject", strings.NewReader("not valid json"))
	w := httptest.NewRecorder()

	webServer.handleInject(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}
}

func TestHandleInject_InvalidHex(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: 8899,
		ListenPort:   18899,
		MaxClients:   10,
		WebPort:      18080,
	}

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)
	webServer := NewServer(cfg, p, log)

	body := `{"target": "upstream", "format": "hex", "data": "not valid hex ZZ"}`
	req := httptest.NewRequest(http.MethodPost, "/api/inject", strings.NewReader(body))
	w := httptest.NewRecorder()

	webServer.handleInject(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}
}

func TestHandleInject_HexWithSpaces(t *testing.T) {
	// Start mock upstream
	upstreamListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start mock upstream: %v", err)
	}
	defer upstreamListener.Close()

	go func() {
		conn, err := upstreamListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		time.Sleep(5 * time.Second)
	}()

	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: upstreamListener.Addr().(*net.TCPAddr).Port,
		ListenPort:   0,
		MaxClients:   10,
		WebPort:      18080,
	}

	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}
	cfg.ListenPort = proxyListener.Addr().(*net.TCPAddr).Port
	proxyListener.Close()

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)

	err = p.Start()
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer p.Stop()

	time.Sleep(200 * time.Millisecond)

	webServer := NewServer(cfg, p, log)

	// Hex with spaces and 0x prefix
	body := `{"target": "upstream", "format": "hex", "data": "0x48 45 4c 4c 4f"}`
	req := httptest.NewRequest(http.MethodPost, "/api/inject", strings.NewReader(body))
	w := httptest.NewRecorder()

	webServer.handleInject(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Errorf("Expected status 200, got %d: %s", resp.StatusCode, string(bodyBytes))
	}
}

func TestHandleInject_ASCII(t *testing.T) {
	// Start mock upstream
	upstreamListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start mock upstream: %v", err)
	}
	defer upstreamListener.Close()

	go func() {
		conn, err := upstreamListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		time.Sleep(5 * time.Second)
	}()

	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: upstreamListener.Addr().(*net.TCPAddr).Port,
		ListenPort:   0,
		MaxClients:   10,
		WebPort:      18080,
	}

	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}
	cfg.ListenPort = proxyListener.Addr().(*net.TCPAddr).Port
	proxyListener.Close()

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)

	err = p.Start()
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer p.Stop()

	time.Sleep(200 * time.Millisecond)

	webServer := NewServer(cfg, p, log)

	body := `{"target": "upstream", "format": "ascii", "data": "Hello World"}`
	req := httptest.NewRequest(http.MethodPost, "/api/inject", strings.NewReader(body))
	w := httptest.NewRecorder()

	webServer.handleInject(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Errorf("Expected status 200, got %d: %s", resp.StatusCode, string(bodyBytes))
	}
}

func TestHandleInject_NoUpstream(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost: "192.168.255.255",
		UpstreamPort: 9999,
		ListenPort:   0,
		MaxClients:   10,
		WebPort:      18080,
	}

	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}
	cfg.ListenPort = proxyListener.Addr().(*net.TCPAddr).Port
	proxyListener.Close()

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)

	err = p.Start()
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer p.Stop()

	webServer := NewServer(cfg, p, log)

	body := `{"target": "upstream", "format": "ascii", "data": "test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/inject", strings.NewReader(body))
	w := httptest.NewRecorder()

	webServer.handleInject(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	// Should fail because upstream is not connected
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("Expected status 500 (no upstream), got %d", resp.StatusCode)
	}
}

func TestBroadcastLog(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: 8899,
		ListenPort:   18899,
		MaxClients:   10,
		WebPort:      18080,
	}

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)
	webServer := NewServer(cfg, p, log)

	// Create a client channel and register it
	clientChan := make(chan string, 10)
	webServer.clientsMu.Lock()
	webServer.clients[clientChan] = true
	webServer.clientsMu.Unlock()

	// Broadcast a message
	webServer.broadcastLog("test message")

	// Check if client received message
	select {
	case msg := <-clientChan:
		if msg != "test message" {
			t.Errorf("Expected 'test message', got '%s'", msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for broadcast message")
	}

	// Check if message is in buffer
	webServer.logBufferMu.Lock()
	found := false
	for _, m := range webServer.logBuffer {
		if m == "test message" {
			found = true
			break
		}
	}
	webServer.logBufferMu.Unlock()

	if !found {
		t.Error("Message not found in log buffer")
	}

	// Clean up
	webServer.clientsMu.Lock()
	delete(webServer.clients, clientChan)
	webServer.clientsMu.Unlock()
	close(clientChan)
}

func TestBroadcastLog_BufferLimit(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: 8899,
		ListenPort:   18899,
		MaxClients:   10,
		WebPort:      18080,
	}

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)
	webServer := NewServer(cfg, p, log)

	// Fill buffer beyond limit
	for i := 0; i < 1005; i++ {
		webServer.broadcastLog("message")
	}

	webServer.logBufferMu.Lock()
	bufferLen := len(webServer.logBuffer)
	webServer.logBufferMu.Unlock()

	if bufferLen > 1000 {
		t.Errorf("Expected buffer len <= 1000, got %d", bufferLen)
	}
}

func TestBroadcastLog_SlowClient(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: 8899,
		ListenPort:   18899,
		MaxClients:   10,
		WebPort:      18080,
	}

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)
	webServer := NewServer(cfg, p, log)

	// Create a slow client (buffer size 1)
	slowClient := make(chan string, 1)
	webServer.clientsMu.Lock()
	webServer.clients[slowClient] = true
	webServer.clientsMu.Unlock()

	// Fill the channel
	slowClient <- "existing"

	// This should not block even though client is full
	done := make(chan bool)
	go func() {
		webServer.broadcastLog("new message")
		done <- true
	}()

	select {
	case <-done:
		// Good, broadcast didn't block
	case <-time.After(100 * time.Millisecond):
		t.Error("BroadcastLog blocked on slow client")
	}

	// Clean up
	webServer.clientsMu.Lock()
	delete(webServer.clients, slowClient)
	webServer.clientsMu.Unlock()
	close(slowClient)
}

func TestSetVersion(t *testing.T) {
	originalVersion := Version
	defer func() { Version = originalVersion }()

	SetVersion("1.2.3")
	if Version != "1.2.3" {
		t.Errorf("Expected version '1.2.3', got '%s'", Version)
	}

	SetVersion("v2.0.0-beta")
	if Version != "v2.0.0-beta" {
		t.Errorf("Expected version 'v2.0.0-beta', got '%s'", Version)
	}
}

func TestServerStartStop(t *testing.T) {
	// Start mock upstream
	upstreamListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start mock upstream: %v", err)
	}
	defer upstreamListener.Close()

	go func() {
		for {
			conn, err := upstreamListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				time.Sleep(5 * time.Second)
			}(conn)
		}
	}()

	// Get free port for web server
	webListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to get free port for web: %v", err)
	}
	webPort := webListener.Addr().(*net.TCPAddr).Port
	webListener.Close()

	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: upstreamListener.Addr().(*net.TCPAddr).Port,
		ListenPort:   0,
		MaxClients:   10,
		WebPort:      webPort,
	}

	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}
	cfg.ListenPort = proxyListener.Addr().(*net.TCPAddr).Port
	proxyListener.Close()

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)

	err = p.Start()
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer p.Stop()

	webServer := NewServer(cfg, p, log)

	// Start web server
	err = webServer.Start()
	if err != nil {
		t.Fatalf("Failed to start web server: %v", err)
	}

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Verify server is accessible
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/health", webPort))
	if err != nil {
		t.Fatalf("Failed to access web server: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Stop web server
	webServer.Stop()

	// Give server time to stop
	time.Sleep(100 * time.Millisecond)

	// Verify server is no longer accessible
	_, err = http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/health", webPort))
	if err == nil {
		t.Error("Expected error after server stop, but got none")
	}
}

func TestServerStop_NilServer(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: 8899,
		ListenPort:   18899,
		MaxClients:   10,
		WebPort:      18080,
	}

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)
	webServer := NewServer(cfg, p, log)

	// Stop without Start should not panic
	webServer.Stop()
}

type noFlusher struct {
	http.ResponseWriter
}

func TestHandleEvents_NoFlusher(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: 8899,
		ListenPort:   18899,
		MaxClients:   10,
		WebPort:      18080,
	}

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)
	webServer := NewServer(cfg, p, log)

	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	w := &noFlusher{httptest.NewRecorder()}

	webServer.handleEvents(w, req)

	// Check response (should be error because no Flusher support)
	recorder := w.ResponseWriter.(*httptest.ResponseRecorder)
	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", recorder.Code)
	}
}

type mockFlusher struct {
	*httptest.ResponseRecorder
	flushed int
}

func (m *mockFlusher) Flush() {
	m.flushed++
}

func TestHandleEvents_SSE(t *testing.T) {
	// Start mock upstream
	upstreamListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start mock upstream: %v", err)
	}
	defer upstreamListener.Close()

	go func() {
		conn, err := upstreamListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		time.Sleep(5 * time.Second)
	}()

	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: upstreamListener.Addr().(*net.TCPAddr).Port,
		ListenPort:   0,
		MaxClients:   10,
		WebPort:      18080,
	}

	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}
	cfg.ListenPort = proxyListener.Addr().(*net.TCPAddr).Port
	proxyListener.Close()

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)

	err = p.Start()
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer p.Stop()

	time.Sleep(200 * time.Millisecond)

	webServer := NewServer(cfg, p, log)

	// Add some log messages to buffer
	webServer.broadcastLog("buffered message 1")
	webServer.broadcastLog("buffered message 2")

	// Create a context that can be cancelled
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
	w := &mockFlusher{ResponseRecorder: httptest.NewRecorder()}

	// Run handleEvents in a goroutine
	done := make(chan bool)
	go func() {
		webServer.handleEvents(w, req)
		done <- true
	}()

	// Give it some time to process
	time.Sleep(100 * time.Millisecond)

	// Cancel the context to stop the handler
	cancel()

	select {
	case <-done:
		// Good
	case <-time.After(1 * time.Second):
		t.Fatal("handleEvents didn't return after context cancel")
	}

	// Check headers
	resp := w.Result()
	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("Expected Content-Type 'text/event-stream', got '%s'", resp.Header.Get("Content-Type"))
	}
	if resp.Header.Get("Cache-Control") != "no-cache, no-store, must-revalidate" {
		t.Errorf("Expected Cache-Control 'no-cache, no-store, must-revalidate', got '%s'", resp.Header.Get("Cache-Control"))
	}
	if resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Errorf("Expected X-Accel-Buffering 'no', got '%s'", resp.Header.Get("X-Accel-Buffering"))
	}

	// Check that Flush was called
	if w.flushed == 0 {
		t.Error("Expected Flush to be called")
	}

	// Check response body contains SSE events
	body := w.Body.String()
	if !strings.Contains(body, "event: status") {
		t.Error("Expected 'event: status' in response")
	}
	if !strings.Contains(body, "event: log") {
		t.Error("Expected 'event: log' in response")
	}
	if !strings.Contains(body, "buffered message 1") {
		t.Error("Expected buffered message 1 in response")
	}
}

func TestHandleEvents_ClientRegistration(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost: "192.168.255.255",
		UpstreamPort: 9999,
		ListenPort:   0,
		MaxClients:   10,
		WebPort:      18080,
	}

	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}
	cfg.ListenPort = proxyListener.Addr().(*net.TCPAddr).Port
	proxyListener.Close()

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)

	err = p.Start()
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer p.Stop()

	webServer := NewServer(cfg, p, log)

	// Check initial client count
	webServer.clientsMu.Lock()
	initialCount := len(webServer.clients)
	webServer.clientsMu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
	w := &mockFlusher{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan bool)
	go func() {
		webServer.handleEvents(w, req)
		done <- true
	}()

	// Give handler time to register
	time.Sleep(50 * time.Millisecond)

	// Check client was registered
	webServer.clientsMu.Lock()
	afterStartCount := len(webServer.clients)
	webServer.clientsMu.Unlock()

	if afterStartCount != initialCount+1 {
		t.Errorf("Expected client count %d, got %d", initialCount+1, afterStartCount)
	}

	// Cancel to stop handler
	cancel()

	<-done

	// Check client was unregistered
	webServer.clientsMu.Lock()
	afterStopCount := len(webServer.clients)
	webServer.clientsMu.Unlock()

	if afterStopCount != initialCount {
		t.Errorf("Expected client count %d after stop, got %d", initialCount, afterStopCount)
	}
}

func TestHandleInject_HexWithNewlines(t *testing.T) {
	// Start mock upstream
	upstreamListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start mock upstream: %v", err)
	}
	defer upstreamListener.Close()

	go func() {
		conn, err := upstreamListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		time.Sleep(5 * time.Second)
	}()

	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: upstreamListener.Addr().(*net.TCPAddr).Port,
		ListenPort:   0,
		MaxClients:   10,
		WebPort:      18080,
	}

	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}
	cfg.ListenPort = proxyListener.Addr().(*net.TCPAddr).Port
	proxyListener.Close()

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)

	err = p.Start()
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer p.Stop()

	time.Sleep(200 * time.Millisecond)

	webServer := NewServer(cfg, p, log)

	// Hex with newlines
	body := `{"target": "upstream", "format": "hex", "data": "48454C4C4F\n574F524C44"}`
	req := httptest.NewRequest(http.MethodPost, "/api/inject", strings.NewReader(body))
	w := httptest.NewRecorder()

	webServer.handleInject(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Errorf("Expected status 200, got %d: %s", resp.StatusCode, string(bodyBytes))
	}
}

func TestHealthEndpoint_Unhealthy(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost: "192.168.255.255",
		UpstreamPort: 9999,
		ListenPort:   0,
		MaxClients:   10,
		LogPackets:   false,
		WebPort:      18080,
	}

	// Don't get a free port - use invalid one
	cfg.ListenPort = 99999 // Invalid port

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)

	// Don't start proxy - it won't be listening
	webServer := NewServer(cfg, p, log)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()

	webServer.handleHealth(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	// Should return 503 when not listening
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503, got %d", resp.StatusCode)
	}

	var health HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if health.Status != HealthStatusUnhealthy {
		t.Errorf("Expected status 'unhealthy', got '%s'", health.Status)
	}
}

func TestNewServer(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: 8899,
		ListenPort:   18899,
		MaxClients:   10,
		WebPort:      18080,
	}

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)
	webServer := NewServer(cfg, p, log)

	if webServer.config != cfg {
		t.Error("Config not set correctly")
	}
	if webServer.proxy != p {
		t.Error("Proxy not set correctly")
	}
	if webServer.logger != log {
		t.Error("Logger not set correctly")
	}
	if webServer.clients == nil {
		t.Error("Clients map not initialized")
	}
	if webServer.logBuffer == nil {
		t.Error("Log buffer not initialized")
	}
}

func TestHandleInject_Downstream(t *testing.T) {
	// Start mock upstream
	upstreamListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start mock upstream: %v", err)
	}
	defer upstreamListener.Close()

	go func() {
		for {
			conn, err := upstreamListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				time.Sleep(5 * time.Second)
			}(conn)
		}
	}()

	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: upstreamListener.Addr().(*net.TCPAddr).Port,
		ListenPort:   0,
		MaxClients:   10,
		WebPort:      18080,
	}

	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}
	proxyAddr := proxyListener.Addr().String()
	cfg.ListenPort = proxyListener.Addr().(*net.TCPAddr).Port
	proxyListener.Close()

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)

	err = p.Start()
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer p.Stop()

	time.Sleep(200 * time.Millisecond)

	// Connect a client to receive downstream data
	client, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("Failed to connect client: %v", err)
	}
	defer client.Close()

	time.Sleep(100 * time.Millisecond)

	webServer := NewServer(cfg, p, log)

	body := `{"target": "downstream", "format": "ascii", "data": "Hello Client"}`
	req := httptest.NewRequest(http.MethodPost, "/api/inject", strings.NewReader(body))
	w := httptest.NewRecorder()

	webServer.handleInject(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Errorf("Expected status 200, got %d: %s", resp.StatusCode, string(bodyBytes))
	}
}

func TestHandleClients(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: 8899,
		ListenPort:   18899,
		MaxClients:   10,
		WebPort:      18080,
	}

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)
	webServer := NewServer(cfg, p, log)

	// Test GET request
	req := httptest.NewRequest(http.MethodGet, "/api/clients", nil)
	w := httptest.NewRecorder()

	webServer.handleClients(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result ClientsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result.MaxClients != 10 {
		t.Errorf("Expected MaxClients 10, got %d", result.MaxClients)
	}
}

func TestHandleClients_MethodNotAllowed(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: 8899,
		ListenPort:   18899,
		MaxClients:   10,
		WebPort:      18080,
	}

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)
	webServer := NewServer(cfg, p, log)

	req := httptest.NewRequest(http.MethodPost, "/api/clients", nil)
	w := httptest.NewRecorder()

	webServer.handleClients(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", resp.StatusCode)
	}
}

func TestHandleDisconnectClient_InvalidJSON(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: 8899,
		ListenPort:   18899,
		MaxClients:   10,
		WebPort:      18080,
	}

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)
	webServer := NewServer(cfg, p, log)

	req := httptest.NewRequest(http.MethodPost, "/api/clients/disconnect", strings.NewReader("invalid json"))
	w := httptest.NewRecorder()

	webServer.handleDisconnectClient(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}
}

func TestHandleDisconnectClient_MissingClientID(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: 8899,
		ListenPort:   18899,
		MaxClients:   10,
		WebPort:      18080,
	}

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)
	webServer := NewServer(cfg, p, log)

	req := httptest.NewRequest(http.MethodPost, "/api/clients/disconnect", strings.NewReader(`{"client_id": ""}`))
	w := httptest.NewRecorder()

	webServer.handleDisconnectClient(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}
}

func TestHandleDisconnectClient_NotFound(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: 8899,
		ListenPort:   18899,
		MaxClients:   10,
		WebPort:      18080,
	}

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)
	webServer := NewServer(cfg, p, log)

	req := httptest.NewRequest(http.MethodPost, "/api/clients/disconnect", strings.NewReader(`{"client_id": "client#999"}`))
	w := httptest.NewRecorder()

	webServer.handleDisconnectClient(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", resp.StatusCode)
	}
}

func TestDisconnectWebClient_NotFound(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: 8899,
		ListenPort:   18899,
		MaxClients:   10,
		WebPort:      18080,
	}

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)
	webServer := NewServer(cfg, p, log)

	result := webServer.disconnectWebClient("web#999")
	if result {
		t.Error("Expected false for non-existent web client")
	}
}

func TestRemoveWebClient_NegativeProtection(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: 8899,
		ListenPort:   18899,
		MaxClients:   10,
		WebPort:      18080,
	}

	log := newTestLogger()
	p := proxy.NewServer(cfg, log)

	// Call RemoveWebClient without any AddWebClient
	// Should not panic and count should stay at 0
	p.RemoveWebClient()
	p.RemoveWebClient()

	count := p.GetWebClientCount()
	if count < 0 {
		t.Errorf("Web client count went negative: %d", count)
	}
}
