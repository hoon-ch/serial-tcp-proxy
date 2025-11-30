# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Added
- **Home Assistant Add-on**: Auth configuration options now available in Add-on settings
  - `web_auth_enabled`: Enable/disable Web UI authentication
  - `web_auth_username`: Basic auth username
  - `web_auth_password`: Basic auth password (password input type)

### Changed
- **Documentation**: Updated Web UI section with feature screenshots
  - Dashboard, Packet Inspector, Client Management, Packet Injection screenshots

## [1.2.1] - 2025-11-29

### Fixed
- **Potential deadlock in packet logging** causing Recv-Q accumulation
  - Moved callback invocation outside of mutex lock in LogPacket()
  - Copy log buffer before sending to WebSocket to avoid holding lock
  - Use non-blocking channel send for buffered logs
- Client management modal not working via Home Assistant Ingress
  - Use apiUrl() helper for /api/clients endpoints

### Changed
- Client management modal now sorted by connection time (oldest first)

## [1.2.0] - 2025-11-30

### Added
- **Packet Inspector Enhancements**
  - Auto-scroll toggle button to follow latest packets
  - "Go to Latest" floating button with new packet count
  - Advanced filter syntax: `dir:up/down`, `len:>10`, `hex:f7 0e`, `ascii:hello`, `/regex/`
  - Filter presets with save/load to localStorage
  - Highlight mode to show all packets but highlight matches
  - Direction filter quick buttons (All / UP -> / -> UP)
- **Client Management Modal**
  - Click "Clients" card to open management modal
  - View all connected clients (TCP and Web)
  - Disconnect individual clients from the modal
  - Real-time client list refresh every 2 seconds
- **Performance**
  - Virtual scrolling for packet table (supports 10,000+ packets)
  - DOM element pooling for efficient rendering

### Fixed
- Client disconnection after 60 seconds due to read deadline
  - Now uses TCP keepalive (30s interval) instead of read deadline
  - Connections stay open indefinitely for idle clients (e.g., overnight)
- WebSocket client cleanup race conditions causing system hangs
- Long upstream addresses truncated in dashboard card
- Mobile devices unable to scroll page content

### Changed
- Web UI clients now count toward maxClients limit
- Responsive layout improvements for mobile devices

## [1.1.5] - 2025-11-28

### Added
- WebSocket support for real-time events (`/api/ws` endpoint)
  - More reliable than SSE through reverse proxies like Home Assistant Ingress
  - Automatic reconnection with exponential backoff
  - Falls back to SSE if WebSocket connection fails
  - Ping/pong mechanism to keep connections alive

### Changed
- Frontend now uses WebSocket by default with SSE as fallback

## [1.1.4] - 2025-11-28

### Fixed
- SSE event stream buffering issue with Home Assistant Ingress proxy
  - Enhanced SSE headers for better proxy compatibility
  - Added `X-Accel-Buffering: no` header to disable nginx buffering
  - Added `Pragma`, `Expires` headers for comprehensive cache control
  - Added `X-Content-Type-Options: nosniff` to prevent content sniffing
  - Explicit `WriteHeader` call before first flush
  - Added heartbeat mechanism (15s) to keep connections alive through proxies
  - Events now delivered in real-time instead of batched

## [1.1.3] - 2025-11-28

### Added
- Pre-built Docker images for Home Assistant add-on (faster installation)
  - Supports amd64, aarch64, armv7, i386 architectures
  - Images hosted on GitHub Container Registry (ghcr.io)

## [1.1.2] - 2025-11-28

### Fixed
- Support Home Assistant Ingress for Web UI API calls

## [1.1.1] - 2025-11-28

### Added
- **Health Check Endpoint**: `/api/health` for container orchestration (Docker, Kubernetes)
  - Reports `healthy`, `degraded`, or `unhealthy` status
  - Includes upstream connection details, client counts, and web server status
  - Returns HTTP 200 for healthy/degraded, 503 for unhealthy
- Docker HEALTHCHECK directive with configurable port via `WEB_PORT` environment variable
- Upstream last connected time tracking

### Changed
- Improved thread safety for listener state management with mutex protection
- Version is now synchronized between main package and web package

## [1.1.0] - 2025-11-28

### Added
- **Web UI**: Built-in web interface for monitoring and debugging
  - Real-time dashboard with upstream status, client count, and uptime
  - Live log viewer with Server-Sent Events (SSE) streaming
  - Packet inspector with HEX/ASCII view, filtering, and sorting
  - Packet diff view to compare two packets
  - Data inspector for binary data interpretation (Int8/16/32, Float32, String)
  - Packet injection feature for testing (upstream/downstream)
  - Packet export functionality
  - Dark/Light theme support
- New environment variable `WEB_PORT` for configuring Web UI port (default: 18080)
- Home Assistant Add-on Ingress support for sidebar panel access
- Logger callback support for real-time log streaming
- Packet injection API endpoint (`POST /api/inject`)
- Server uptime tracking

### Changed
- Packet direction indicators changed from Unicode arrows (→) to ASCII (->) for better compatibility

## [1.0.0] - 2024-01-15

### Added
- Initial release
- Multi-client TCP proxy support (up to 10 clients by default)
- Automatic reconnection with exponential backoff (1s, 2s, 4s... max 30s)
- Packet logging with HEX format
- Home Assistant Add-on support
- Multi-architecture Docker builds (amd64, arm64, armv7)
- Zero-copy packet forwarding with buffer pools
- Graceful shutdown handling

### Features
- Upstream to all clients broadcast
- Client to upstream only forwarding
- Configurable via environment variables or HA Add-on options
- Host network mode for minimal latency
