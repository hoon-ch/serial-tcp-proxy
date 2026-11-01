# Rust serial TCP proxy

The Rust binary embeds `internal/web/static` from the sibling Go implementation. It uses the same configuration environment variables and `/data/options.json` as the Go server.

Build a static Linux arm64 release binary with Docker. The named volumes cache the Cargo registry and build artifacts.

```sh
docker run --rm --platform linux/arm64 -v /tmp/stp-rs:/work \
  -v stp-spike-cargo:/usr/local/cargo/registry -v stp-rs-release-target:/target \
  -w /work/rust -e CARGO_TARGET_DIR=/target rust:alpine \
  sh -c 'apk add --no-cache musl-dev && cargo build --release --locked'
```

The binary is `/target/release/serial-tcp-proxy-rs` inside the target volume. Add `--features mimalloc` to use mimalloc instead of the default allocator.

Run the black-box contract against a binary accessible inside the container:

```sh
docker run --rm --platform linux/arm64 -v /tmp/stp-rs:/work \
  -w /work oven/bun:alpine bun bench/contract/contract.mjs --bin /work/bench/bin/rs/server
```
