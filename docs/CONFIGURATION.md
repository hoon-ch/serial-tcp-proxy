# Configuration Guide

Serial TCP Proxy can be configured via environment variables or Home Assistant Add-on options.

## Environment Variables

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `UPSTREAM_HOST` | Serial-TCP converter IP address | - | Yes |
| `UPSTREAM_PORT` | Serial-TCP converter port | `8899` | No |
| `LISTEN_PORT` | Proxy listening port | `18899` | No |
| `MAX_CLIENTS` | Maximum simultaneous clients | `10` | No |
| `LOG_PACKETS` | Enable packet logging | `false` | No |
| `LOG_FILE` | Packet log file path | `/data/packets.log` | No |
| `WEB_PORT` | Web UI port | `18080` | No |
| `WEB_AUTH_ENABLED` | Enable Web UI authentication | `false` | No |
| `WEB_AUTH_USERNAME` | Basic auth username | - | If auth enabled |
| `WEB_AUTH_PASSWORD` | Basic auth password | - | If auth enabled |

## Detailed Configuration

### Upstream Connection

```bash
UPSTREAM_HOST=192.168.50.143   # IP address of your Serial-TCP converter
UPSTREAM_PORT=8899              # Port (default for most devices)
```

The proxy will automatically reconnect to the upstream server if the connection is lost, using exponential backoff.

### Client Connections

```bash
LISTEN_PORT=18899   # Port where clients connect
MAX_CLIENTS=10      # Maximum simultaneous connections
```

When `MAX_CLIENTS` is reached, new connections will be rejected.

### Packet Logging

```bash
LOG_PACKETS=true
LOG_FILE=/data/packets.log
```

Log format:
```
2024-01-15T10:30:50.100Z [PKT] [UP→] f7 0e 11 41 01 01 5e 02 (8 bytes)
2024-01-15T10:30:50.150Z [PKT] [→UP] f7 0e 11 41 01 00 5f 00 (8 bytes) from client#1
```

- `[UP→]`: Upstream → Clients (broadcast)
- `[→UP]`: Client → Upstream

### Web UI

```bash
WEB_PORT=18080
```

Access the Web UI at `http://localhost:18080`.

### Authentication

```bash
WEB_AUTH_ENABLED=true
WEB_AUTH_USERNAME=admin
WEB_AUTH_PASSWORD=your-secure-password
```

> **Security Note**: Basic Authentication transmits credentials in Base64 encoding, which is NOT encrypted. When exposing the Web UI outside a trusted network:
> - Always use HTTPS (TLS)
> - Use a reverse proxy with TLS termination
> - Use strong, unique passwords

---

## Deployment Configurations

### Standalone

```bash
export UPSTREAM_HOST=192.168.50.143
export UPSTREAM_PORT=8899
export LISTEN_PORT=18899
export MAX_CLIENTS=10
export LOG_PACKETS=true
export LOG_FILE=/tmp/packets.log
export WEB_PORT=18080

./serial-tcp-proxy
```

### Docker

```bash
docker run -d \
  --name serial-tcp-proxy \
  --network host \
  -e UPSTREAM_HOST=192.168.50.143 \
  -e UPSTREAM_PORT=8899 \
  -e LISTEN_PORT=18899 \
  -e MAX_CLIENTS=10 \
  -e LOG_PACKETS=true \
  -e WEB_PORT=18080 \
  ghcr.io/hoon-ch/serial-tcp-proxy:latest
```

### Docker Compose

```yaml
version: '3.8'

services:
  serial-tcp-proxy:
    image: ghcr.io/hoon-ch/serial-tcp-proxy:latest
    network_mode: host
    environment:
      - UPSTREAM_HOST=192.168.50.143
      - UPSTREAM_PORT=8899
      - LISTEN_PORT=18899
      - MAX_CLIENTS=10
      - LOG_PACKETS=true
      - WEB_PORT=18080
      - WEB_AUTH_ENABLED=true
      - WEB_AUTH_USERNAME=admin
      - WEB_AUTH_PASSWORD=${WEB_PASSWORD}
    restart: unless-stopped
```

### Home Assistant Add-on

```yaml
# Add-on configuration
upstream_host: "192.168.50.143"
upstream_port: 8899
listen_port: 18899
max_clients: 10
log_packets: false
log_file: "/data/packets.log"
```

### Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: serial-tcp-proxy
spec:
  replicas: 1
  selector:
    matchLabels:
      app: serial-tcp-proxy
  template:
    metadata:
      labels:
        app: serial-tcp-proxy
    spec:
      containers:
      - name: serial-tcp-proxy
        image: ghcr.io/hoon-ch/serial-tcp-proxy:latest
        env:
        - name: UPSTREAM_HOST
          value: "192.168.50.143"
        - name: UPSTREAM_PORT
          value: "8899"
        - name: LISTEN_PORT
          value: "18899"
        - name: WEB_PORT
          value: "18080"
        ports:
        - containerPort: 18899
          name: proxy
        - containerPort: 18080
          name: web
        livenessProbe:
          httpGet:
            path: /api/health
            port: 18080
          initialDelaySeconds: 5
          periodSeconds: 30
        readinessProbe:
          httpGet:
            path: /api/health
            port: 18080
          initialDelaySeconds: 5
          periodSeconds: 10
---
apiVersion: v1
kind: Service
metadata:
  name: serial-tcp-proxy
spec:
  selector:
    app: serial-tcp-proxy
  ports:
  - name: proxy
    port: 18899
    targetPort: 18899
  - name: web
    port: 18080
    targetPort: 18080
```

---

## Compatible Devices

The proxy has been tested with:

| Device | Status |
|--------|--------|
| Elfin-EW11 | Tested |
| USR-W610 | Compatible |
| Other Serial-to-TCP converters | Should work |

## Troubleshooting

### Upstream Connection Issues

1. Verify upstream device is reachable:
   ```bash
   nc -zv 192.168.50.143 8899
   ```

2. Check proxy logs for connection errors

3. Ensure no firewall blocking the connection

### Client Connection Issues

1. Verify proxy is listening:
   ```bash
   nc -zv localhost 18899
   ```

2. Check if `MAX_CLIENTS` limit is reached

3. Check client application configuration

### Web UI Issues

1. Check Web UI is accessible:
   ```bash
   curl http://localhost:18080/api/health
   ```

2. If using authentication, verify credentials

3. Check browser console for errors
