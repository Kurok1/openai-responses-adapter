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

package upstream

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Kurok1/openai-responses-adapter/internal/config"
)

type Client struct {
	baseURL  string
	chatPath string
	apiKey   string
	http     *http.Client
}

func NewClient(cfg config.Config) *Client {
	return &Client{
		baseURL:  strings.TrimRight(cfg.UpstreamBaseURL, "/"),
		chatPath: cfg.UpstreamChatPath,
		apiKey:   cfg.UpstreamAPIKey,
		http: &http.Client{
			Timeout: 90 * time.Second,
		},
	}
}

func (c *Client) CallChatCompletions(ctx context.Context, body []byte, inboundAuth string) (*http.Response, error) {
	url := c.baseURL + c.chatPath

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create upstream request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if auth := c.selectAuthorization(inboundAuth); auth != "" {
		req.Header.Set("Authorization", auth)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call upstream chat completions: %w", err)
	}
	return resp, nil
}

func (c *Client) selectAuthorization(inboundAuth string) string {
	if strings.TrimSpace(inboundAuth) != "" {
		return inboundAuth
	}
	if strings.TrimSpace(c.apiKey) != "" {
		return "Bearer " + c.apiKey
	}
	return ""
}

func ReadBodyAndClose(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read upstream response body: %w", err)
	}
	return body, nil
}
