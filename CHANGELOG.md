# Changelog

All notable changes to this project will be documented in this file.

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
