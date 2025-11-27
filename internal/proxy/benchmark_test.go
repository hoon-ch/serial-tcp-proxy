package proxy

import (
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/hoon-ch/serial-tcp-proxy/internal/config"
	"github.com/hoon-ch/serial-tcp-proxy/internal/logger"
)

func newBenchLogger() *logger.Logger {
	log, _ := logger.New(false, "")
	log.SetOutput(io.Discard)
	return log
}

// getFreePort returns an available port and its address
func getFreePort() (int, string) {
	listener, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := listener.Addr().String()
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port, addr
}

// BenchmarkLatency measures the latency of packet forwarding through the proxy
func BenchmarkLatency(b *testing.B) {
	// Start mock upstream server
	upstreamListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("Failed to start mock upstream: %v", err)
	}
	defer upstreamListener.Close()

	upstreamPort := upstreamListener.Addr().(*net.TCPAddr).Port

	// Accept upstream connections and echo back
	go func() {
		for {
			conn, err := upstreamListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					c.Write(buf[:n])
				}
			}(conn)
		}
	}()

	// Get a free port for proxy
	proxyPort, proxyAddr := getFreePort()

	// Create proxy server
	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: upstreamPort,
		ListenPort:   proxyPort,
		MaxClients:   10,
		LogPackets:   false,
	}

	log := newBenchLogger()
	server := NewServer(cfg, log)

	if err := server.Start(); err != nil {
		b.Fatalf("Failed to start proxy: %v", err)
	}
	defer server.Stop()

	// Wait for upstream connection
	time.Sleep(200 * time.Millisecond)

	// Connect client
	client, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		b.Fatalf("Failed to connect client: %v", err)
	}
	defer client.Close()

	time.Sleep(100 * time.Millisecond)

	testPacket := []byte{0xf7, 0x0e, 0x11, 0x41, 0x01, 0x00, 0x5f, 0x00}
	recvBuf := make([]byte, 1024)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		start := time.Now()

		// Send packet
		_, err := client.Write(testPacket)
		if err != nil {
			b.Fatalf("Write failed: %v", err)
		}

		// Receive echoed packet
		client.SetReadDeadline(time.Now().Add(time.Second))
		_, err = client.Read(recvBuf)
		if err != nil {
			b.Fatalf("Read failed: %v", err)
		}

		elapsed := time.Since(start)
		if elapsed > time.Millisecond {
			b.Logf("Latency exceeded 1ms: %v", elapsed)
		}
	}
}

// BenchmarkThroughput measures the throughput of the proxy
func BenchmarkThroughput(b *testing.B) {
	// Start mock upstream server
	upstreamListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("Failed to start mock upstream: %v", err)
	}
	defer upstreamListener.Close()

	upstreamPort := upstreamListener.Addr().(*net.TCPAddr).Port

	// Accept upstream connections and discard data
	go func() {
		for {
			conn, err := upstreamListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				for {
					_, err := c.Read(buf)
					if err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	proxyPort, proxyAddr := getFreePort()

	// Create proxy server
	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: upstreamPort,
		ListenPort:   proxyPort,
		MaxClients:   10,
		LogPackets:   false,
	}

	log := newBenchLogger()
	server := NewServer(cfg, log)

	if err := server.Start(); err != nil {
		b.Fatalf("Failed to start proxy: %v", err)
	}
	defer server.Stop()

	time.Sleep(200 * time.Millisecond)

	client, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		b.Fatalf("Failed to connect client: %v", err)
	}
	defer client.Close()

	time.Sleep(100 * time.Millisecond)

	testPacket := []byte{0xf7, 0x0e, 0x11, 0x41, 0x01, 0x00, 0x5f, 0x00}

	b.ResetTimer()
	b.SetBytes(int64(len(testPacket)))

	for i := 0; i < b.N; i++ {
		_, err := client.Write(testPacket)
		if err != nil {
			b.Fatalf("Write failed: %v", err)
		}
	}
}

// BenchmarkMultiClientBroadcast measures broadcast performance to multiple clients
func BenchmarkMultiClientBroadcast(b *testing.B) {
	clientCounts := []int{1, 5, 10}

	for _, numClients := range clientCounts {
		b.Run(fmt.Sprintf("%d_clients", numClients), func(b *testing.B) {
			benchmarkBroadcast(b, numClients)
		})
	}
}

