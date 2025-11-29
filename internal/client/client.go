package client

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hoon-ch/serial-tcp-proxy/internal/logger"
)

type Client struct {
	ID          string
	Conn        net.Conn
	Addr        string
	ConnectedAt time.Time
}

type Manager struct {
	clients      map[string]*Client
	mu           sync.RWMutex
	maxClients   int
	counter      atomic.Uint64
	webClients   atomic.Int32 // Count of web UI clients (SSE/WebSocket)
	logger       *logger.Logger
}

func NewManager(maxClients int, log *logger.Logger) *Manager {
	return &Manager{
		clients:    make(map[string]*Client),
		maxClients: maxClients,
		logger:     log,
	}
}

func (cm *Manager) Add(conn net.Conn) (*Client, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	totalClients := len(cm.clients) + int(cm.webClients.Load())
	if totalClients >= cm.maxClients {
		return nil, fmt.Errorf("max clients (%d) reached", cm.maxClients)
	}

	id := fmt.Sprintf("client#%d", cm.counter.Add(1))
	client := &Client{
		ID:          id,
		Conn:        conn,
		Addr:        conn.RemoteAddr().String(),
		ConnectedAt: time.Now(),
	}

	cm.clients[id] = client
	newTotal := len(cm.clients) + int(cm.webClients.Load())
	cm.logger.Info("Client connected: %s [%s] (total: %d)", client.Addr, id, newTotal)

	return client, nil
}

func (cm *Manager) Remove(id string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if client, ok := cm.clients[id]; ok {
		client.Conn.Close()
		delete(cm.clients, id)
		newTotal := len(cm.clients) + int(cm.webClients.Load())
		cm.logger.Info("Client disconnected: %s [%s] (total: %d)", client.Addr, id, newTotal)
	}
}

func (cm *Manager) Get(id string) *Client {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.clients[id]
}

func (cm *Manager) GetAll() []*Client {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	clients := make([]*Client, 0, len(cm.clients))
	for _, client := range cm.clients {
		clients = append(clients, client)
	}
	return clients
}

func (cm *Manager) Count() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.clients)
}

// TotalCount returns the total count of all clients (TCP + Web)
func (cm *Manager) TotalCount() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.clients) + int(cm.webClients.Load())
}

// AddWebClient increments the web client counter
// Returns error if max clients would be exceeded
func (cm *Manager) AddWebClient() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	totalClients := len(cm.clients) + int(cm.webClients.Load())
	if totalClients >= cm.maxClients {
		return fmt.Errorf("max clients (%d) reached", cm.maxClients)
	}

	cm.webClients.Add(1)
	newTotal := len(cm.clients) + int(cm.webClients.Load())
	cm.logger.Info("Web client connected (total: %d)", newTotal)
	return nil
}

// RemoveWebClient decrements the web client counter
func (cm *Manager) RemoveWebClient() {
	// Prevent negative count
	for {
		current := cm.webClients.Load()
		if current <= 0 {
			return
		}
		if cm.webClients.CompareAndSwap(current, current-1) {
			cm.logger.Info("Web client disconnected (total: %d)", cm.TotalCount())
			return
		}
	}
}

// WebClientCount returns the count of web clients
func (cm *Manager) WebClientCount() int {
	return int(cm.webClients.Load())
}

func (cm *Manager) Broadcast(data []byte) {
	cm.mu.RLock()
	clients := make([]*Client, 0, len(cm.clients))
	for _, c := range cm.clients {
		clients = append(clients, c)
	}
	cm.mu.RUnlock()

	var failedClients []string

	for _, client := range clients {
		// Set write deadline to prevent blocking on slow clients
		_ = client.Conn.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
		_, err := client.Conn.Write(data)
		_ = client.Conn.SetWriteDeadline(time.Time{})

		if err != nil {
			cm.logger.Warn("Failed to write to %s [%s]: %v", client.Addr, client.ID, err)
			failedClients = append(failedClients, client.ID)
		}
	}

	// Remove failed clients
	for _, id := range failedClients {
		cm.Remove(id)
	}
}

func (cm *Manager) CloseAll() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for id, client := range cm.clients {
		client.Conn.Close()
		delete(cm.clients, id)
	}
	cm.logger.Info("All clients disconnected")
}
