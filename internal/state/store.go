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
	"container/list"
	"sync"
	"time"

	"github.com/Kurok1/openai-responses-adapter/internal/adapter"
)

type Conversation struct {
	Messages []adapter.ChatMessage
}

type Record struct {
	Conversation Conversation
	Response     adapter.ResponsesOutput
}

type Store interface {
	GetConversation(responseID string) (Conversation, bool)
	GetResponse(responseID string) (adapter.ResponsesOutput, bool)
	Put(responseID string, record Record)
}

type MemoryStore struct {
	mu         sync.Mutex
	items      map[string]*entry
	lru        *list.List
	maxEntries int
	ttl        time.Duration
	now        func() time.Time
}

type entry struct {
	key       string
	record    Record
	expiresAt time.Time
	elem      *list.Element
}

func NewMemoryStore(maxEntries int, ttl time.Duration) *MemoryStore {
	if maxEntries <= 0 {
		maxEntries = 1000
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &MemoryStore{
		items:      make(map[string]*entry, maxEntries),
		lru:        list.New(),
		maxEntries: maxEntries,
		ttl:        ttl,
		now:        time.Now,
	}
}

func (s *MemoryStore) GetConversation(responseID string) (Conversation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.items[responseID]
	if !ok {
		return Conversation{}, false
	}
	if s.now().After(e.expiresAt) {
		s.remove(e)
		return Conversation{}, false
	}
	s.lru.MoveToFront(e.elem)
	return cloneConversation(e.record.Conversation), true
}

func (s *MemoryStore) GetResponse(responseID string) (adapter.ResponsesOutput, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.items[responseID]
	if !ok {
		return adapter.ResponsesOutput{}, false
	}
	if s.now().After(e.expiresAt) {
		s.remove(e)
		return adapter.ResponsesOutput{}, false
	}
	s.lru.MoveToFront(e.elem)
	return cloneResponse(e.record.Response), true
}

func (s *MemoryStore) Put(responseID string, record Record) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if e, ok := s.items[responseID]; ok {
		e.record = cloneRecord(record)
		e.expiresAt = s.now().Add(s.ttl)
		s.lru.MoveToFront(e.elem)
		return
	}

	e := &entry{
		key:       responseID,
		record:    cloneRecord(record),
		expiresAt: s.now().Add(s.ttl),
	}
	e.elem = s.lru.PushFront(e)
	s.items[responseID] = e

	for len(s.items) > s.maxEntries {
		back := s.lru.Back()
		if back == nil {
			break
		}
		victim := back.Value.(*entry)
		s.remove(victim)
	}
}

func (s *MemoryStore) remove(e *entry) {
	delete(s.items, e.key)
	s.lru.Remove(e.elem)
}

func cloneConversation(c Conversation) Conversation {
	out := Conversation{Messages: make([]adapter.ChatMessage, len(c.Messages))}
	copy(out.Messages, c.Messages)
	return out
}

func cloneRecord(r Record) Record {
	return Record{
		Conversation: cloneConversation(r.Conversation),
		Response:     cloneResponse(r.Response),
	}
}

func cloneResponse(resp adapter.ResponsesOutput) adapter.ResponsesOutput {
	out := resp
	out.Output = make([]adapter.ResponseOutputItem, len(resp.Output))
	copy(out.Output, resp.Output)
	out.Usage = cloneUsage(resp.Usage)
	return out
}

func cloneUsage(usage map[string]interface{}) map[string]interface{} {
	if usage == nil {
		return nil
	}
	out := make(map[string]interface{}, len(usage))
	for k, v := range usage {
		out[k] = v
	}
	return out
}
