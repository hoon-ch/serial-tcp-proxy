# Changelog

All notable changes to this project will be documented in this file.

## [1.1.3] - 2025-11-28

### Added
- Pre-built Docker images for Home Assistant add-on (faster installation)
  - Supports amd64, aarch64, armv7, i386 architectures
  - Images hosted on GitHub Container Registry (ghcr.io)

### Fixed
- SSE event stream buffering issue with Home Assistant Ingress proxy
  - Added `X-Accel-Buffering: no` header to disable nginx buffering
  - Events now delivered in real-time instead of batched

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
