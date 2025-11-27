# Changelog

All notable changes to this project will be documented in this file.

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
