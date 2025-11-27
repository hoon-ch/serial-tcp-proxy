# Build stage
FROM --platform=$BUILDPLATFORM golang:1.25.4-alpine3.22 AS builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod ./
RUN go mod download

# Copy source code
COPY cmd/ ./cmd/
COPY internal/ ./internal/

# Build binary
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w -X main.Version=${VERSION}" \
    -o /serial-tcp-proxy ./cmd/serial-tcp-proxy

# Runtime stage
FROM alpine:3.22

RUN apk add --no-cache tzdata jq curl

COPY --from=builder /serial-tcp-proxy /usr/local/bin/
COPY run.sh /

RUN chmod +x /run.sh /usr/local/bin/serial-tcp-proxy

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:${WEB_PORT:-18080}/api/health || exit 1

CMD ["/run.sh"]
