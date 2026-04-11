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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/Kurok1/openai-responses-adapter/internal/adapter"
	"github.com/Kurok1/openai-responses-adapter/internal/config"
	"github.com/Kurok1/openai-responses-adapter/internal/mcp"
	"github.com/Kurok1/openai-responses-adapter/internal/state"
	"github.com/Kurok1/openai-responses-adapter/internal/upstream"
)

type Handler struct {
	cfg    config.Config
	client *upstream.Client
	store  state.Store
	mcp    *mcp.Manager
}

func NewHandler(cfg config.Config, client *upstream.Client, store state.Store) http.Handler {
	return NewHandlerWithMCP(cfg, client, store, nil)
}

func NewHandlerWithMCP(cfg config.Config, client *upstream.Client, store state.Store, mcpManager *mcp.Manager) http.Handler {
	h := &Handler{cfg: cfg, client: client, store: store, mcp: mcpManager}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", h.healthz)
	mux.HandleFunc("/v1/responses", h.responses)
	mux.HandleFunc("/v1/responses/", h.getResponseByID)
	return mux
}

func (h *Handler) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) responses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}

	defer r.Body.Close()
	raw, err := ioReadAllLimit(r, 10<<20)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}

	respReq, err := adapter.ParseResponsesRequest(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}

	log.Printf("[request] model=%s stream=%v tools=%d prev_id=%s input_len=%d",
		respReq.Model, respReq.Stream, len(respReq.Tools), respReq.PreviousResponseID, len(respReq.Input))
	respReq, err = h.rewriteNativeTools(respReq)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}

	history := make([]adapter.ChatMessage, 0)
	if respReq.PreviousResponseID != "" {
		conv, ok := h.store.GetConversation(respReq.PreviousResponseID)
		if !ok {
			writeError(w, http.StatusBadRequest, "previous_response_id not found", "invalid_request_error")
			return
		}
		history = append(history, conv.Messages...)
	}

	inputMessages, err := adapter.BuildInputMessages(respReq.Input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}

	chatReq, err := adapter.BuildChatCompletionRequest(respReq, history, inputMessages, h.cfg.AllowDowngradeDeveloperRole)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}

	responseID := newResponseID()
	if respReq.Stream {
		resp, err := h.callUpstreamChat(r.Context(), chatReq, r.Header.Get("Authorization"))
		if err != nil {
			log.Printf("upstream call failed: %v", err)
			writeError(w, http.StatusBadGateway, "upstream call failed", "upstream_error")
			return
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			respBody, err := upstream.ReadBodyAndClose(resp)
			if err != nil {
				log.Printf("upstream response read failed: %v", err)
				writeError(w, http.StatusBadGateway, "upstream response read failed", "upstream_error")
				return
			}
			writeUpstreamError(w, resp.StatusCode, respBody)
			return
		}
		if err := h.streamResponses(w, responseID, respReq, history, inputMessages, resp); err != nil {
			log.Printf("stream responses failed: %v", err)
		}
		log.Printf("[stream-done] id=%s model=%s", responseID, respReq.Model)
		return
	}

	out, conversationMessages, err := h.completeWithMCPTools(r.Context(), respReq, chatReq, r.Header.Get("Authorization"), history, inputMessages, responseID)
	if err != nil {
		var upstreamErr upstreamHTTPError
		if ok := errors.As(err, &upstreamErr); ok {
			writeUpstreamError(w, upstreamErr.statusCode, upstreamErr.body)
			return
		}
		log.Printf("complete with mcp tools failed: %v", err)
		writeError(w, http.StatusBadGateway, err.Error(), "upstream_error")
		return
	}

	if adapter.ShouldStore(respReq) {
		h.store.Put(responseID, state.Record{
			Conversation: state.Conversation{Messages: conversationMessages},
			Response:     out,
		})
	}

	log.Printf("[response] id=%s model=%s status=%s output_items=%d", out.ID, out.Model, out.Status, len(out.Output))
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) getResponseByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}

	responseID := strings.TrimPrefix(r.URL.Path, "/v1/responses/")
	if strings.TrimSpace(responseID) == "" {
		writeError(w, http.StatusBadRequest, "response id is required", "invalid_request_error")
		return
	}

	resp, ok := h.store.GetResponse(responseID)
	if !ok {
		writeError(w, http.StatusNotFound, "response not found", "invalid_request_error")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func buildConversation(history, inputMessages []adapter.ChatMessage, assistant adapter.ChatMessage) state.Conversation {
	conv := state.Conversation{Messages: make([]adapter.ChatMessage, 0, len(history)+len(inputMessages)+1)}
	conv.Messages = append(conv.Messages, history...)
	conv.Messages = append(conv.Messages, inputMessages...)
	conv.Messages = append(conv.Messages, assistant)
	return conv
}

func (h *Handler) rewriteNativeTools(req adapter.ResponsesRequest) (adapter.ResponsesRequest, error) {
	if h.mcp == nil {
		return req, nil
	}

	if len(req.Tools) > 0 {
		tools := make([]adapter.ResponseTool, 0, len(req.Tools))
		for _, t := range req.Tools {
			if t.Type == "function" {
				tools = append(tools, t)
				continue
			}
			def, ok := h.mcp.Tool(t.Type)
			if !ok {
				tools = append(tools, t)
				continue
			}
			strict := false
			tools = append(tools, adapter.ResponseTool{
				Type:        "function",
				Name:        def.Name,
				Description: def.Description,
				Parameters:  def.InputSchema,
				Strict:      &strict,
			})
		}
		req.Tools = tools
	}

	if len(req.ToolChoice) == 0 {
		return req, nil
	}

	var tcObj map[string]interface{}
	if err := json.Unmarshal(req.ToolChoice, &tcObj); err != nil {
		return req, nil
	}
	typeName, _ := tcObj["type"].(string)
	if typeName == "" || typeName == "function" {
		return req, nil
	}
	if !h.mcp.HasTool(typeName) {
		return req, nil
	}
	converted, err := json.Marshal(map[string]interface{}{
		"type": "function",
		"name": typeName,
	})
	if err != nil {
		return req, err
	}
	req.ToolChoice = converted
	return req, nil
}

func (h *Handler) callUpstreamChat(ctx context.Context, chatReq adapter.ChatCompletionRequest, inboundAuth string) (*http.Response, error) {
	chatBody, err := json.Marshal(chatReq)
	if err != nil {
		return nil, err
	}
	return h.client.CallChatCompletions(ctx, chatBody, inboundAuth)
}

func decodeChatCompletionResponse(resp *http.Response) (adapter.ChatCompletionResponse, error) {
	respBody, err := upstream.ReadBodyAndClose(resp)
	if err != nil {
		return adapter.ChatCompletionResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return adapter.ChatCompletionResponse{}, upstreamHTTPError{statusCode: resp.StatusCode, body: respBody}
	}

	var chatResp adapter.ChatCompletionResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return adapter.ChatCompletionResponse{}, err
	}
	return chatResp, nil
}

