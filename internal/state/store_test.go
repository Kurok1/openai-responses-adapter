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

package state

import (
	"testing"
	"time"

	"github.com/Kurok1/openai-responses-adapter/internal/adapter"
)

func TestMemoryStore_PutGet(t *testing.T) {
	s := NewMemoryStore(2, time.Minute)
	s.Put("resp_1", Record{
		Conversation: Conversation{Messages: []adapter.ChatMessage{{Role: "user", Content: "hi"}}},
		Response:     adapter.ResponsesOutput{ID: "resp_1", Object: "response"},
	})

	conv, ok := s.GetConversation("resp_1")
	if !ok {
		t.Fatalf("expected key resp_1")
	}
	if len(conv.Messages) != 1 || conv.Messages[0].Content != "hi" {
		t.Fatalf("unexpected conversation: %+v", conv)
	}

	resp, ok := s.GetResponse("resp_1")
	if !ok || resp.ID != "resp_1" {
		t.Fatalf("unexpected response: %+v (ok=%v)", resp, ok)
	}
}

func TestMemoryStore_LRUEvict(t *testing.T) {
	s := NewMemoryStore(2, time.Minute)
	s.Put("a", Record{Conversation: Conversation{}})
	s.Put("b", Record{Conversation: Conversation{}})
	_, _ = s.GetConversation("a")
	s.Put("c", Record{Conversation: Conversation{}})

	if _, ok := s.GetConversation("b"); ok {
		t.Fatalf("expected b to be evicted")
	}
	if _, ok := s.GetConversation("a"); !ok {
		t.Fatalf("expected a to remain")
	}
}

func TestMemoryStore_Expiration(t *testing.T) {
	s := NewMemoryStore(2, time.Second)
	now := time.Now()
	s.now = func() time.Time { return now }
	s.Put("a", Record{Conversation: Conversation{}})
	now = now.Add(2 * time.Second)

	if _, ok := s.GetConversation("a"); ok {
		t.Fatalf("expected a to expire")
	}
}
