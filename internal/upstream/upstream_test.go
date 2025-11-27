package upstream

import (
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/hoon-ch/serial-tcp-proxy/internal/logger"
)

func newTestLogger() *logger.Logger {
	log, _ := logger.New(false, "")
	log.SetOutput(io.Discard)
	return log
}

func TestConnection_State(t *testing.T) {
	log := newTestLogger()
	conn := NewConnection("127.0.0.1:19999", log, nil)

	if conn.GetState() != StateDisconnected {
		t.Errorf("Expected initial state=Disconnected, got %s", conn.GetState())
	}

	if conn.IsConnected() {
		t.Error("Expected IsConnected=false initially")
	}
}

func TestConnectionState_String(t *testing.T) {
	tests := []struct {
		state    ConnectionState
		expected string
	}{
		{StateDisconnected, "Disconnected"},
		{StateConnecting, "Connecting"},
		{StateConnected, "Connected"},
		{StateStopped, "Stopped"},
	}

	for _, tt := range tests {
		if tt.state.String() != tt.expected {
			t.Errorf("Expected %s, got %s", tt.expected, tt.state.String())
		}
	}
}

func TestConnection_SetState(t *testing.T) {
	log := newTestLogger()
	conn := NewConnection("127.0.0.1:19999", log, nil)

	conn.setState(StateConnecting)
	if conn.GetState() != StateConnecting {
		t.Errorf("Expected state=Connecting, got %s", conn.GetState())
	}

	conn.setState(StateConnected)
	if conn.GetState() != StateConnected {
		t.Errorf("Expected state=Connected, got %s", conn.GetState())
	}

	if !conn.IsConnected() {
		t.Error("Expected IsConnected=true")
	}
}

func TestConnection_ConnectAndReceive(t *testing.T) {
	// Start mock upstream server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start mock server: %v", err)
	}
	defer listener.Close()

	// Track received data
	var receivedData []byte
	var mu sync.Mutex
	onData := func(data []byte) {
		mu.Lock()
		receivedData = append(receivedData, data...)
		mu.Unlock()
	}

	log := newTestLogger()
	conn := NewConnection(listener.Addr().String(), log, onData)

	// Accept and send data in goroutine
	go func() {
		c, err := listener.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = c.Write([]byte{0xf7, 0x0e, 0x1f})
		time.Sleep(100 * time.Millisecond)
	}()

	conn.Start()
	defer conn.Stop()

	// Wait for connection and data
	time.Sleep(200 * time.Millisecond)

	if !conn.IsConnected() {
		t.Error("Expected connection to be established")
	}

	mu.Lock()
	if len(receivedData) == 0 {
		t.Error("Expected to receive data")
	}
	mu.Unlock()
}

func TestConnection_Reconnect(t *testing.T) {
	// Start mock upstream server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start mock server: %v", err)
	}
	defer listener.Close()
	addr := listener.Addr().String()

	log := newTestLogger()
	conn := NewConnection(addr, log, nil)

	// Accept first connection then close it
	var serverConn net.Conn
	var mu sync.Mutex
	connReady := make(chan struct{})
	go func() {
		c, _ := listener.Accept()
		mu.Lock()
		serverConn = c
		mu.Unlock()
		close(connReady)
	}()

	conn.Start()
	defer conn.Stop()

	// Wait for first connection from server side
	select {
	case <-connReady:
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for first connection")
	}

	// Wait for client side to be connected
	for i := 0; i < 20; i++ {
		if conn.IsConnected() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !conn.IsConnected() {
		t.Error("Expected first connection to be established")
	}

	// Close server connection to trigger reconnect
	mu.Lock()
	if serverConn != nil {
		serverConn.Close()
	}
	mu.Unlock()

	// Wait for disconnect detection and reconnect attempt
	time.Sleep(200 * time.Millisecond)

	// Accept reconnection
	reconnectReady := make(chan struct{})
	go func() {
		_, _ = listener.Accept()
		close(reconnectReady)
	}()

	// Wait for reconnection (backoff is 1 second minimum)
	select {
	case <-reconnectReady:
	case <-time.After(3 * time.Second):
		t.Fatal("Timeout waiting for reconnection")
	}

	// Wait for client state to update
	for i := 0; i < 20; i++ {
		if conn.IsConnected() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !conn.IsConnected() {
		t.Error("Expected reconnection to be established")
	}
}

func TestConnection_Write(t *testing.T) {
	// Start mock upstream server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start mock server: %v", err)
	}
	defer listener.Close()

	var receivedData []byte
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		c, err := listener.Accept()
		if err != nil {
			return
		}
		defer c.Close()

		buf := make([]byte, 1024)
		_ = c.SetReadDeadline(time.Now().Add(time.Second))
		n, _ := c.Read(buf)
		receivedData = buf[:n]
	}()

	log := newTestLogger()
	conn := NewConnection(listener.Addr().String(), log, nil)
	conn.Start()
	defer conn.Stop()

	time.Sleep(100 * time.Millisecond)

	// Write data
	testData := []byte{0xf7, 0x12, 0x01}
	err = conn.Write(testData)
	if err != nil {
		t.Errorf("Write failed: %v", err)
	}

	wg.Wait()

	if string(receivedData) != string(testData) {
		t.Errorf("Expected %x, got %x", testData, receivedData)
	}
}

func TestConnection_WriteWhenDisconnected(t *testing.T) {
	log := newTestLogger()
	conn := NewConnection("127.0.0.1:19999", log, nil)

	// Try to write without starting (not connected)
	err := conn.Write([]byte{0xf7})
	if err == nil {
		t.Error("Expected error when writing to disconnected connection")
	}
}

func TestConnection_Stop(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start mock server: %v", err)
	}
	defer listener.Close()

	go func() {
		c, _ := listener.Accept()
		if c != nil {
			// Keep connection open
			time.Sleep(5 * time.Second)
			c.Close()
		}
	}()

	log := newTestLogger()
	conn := NewConnection(listener.Addr().String(), log, nil)
	conn.Start()

	time.Sleep(100 * time.Millisecond)

	if !conn.IsConnected() {
		t.Error("Expected connection to be established")
	}

	// Stop should complete gracefully
	done := make(chan struct{})
	go func() {
		conn.Stop()
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Error("Stop() did not complete in time")
	}

	if conn.GetState() != StateStopped {
		t.Errorf("Expected state=Stopped, got %s", conn.GetState())
	}
}
