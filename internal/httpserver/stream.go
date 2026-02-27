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
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/Kurok1/openai-responses-adapter/internal/adapter"
	"github.com/Kurok1/openai-responses-adapter/internal/state"
)

type chatStreamChunk struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Role      string                `json:"role"`
			Content   string                `json:"content"`
			ToolCalls []chatStreamToolDelta `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

type chatStreamToolDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type streamToolCall struct {
	ID        string
	Name      string
	Arguments strings.Builder
}

func (h *Handler) streamResponses(
	w http.ResponseWriter,
	responseID string,
	respReq adapter.ResponsesRequest,
	history []adapter.ChatMessage,
	inputMessages []adapter.ChatMessage,
	upstreamResp *http.Response,
) error {
	defer upstreamResp.Body.Close()

	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming unsupported by response writer")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	created := map[string]interface{}{
		"type":        "response.created",
		"response_id": responseID,
		"model":       respReq.Model,
		"status":      "in_progress",
	}
	if err := writeSSEJSON(w, created); err != nil {
		return err
	}
	inProgress := map[string]interface{}{
		"type":        "response.in_progress",
		"response_id": responseID,
		"status":      "in_progress",
	}
	if err := writeSSEJSON(w, inProgress); err != nil {
		return err
	}
	flusher.Flush()

	assistant := adapter.ChatMessage{Role: "assistant"}
	contentBuilder := strings.Builder{}
	toolCalls := map[int]*streamToolCall{}
	toolAdded := map[int]bool{}
	messageAdded := false
	var upstreamModel string

	if err := consumeSSE(upstreamResp.Body, func(data []byte) error {
		if bytes.Equal(data, []byte("[DONE]")) {
			return nil
		}

		var chunk chatStreamChunk
		if err := json.Unmarshal(data, &chunk); err != nil {
			return nil
		}
		if chunk.Model != "" {
			upstreamModel = chunk.Model
		}
		if len(chunk.Choices) == 0 {
			return nil
		}

		delta := chunk.Choices[0].Delta
		if delta.Role != "" {
			assistant.Role = delta.Role
		}
		if delta.Content != "" {
			if !messageAdded {
				if err := writeSSEJSON(w, map[string]interface{}{
					"type":        "response.output_item.added",
					"response_id": responseID,
					"item": map[string]interface{}{
						"type": "message",
						"role": assistant.Role,
					},
				}); err != nil {
					return err
				}
				messageAdded = true
			}
			contentBuilder.WriteString(delta.Content)
			event := map[string]interface{}{
				"type":        "response.output_text.delta",
				"response_id": responseID,
				"delta":       delta.Content,
			}
			if err := writeSSEJSON(w, event); err != nil {
				return err
			}
			flusher.Flush()
		}

		for _, tc := range delta.ToolCalls {
			acc, ok := toolCalls[tc.Index]
			if !ok {
				acc = &streamToolCall{}
				toolCalls[tc.Index] = acc
			}
			if !toolAdded[tc.Index] {
				if err := writeSSEJSON(w, map[string]interface{}{
					"type":        "response.output_item.added",
					"response_id": responseID,
					"item": map[string]interface{}{
						"type":    "function_call",
						"call_id": tc.ID,
					},
				}); err != nil {
					return err
				}
				toolAdded[tc.Index] = true
			}
			if tc.ID != "" {
				acc.ID = tc.ID
			}
			if tc.Function.Name != "" {
				acc.Name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				acc.Arguments.WriteString(tc.Function.Arguments)
				event := map[string]interface{}{
					"type":        "response.function_call_arguments.delta",
					"response_id": responseID,
					"call_id":     acc.ID,
					"name":        acc.Name,
					"delta":       tc.Function.Arguments,
				}
				if err := writeSSEJSON(w, event); err != nil {
					return err
				}
				flusher.Flush()
			}
		}
		return nil
	}); err != nil {
		return err
	}

	assistant.Content = contentBuilder.String()
	if len(toolCalls) > 0 {
		ordered := make([]adapter.ChatToolCall, 0, len(toolCalls))
		for _, idx := range sortedToolIndexes(toolCalls) {
			tc := toolCalls[idx]
			ordered = append(ordered, adapter.ChatToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: adapter.ChatToolFunction{
					Name:      tc.Name,
					Arguments: tc.Arguments.String(),
				},
			})
		}
		assistant.ToolCalls = ordered
	}

	finalOut := adapter.BuildResponsesOutputFromAssistant(responseID, firstNonEmpty(upstreamModel, respReq.Model), assistant, nil)
	if adapter.ShouldStore(respReq) {
		h.store.Put(responseID, state.Record{
			Conversation: buildConversation(history, inputMessages, assistant),
			Response:     finalOut,
		})
	}

	for _, tc := range assistant.ToolCalls {
		if err := writeSSEJSON(w, map[string]interface{}{
			"type":        "response.function_call_arguments.done",
			"response_id": responseID,
			"call_id":     tc.ID,
			"name":        tc.Function.Name,
			"arguments":   tc.Function.Arguments,
		}); err != nil {
			return err
		}
		doneEvent := map[string]interface{}{
			"type":        "response.output_item.done",
			"response_id": responseID,
			"item": map[string]interface{}{
				"type":      "function_call",
				"call_id":   tc.ID,
				"name":      tc.Function.Name,
				"arguments": tc.Function.Arguments,
				"status":    "completed",
			},
		}
		if err := writeSSEJSON(w, doneEvent); err != nil {
			return err
		}
		flusher.Flush()
	}

	if assistant.Content != "" {
		if err := writeSSEJSON(w, map[string]interface{}{
			"type":        "response.output_text.done",
			"response_id": responseID,
			"text":        assistant.Content,
		}); err != nil {
			return err
		}
		messageDone := map[string]interface{}{
			"type":        "response.output_item.done",
			"response_id": responseID,
			"item": map[string]interface{}{
				"type": "message",
				"role": assistant.Role,
				"content": []map[string]interface{}{{
					"type": "output_text",
					"text": assistant.Content,
				}},
			},
		}
		if err := writeSSEJSON(w, messageDone); err != nil {
			return err
		}
		flusher.Flush()
	}

	completed := map[string]interface{}{
		"type":        "response.completed",
		"response_id": responseID,
		"response":    finalOut,
	}
	if err := writeSSEJSON(w, completed); err != nil {
		return err
	}
	if _, err := w.Write([]byte("data: [DONE]\n\n")); err != nil {
		return err
	}
	flusher.Flush()

	return nil
}

func consumeSSE(bodyReader io.Reader, onData func(data []byte) error) error {
	reader := bufio.NewReader(bodyReader)
	var dataLines [][]byte

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			trimmed := bytes.TrimSpace(line)
			if len(trimmed) == 0 {
				if len(dataLines) > 0 {
					joined := bytes.Join(dataLines, []byte("\n"))
					if err := onData(joined); err != nil {
						return err
					}
					dataLines = dataLines[:0]
				}
			} else if bytes.HasPrefix(trimmed, []byte("data:")) {
				data := bytes.TrimSpace(trimmed[5:])
				dataLines = append(dataLines, data)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if len(dataLines) > 0 {
					joined := bytes.Join(dataLines, []byte("\n"))
					if e := onData(joined); e != nil {
						return e
					}
				}
				return nil
			}
			return err
		}
	}
}

func writeSSEJSON(w http.ResponseWriter, payload interface{}) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal sse event: %w", err)
	}
	if _, err := w.Write([]byte("data: ")); err != nil {
		return err
	}
	if _, err := w.Write(b); err != nil {
		return err
	}
	if _, err := w.Write([]byte("\n\n")); err != nil {
		return err
	}
	return nil
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func sortedToolIndexes(toolCalls map[int]*streamToolCall) []int {
	indexes := make([]int, 0, len(toolCalls))
	for idx := range toolCalls {
		indexes = append(indexes, idx)
	}
	sort.Ints(indexes)
	return indexes
}
