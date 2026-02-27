// Copyright 2026 Kurok1
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package httpserver

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kurok1/openai-responses-adapter/internal/adapter"
	"github.com/Kurok1/openai-responses-adapter/internal/config"
	"github.com/Kurok1/openai-responses-adapter/internal/mcp"
	"github.com/Kurok1/openai-responses-adapter/internal/state"
	"github.com/Kurok1/openai-responses-adapter/internal/upstream"
)

type testResponse struct {
	ID string `json:"id"`
}

type functionCallOutputResponse struct {
	ID     string `json:"id"`
	Output []struct {
		Type   string `json:"type"`
		CallID string `json:"call_id"`
		Name   string `json:"name"`
	} `json:"output"`
}

func TestResponses_PreviousResponseID(t *testing.T) {
	var (
		mu       sync.Mutex
		requests []adapter.ChatCompletionRequest
	)

	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req adapter.ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		mu.Lock()
		requests = append(requests, req)
		idx := len(requests)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","model":"gpt-4o-mini","choices":[{"message":{"role":"assistant","content":"reply` + string(rune('0'+idx)) + `"}}],"usage":{"total_tokens":10}}`))
	}))
	defer up.Close()

	cfg := config.Config{
		UpstreamBaseURL:  up.URL,
		UpstreamChatPath: "/v1/chat/completions",
		StoreMaxEntries:  100,
		StoreTTL:         time.Minute,
	}
	client := upstream.NewClient(cfg)
	store := state.NewMemoryStore(cfg.StoreMaxEntries, cfg.StoreTTL)
	h := httptest.NewServer(NewHandler(cfg, client, store))
	defer h.Close()

	firstBody := []byte(`{"model":"gpt-4o-mini","input":"hello"}`)
	resp1, err := http.Post(h.URL+"/v1/responses", "application/json", bytes.NewReader(firstBody))
	if err != nil {
		t.Fatalf("first post: %v", err)
	}
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("first status: %d", resp1.StatusCode)
	}

	var out1 testResponse
	if err := json.NewDecoder(resp1.Body).Decode(&out1); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	if out1.ID == "" {
		t.Fatalf("first response id empty")
	}

	secondBody := []byte(`{"model":"gpt-4o-mini","input":"next","previous_response_id":"` + out1.ID + `"}`)
	resp2, err := http.Post(h.URL+"/v1/responses", "application/json", bytes.NewReader(secondBody))
	if err != nil {
		t.Fatalf("second post: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second status: %d", resp2.StatusCode)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("expected 2 upstream requests, got %d", len(requests))
	}
	if got := len(requests[1].Messages); got != 3 {
		t.Fatalf("expected second upstream messages len=3, got %d", got)
	}
	if requests[1].Messages[0].Content != "hello" || requests[1].Messages[1].Role != "assistant" || requests[1].Messages[2].Content != "next" {
		t.Fatalf("unexpected second conversation: %+v", requests[1].Messages)
	}
}

