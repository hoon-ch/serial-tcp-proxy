package upstream

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/hoon-ch/serial-tcp-proxy/internal/logger"
)

// Buffer pool for zero-copy packet forwarding
var bufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 4096)
		return &buf
	},
}

type ConnectionState int

const (
	StateDisconnected ConnectionState = iota
	StateConnecting
	StateConnected
	StateStopped
)

func (s ConnectionState) String() string {
	switch s {
	case StateDisconnected:
		return "Disconnected"
	case StateConnecting:
		return "Connecting"
	case StateConnected:
		return "Connected"
	case StateStopped:
		return "Stopped"
	default:
		return "Unknown"
	}
}

type Connection struct {
	addr    string
	conn    net.Conn
	connMu  sync.RWMutex
	writeMu sync.Mutex
	state   ConnectionState
	stateMu sync.RWMutex
	logger  *logger.Logger
	onData  func([]byte)
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

func NewConnection(addr string, log *logger.Logger, onData func([]byte)) *Connection {
	ctx, cancel := context.WithCancel(context.Background())
	return &Connection{
		addr:   addr,
		logger: log,
		onData: onData,
		ctx:    ctx,
		cancel: cancel,
		state:  StateDisconnected,
	}
}

func (u *Connection) setState(state ConnectionState) {
	u.stateMu.Lock()
	u.state = state
	u.stateMu.Unlock()
}

func (u *Connection) GetState() ConnectionState {
	u.stateMu.RLock()
	defer u.stateMu.RUnlock()
	return u.state
}

func (u *Connection) IsConnected() bool {
	return u.GetState() == StateConnected
}

func (u *Connection) Start() {
	u.wg.Add(1)
	go u.connectionLoop()
}

func (u *Connection) Stop() {
	u.setState(StateStopped)
	u.cancel()

	u.connMu.Lock()
	if u.conn != nil {
		u.conn.Close()
	}
	u.connMu.Unlock()

	u.wg.Wait()
	u.logger.Info("Upstream connection stopped")
}

func (u *Connection) connectionLoop() {
	defer u.wg.Done()

	backoff := time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-u.ctx.Done():
			return
		default:
		}

		if u.GetState() == StateStopped {
			return
		}

		u.setState(StateConnecting)
		u.logger.Info("Connecting to upstream %s", u.addr)

		conn, err := net.DialTimeout("tcp", u.addr, 10*time.Second)
		if err != nil {
			u.logger.Error("Failed to connect to upstream: %v", err)
			u.setState(StateDisconnected)

			select {
			case <-u.ctx.Done():
				return
			case <-time.After(backoff):
				backoff = min(backoff*2, maxBackoff)
				continue
			}
		}

		// Reset backoff on successful connection
		backoff = time.Second

		u.connMu.Lock()
		u.conn = conn
		u.connMu.Unlock()
		u.setState(StateConnected)

		u.logger.Info("Connected to upstream %s", u.addr)

		// Read loop
		u.readLoop(conn)

		// Connection lost
		u.connMu.Lock()
		u.conn = nil
		u.connMu.Unlock()

		if u.GetState() != StateStopped {
			u.setState(StateDisconnected)
			u.logger.Warn("Upstream connection lost, reconnecting...")
		}
	}
}

func (u *Connection) readLoop(conn net.Conn) {
	// Get buffer from pool for zero-copy
	bufPtr := bufferPool.Get().(*[]byte)
	buf := *bufPtr
	defer bufferPool.Put(bufPtr)

	for {
		select {
		case <-u.ctx.Done():
			return
		default:
		}

		_ = conn.SetReadDeadline(time.Now().Add(time.Minute))
		n, err := conn.Read(buf)
		if err != nil {
			if u.GetState() != StateStopped {
				u.logger.Warn("Upstream read error: %v", err)
			}
			return
		}

		if n > 0 {
			// Create a copy for the callback since buffer will be reused
			data := make([]byte, n)
			copy(data, buf[:n])

			if u.onData != nil {
				u.onData(data)
			}
		}
	}
}

func (u *Connection) Write(data []byte) error {
	u.writeMu.Lock()
	defer u.writeMu.Unlock()

	u.connMu.RLock()
	conn := u.conn
	u.connMu.RUnlock()

	if conn == nil {
		return net.ErrClosed
	}

	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := conn.Write(data)
	_ = conn.SetWriteDeadline(time.Time{})

	return err
}