type upstreamHTTPError struct {
	statusCode int
	body       []byte
}

func (e upstreamHTTPError) Error() string {
	return "upstream returned non-success status"
}

func (h *Handler) completeWithMCPTools(
	ctx context.Context,
	respReq adapter.ResponsesRequest,
	chatReq adapter.ChatCompletionRequest,
	inboundAuth string,
	history []adapter.ChatMessage,
	inputMessages []adapter.ChatMessage,
	responseID string,
) (adapter.ResponsesOutput, []adapter.ChatMessage, error) {
	resp, err := h.callUpstreamChat(ctx, chatReq, inboundAuth)
	if err != nil {
		return adapter.ResponsesOutput{}, nil, err
	}
	chatResp, err := decodeChatCompletionResponse(resp)
	if err != nil {
		return adapter.ResponsesOutput{}, nil, err
	}

	conversation := make([]adapter.ChatMessage, 0, len(history)+len(inputMessages)+mcp.MaxAutoToolRounds()*2+1)
	conversation = append(conversation, history...)
	conversation = append(conversation, inputMessages...)

	currentReq := chatReq
	for i := 0; i < mcp.MaxAutoToolRounds(); i++ {
		if len(chatResp.Choices) == 0 {
			return adapter.ResponsesOutput{}, nil, fmt.Errorf("upstream response has no choices")
		}
		assistant := chatResp.Choices[0].Message
		if assistant.Role == "" {
			assistant.Role = "assistant"
		}
		conversation = append(conversation, assistant)

		out := adapter.BuildResponsesOutputFromAssistant(responseID, chatResp.Model, assistant, chatResp.Usage)
		if len(assistant.ToolCalls) == 0 {
			return out, conversation, nil
		}
		if h.mcp == nil || !h.allToolCallsResolvable(assistant.ToolCalls) {
			return out, conversation, nil
		}

		toolMessages := make([]adapter.ChatMessage, 0, len(assistant.ToolCalls))
		for _, tc := range assistant.ToolCalls {
			toolOutput, callErr := h.mcp.CallTool(ctx, tc.Function.Name, tc.Function.Arguments)
			if callErr != nil {
				return adapter.ResponsesOutput{}, nil, fmt.Errorf("call mcp tool %s: %w", tc.Function.Name, callErr)
			}
			msg := adapter.ChatMessage{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    toolOutput,
			}
			toolMessages = append(toolMessages, msg)
			conversation = append(conversation, msg)
		}

		currentReq.Messages = append(currentReq.Messages, assistant)
		currentReq.Messages = append(currentReq.Messages, toolMessages...)
		resp, callErr := h.callUpstreamChat(ctx, currentReq, inboundAuth)
		if callErr != nil {
			return adapter.ResponsesOutput{}, nil, callErr
		}
		chatResp, callErr = decodeChatCompletionResponse(resp)
		if callErr != nil {
			return adapter.ResponsesOutput{}, nil, callErr
		}
	}

	return adapter.ResponsesOutput{}, nil, fmt.Errorf("too many tool-call rounds")
}

func (h *Handler) allToolCallsResolvable(calls []adapter.ChatToolCall) bool {
	for _, tc := range calls {
		if !h.mcp.HasTool(tc.Function.Name) {
			return false
		}
	}
	return true
}
