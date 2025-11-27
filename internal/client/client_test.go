package client

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"

	"github.com/hoon-ch/serial-tcp-proxy/internal/logger"
)

type mockConn struct {
	readBuf  *bytes.Buffer
	writeBuf *bytes.Buffer
	closed   bool
}

func newMockConn() *mockConn {
	return &mockConn{
		readBuf:  new(bytes.Buffer),
		writeBuf: new(bytes.Buffer),
	}
}

func (m *mockConn) Read(b []byte) (n int, err error) {
	return m.readBuf.Read(b)
}

func (m *mockConn) Write(b []byte) (n int, err error) {
	return m.writeBuf.Write(b)
}

func (m *mockConn) Close() error {
	m.closed = true
	return nil
}

func (m *mockConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 18899}
}

func (m *mockConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("192.168.1.10"), Port: 54321}
}

func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }

func newTestLogger() *logger.Logger {
	log, _ := logger.New(false, "")
	log.SetOutput(io.Discard)
	return log
}

func TestManager_Add(t *testing.T) {
	log := newTestLogger()
	cm := NewManager(10, log)

	conn := newMockConn()
	client, err := cm.Add(conn)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if client == nil {
		t.Fatal("Expected client to be returned")
	}

	if client.ID != "client#1" {
		t.Errorf("Expected client#1, got %s", client.ID)
	}

	if cm.Count() != 1 {
		t.Errorf("Expected count=1, got %d", cm.Count())
	}
}

func TestManager_AddMultiple(t *testing.T) {
	log := newTestLogger()
	cm := NewManager(10, log)

	for i := 0; i < 5; i++ {
		conn := newMockConn()
		_, err := cm.Add(conn)
		if err != nil {
			t.Fatalf("Unexpected error at iteration %d: %v", i, err)
		}
	}

	if cm.Count() != 5 {
		t.Errorf("Expected count=5, got %d", cm.Count())
	}
}

func TestManager_MaxClients(t *testing.T) {
	log := newTestLogger()
	cm := NewManager(2, log)

	// Add 2 clients (should succeed)
	for i := 0; i < 2; i++ {
		conn := newMockConn()
		_, err := cm.Add(conn)
		if err != nil {
			t.Fatalf("Unexpected error at iteration %d: %v", i, err)
		}
	}

	// Add 3rd client (should fail)
	conn := newMockConn()
	_, err := cm.Add(conn)
	if err == nil {
		t.Error("Expected error when max clients reached")
	}
}

func TestManager_Remove(t *testing.T) {
	log := newTestLogger()
	cm := NewManager(10, log)

	conn := newMockConn()
	client, _ := cm.Add(conn)

	cm.Remove(client.ID)

	if cm.Count() != 0 {
		t.Errorf("Expected count=0, got %d", cm.Count())
	}

	if !conn.closed {
		t.Error("Expected connection to be closed")
	}
}

func TestManager_Get(t *testing.T) {
	log := newTestLogger()
	cm := NewManager(10, log)

	conn := newMockConn()
	client, _ := cm.Add(conn)

	found := cm.Get(client.ID)
	if found == nil {
		t.Error("Expected to find client")
	}

	notFound := cm.Get("nonexistent")
	if notFound != nil {
		t.Error("Expected nil for nonexistent client")
	}
}

func TestManager_GetAll(t *testing.T) {
	log := newTestLogger()
	cm := NewManager(10, log)

	for i := 0; i < 3; i++ {
		conn := newMockConn()
		cm.Add(conn)
	}

	clients := cm.GetAll()
	if len(clients) != 3 {
		t.Errorf("Expected 3 clients, got %d", len(clients))
	}
}

func TestManager_Broadcast(t *testing.T) {
	log := newTestLogger()
	cm := NewManager(10, log)

	conns := make([]*mockConn, 3)
	for i := 0; i < 3; i++ {
		conns[i] = newMockConn()
		cm.Add(conns[i])
	}

	data := []byte{0xf7, 0x0e, 0x1f}
	cm.Broadcast(data)

	for i, conn := range conns {
		if !bytes.Equal(conn.writeBuf.Bytes(), data) {
			t.Errorf("Client %d did not receive broadcast data", i)
		}
	}
}

func TestManager_CloseAll(t *testing.T) {
	log := newTestLogger()
	cm := NewManager(10, log)

	conns := make([]*mockConn, 3)
	for i := 0; i < 3; i++ {
		conns[i] = newMockConn()
		cm.Add(conns[i])
	}

	cm.CloseAll()

	if cm.Count() != 0 {
		t.Errorf("Expected count=0, got %d", cm.Count())
	}

	for i, conn := range conns {
		if !conn.closed {
			t.Errorf("Client %d connection not closed", i)
		}
	}
}

func TestClient_Fields(t *testing.T) {
	conn := newMockConn()
	client := &Client{
		ID:          "client#1",
		Conn:        conn,
		Addr:        "192.168.1.10:54321",
		ConnectedAt: time.Now(),
	}

	if client.ID != "client#1" {
		t.Errorf("Expected ID=client#1, got %s", client.ID)
	}

	if client.Addr != "192.168.1.10:54321" {
		t.Errorf("Expected Addr=192.168.1.10:54321, got %s", client.Addr)
	}
}
