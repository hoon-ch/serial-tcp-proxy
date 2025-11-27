package web

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
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
