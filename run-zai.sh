#!/bin/bash
cd "$(dirname "$0")"
if [ -f .env ]; then source .env; fi
export LISTEN_ADDR=":9090"
export UPSTREAM_BASE_URL="https://api.z.ai/api/coding/paas"
export UPSTREAM_CHAT_PATH="/v4/chat/completions"
export UPSTREAM_API_KEY="${ZAI_KEY}"
export ALLOW_DOWNGRADE_DEVELOPER=1
exec ./adapter "$@"
