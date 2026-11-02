FROM --platform=$BUILDPLATFORM messense/cargo-zigbuild:0.24.0@sha256:eb5578cfc06e070a09deb7b5852474bd128a4e561f9f123261b5ed2405e478b1 AS builder

ARG TARGETPLATFORM
ARG VERSION=dev

RUN rustup target add x86_64-unknown-linux-musl aarch64-unknown-linux-musl \
    armv7-unknown-linux-musleabihf i686-unknown-linux-musl

WORKDIR /app
COPY Cargo.toml Cargo.lock ./
COPY src/ ./src/
COPY web/static/ ./web/static/
RUN --mount=type=cache,target=/usr/local/cargo/registry,sharing=locked \
    --mount=type=cache,target=/app/target,sharing=locked \
    case "$TARGETPLATFORM" in \
      linux/amd64) target=x86_64-unknown-linux-musl ;; \
      linux/arm64) target=aarch64-unknown-linux-musl ;; \
      linux/arm/v7) target=armv7-unknown-linux-musleabihf ;; \
      linux/386) target=i686-unknown-linux-musl ;; \
      *) echo "Unsupported platform: $TARGETPLATFORM" >&2; exit 1 ;; \
    esac && \
    STP_VERSION="$VERSION" cargo zigbuild --release --locked --target "$target" && \
    cp "target/$target/release/serial-tcp-proxy" /serial-tcp-proxy

FROM alpine:3.22

RUN apk add --no-cache tzdata jq curl

COPY --from=builder /serial-tcp-proxy /usr/local/bin/
COPY run.sh /

RUN chmod +x /run.sh /usr/local/bin/serial-tcp-proxy

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:${WEB_PORT:-18080}/api/health || exit 1

CMD ["/run.sh"]
