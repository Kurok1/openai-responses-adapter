# openai-responses-adapter

A lightweight Go proxy that allows clients to call the OpenAI **Responses API** style endpoint, while the adapter forwards requests to an upstream **Chat Completions API** provider.

## Current status

This is an MVP scaffold with:

- `POST /v1/responses` (Responses -> Chat Completions mapping)
- `GET /v1/responses/{response_id}` (read stored response by id)
- `GET /healthz`
- `previous_response_id` multi-turn context chaining
- function calling mapping (`tools`, `tool_choice`, `function_call_output` subset)
- MCP tool bridge:
  - load MCP servers from config file
  - aggregate MCP tools as internal native tools
  - rewrite native tools to upstream `function` tools
  - auto-execute MCP tools when upstream emits tool calls (non-stream requests)
- `stream=true` SSE passthrough with Responses-style delta events
- in-memory response store with TTL + LRU
- passthrough auth header or fallback to `UPSTREAM_API_KEY`

Current MVP request constraint:

- `model` is required
- `input` supports: string or item array (`message` / `function_call_output` subset)
- `downgrade_developer_to_user` is optional:
  - default `true`: convert outbound `role=developer` to `role=user` for upstream compatibility
  - set `false` to keep `developer` role unchanged
- tool compatibility:
  - `function` is supported directly
  - non-function/native tool types are supported when they match configured MCP tool names
- streaming currently covers: `response.created`, `response.in_progress`, `response.output_item.added`, `response.output_text.delta/done`, `response.function_call_arguments.delta/done`, `response.output_item.done`, `response.completed`, `[DONE]` (MVP subset)

## Run

```bash
go run ./cmd/adapter
```

## Build Docker image (local)

```bash
docker build -t openai-responses-adapter:dev .
```

## Environment variables

- `LISTEN_ADDR` (default `:8080`)
- `UPSTREAM_BASE_URL` (default `https://api.openai.com`)
- `UPSTREAM_CHAT_PATH` (default `/v1/chat/completions`)
- `UPSTREAM_API_KEY` (optional fallback if request has no `Authorization`)
- `STORE_MAX_ENTRIES` (default `1000`)
- `STORE_TTL` (default `1h`, Go duration format such as `30m`, `2h`)
- `MCP_CONFIG_PATH` (optional path to MCP server config JSON)

### MCP config example

Claude `mcpServers` format:

```json
{
  "mcpServers": {
    "web-search-prime": {
      "type": "http",
      "url": "https://open.bigmodel.cn/api/mcp/web_search_prime/mcp",
      "headers": {
        "Authorization": "Bearer your_api_key"
      }
    }
  }
}
```

## Release artifacts

When a GitHub Release is published, GitHub Actions will run GoReleaser and publish:

- Multi-platform binaries to the release assets:
  - linux/darwin/windows
  - amd64/arm64
- Docker images to GHCR:
  - `ghcr.io/kurok1/openai-responses-adapter:<tag>`

## Example

```bash
curl -sS http://localhost:8080/v1/responses \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer <token>' \
  -d '{
    "model":"gpt-4o-mini",
    "input":"你好，介绍一下你自己",
    "downgrade_developer_to_user": true,
    "stream":false
  }'
```
