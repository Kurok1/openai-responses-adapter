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
	"fmt"
	"log"
	"strings"
)

type ResponsesRequest struct {
	Model              string          `json:"model"`
	Input              json.RawMessage `json:"input"`
	Instructions       string          `json:"instructions,omitempty"`
	Tools              []ResponseTool  `json:"tools,omitempty"`
	ToolChoice         json.RawMessage `json:"tool_choice,omitempty"`
	Stream             bool            `json:"stream,omitempty"`
	PreviousResponseID string          `json:"previous_response_id,omitempty"`
	Store              *bool           `json:"store,omitempty"`
	DowngradeDeveloper *bool           `json:"downgrade_developer_to_user,omitempty"`
}

type ResponseTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
	Raw         json.RawMessage `json:"-"`
}

func (t *ResponseTool) UnmarshalJSON(data []byte) error {
	type alias ResponseTool
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*t = ResponseTool(decoded)
	t.Raw = append([]byte(nil), data...)
	return nil
}

type ChatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Name       string         `json:"name,omitempty"`
	ToolCalls  []ChatToolCall `json:"tool_calls,omitempty"`
}

type ChatToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ChatToolFunction `json:"function"`
}

type ChatToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ChatTool struct {
	Type     string       `json:"type"`
	Function ChatFunction `json:"function"`
}

type ChatFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      bool            `json:"strict"`
}

type ChatToolChoiceFunction struct {
	Name string `json:"name"`
}

type ChatToolChoice struct {
	Type     string                 `json:"type"`
	Function ChatToolChoiceFunction `json:"function,omitempty"`
}

type ChatCompletionRequest struct {
	Model      string        `json:"model"`
	Messages   []ChatMessage `json:"messages"`
	Tools      []interface{} `json:"tools,omitempty"`
	ToolChoice interface{}   `json:"tool_choice,omitempty"`
	Stream     bool          `json:"stream,omitempty"`
}

type ChatCompletionResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message ChatMessage `json:"message"`
	} `json:"choices"`
	Usage map[string]interface{} `json:"usage,omitempty"`
}

type ResponseContentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ResponseOutputItem struct {
	Type      string                `json:"type"`
	Role      string                `json:"role,omitempty"`
	Content   []ResponseContentPart `json:"content,omitempty"`
	CallID    string                `json:"call_id,omitempty"`
	Name      string                `json:"name,omitempty"`
	Arguments string                `json:"arguments,omitempty"`
	Status    string                `json:"status,omitempty"`
}

type ResponsesOutput struct {
	ID         string                 `json:"id"`
	Object     string                 `json:"object"`
	Model      string                 `json:"model"`
	Status     string                 `json:"status"`
	Output     []ResponseOutputItem   `json:"output"`
	OutputText string                 `json:"output_text"`
	Usage      map[string]interface{} `json:"usage,omitempty"`
}

func ParseResponsesRequest(raw []byte) (ResponsesRequest, error) {
	var req ResponsesRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return ResponsesRequest{}, fmt.Errorf("unmarshal responses request: %w", err)
	}
	if strings.TrimSpace(req.Model) == "" {
		return ResponsesRequest{}, fmt.Errorf("field model is required")
	}
	if len(req.Input) == 0 {
		return ResponsesRequest{}, fmt.Errorf("field input is required")
	}
	return req, nil
}

