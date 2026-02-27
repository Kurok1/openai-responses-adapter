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

func TestShouldDowngradeDeveloperRole_DefaultTrue(t *testing.T) {
	req := ResponsesRequest{}
	if !ShouldDowngradeDeveloperRole(req) {
		t.Fatalf("expected default downgrade_developer_to_user=true")
	}
}

func TestShouldDowngradeDeveloperRole_ExplicitFalse(t *testing.T) {
	v := false
	req := ResponsesRequest{DowngradeDeveloper: &v}
	if ShouldDowngradeDeveloperRole(req) {
		t.Fatalf("expected downgrade_developer_to_user=false")
	}
}

func TestParseResponsesRequest_CapturesRawTool(t *testing.T) {
	raw := []byte(`{"model":"gpt-5","input":"hi","tools":[{"type":"web_search","search_context_size":"medium"}]}`)
	req, err := ParseResponsesRequest(raw)
	if err != nil {
		t.Fatalf("ParseResponsesRequest error: %v", err)
	}
	if len(req.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(req.Tools))
	}
	if len(req.Tools[0].Raw) == 0 {
		t.Fatalf("expected raw tool payload to be captured")
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
	tool, ok := chatReq.Tools[0].(ChatTool)
	if !ok {
		t.Fatalf("expected ChatTool, got %T", chatReq.Tools[0])
	}
	if !tool.Function.Strict {
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

func TestBuildChatCompletionRequest_BuiltinToolsPassThrough(t *testing.T) {
	req := ResponsesRequest{
		Model: "gpt-5",
		Tools: []ResponseTool{
			{Type: "web_search", Raw: json.RawMessage(`{"type":"web_search"}`)},
		},
		ToolChoice: json.RawMessage(`{"type":"web_search"}`),
	}

	chatReq, err := BuildChatCompletionRequest(req, nil, []ChatMessage{{Role: "user", Content: "who is president of france"}})
	if err != nil {
		t.Fatalf("BuildChatCompletionRequest error: %v", err)
	}
	if len(chatReq.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(chatReq.Tools))
	}
	passthrough, ok := chatReq.Tools[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected passthrough tool map, got %T", chatReq.Tools[0])
	}
	if passthrough["type"] != "web_search" {
		t.Fatalf("unexpected tool type: %#v", passthrough["type"])
	}

	tc, ok := chatReq.ToolChoice.(map[string]interface{})
	if !ok {
		t.Fatalf("expected passthrough tool_choice map, got %T", chatReq.ToolChoice)
	}
	if tc["type"] != "web_search" {
		t.Fatalf("unexpected tool_choice type: %#v", tc["type"])
	}
}

func TestBuildChatCompletionRequest_DeveloperRoleDowngradedToUser(t *testing.T) {
	req := ResponsesRequest{Model: "gpt-5"}
	history := []ChatMessage{
		{Role: "developer", Content: "history developer message"},
		{Role: "assistant", Content: "history assistant message"},
	}
	input := []ChatMessage{
		{Role: "developer", Content: "current developer message"},
	}

	chatReq, err := BuildChatCompletionRequest(req, history, input)
	if err != nil {
		t.Fatalf("BuildChatCompletionRequest error: %v", err)
	}
	if len(chatReq.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(chatReq.Messages))
	}
	if chatReq.Messages[0].Role != "user" {
		t.Fatalf("expected history developer role to be downgraded to user, got %s", chatReq.Messages[0].Role)
	}
	if chatReq.Messages[1].Role != "assistant" {
		t.Fatalf("expected assistant role unchanged, got %s", chatReq.Messages[1].Role)
	}
	if chatReq.Messages[2].Role != "user" {
		t.Fatalf("expected input developer role to be downgraded to user, got %s", chatReq.Messages[2].Role)
	}
}

func TestBuildChatCompletionRequest_DeveloperRoleKeepWhenDisabled(t *testing.T) {
	downgrade := false
	req := ResponsesRequest{
		Model:              "gpt-5",
		DowngradeDeveloper: &downgrade,
	}
	input := []ChatMessage{
		{Role: "developer", Content: "current developer message"},
	}

	chatReq, err := BuildChatCompletionRequest(req, nil, input)
	if err != nil {
		t.Fatalf("BuildChatCompletionRequest error: %v", err)
	}
	if len(chatReq.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(chatReq.Messages))
	}
	if chatReq.Messages[0].Role != "developer" {
		t.Fatalf("expected developer role unchanged when disabled, got %s", chatReq.Messages[0].Role)
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