func TestResponses_PreviousResponseIDNotFound(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","model":"gpt-4o-mini","choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer up.Close()

	cfg := config.Config{UpstreamBaseURL: up.URL, UpstreamChatPath: "/v1/chat/completions", StoreMaxEntries: 10, StoreTTL: time.Minute}
	client := upstream.NewClient(cfg)
	h := httptest.NewServer(NewHandler(cfg, client, state.NewMemoryStore(10, time.Minute)))
	defer h.Close()

	body := []byte(`{"model":"gpt-4o-mini","input":"hello","previous_response_id":"resp_missing"}`)
	resp, err := http.Post(h.URL+"/v1/responses", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestResponses_FunctionCallOutputWithPreviousResponseID(t *testing.T) {
	var (
		mu       sync.Mutex
		requests []adapter.ChatCompletionRequest
	)

	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req adapter.ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		mu.Lock()
		requests = append(requests, req)
		idx := len(requests)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if idx == 1 {
			_, _ = w.Write([]byte(`{"id":"chatcmpl_1","model":"gpt-4o-mini","choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_abc","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Tokyo\"}"}}]}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"chatcmpl_2","model":"gpt-4o-mini","choices":[{"message":{"role":"assistant","content":"晴天"}}]}`))
	}))
	defer up.Close()

	cfg := config.Config{
		UpstreamBaseURL:  up.URL,
		UpstreamChatPath: "/v1/chat/completions",
		StoreMaxEntries:  100,
		StoreTTL:         time.Minute,
	}
	client := upstream.NewClient(cfg)
	store := state.NewMemoryStore(cfg.StoreMaxEntries, cfg.StoreTTL)
	h := httptest.NewServer(NewHandler(cfg, client, store))
	defer h.Close()

	firstBody := []byte(`{"model":"gpt-4o-mini","input":"先查东京天气","tools":[{"type":"function","name":"get_weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}]}`)
	resp1, err := http.Post(h.URL+"/v1/responses", "application/json", bytes.NewReader(firstBody))
	if err != nil {
		t.Fatalf("first post: %v", err)
	}
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("first status: %d", resp1.StatusCode)
	}

	var firstOut functionCallOutputResponse
	if err := json.NewDecoder(resp1.Body).Decode(&firstOut); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	if firstOut.ID == "" {
		t.Fatalf("first response id empty")
	}
	if len(firstOut.Output) != 1 || firstOut.Output[0].Type != "function_call" || firstOut.Output[0].CallID != "call_abc" {
		t.Fatalf("unexpected first output: %+v", firstOut.Output)
	}

	secondBody := []byte(`{"model":"gpt-4o-mini","previous_response_id":"` + firstOut.ID + `","input":[{"type":"function_call_output","call_id":"call_abc","output":{"temp_c":12}}]}`)
	resp2, err := http.Post(h.URL+"/v1/responses", "application/json", bytes.NewReader(secondBody))
	if err != nil {
		t.Fatalf("second post: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second status: %d", resp2.StatusCode)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("expected 2 upstream requests, got %d", len(requests))
	}
	secondReq := requests[1]
	if len(secondReq.Messages) != 3 {
		t.Fatalf("expected 3 messages in second upstream request, got %d", len(secondReq.Messages))
	}
	if secondReq.Messages[1].Role != "assistant" || len(secondReq.Messages[1].ToolCalls) != 1 || secondReq.Messages[1].ToolCalls[0].ID != "call_abc" {
		t.Fatalf("assistant history missing tool_calls: %+v", secondReq.Messages[1])
	}
	if secondReq.Messages[2].Role != "tool" || secondReq.Messages[2].ToolCallID != "call_abc" || secondReq.Messages[2].Content != `{"temp_c":12}` {
		t.Fatalf("tool output message mismatch: %+v", secondReq.Messages[2])
	}
}

func TestResponses_StreamSSE(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_stream\",\"model\":\"gpt-4o-mini\",\"choices\":[{\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_stream\",\"model\":\"gpt-4o-mini\",\"choices\":[{\"delta\":{\"content\":\"hel\"},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_stream\",\"model\":\"gpt-4o-mini\",\"choices\":[{\"delta\":{\"content\":\"lo\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer up.Close()

	cfg := config.Config{
		UpstreamBaseURL:  up.URL,
		UpstreamChatPath: "/v1/chat/completions",
		StoreMaxEntries:  100,
		StoreTTL:         time.Minute,
	}
	client := upstream.NewClient(cfg)
	h := httptest.NewServer(NewHandler(cfg, client, state.NewMemoryStore(100, time.Minute)))
	defer h.Close()

	reqBody := []byte(`{"model":"gpt-4o-mini","input":"hi","stream":true}`)
	resp, err := http.Post(h.URL+"/v1/responses", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post stream request: %v", err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("unexpected content-type: %s", got)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read stream response body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, `"type":"response.created"`) {
		t.Fatalf("missing response.created event: %s", text)
	}
	if !strings.Contains(text, `"type":"response.in_progress"`) {
		t.Fatalf("missing response.in_progress event: %s", text)
	}
	if !strings.Contains(text, `"type":"response.output_item.added"`) {
		t.Fatalf("missing response.output_item.added event: %s", text)
	}
	if !strings.Contains(text, `"type":"response.output_text.delta"`) {
		t.Fatalf("missing response.output_text.delta event: %s", text)
	}
	if !strings.Contains(text, `"delta":"hel"`) || !strings.Contains(text, `"delta":"lo"`) {
		t.Fatalf("missing expected text deltas: %s", text)
	}
	if !strings.Contains(text, `"type":"response.output_text.done"`) {
		t.Fatalf("missing response.output_text.done event: %s", text)
	}
	if !strings.Contains(text, `"type":"response.completed"`) {
		t.Fatalf("missing response.completed event: %s", text)
	}
	if !strings.Contains(text, "data: [DONE]") {
		t.Fatalf("missing done marker: %s", text)
	}
}

