#!/bin/bash
cd "$(dirname "$0")"
if [ -f .env ]; then source .env; fi
export LISTEN_ADDR=":9091"
export UPSTREAM_BASE_URL="https://api.minimax.io"
export UPSTREAM_CHAT_PATH="/v1/chat/completions"
export UPSTREAM_API_KEY="${MINIMAX_KEY}"
export ALLOW_DOWNGRADE_DEVELOPER=1
exec ./adapter "$@"
