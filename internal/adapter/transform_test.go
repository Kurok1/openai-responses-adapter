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

package adapter

import (
	"encoding/json"
	"testing"
)

func TestBuildInputMessages_String(t *testing.T) {
	msgs, err := BuildInputMessages(json.RawMessage(`"hello"`))
	if err != nil {
		t.Fatalf("BuildInputMessages error: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "hello" {
		t.Fatalf("unexpected message: %+v", msgs[0])
	}
}

func TestBuildInputMessages_ArrayWithFunctionCallOutput(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"message","role":"user","content":"question"},
		{"type":"function_call_output","call_id":"call_1","output":"{\"ok\":true}"}
	]`)

	msgs, err := BuildInputMessages(raw)
	if err != nil {
		t.Fatalf("BuildInputMessages error: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[1].Role != "tool" || msgs[1].ToolCallID != "call_1" {
		t.Fatalf("unexpected tool message: %+v", msgs[1])
	}
}

func TestShouldStore_DefaultTrue(t *testing.T) {
	req := ResponsesRequest{}
	if !ShouldStore(req) {
		t.Fatalf("expected default store to be true")
	}
}

func TestShouldStore_ExplicitFalse(t *testing.T) {
	v := false
	req := ResponsesRequest{Store: &v}
	if ShouldStore(req) {
		t.Fatalf("expected explicit store=false")
	}
}

func TestBuildChatCompletionRequest_ToolsAndToolChoice(t *testing.T) {
	req := ResponsesRequest{
		Model: "gpt-4o-mini",
		Tools: []ResponseTool{
			{
				Type:        "function",
				Name:        "get_weather",
				Description: "Get weather",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`),
			},
		},
		ToolChoice: json.RawMessage(`{"type":"function","name":"get_weather"}`),
	}

	chatReq, err := BuildChatCompletionRequest(req, nil, []ChatMessage{{Role: "user", Content: "weather in tokyo"}})
	if err != nil {
		t.Fatalf("BuildChatCompletionRequest error: %v", err)
	}
	if len(chatReq.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(chatReq.Tools))
	}
	if !chatReq.Tools[0].Function.Strict {
		t.Fatalf("expected strict default true")
	}
	tc, ok := chatReq.ToolChoice.(ChatToolChoice)
	if !ok {
		t.Fatalf("expected tool_choice ChatToolChoice, got %T", chatReq.ToolChoice)
	}
	if tc.Function.Name != "get_weather" {
		t.Fatalf("unexpected tool_choice: %+v", tc)
	}
}

func TestBuildResponsesOutput_FunctionCalls(t *testing.T) {
	up := ChatCompletionResponse{
		Model: "gpt-4o-mini",
		Choices: []struct {
			Message ChatMessage `json:"message"`
		}{
			{
				Message: ChatMessage{
					Role: "assistant",
					ToolCalls: []ChatToolCall{
						{
							ID:   "call_123",
							Type: "function",
							Function: ChatToolFunction{
								Name:      "get_weather",
								Arguments: `{"city":"Tokyo"}`,
							},
						},
					},
				},
			},
		},
	}

	out, assistant, err := BuildResponsesOutput("resp_1", up)
	if err != nil {
		t.Fatalf("BuildResponsesOutput error: %v", err)
	}
	if len(out.Output) != 1 {
		t.Fatalf("expected 1 output item, got %d", len(out.Output))
	}
	if out.Output[0].Type != "function_call" || out.Output[0].CallID != "call_123" {
		t.Fatalf("unexpected output item: %+v", out.Output[0])
	}
	if len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].ID != "call_123" {
		t.Fatalf("unexpected assistant tool_calls: %+v", assistant.ToolCalls)
	}
}
