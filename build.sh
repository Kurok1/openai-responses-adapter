#!/bin/bash
set -e
cd "$(dirname "$0")"
echo "Building..."
CGO_ENABLED=0 go build -o adapter -trimpath ./cmd/adapter/
echo "Done: $(pwd)/adapter ($(stat -c%s adapter) bytes)"
