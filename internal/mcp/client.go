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

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const defaultHTTPTimeout = 15 * time.Second

type sdkClient struct {
	name    string
	url     string
	headers map[string]string
	http    *http.Client
	session *sdkmcp.ClientSession
}

func newRPCClient(cfg ServerConfig) *sdkClient {
	timeout := defaultHTTPTimeout
	if cfg.TimeoutMS > 0 {
		timeout = time.Duration(cfg.TimeoutMS) * time.Millisecond
	}

	return &sdkClient{
		name:    cfg.Name,
		url:     strings.TrimSpace(cfg.Endpoint),
		headers: cfg.Headers,
		http: &http.Client{
			Timeout: timeout,
			Transport: &headerRoundTripper{
				base:    http.DefaultTransport,
				headers: cfg.Headers,
			},
		},
	}
}

func (c *sdkClient) initialize(ctx context.Context) error {
	client := sdkmcp.NewClient(&sdkmcp.Implementation{
		Name:    "openai-responses-adapter",
		Version: "0.1.0",
	}, nil)

	session, err := client.Connect(ctx, &sdkmcp.StreamableClientTransport{
		Endpoint:             c.url,
		HTTPClient:           c.http,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		return fmt.Errorf("connect mcp server %s: %w", c.name, err)
	}
	c.session = session
	return nil
}

func (c *sdkClient) listTools(ctx context.Context) ([]ToolDefinition, error) {
	if c.session == nil {
		return nil, fmt.Errorf("mcp session is not initialized")
	}

	out := make([]ToolDefinition, 0)
	cursor := ""
	for {
		res, err := c.session.ListTools(ctx, &sdkmcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, fmt.Errorf("list tools on mcp server %s: %w", c.name, err)
		}
		for _, t := range res.Tools {
			if t == nil {
				continue
			}
			name := strings.TrimSpace(t.Name)
			if name == "" {
				continue
			}
			var schema json.RawMessage
			if t.InputSchema != nil {
				s, err := json.Marshal(t.InputSchema)
				if err != nil {
					return nil, fmt.Errorf("marshal input schema for tool %s: %w", name, err)
				}
				schema = s
			}
			out = append(out, ToolDefinition{
				ServerName:  c.name,
				Name:        name,
				Description: t.Description,
				InputSchema: schema,
			})
		}
		if strings.TrimSpace(res.NextCursor) == "" {
			break
		}
		cursor = res.NextCursor
	}
	return out, nil
}

func (c *sdkClient) callTool(ctx context.Context, name string, rawArguments string) (string, error) {
	if c.session == nil {
		return "", fmt.Errorf("mcp session is not initialized")
	}

	var args interface{}
	if strings.TrimSpace(rawArguments) != "" {
		var decoded interface{}
		if err := json.Unmarshal([]byte(rawArguments), &decoded); err != nil {
			return "", fmt.Errorf("decode tool arguments: %w", err)
		}
		args = decoded
	}

	res, err := c.session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		return "", fmt.Errorf("call mcp tool %s on server %s: %w", name, c.name, err)
	}
	if callErr := res.GetError(); callErr != nil {
		return "", fmt.Errorf("mcp tool %s returned error: %w", name, callErr)
	}

	return extractToolResult(res)
}

func extractToolResult(res *sdkmcp.CallToolResult) (string, error) {
	if res == nil {
		return "", nil
	}

	texts := make([]string, 0, len(res.Content))
	for _, content := range res.Content {
		switch c := content.(type) {
		case *sdkmcp.TextContent:
			if strings.TrimSpace(c.Text) != "" {
				texts = append(texts, c.Text)
			}
		default:
			b, err := json.Marshal(content)
			if err != nil {
				continue
			}
			var generic map[string]interface{}
			if err := json.Unmarshal(b, &generic); err != nil {
				continue
			}
			contentType, _ := generic["type"].(string)
			if contentType != "text" {
				continue
			}
			text, _ := generic["text"].(string)
			if strings.TrimSpace(text) != "" {
				texts = append(texts, text)
			}
		}
	}
	if len(texts) > 0 {
		return strings.Join(texts, "\n"), nil
	}
	if res.StructuredContent != nil {
		b, err := json.Marshal(res.StructuredContent)
		if err != nil {
			return "", fmt.Errorf("marshal structured content: %w", err)
		}
		return string(b), nil
	}
	return "", nil
}

type headerRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

func (rt *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	base := rt.base
	if base == nil {
		base = http.DefaultTransport
	}
	cloned := req.Clone(req.Context())
	for k, v := range rt.headers {
		cloned.Header.Set(k, v)
	}
	return base.RoundTrip(cloned)
}
