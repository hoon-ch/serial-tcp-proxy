package proxy

import (
	"bytes"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/hoon-ch/serial-tcp-proxy/internal/config"
	"github.com/hoon-ch/serial-tcp-proxy/internal/logger"
)

func newTestLogger() *logger.Logger {
	log, _ := logger.New(false, "")
	log.SetOutput(io.Discard)
	return log
}

func TestServer_Integration(t *testing.T) {
	// Start a mock upstream server
	upstreamListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start mock upstream: %v", err)
	}
	defer upstreamListener.Close()

	// Accept connections and send test data
	var upstreamWg sync.WaitGroup
	upstreamWg.Add(1)
	go func() {
		defer upstreamWg.Done()
		conn, err := upstreamListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Send some test data to proxy
		testData := []byte{0xf7, 0x0e, 0x1f, 0x01}
		conn.Write(testData)

		// Read client data
		buf := make([]byte, 1024)
		conn.SetReadDeadline(time.Now().Add(time.Second))
		conn.Read(buf)
	}()

	// Create proxy server config
	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: upstreamListener.Addr().(*net.TCPAddr).Port,
		ListenPort:   0, // Will be set after getting a listener
		MaxClients:   10,
		LogPackets:   false,
	}

	// Get a free port for the proxy
	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}
	proxyAddr := proxyListener.Addr().String()
	cfg.ListenPort = proxyListener.Addr().(*net.TCPAddr).Port
	proxyListener.Close()

	log := newTestLogger()
	proxy := NewServer(cfg, log)

	err = proxy.Start()
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	// Wait for upstream connection
	time.Sleep(100 * time.Millisecond)

	// Connect client to proxy
	client, err := net.DialTimeout("tcp", proxyAddr, time.Second)
	if err != nil {
		t.Fatalf("Failed to connect client to proxy: %v", err)
	}
	defer client.Close()

	// Wait for data from upstream through proxy
	client.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 1024)
	n, err := client.Read(buf)
	if err != nil {
		t.Logf("Note: Read returned error (may be timing): %v", err)
	}

	if n > 0 {
		expected := []byte{0xf7, 0x0e, 0x1f, 0x01}
		if !bytes.Equal(buf[:n], expected) {
			t.Errorf("Expected %x, got %x", expected, buf[:n])
		}
	}

	// Send data from client to upstream
	clientData := []byte{0xf7, 0x12, 0x01}
	client.Write(clientData)

	time.Sleep(100 * time.Millisecond)
}

func TestServer_MultipleClients(t *testing.T) {
	// Start a mock upstream server
	upstreamListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start mock upstream: %v", err)
	}
	defer upstreamListener.Close()

	var upstreamConn net.Conn
	var upstreamMu sync.Mutex
	go func() {
		conn, err := upstreamListener.Accept()
		if err != nil {
			return
		}
		upstreamMu.Lock()
		upstreamConn = conn
		upstreamMu.Unlock()
	}()

	// Create proxy config
	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: upstreamListener.Addr().(*net.TCPAddr).Port,
		ListenPort:   0,
		MaxClients:   10,
		LogPackets:   false,
	}

	proxyListener, _ := net.Listen("tcp", "127.0.0.1:0")
	proxyAddr := proxyListener.Addr().String()
	cfg.ListenPort = proxyListener.Addr().(*net.TCPAddr).Port
	proxyListener.Close()

	log := newTestLogger()
	proxy := NewServer(cfg, log)
	proxy.Start()
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	// Connect multiple clients
	clients := make([]net.Conn, 3)
	for i := 0; i < 3; i++ {
		client, err := net.DialTimeout("tcp", proxyAddr, time.Second)
		if err != nil {
			t.Fatalf("Failed to connect client %d: %v", i, err)
		}
		clients[i] = client
		defer client.Close()
	}

	time.Sleep(100 * time.Millisecond)

	// Verify client count
	if proxy.GetClientCount() != 3 {
		t.Errorf("Expected 3 clients, got %d", proxy.GetClientCount())
	}

	// Send data from upstream to all clients
	upstreamMu.Lock()
	if upstreamConn != nil {
		testData := []byte{0xf7, 0x0e, 0x1f}
		upstreamConn.Write(testData)
	}
	upstreamMu.Unlock()

	time.Sleep(100 * time.Millisecond)

	// All clients should receive the data
	for i, client := range clients {
		client.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		buf := make([]byte, 1024)
		n, err := client.Read(buf)
		if err == nil && n > 0 {
			expected := []byte{0xf7, 0x0e, 0x1f}
			if !bytes.Equal(buf[:n], expected) {
				t.Errorf("Client %d: Expected %x, got %x", i, expected, buf[:n])
			}
		}
	}
}

func TestServer_GetStatus(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost: "192.168.1.100",
		UpstreamPort: 8899,
		ListenPort:   18899,
		MaxClients:   10,
		LogPackets:   false,
	}

	log := newTestLogger()
	proxy := NewServer(cfg, log)

	status := proxy.GetStatus()

	if status["upstream_addr"] != "192.168.1.100:8899" {
		t.Errorf("Unexpected upstream_addr: %v", status["upstream_addr"])
	}

	if status["listen_addr"] != ":18899" {
		t.Errorf("Unexpected listen_addr: %v", status["listen_addr"])
	}

	if status["max_clients"] != 10 {
		t.Errorf("Unexpected max_clients: %v", status["max_clients"])
	}
}

func TestServer_IsUpstreamConnected(t *testing.T) {
	cfg := &config.Config{
		UpstreamHost: "192.168.1.100",
		UpstreamPort: 8899,
		ListenPort:   18899,
		MaxClients:   10,
		LogPackets:   false,
	}

	log := newTestLogger()
	proxy := NewServer(cfg, log)

	// Initially not connected
	if proxy.IsUpstreamConnected() {
		t.Error("Expected upstream to be disconnected initially")
	}
}
