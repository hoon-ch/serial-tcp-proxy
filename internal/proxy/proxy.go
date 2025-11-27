package proxy

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/hoon-ch/serial-tcp-proxy/internal/client"
	"github.com/hoon-ch/serial-tcp-proxy/internal/config"
	"github.com/hoon-ch/serial-tcp-proxy/internal/logger"
	"github.com/hoon-ch/serial-tcp-proxy/internal/upstream"
)

// Buffer pool for zero-copy packet forwarding
var bufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 4096)
		return &buf
	},
}

type Server struct {
	config    *config.Config
	upstream  *upstream.Connection
	clients   *client.Manager
	logger    *logger.Logger
	listener  net.Listener
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	startTime time.Time
}

func NewServer(cfg *config.Config, log *logger.Logger) *Server {
	ctx, cancel := context.WithCancel(context.Background())

	ps := &Server{
		config:    cfg,
		logger:    log,
		clients:   client.NewManager(cfg.MaxClients, log),
		ctx:       ctx,
		cancel:    cancel,
		startTime: time.Now(),
	}

	// Create upstream connection with callback for received data
	ps.upstream = upstream.NewConnection(cfg.UpstreamAddr(), log, ps.onUpstreamData)

	return ps
}

func (ps *Server) onUpstreamData(data []byte) {
	// Log packet if enabled
	ps.logger.LogPacket("UP->", data, "")

	// Broadcast to all connected clients
	ps.clients.Broadcast(data)
}

func (ps *Server) Start() error {
	// Start upstream connection
	ps.upstream.Start()

	// Start client listener
	listener, err := net.Listen("tcp", ps.config.ListenAddr())
	if err != nil {
		return err
	}
	ps.listener = listener

	ps.logger.Info("Listening on %s", ps.config.ListenAddr())

	ps.wg.Add(1)
	go ps.acceptLoop()

	return nil
}

func (ps *Server) Stop() {
	ps.logger.Info("Shutting down proxy server...")

	// Stop accepting new connections
	ps.cancel()

	if ps.listener != nil {
		ps.listener.Close()
	}

	// Give existing clients time to finish (max 5 seconds)
	done := make(chan struct{})
	go func() {
		ps.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		ps.logger.Warn("Timeout waiting for clients, forcing shutdown")
	}

	// Close all client connections
	ps.clients.CloseAll()

	// Stop upstream connection
	ps.upstream.Stop()

	// Close logger
	ps.logger.Close()

	ps.logger.Info("Proxy server stopped")
}

func (ps *Server) acceptLoop() {
	defer ps.wg.Done()

	for {
		select {
		case <-ps.ctx.Done():
			return
		default:
		}

		// Set accept deadline to allow checking context
		_ = ps.listener.(*net.TCPListener).SetDeadline(time.Now().Add(time.Second))

		conn, err := ps.listener.Accept()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			select {
			case <-ps.ctx.Done():
				return
			default:
				ps.logger.Error("Accept error: %v", err)
				continue
			}
		}

		cl, err := ps.clients.Add(conn)
		if err != nil {
			ps.logger.Warn("Rejecting connection from %s: %v", conn.RemoteAddr(), err)
			conn.Close()
			continue
		}

		ps.wg.Add(1)
		go ps.handleClient(cl)
	}
}

func (ps *Server) handleClient(cl *client.Client) {
	defer ps.wg.Done()
	defer ps.clients.Remove(cl.ID)

	// Get buffer from pool for zero-copy
	bufPtr := bufferPool.Get().(*[]byte)
	buf := *bufPtr
	defer bufferPool.Put(bufPtr)

	for {
		select {
		case <-ps.ctx.Done():
			return
		default:
		}

		// Set read deadline
		_ = cl.Conn.SetReadDeadline(time.Now().Add(time.Minute))

		n, err := cl.Conn.Read(buf)
		if err != nil {
			return
		}

		if n > 0 {
			// Create a copy for logging and upstream write since buffer will be reused
			data := make([]byte, n)
			copy(data, buf[:n])

			// Log packet if enabled
			ps.logger.LogPacket("->UP", data, cl.ID)

			// Forward to upstream only (not to other clients)
			if ps.upstream.IsConnected() {
				if err := ps.upstream.Write(data); err != nil {
					ps.logger.Warn("Failed to write to upstream from %s: %v", cl.ID, err)
				}
			} else {
				ps.logger.Warn("Upstream not connected, dropping packet from %s", cl.ID)
			}
		}
	}
}

func (ps *Server) GetStatus() map[string]interface{} {
	return map[string]interface{}{
		"upstream_state":    ps.upstream.GetState().String(),
		"upstream_addr":     ps.config.UpstreamAddr(),
		"listen_addr":       ps.config.ListenAddr(),
		"connected_clients": ps.clients.Count(),
		"max_clients":       ps.config.MaxClients,
		"start_time":        ps.startTime.Format(time.RFC3339),
	}
}

// GetClientCount returns the number of connected clients
func (ps *Server) GetClientCount() int {
	return ps.clients.Count()
}

// IsUpstreamConnected returns whether the upstream is connected
func (ps *Server) IsUpstreamConnected() bool {
	return ps.upstream.IsConnected()
}

// ErrInvalidTarget is returned when an invalid target is specified for packet injection
var ErrInvalidTarget = fmt.Errorf("invalid target: must be 'upstream' or 'downstream'")

// InjectPacket injects a packet to the specified target (upstream or downstream)
func (ps *Server) InjectPacket(target string, data []byte) error {
	if target == "upstream" {
		if !ps.upstream.IsConnected() {
			return net.ErrClosed
		}
		// Log as if it came from a client (Client -> Upstream)
		ps.logger.LogPacket("->UP", data, "INJECT")
		return ps.upstream.Write(data)
	} else if target == "downstream" {
		// Log as if it came from upstream (Upstream -> Client)
		ps.logger.LogPacket("UP->", data, "INJECT")
		ps.clients.Broadcast(data)
		return nil
	}
	return ErrInvalidTarget
}
