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
	"os"
	"strings"
	"sync"
)

const maxAutoToolRounds = 8

type toolProvider interface {
	initialize(ctx context.Context) error
	listTools(ctx context.Context) ([]ToolDefinition, error)
	callTool(ctx context.Context, name string, rawArguments string) (string, error)
}

type Manager struct {
	mu       sync.RWMutex
	clients  map[string]toolProvider
	toolDefs map[string]ToolDefinition
}

func LoadManagerFromFile(path string) (*Manager, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read mcp config %s: %w", path, err)
	}
	var cfg FileConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse mcp config %s: %w", path, err)
	}
	if len(cfg.Servers) == 0 {
		return nil, nil
	}

	mgr := &Manager{
		clients:  make(map[string]toolProvider, len(cfg.Servers)),
		toolDefs: make(map[string]ToolDefinition),
	}

	ctx := context.Background()
	for _, s := range cfg.Servers {
		name := strings.TrimSpace(s.Name)
		if name == "" {
			return nil, fmt.Errorf("mcp server name is required")
		}
		if strings.TrimSpace(s.Endpoint) == "" {
			return nil, fmt.Errorf("mcp server endpoint is required for %s", name)
		}

		client := newRPCClient(s)
		if err := client.initialize(ctx); err != nil {
			return nil, fmt.Errorf("initialize mcp server %s: %w", name, err)
		}
		tools, err := client.listTools(ctx)
		if err != nil {
			return nil, fmt.Errorf("list tools from mcp server %s: %w", name, err)
		}

		mgr.clients[name] = client
		for _, tool := range tools {
			if _, exists := mgr.toolDefs[tool.Name]; exists {
				return nil, fmt.Errorf("duplicated mcp tool name across servers: %s", tool.Name)
			}
			mgr.toolDefs[tool.Name] = tool
		}
	}

	if len(mgr.toolDefs) == 0 {
		return nil, nil
	}
	return mgr, nil
}

func (m *Manager) HasTool(name string) bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.toolDefs[name]
	return ok
}

func (m *Manager) Tool(name string) (ToolDefinition, bool) {
	if m == nil {
		return ToolDefinition{}, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.toolDefs[name]
	return t, ok
}

func (m *Manager) CallTool(ctx context.Context, name string, rawArguments string) (string, error) {
	if m == nil {
		return "", fmt.Errorf("mcp manager is not configured")
	}

	m.mu.RLock()
	tool, ok := m.toolDefs[name]
	if !ok {
		m.mu.RUnlock()
		return "", fmt.Errorf("mcp tool not found: %s", name)
	}
	client, ok := m.clients[tool.ServerName]
	m.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("mcp server client not found: %s", tool.ServerName)
	}
	return client.callTool(ctx, name, rawArguments)
}

func MaxAutoToolRounds() int {
	return maxAutoToolRounds
}
