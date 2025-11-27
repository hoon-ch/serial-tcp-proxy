# Serial TCP Proxy

[![CI](https://github.com/hoon-ch/serial-tcp-proxy/actions/workflows/ci.yml/badge.svg)](https://github.com/hoon-ch/serial-tcp-proxy/actions/workflows/ci.yml)
[![Release](https://github.com/hoon-ch/serial-tcp-proxy/actions/workflows/release.yml/badge.svg)](https://github.com/hoon-ch/serial-tcp-proxy/actions/workflows/release.yml)

A TCP proxy server that sits between Serial-to-TCP converters (e.g., Elfin-EW11) and clients (e.g., Home Assistant).

[한국어 문서 (Korean)](docs/README_ko.md)

## Features

- **Multi-client support**: Up to 10 (configurable) simultaneous client connections
- **Packet broadcast**: Data received from upstream is forwarded to all connected clients
- **Packet logging**: HEX packet logging for debugging
- **Auto-reconnect**: Automatic reconnection with exponential backoff
- **Low latency**: < 1ms additional latency
- **Home Assistant Add-on**: Easy deployment as HA Add-on

## Use Cases

- Monitor packets while Home Assistant is connected to EW11
- Use debugging tools without disabling HA integration
- Multiple clients receiving the same RS-485 bus data

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

## Installation

### Home Assistant Add-on

1. Add repository to Home Assistant Add-on Store:
   ```
   https://github.com/hoon-ch/serial-tcp-proxy
   ```

2. Install "Serial TCP Proxy" Add-on

3. Configure settings:
   ```yaml
   upstream_host: "192.168.0.100"  # EW11 IP address
   upstream_port: 8899              # EW11 port
   listen_port: 18899               # Proxy listening port
   max_clients: 10                  # Maximum client count
   log_packets: false               # Enable packet logging
   log_file: "/data/packets.log"    # Log file path
   ```

4. Start the Add-on

### Standalone

Configure via environment variables:

```bash
export UPSTREAM_HOST=192.168.50.143
export UPSTREAM_PORT=8899
export LISTEN_PORT=18899
export MAX_CLIENTS=10
export LOG_PACKETS=true
export LOG_FILE=/tmp/packets.log

./serial-tcp-proxy
```

### Docker

```bash
docker pull ghcr.io/hoon-ch/serial-tcp-proxy:latest

docker run -d \
  --name serial-tcp-proxy \
  --network host \
  -e UPSTREAM_HOST=192.168.50.143 \
  -e UPSTREAM_PORT=8899 \
  -e LISTEN_PORT=18899 \
  -e LOG_PACKETS=true \
  ghcr.io/hoon-ch/serial-tcp-proxy:latest
```

## Configuration

| Environment Variable | Description | Default |
|---------------------|-------------|---------|
| `UPSTREAM_HOST` | Serial-TCP converter IP address | Required |
| `UPSTREAM_PORT` | Serial-TCP converter port | 8899 |
| `LISTEN_PORT` | Proxy listening port | 18899 |
| `MAX_CLIENTS` | Maximum simultaneous clients | 10 |
| `LOG_PACKETS` | Enable packet logging | false |
| `LOG_FILE` | Packet log file path | /data/packets.log |

## Log Format

```
2024-01-15T10:30:45.123Z [INFO] Starting Serial TCP Proxy v1.0.0
2024-01-15T10:30:45.130Z [INFO] Connected to upstream
2024-01-15T10:30:50.000Z [INFO] Client connected: 192.168.50.10:52341 (total: 1)
2024-01-15T10:30:50.100Z [PKT] [UP→] f7 0e 11 41 01 01 5e 02 (8 bytes)
2024-01-15T10:30:50.150Z [PKT] [→UP] f7 0e 11 41 01 00 5f 00 (8 bytes) from client#1
```

- `[UP→]`: Upstream → Client direction
- `[→UP]`: Client → Upstream direction

## Routing Rules

1. **Upstream → Clients**: Broadcast to all connected clients
2. **Client → Upstream**: Forward to upstream only (not to other clients)

## Building

```bash
go build -o serial-tcp-proxy ./cmd/serial-tcp-proxy
```

## Testing

```bash
go test -v ./...
```

## Compatible Devices

- Elfin-EW11 (tested)
- USR-W610
- Other Serial-to-TCP converters

## License

MIT License
