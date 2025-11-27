#!/bin/sh
set -e

# Read options from Home Assistant Add-on config
CONFIG_PATH=/data/options.json

if [ -f "$CONFIG_PATH" ]; then
    export UPSTREAM_HOST=$(jq -r '.upstream_host' "$CONFIG_PATH")
    export UPSTREAM_PORT=$(jq -r '.upstream_port' "$CONFIG_PATH")
    export LISTEN_PORT=$(jq -r '.listen_port' "$CONFIG_PATH")
    export MAX_CLIENTS=$(jq -r '.max_clients' "$CONFIG_PATH")
    export LOG_PACKETS=$(jq -r '.log_packets' "$CONFIG_PATH")
    export LOG_FILE=$(jq -r '.log_file' "$CONFIG_PATH")
    export WEB_PORT=$(jq -r '.web_port // 18080' "$CONFIG_PATH")
fi

exec /usr/local/bin/serial-tcp-proxy
