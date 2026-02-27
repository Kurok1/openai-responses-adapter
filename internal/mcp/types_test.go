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
	"testing"
)

func TestFileConfigUnmarshal_StandardServersRejected(t *testing.T) {
	raw := []byte(`{
		"servers": [
			{"name":"a","endpoint":"https://a.example.com/mcp"}
		]
	}`)
	var cfg FileConfig
	err := json.Unmarshal(raw, &cfg)
	if err == nil {
		t.Fatalf("expected standard servers format to be rejected")
	}
}

func TestFileConfigUnmarshal_ClaudeMCPServers(t *testing.T) {
	raw := []byte(`{
		"mcpServers": {
			"web-search-prime": {
				"type": "http",
				"url": "https://open.bigmodel.cn/api/mcp/web_search_prime/mcp",
				"headers": {
					"Authorization": "Bearer your_api_key"
				}
			}
		}
	}`)

	var cfg FileConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if len(cfg.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(cfg.Servers))
	}
	s := cfg.Servers[0]
	if s.Name != "web-search-prime" {
		t.Fatalf("unexpected server name: %s", s.Name)
	}
	if s.Endpoint != "https://open.bigmodel.cn/api/mcp/web_search_prime/mcp" {
		t.Fatalf("unexpected endpoint: %s", s.Endpoint)
	}
	if s.Headers["Authorization"] != "Bearer your_api_key" {
		t.Fatalf("unexpected headers: %+v", s.Headers)
	}
}

func TestFileConfigUnmarshal_ClaudeUnsupportedTypeSkipped(t *testing.T) {
	raw := []byte(`{
		"mcpServers": {
			"stdio-server": {
				"type": "stdio",
				"url": "unused"
			}
		}
	}`)

	var cfg FileConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if len(cfg.Servers) != 0 {
		t.Fatalf("expected unsupported type to be skipped, got %+v", cfg.Servers)
	}
}
