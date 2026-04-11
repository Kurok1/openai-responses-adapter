# Patches for Non-OpenAI Provider Compatibility

These patches extend [Kurok1/openai-responses-adapter](https://github.com/Kurok1/openai-responses-adapter) to work with non-OpenAI chat completion providers (Z.AI, Minimax, OpenRouter, etc.) that only support `/v1/chat/completions`.

---

## Patch 1: Strip non-function tools

**File:** `internal/adapter/transform.go` — `mapTools()`

**Problem:** Codex CLI sends tools like `{"type": "web_search"}` in the request. Non-OpenAI providers reject these with errors like `"tools[7].web_search can not be null"`.

**Fix:** Instead of passing through non-`function` tool types as raw JSON (which upstream can't handle), silently drop them. Only `function`-type tools are forwarded.

```go
// Before: non-function tools were unmarshalled and passed through as map[string]interface{}
// After: log and skip them
log.Printf("[tool-filter] dropping non-function tool: type=%s name=%s", t.Type, t.Name)
continue
```

---

## Patch 2: Handle `function_call` input items

**File:** `internal/adapter/transform.go` — `itemToMessage()`

**Problem:** After a tool call completes, Codex CLI sends `{"type": "function_call", ...}` items in subsequent `input` arrays to represent the model's tool invocation. The adapter returned `"unsupported input item type: function_call"`.

**Fix:** Convert `function_call` input items into assistant messages with `tool_calls`, matching the Chat Completions API format:

```go
case "function_call":
    callID, _ := item["call_id"].(string)
    name, _ := item["name"].(string)
    args, _ := item["arguments"].(string)
    return ChatMessage{
        Role: "assistant",
        ToolCalls: []ChatToolCall{{
            ID: callID, Type: "function",
            Function: ChatToolFunction{Name: name, Arguments: args},
        }},
    }, nil
```

---

## Patch 3: Allow empty message content

**File:** `internal/adapter/transform.go` — `messageContentToString()`

**Problem:** Some providers (e.g. Minimax with MiniMax-M2.7) send messages with content that has no text parts (e.g. only reasoning/thinking tokens). The adapter errored with `"message content has no text"`.

**Fix:** Return empty string instead of erroring when content is empty or has no text parts:

```go
// Before: return "", fmt.Errorf("message content cannot be empty")
// After:  return "", nil

// Before: return "", fmt.Errorf("message content has no text")
// After:  return "", nil
```

---

## Patch 4: Request/response logging

**File:** `internal/httpserver/handler.go` — `responses()`

**Problem:** The adapter produced almost no logs, making debugging difficult.

**Fix:** Added structured logging at three points:

- **Request received:** `[request] model=glm-5.1 stream=true tools=8 prev_id= input_len=45`
- **Stream completed:** `[stream-done] id=resp_... model=glm-5.1`
- **Non-stream response:** `[response] id=resp_... model=glm-5.1 status=completed output_items=1`

---

## Extra: Run-time configuration

The following env vars are set in the run scripts to ensure compatibility:

| Variable | Value | Why |
|---|---|---|
| `ALLOW_DOWNGRADE_DEVELOPER=1` | Converts `role: developer` to `role: user` | Minimax and others reject `developer` role |
| `UPSTREAM_BASE_URL` | Provider-specific | Z.AI uses `https://api.z.ai/api/coding/paas` with path `/v4/chat/completions` |
