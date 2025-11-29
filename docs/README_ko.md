# Serial TCP Proxy

[![CI](https://github.com/hoon-ch/serial-tcp-proxy/actions/workflows/ci.yml/badge.svg)](https://github.com/hoon-ch/serial-tcp-proxy/actions/workflows/ci.yml)
[![Release](https://github.com/hoon-ch/serial-tcp-proxy/actions/workflows/release.yml/badge.svg)](https://github.com/hoon-ch/serial-tcp-proxy/actions/workflows/release.yml)

Serial-to-TCP 변환기(Elfin-EW11 등)와 클라이언트(Home Assistant 등) 사이에서 동작하는 TCP 프록시 서버입니다.

[English Documentation](../README.md)

## 기능

- **다중 클라이언트 지원**: 최대 10개(설정 가능)의 클라이언트가 동시 연결 가능
- **패킷 브로드캐스트**: Upstream에서 수신한 데이터를 모든 클라이언트에 전달
- **패킷 로깅**: 디버깅을 위한 HEX 패킷 로깅 지원
- **자동 재연결**: 연결 끊김 시 지수 백오프로 자동 재연결
- **낮은 지연시간**: < 1ms 추가 지연
- **Home Assistant Add-on**: HA Add-on으로 쉽게 배포 가능
- **Web UI**: 실시간 모니터링 대시보드 및 패킷 인스펙터
- **헬스 체크 엔드포인트**: 컨테이너 오케스트레이션 지원 (Docker, Kubernetes)

## 사용 사례

- Home Assistant가 EW11에 연결된 상태에서 패킷 모니터링
- 디버깅 시 HA 비활성화 없이 테스트 도구 사용
- 여러 클라이언트가 동일한 RS-485 버스 데이터 수신

## 아키텍처

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

## 설치

### Home Assistant Add-on

1. Home Assistant의 Add-on Store에서 저장소 추가:
   ```
   https://github.com/hoon-ch/serial-tcp-proxy
   ```

2. "Serial TCP Proxy" Add-on 설치

3. 설정 구성:
   ```yaml
   upstream_host: "192.168.0.100"  # EW11 IP 주소
   upstream_port: 8899              # EW11 포트
   listen_port: 18899               # 프록시 리스닝 포트
   max_clients: 10                  # 최대 클라이언트 수
   log_packets: false               # 패킷 로깅 활성화
   log_file: "/data/packets.log"    # 로그 파일 경로
   ```

4. Add-on 시작

### 독립 실행

환경변수로 설정:

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

## 설정

| 환경변수 | 설명 | 기본값 |
|----------|------|--------|
| `UPSTREAM_HOST` | Serial-TCP 변환기 IP 주소 | 필수 |
| `UPSTREAM_PORT` | Serial-TCP 변환기 포트 | 8899 |
| `LISTEN_PORT` | 프록시 리스닝 포트 | 18899 |
| `MAX_CLIENTS` | 최대 동시 클라이언트 수 | 10 |
| `LOG_PACKETS` | 패킷 로깅 활성화 | false |
| `LOG_FILE` | 패킷 로그 파일 경로 | /data/packets.log |
| `WEB_PORT` | Web UI 포트 | 18080 |

## 로그 형식

```
2024-01-15T10:30:45.123Z [INFO] Starting Serial TCP Proxy v1.0.0
2024-01-15T10:30:45.130Z [INFO] Connected to upstream
2024-01-15T10:30:50.000Z [INFO] Client connected: 192.168.50.10:52341 (total: 1)
2024-01-15T10:30:50.100Z [PKT] [UP→] f7 0e 11 41 01 01 5e 02 (8 bytes)
2024-01-15T10:30:50.150Z [PKT] [→UP] f7 0e 11 41 01 00 5f 00 (8 bytes) from client#1
```

- `[UP→]`: Upstream → 클라이언트 방향
- `[→UP]`: 클라이언트 → Upstream 방향

## 라우팅 규칙

1. **Upstream → 클라이언트**: 모든 연결된 클라이언트에게 브로드캐스트
2. **클라이언트 → Upstream**: Upstream에만 전달 (다른 클라이언트에게 전달하지 않음)

## Web UI

프록시에는 모니터링 및 디버깅을 위한 내장 웹 인터페이스가 포함되어 있습니다.

`http://localhost:18080` (또는 설정된 WEB_PORT)로 접속하세요.

Home Assistant Add-on으로 실행 시, Ingress를 통해 사이드바 패널에서 Web UI에 접근할 수 있습니다.

### 기능

- **대시보드**: 실시간 상태 모니터링 (upstream 연결, 클라이언트 수, 가동 시간)
- **실시간 로그**: 일시 정지/지우기 기능과 함께 실시간으로 로그 스트리밍
- **패킷 인스펙터**:
  - HEX 및 ASCII 형식으로 패킷 보기
  - 고급 필터: `dir:up`, `len:>10`, `hex:f7 0e`, `ascii:hello`, `/regex/`
  - 필터 프리셋 저장/불러오기
  - 가상 스크롤링 (10,000개 이상 패킷 지원)
  - 자동 스크롤 및 "최신으로 이동" 버튼
  - 두 패킷 비교 (diff 뷰)
  - 데이터 인스펙터 (Binary, Int8/16/32, Float32, String 해석)
  - 패킷 파일로 내보내기
- **클라이언트 관리**: 연결된 클라이언트 목록 보기 및 연결 해제
- **패킷 인젝션**: upstream으로 테스트 패킷 전송 또는 downstream 클라이언트에 브로드캐스트
- **다크/라이트 테마**: 테마 전환 지원
- **모바일 반응형**: 모바일 기기에서도 사용 가능

![Web UI 스크린샷](images/webui.png)

## 헬스 체크

프록시는 컨테이너 오케스트레이션 플랫폼을 위한 헬스 체크 엔드포인트를 제공합니다.

### 엔드포인트

`GET /api/health`

### 응답

```json
{
  "status": "healthy|degraded|unhealthy",
  "version": "1.1.1",
  "uptime": 3600,
  "checks": {
    "upstream": {
      "status": "healthy|unhealthy",
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

### 헬스 상태

| 상태 | 설명 | HTTP 코드 |
|------|------|-----------|
| `healthy` | Upstream 연결됨 및 프록시 리스닝 중 | 200 |
| `degraded` | Upstream 연결 끊김, 프록시는 실행 중 (자동 재연결 중) | 200 |
| `unhealthy` | 프록시 리스닝 안 함 | 503 |

### Docker HEALTHCHECK

Docker 이미지에는 내장 헬스 체크가 포함되어 있습니다:

```dockerfile
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:${WEB_PORT:-18080}/api/health || exit 1
```

### Kubernetes

Liveness 및 Readiness 프로브 예시:

```yaml
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
```

## 빌드

```bash
go build -o serial-tcp-proxy ./cmd/serial-tcp-proxy
```

## 테스트

```bash
go test -v ./...
```

## 호환 장치

- Elfin-EW11 (테스트 완료)
- USR-W610
- 기타 Serial-to-TCP 변환기

## 라이선스

MIT License