func TestResponses_StreamSSEFunctionCallsMultiToolSparseIndexes(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_stream\",\"model\":\"gpt-4o-mini\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":2,\"id\":\"call_b\",\"type\":\"function\",\"function\":{\"name\":\"fn_b\",\"arguments\":\"{\\\"b\\\":\"}}]},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_stream\",\"model\":\"gpt-4o-mini\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_a\",\"type\":\"function\",\"function\":{\"name\":\"fn_a\",\"arguments\":\"{\\\"a\\\":\"}}]},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_stream\",\"model\":\"gpt-4o-mini\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":2,\"function\":{\"arguments\":\"1}\"}},{\"index\":0,\"function\":{\"arguments\":\"2}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer up.Close()

	cfg := config.Config{
		UpstreamBaseURL:  up.URL,
		UpstreamChatPath: "/v1/chat/completions",
		StoreMaxEntries:  100,
		StoreTTL:         time.Minute,
	}
	client := upstream.NewClient(cfg)
	h := httptest.NewServer(NewHandler(cfg, client, state.NewMemoryStore(100, time.Minute)))
	defer h.Close()

	reqBody := []byte(`{"model":"gpt-4o-mini","input":"call tools","stream":true}`)
	resp, err := http.Post(h.URL+"/v1/responses", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post stream request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read stream response body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, `"type":"response.function_call_arguments.delta"`) {
		t.Fatalf("missing arguments delta event: %s", text)
	}
	if !strings.Contains(text, `"call_id":"call_a"`) || !strings.Contains(text, `"call_id":"call_b"`) {
		t.Fatalf("missing expected call ids: %s", text)
	}
	if !strings.Contains(text, `"type":"response.function_call_arguments.done"`) {
		t.Fatalf("missing arguments done event: %s", text)
	}
	if !strings.Contains(text, `"arguments":"{\"a\":2}"`) || !strings.Contains(text, `"arguments":"{\"b\":1}"`) {
		t.Fatalf("missing merged arguments output: %s", text)
	}
}

func TestGetResponseByID(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","model":"gpt-4o-mini","choices":[{"message":{"role":"assistant","content":"hello"}}]}`))
	}))
	defer up.Close()

	cfg := config.Config{
		UpstreamBaseURL:  up.URL,
		UpstreamChatPath: "/v1/chat/completions",
		StoreMaxEntries:  100,
		StoreTTL:         time.Minute,
	}
	client := upstream.NewClient(cfg)
	h := httptest.NewServer(NewHandler(cfg, client, state.NewMemoryStore(100, time.Minute)))
	defer h.Close()

	createReq := []byte(`{"model":"gpt-4o-mini","input":"hi","store":true}`)
	resp, err := http.Post(h.URL+"/v1/responses", "application/json", bytes.NewReader(createReq))
	if err != nil {
		t.Fatalf("create response: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected create status: %d", resp.StatusCode)
	}

	var created adapter.ResponsesOutput
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created response: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("empty created id")
	}

	getResp, err := http.Get(h.URL + "/v1/responses/" + created.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected get status: %d", getResp.StatusCode)
	}

	var fetched adapter.ResponsesOutput
	if err := json.NewDecoder(getResp.Body).Decode(&fetched); err != nil {
		t.Fatalf("decode fetched response: %v", err)
	}
	if fetched.ID != created.ID || fetched.OutputText != "hello" {
		t.Fatalf("unexpected fetched response: %+v", fetched)
	}
}

