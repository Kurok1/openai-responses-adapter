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
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/Kurok1/openai-responses-adapter/internal/adapter"
	"github.com/Kurok1/openai-responses-adapter/internal/config"
	"github.com/Kurok1/openai-responses-adapter/internal/state"
	"github.com/Kurok1/openai-responses-adapter/internal/upstream"
)

type Handler struct {
	cfg    config.Config
	client *upstream.Client
	store  state.Store
}

func NewHandler(cfg config.Config, client *upstream.Client, store state.Store) http.Handler {
	h := &Handler{cfg: cfg, client: client, store: store}

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

	chatReq, err := adapter.BuildChatCompletionRequest(respReq, history, inputMessages)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}
	chatBody, err := json.Marshal(chatReq)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "marshal upstream request failed", "server_error")
		return
	}

	resp, err := h.client.CallChatCompletions(r.Context(), chatBody, r.Header.Get("Authorization"))
	if err != nil {
		log.Printf("upstream call failed: %v", err)
		writeError(w, http.StatusBadGateway, "upstream call failed", "upstream_error")
		return
	}

	responseID := newResponseID()
	if respReq.Stream {
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
		return
	}

	respBody, err := upstream.ReadBodyAndClose(resp)
	if err != nil {
		log.Printf("upstream response read failed: %v", err)
		writeError(w, http.StatusBadGateway, "upstream response read failed", "upstream_error")
		return
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		writeUpstreamError(w, resp.StatusCode, respBody)
		return
	}

	var chatResp adapter.ChatCompletionResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		log.Printf("unmarshal upstream response failed: %v", err)
		writeError(w, http.StatusBadGateway, "invalid upstream response format", "upstream_error")
		return
	}

	out, assistantMessage, err := adapter.BuildResponsesOutput(responseID, chatResp)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error(), "upstream_error")
		return
	}

	if adapter.ShouldStore(respReq) {
		conv := buildConversation(history, inputMessages, assistantMessage)
		h.store.Put(responseID, state.Record{
			Conversation: conv,
			Response:     out,
		})
	}

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
