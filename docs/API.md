# API Reference

Serial TCP Proxy provides a RESTful API for monitoring and control.

## Base URL

```
http://localhost:18080
```

The port can be configured via `WEB_PORT` environment variable.

## Authentication

When `WEB_AUTH_ENABLED=true`, most endpoints require Basic Authentication.

```bash
curl -u admin:password http://localhost:18080/api/status
```

| Endpoint | Authentication Required |
|----------|------------------------|
| `/api/health` | No (for health probes) |
| `/api/status` | Yes |
| `/api/config` | Yes |
| `/api/events` | Yes |
| `/api/inject` | Yes |
| `/` (static files) | Yes |

---

## Endpoints

### Health Check

Check the health status of the proxy server.

```
GET /api/health
```

#### Response

```json
{
  "status": "healthy",
  "version": "1.1.1",
  "uptime": 3600,
  "checks": {
    "upstream": {
      "status": "healthy",
      "connected": true,
      "address": "192.168.50.143:8899",
      "last_connected": "2025-11-28T00:00:00Z"
    },
    "clients": {
      "status": "healthy",
      "count": 2,
      "max": 10
    },
    "web_server": {
      "status": "healthy",
      "port": 18080
    }
  },
  "timestamp": "2025-11-28T00:00:00Z"
}
```

#### Health Status Values

| Status | Description | HTTP Code |
|--------|-------------|-----------|
| `healthy` | Upstream connected, proxy listening | 200 |
| `degraded` | Upstream disconnected, proxy still running | 200 |
| `unhealthy` | Proxy not listening | 503 |

---

### Proxy Status

Get real-time proxy status including connection details.

```
GET /api/status
```

**Authentication:** Required

#### Response

```json
{
  "upstream_connected": true,
  "upstream_address": "192.168.50.143:8899",
  "client_count": 2,
  "listening": true,
  "listen_address": ":18899",
  "start_time": "2025-11-28T00:00:00Z",
  "bytes_from_upstream": 1024,
  "bytes_to_upstream": 512
}
```

---

### Configuration

Get current proxy configuration (non-sensitive fields only).

```
GET /api/config
```

**Authentication:** Required

#### Response

```json
{
  "upstream_host": "192.168.50.143",
  "upstream_port": 8899,
  "listen_port": 18899,
  "max_clients": 10,
  "log_packets": true,
  "web_port": 18080
}
```

---

### Server-Sent Events (SSE)

Subscribe to real-time log and status updates.

```
GET /api/events
```

**Authentication:** Required

#### Event Types

**Status Event**
```
event: status
data: {"upstream_connected":true,"client_count":2,...}
```

**Log Event**
```
event: log
data: 2025-11-28T00:00:00Z [PKT] [UP→] f7 0e 11 41 01 01 5e 02 (8 bytes)
```

#### Example Usage

```javascript
const eventSource = new EventSource('/api/events');

eventSource.addEventListener('status', (e) => {
  const status = JSON.parse(e.data);
  console.log('Status:', status);
});

eventSource.addEventListener('log', (e) => {
  console.log('Log:', e.data);
});
```

---

### Packet Injection

Inject packets to upstream or downstream.

```
POST /api/inject
```

**Authentication:** Required

#### Request Body

```json
{
  "target": "upstream",
  "format": "hex",
  "data": "f7 0e 11 41 01 01 5e 02"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `target` | string | `upstream` or `downstream` |
| `format` | string | `hex` or `ascii` |
| `data` | string | Data to send |

#### Hex Format Options

The following hex formats are supported:

```json
{"data": "f70e114101015e02"}
{"data": "f7 0e 11 41 01 01 5e 02"}
{"data": "0xf70e114101015e02"}
{"data": "F7 0E 11 41\n01 01 5E 02"}
```

#### Response

**Success (200)**
```json
{
  "success": true
}
```

**Error (400)** - Invalid hex
```
Invalid Hex: encoding/hex: invalid byte: U+005A 'Z'
```

**Error (500)** - Upstream not connected
```
Injection failed: upstream not connected
```

---

## Error Responses

All endpoints return standard HTTP error codes:

| Code | Description |
|------|-------------|
| 200 | Success |
| 400 | Bad Request (invalid input) |
| 401 | Unauthorized (auth required) |
| 405 | Method Not Allowed |
| 500 | Internal Server Error |
| 503 | Service Unavailable |

Error response body:
```
Error message text
```

---

## Usage Examples

### cURL

```bash
# Health check
curl http://localhost:18080/api/health

# Status (with auth)
curl -u admin:secret http://localhost:18080/api/status

# Inject packet
curl -u admin:secret \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{"target":"upstream","format":"hex","data":"f70e114101015e02"}' \
  http://localhost:18080/api/inject
```

### Python

```python
import requests

# Health check
r = requests.get('http://localhost:18080/api/health')
print(r.json())

# Status with auth
r = requests.get(
    'http://localhost:18080/api/status',
    auth=('admin', 'secret')
)
print(r.json())

# Inject packet
r = requests.post(
    'http://localhost:18080/api/inject',
    auth=('admin', 'secret'),
    json={
        'target': 'upstream',
        'format': 'hex',
        'data': 'f70e114101015e02'
    }
)
print(r.json())
```

### JavaScript

```javascript
// Health check
const health = await fetch('/api/health').then(r => r.json());

// Inject packet
const result = await fetch('/api/inject', {
  method: 'POST',
  headers: {
    'Content-Type': 'application/json',
    'Authorization': 'Basic ' + btoa('admin:secret')
  },
  body: JSON.stringify({
    target: 'upstream',
    format: 'hex',
    data: 'f70e114101015e02'
  })
}).then(r => r.json());
```