func TestGetResponseByIDNotFound(t *testing.T) {
	cfg := config.Config{
		UpstreamBaseURL:  "http://127.0.0.1:1",
		UpstreamChatPath: "/v1/chat/completions",
		StoreMaxEntries:  100,
		StoreTTL:         time.Minute,
	}
	client := upstream.NewClient(cfg)
	h := httptest.NewServer(NewHandler(cfg, client, state.NewMemoryStore(100, time.Minute)))
	defer h.Close()

	resp, err := http.Get(h.URL + "/v1/responses/resp_missing")
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestResponses_MCPNativeToolRewriteAndAutoExecute(t *testing.T) {
	var upstreamReqBodies [][]byte
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read upstream request body: %v", err)
		}
		upstreamReqBodies = append(upstreamReqBodies, body)

		w.Header().Set("Content-Type", "application/json")
		if len(upstreamReqBodies) == 1 {
			_, _ = w.Write([]byte(`{"id":"chatcmpl_1","model":"gpt-5","choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"current president of france\"}"}}]}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"chatcmpl_2","model":"gpt-5","choices":[{"message":{"role":"assistant","content":"France's current president is Emmanuel Macron."}}]}`))
	}))
	defer up.Close()

	var mcpCallCount int
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req struct {
			Method string                 `json:"method"`
			Params map[string]interface{} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode mcp request: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26","capabilities":{"tools":{}},"serverInfo":{"name":"test-mcp","version":"1.0.0"}}}`))
		case "notifications/initialized":
			_, _ = w.Write([]byte(`{}`))
		case "tools/list":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"web_search","description":"Web search","inputSchema":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}}]}}`))
		case "tools/call":
			mcpCallCount++
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"{\"items\":[{\"title\":\"France President\",\"snippet\":\"Emmanuel Macron\"}]}"}]}}`))
		default:
			t.Fatalf("unexpected mcp method: %s", req.Method)
		}
	}))
	defer mcpServer.Close()

	tempDir := t.TempDir()
	mcpConfigPath := filepath.Join(tempDir, "mcp.json")
	mcpConfig := `{"mcpServers":{"test":{"type":"http","url":"` + mcpServer.URL + `"}}}`
	if err := os.WriteFile(mcpConfigPath, []byte(mcpConfig), 0644); err != nil {
		t.Fatalf("write mcp config: %v", err)
	}
	mcpManager, err := mcp.LoadManagerFromFile(mcpConfigPath)
	if err != nil {
		t.Fatalf("load mcp manager: %v", err)
	}
	if mcpManager == nil {
		t.Fatalf("mcp manager should not be nil")
	}

	cfg := config.Config{
		UpstreamBaseURL:  up.URL,
		UpstreamChatPath: "/v1/chat/completions",
		StoreMaxEntries:  100,
		StoreTTL:         time.Minute,
	}
	client := upstream.NewClient(cfg)
	h := httptest.NewServer(NewHandlerWithMCP(cfg, client, state.NewMemoryStore(100, time.Minute), mcpManager))
	defer h.Close()

	reqBody := []byte(`{"model":"gpt-5","input":"Who is the current president of France?","tools":[{"type":"web_search"}]}`)
	resp, err := http.Post(h.URL+"/v1/responses", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post responses request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status=%d body=%s", resp.StatusCode, body)
	}

	var out adapter.ResponsesOutput
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode final response: %v", err)
	}
	if !strings.Contains(out.OutputText, "Emmanuel Macron") {
		t.Fatalf("unexpected output_text: %s", out.OutputText)
	}
	if mcpCallCount != 1 {
		t.Fatalf("expected mcp tool to be called once, got %d", mcpCallCount)
	}
	if len(upstreamReqBodies) != 2 {
		t.Fatalf("expected 2 upstream calls, got %d", len(upstreamReqBodies))
	}

	var firstReq map[string]interface{}
	if err := json.Unmarshal(upstreamReqBodies[0], &firstReq); err != nil {
		t.Fatalf("decode first upstream request: %v", err)
	}
	tools, _ := firstReq["tools"].([]interface{})
	if len(tools) != 1 {
		t.Fatalf("expected one upstream tool, got %+v", firstReq["tools"])
	}
	firstTool, _ := tools[0].(map[string]interface{})
	if firstTool["type"] != "function" {
		t.Fatalf("expected rewritten function tool, got %+v", firstTool)
	}
	fn, _ := firstTool["function"].(map[string]interface{})
	if fn["name"] != "web_search" {
		t.Fatalf("expected function name web_search, got %+v", fn)
	}

	var secondReq map[string]interface{}
	if err := json.Unmarshal(upstreamReqBodies[1], &secondReq); err != nil {
		t.Fatalf("decode second upstream request: %v", err)
	}
	msgs, _ := secondReq["messages"].([]interface{})
	foundToolMsg := false
	for _, m := range msgs {
		msg, _ := m.(map[string]interface{})
		if msg["role"] == "tool" && msg["tool_call_id"] == "call_1" {
			foundToolMsg = true
		}
	}
	if !foundToolMsg {
		t.Fatalf("second request missing tool result message: %+v", secondReq["messages"])
	}
}