func benchmarkBroadcast(b *testing.B, numClients int) {
	// Start mock upstream server that sends data
	upstreamListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("Failed to start mock upstream: %v", err)
	}
	defer upstreamListener.Close()

	upstreamPort := upstreamListener.Addr().(*net.TCPAddr).Port

	var upstreamConn net.Conn
	upstreamReady := make(chan struct{})

	go func() {
		conn, err := upstreamListener.Accept()
		if err != nil {
			return
		}
		upstreamConn = conn
		close(upstreamReady)
		// Keep connection open
		buf := make([]byte, 4096)
		for {
			_, err := conn.Read(buf)
			if err != nil {
				return
			}
		}
	}()

	proxyPort, proxyAddr := getFreePort()

	// Create proxy server
	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: upstreamPort,
		ListenPort:   proxyPort,
		MaxClients:   numClients + 1,
		LogPackets:   false,
	}

	log := newBenchLogger()
	server := NewServer(cfg, log)

	if err := server.Start(); err != nil {
		b.Fatalf("Failed to start proxy: %v", err)
	}
	defer server.Stop()

	// Wait for upstream connection
	select {
	case <-upstreamReady:
	case <-time.After(2 * time.Second):
		b.Fatal("Timeout waiting for upstream connection")
	}

	// Connect multiple clients
	clients := make([]net.Conn, numClients)
	for i := 0; i < numClients; i++ {
		client, err := net.Dial("tcp", proxyAddr)
		if err != nil {
			b.Fatalf("Failed to connect client %d: %v", i, err)
		}
		clients[i] = client
		defer client.Close()
	}

	time.Sleep(100 * time.Millisecond)

	testPacket := []byte{0xf7, 0x0e, 0x11, 0x41, 0x01, 0x00, 0x5f, 0x00}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Send from upstream
		_, err := upstreamConn.Write(testPacket)
		if err != nil {
			b.Fatalf("Upstream write failed: %v", err)
		}

		// Read from all clients
		var wg sync.WaitGroup
		for _, client := range clients {
			wg.Add(1)
			go func(c net.Conn) {
				defer wg.Done()
				buf := make([]byte, 1024)
				c.SetReadDeadline(time.Now().Add(time.Second))
				c.Read(buf)
			}(client)
		}
		wg.Wait()
	}
}

// BenchmarkMemoryAllocation measures memory allocations during packet forwarding
func BenchmarkMemoryAllocation(b *testing.B) {
	// Start mock upstream server
	upstreamListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("Failed to start mock upstream: %v", err)
	}
	defer upstreamListener.Close()

	upstreamPort := upstreamListener.Addr().(*net.TCPAddr).Port

	// Accept upstream connections and echo back
	go func() {
		for {
			conn, err := upstreamListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					c.Write(buf[:n])
				}
			}(conn)
		}
	}()

	proxyPort, proxyAddr := getFreePort()

	// Create proxy server
	cfg := &config.Config{
		UpstreamHost: "127.0.0.1",
		UpstreamPort: upstreamPort,
		ListenPort:   proxyPort,
		MaxClients:   10,
		LogPackets:   false,
	}

	log := newBenchLogger()
	server := NewServer(cfg, log)

	if err := server.Start(); err != nil {
		b.Fatalf("Failed to start proxy: %v", err)
	}
	defer server.Stop()

	time.Sleep(200 * time.Millisecond)

	client, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		b.Fatalf("Failed to connect client: %v", err)
	}
	defer client.Close()

	time.Sleep(100 * time.Millisecond)

	testPacket := []byte{0xf7, 0x0e, 0x11, 0x41, 0x01, 0x00, 0x5f, 0x00}
	recvBuf := make([]byte, 1024)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		client.Write(testPacket)
		client.SetReadDeadline(time.Now().Add(time.Second))
		client.Read(recvBuf)
	}
}

// BenchmarkBufferPool verifies buffer pool efficiency
func BenchmarkBufferPool(b *testing.B) {
	b.Run("WithPool", func(b *testing.B) {
		pool := sync.Pool{
			New: func() interface{} {
				buf := make([]byte, 4096)
				return &buf
			},
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			bufPtr := pool.Get().(*[]byte)
			_ = *bufPtr
			pool.Put(bufPtr)
		}
	})

	b.Run("WithoutPool", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			buf := make([]byte, 4096)
			_ = buf
		}
	})
}
