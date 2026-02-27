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
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type FileConfig struct {
	Servers []ServerConfig
}

func (c *FileConfig) UnmarshalJSON(data []byte) error {
	type claudeServer struct {
		Type      string            `json:"type,omitempty"`
		URL       string            `json:"url,omitempty"`
		Endpoint  string            `json:"endpoint,omitempty"`
		Headers   map[string]string `json:"headers,omitempty"`
		TimeoutMS int               `json:"timeout_ms,omitempty"`
	}
	type claudeConfig struct {
		Servers map[string]claudeServer `json:"mcpServers"`
		Legacy  []json.RawMessage       `json:"servers"`
	}
	var claude claudeConfig
	if err := json.Unmarshal(data, &claude); err != nil {
		return err
	}
	if len(claude.Legacy) > 0 {
		return fmt.Errorf("unsupported mcp config format: use claude-style mcpServers")
	}
	if len(claude.Servers) == 0 {
		c.Servers = nil
		return nil
	}

	names := make([]string, 0, len(claude.Servers))
	for name := range claude.Servers {
		names = append(names, name)
	}
	sort.Strings(names)

	servers := make([]ServerConfig, 0, len(names))
	for _, name := range names {
		s := claude.Servers[name]
		if strings.TrimSpace(s.Type) != "" && strings.TrimSpace(s.Type) != "http" {
			continue
		}

		endpoint := strings.TrimSpace(s.Endpoint)
		if endpoint == "" {
			endpoint = strings.TrimSpace(s.URL)
		}
		servers = append(servers, ServerConfig{
			Name:      name,
			Endpoint:  endpoint,
			Headers:   s.Headers,
			TimeoutMS: s.TimeoutMS,
		})
	}
	c.Servers = servers
	return nil
}

type ServerConfig struct {
	Name      string            `json:"name"`
	Endpoint  string            `json:"endpoint"`
	Headers   map[string]string `json:"headers,omitempty"`
	TimeoutMS int               `json:"timeout_ms,omitempty"`
}

type ToolDefinition struct {
	ServerName  string          `json:"server_name"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}
