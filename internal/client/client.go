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
	clients    map[string]*Client
	mu         sync.RWMutex
	maxClients int
	counter    atomic.Uint64
	logger     *logger.Logger
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

	if len(cm.clients) >= cm.maxClients {
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
	cm.logger.Info("Client connected: %s [%s] (total: %d)", client.Addr, id, len(cm.clients))

	return client, nil
}

func (cm *Manager) Remove(id string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if client, ok := cm.clients[id]; ok {
		client.Conn.Close()
		delete(cm.clients, id)
		cm.logger.Info("Client disconnected: %s [%s] (total: %d)", client.Addr, id, len(cm.clients))
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