func BuildInputMessages(input json.RawMessage) ([]ChatMessage, error) {
	var asString string
	if err := json.Unmarshal(input, &asString); err == nil {
		if strings.TrimSpace(asString) == "" {
			return nil, fmt.Errorf("field input cannot be empty")
		}
		return []ChatMessage{{Role: "user", Content: asString}}, nil
	}

	var items []map[string]interface{}
	if err := json.Unmarshal(input, &items); err != nil {
		return nil, fmt.Errorf("field input must be string or item array")
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("field input cannot be empty")
	}

	messages := make([]ChatMessage, 0, len(items))
	for _, item := range items {
		msg, err := itemToMessage(item)
		if err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

func BuildChatCompletionRequest(req ResponsesRequest, history []ChatMessage, inputMessages []ChatMessage, downgradeDeveloper bool) (ChatCompletionRequest, error) {
	messages := make([]ChatMessage, 0, len(history)+len(inputMessages)+1)
	if strings.TrimSpace(req.Instructions) != "" {
		messages = append(messages, ChatMessage{
			Role:    "system",
			Content: req.Instructions,
		})
	}
	messages = append(messages, history...)
	messages = append(messages, inputMessages...)
	messages = normalizeOutboundRoles(messages, downgradeDeveloper)

	tools, err := mapTools(req.Tools)
	if err != nil {
		return ChatCompletionRequest{}, err
	}
	toolChoice, err := mapToolChoice(req.ToolChoice)
	if err != nil {
		return ChatCompletionRequest{}, err
	}

	return ChatCompletionRequest{
		Model:      req.Model,
		Messages:   messages,
		Tools:      tools,
		ToolChoice: toolChoice,
		Stream:     req.Stream,
	}, nil
}

func normalizeOutboundRoles(messages []ChatMessage, downgradeDeveloper bool) []ChatMessage {
	if !downgradeDeveloper {
		return messages
	}
	for i := range messages {
		if messages[i].Role == "developer" {
			messages[i].Role = "user"
		}
	}
	return messages
}

func BuildResponsesOutput(respID string, upstreamResp ChatCompletionResponse) (ResponsesOutput, ChatMessage, error) {
	if len(upstreamResp.Choices) == 0 {
		return ResponsesOutput{}, ChatMessage{}, fmt.Errorf("upstream response has no choices")
	}
	assistant := upstreamResp.Choices[0].Message
	if assistant.Role == "" {
		assistant.Role = "assistant"
	}

	return BuildResponsesOutputFromAssistant(respID, upstreamResp.Model, assistant, upstreamResp.Usage), assistant, nil
}

func ShouldStore(req ResponsesRequest) bool {
	if req.Store == nil {
		return true
	}
	return *req.Store
}

func BuildResponsesOutputFromAssistant(respID, model string, assistant ChatMessage, usage map[string]interface{}) ResponsesOutput {
	output := make([]ResponseOutputItem, 0, len(assistant.ToolCalls)+1)
	outputText := assistant.Content
	if strings.TrimSpace(assistant.Content) != "" {
		output = append(output, ResponseOutputItem{
			Type: "message",
			Role: assistant.Role,
			Content: []ResponseContentPart{
				{Type: "output_text", Text: assistant.Content},
			},
		})
	}

	for _, tc := range assistant.ToolCalls {
		output = append(output, ResponseOutputItem{
			Type:      "function_call",
			CallID:    tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
			Status:    "completed",
		})
	}

	if len(output) == 0 {
		output = append(output, ResponseOutputItem{
			Type: "message",
			Role: assistant.Role,
			Content: []ResponseContentPart{
				{Type: "output_text", Text: ""},
			},
		})
	}

	return ResponsesOutput{
		ID:         respID,
		Object:     "response",
		Model:      model,
		Status:     "completed",
		Output:     output,
		OutputText: outputText,
		Usage:      usage,
	}
}

func mapTools(tools []ResponseTool) ([]interface{}, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]interface{}, 0, len(tools))
	for _, t := range tools {
		if strings.TrimSpace(t.Type) == "" {
			return nil, fmt.Errorf("tool.type is required")
		}
		if t.Type == "function" {
			if strings.TrimSpace(t.Name) == "" {
				return nil, fmt.Errorf("tool.name is required for function tool")
			}
			strict := true
			if t.Strict != nil {
				strict = *t.Strict
			}
			out = append(out, ChatTool{
				Type: "function",
				Function: ChatFunction{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  t.Parameters,
					Strict:      strict,
				},
			})
			continue
		}

		log.Printf("[tool-filter] dropping non-function tool: type=%s name=%s", t.Type, t.Name)
		continue
	}
	return out, nil
}

func mapToolChoice(raw json.RawMessage) (interface{}, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		switch asString {
		case "none", "auto", "required":
			return asString, nil
		default:
			return nil, fmt.Errorf("unsupported tool_choice string: %s", asString)
		}
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("invalid tool_choice format")
	}

	typeName, _ := obj["type"].(string)
	if strings.TrimSpace(typeName) == "" {
		return nil, fmt.Errorf("tool_choice.type is required")
	}
	if typeName == "function" {
		name, _ := obj["name"].(string)
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("tool_choice.name is required for function")
		}

		return ChatToolChoice{
			Type:     "function",
			Function: ChatToolChoiceFunction{Name: name},
		}, nil
	}

	return obj, nil
}

func itemToMessage(item map[string]interface{}) (ChatMessage, error) {
	itemType, _ := item["type"].(string)
	switch itemType {
	case "", "message":
		role := "user"
		if r, ok := item["role"].(string); ok && strings.TrimSpace(r) != "" {
			role = r
		}
		content, err := messageContentToString(item["content"])
		if err != nil {
			return ChatMessage{}, err
		}
		return ChatMessage{Role: role, Content: content}, nil
	case "function_call_output":
		callID, _ := item["call_id"].(string)
		if strings.TrimSpace(callID) == "" {
			return ChatMessage{}, fmt.Errorf("function_call_output.call_id is required")
		}
		output, err := normalizeOutput(item["output"])
		if err != nil {
			return ChatMessage{}, err
		}
		return ChatMessage{Role: "tool", ToolCallID: callID, Content: output}, nil
	case "function_call":
		callID, _ := item["call_id"].(string)
		name, _ := item["name"].(string)
		args, _ := item["arguments"].(string)
		return ChatMessage{
			Role: "assistant",
			ToolCalls: []ChatToolCall{
				{
					ID:       callID,
					Type:     "function",
					Function: ChatToolFunction{Name: name, Arguments: args},
				},
			},
		}, nil
	default:
		return ChatMessage{}, fmt.Errorf("unsupported input item type: %s", itemType)
	}
}

func messageContentToString(v interface{}) (string, error) {
	if s, ok := v.(string); ok {
	return s, nil
	}

	parts, ok := v.([]interface{})
	if !ok {
		return "", fmt.Errorf("message content must be string or content array")
	}

	builder := strings.Builder{}
	for _, p := range parts {
		part, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		partType, _ := part["type"].(string)
		if partType != "input_text" && partType != "text" {
			continue
		}
		text, _ := part["text"].(string)
		if text == "" {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString(text)
	}
	if builder.Len() == 0 {
		return "", nil
	}
	return builder.String(), nil
}

func normalizeOutput(v interface{}) (string, error) {
	if s, ok := v.(string); ok {
		return s, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal function_call_output.output: %w", err)
	}
	return string(b), nil
}
