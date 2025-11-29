# Serial TCP Proxy

[![CI](https://github.com/hoon-ch/serial-tcp-proxy/actions/workflows/ci.yml/badge.svg)](https://github.com/hoon-ch/serial-tcp-proxy/actions/workflows/ci.yml)
[![Release](https://github.com/hoon-ch/serial-tcp-proxy/actions/workflows/release.yml/badge.svg)](https://github.com/hoon-ch/serial-tcp-proxy/actions/workflows/release.yml)

A TCP proxy server that sits between Serial-to-TCP converters (e.g., Elfin-EW11) and clients (e.g., Home Assistant).

## Documentation

| Document | Description |
|----------|-------------|
| [Configuration Guide](docs/CONFIGURATION.md) | Environment variables, deployment options |
| [API Reference](docs/API.md) | REST API endpoints and examples |
| [Contributing Guide](docs/CONTRIBUTING.md) | Development setup, testing, code style |
| [한국어 문서](docs/README_ko.md) | Korean documentation |

## Features

- **Multi-client support**: Up to 10 (configurable) simultaneous client connections
- **Packet broadcast**: Data received from upstream is forwarded to all connected clients
- **Packet logging**: HEX packet logging for debugging
- **Auto-reconnect**: Automatic reconnection with exponential backoff
- **Low latency**: < 1ms additional latency
- **Home Assistant Add-on**: Easy deployment as HA Add-on
- **Web UI**: Real-time monitoring dashboard with packet inspector
- **Health Check Endpoint**: Container orchestration support (Docker, Kubernetes)

## Architecture

```
┌──────────┐     ┌──────────┐     ┌─────────────────┐     ┌──────────────┐
│  RS-485  │◄───►│  EW11    │◄───►│  Serial TCP     │◄───►│ Home         │
│  Device  │     │          │     │  Proxy          │     │ Assistant    │
└──────────┘     └──────────┘     │                 │     └──────────────┘
                                  │                 │     ┌──────────────┐
                                  │                 │◄───►│ Packet       │
                                  │                 │     │ Logger       │
                                  │                 │     └──────────────┘
                                  │                 │     ┌──────────────┐
                                  │                 │◄───►│ Test Tool    │
                                  └─────────────────┘     └──────────────┘
```

## Quick Start

### Docker (Recommended)

```bash
docker run -d \
  --name serial-tcp-proxy \
  --network host \
  -e UPSTREAM_HOST=192.168.50.143 \
  -e UPSTREAM_PORT=8899 \
  -e LISTEN_PORT=18899 \
  ghcr.io/hoon-ch/serial-tcp-proxy:latest
```

### Home Assistant Add-on

1. Add repository: `https://github.com/hoon-ch/serial-tcp-proxy`
2. Install "Serial TCP Proxy" Add-on
3. Configure and start

### Standalone

```bash
export UPSTREAM_HOST=192.168.50.143
export UPSTREAM_PORT=8899
export LISTEN_PORT=18899

./serial-tcp-proxy
```

See [Configuration Guide](docs/CONFIGURATION.md) for detailed setup options.

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `UPSTREAM_HOST` | Serial-TCP converter IP | Required |
| `UPSTREAM_PORT` | Serial-TCP converter port | `8899` |
| `LISTEN_PORT` | Proxy listening port | `18899` |
| `MAX_CLIENTS` | Max simultaneous clients | `10` |
| `LOG_PACKETS` | Enable packet logging | `false` |
| `WEB_PORT` | Web UI port | `18080` |
| `WEB_AUTH_ENABLED` | Enable Web UI auth | `false` |

See [Configuration Guide](docs/CONFIGURATION.md) for all options.

## Web UI

Access the built-in web interface at `http://localhost:18080`.

**Features:**
- Real-time status monitoring
- Live log streaming
- Packet inspector with HEX/ASCII view
  - Advanced filtering: `dir:up`, `len:>10`, `hex:f7 0e`, `ascii:hello`, `/regex/`
  - Filter presets with save/load
  - Virtual scrolling (10,000+ packets)
  - Auto-scroll with "Go to Latest" button
- Client management (view/disconnect clients)
- Packet injection for testing
- Dark/Light theme
- Mobile responsive

![Web UI Screenshot](docs/images/webui.png)

## API

The proxy provides a REST API for monitoring and control.

```bash
# Health check (no auth required)
curl http://localhost:18080/api/health

# Status (auth required if enabled)
curl -u admin:secret http://localhost:18080/api/status

# Inject packet
curl -u admin:secret -X POST \
  -d '{"target":"upstream","format":"hex","data":"f70e114101015e02"}' \
  http://localhost:18080/api/inject
```

See [API Reference](docs/API.md) for complete documentation.

## Health Check

```json
GET /api/health

{
  "status": "healthy",
  "version": "1.2.0",
  "uptime": 3600,
  "checks": {
    "upstream": {"status": "healthy", "connected": true},
    "clients": {"status": "healthy", "count": 2, "max": 10},
    "web_server": {"status": "healthy", "port": 18080}
  }
}
```

| Status | Description | HTTP Code |
|--------|-------------|-----------|
| `healthy` | Upstream connected | 200 |
| `degraded` | Upstream disconnected, auto-reconnecting | 200 |
| `unhealthy` | Proxy not listening | 503 |

## Building

```bash
go build -o serial-tcp-proxy ./cmd/serial-tcp-proxy
```

## Testing

```bash
# Run all tests
go test -v ./...

# Run with coverage
go test -cover ./...
```

See [Contributing Guide](docs/CONTRIBUTING.md) for development setup.

## Compatible Devices

- Elfin-EW11 (tested)
- USR-W610
- Other Serial-to-TCP converters

## License

MIT License
