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

package config

import (
	"os"
	"strconv"
	"time"
)

const (
	defaultListenAddr       = ":8080"
	defaultUpstreamBaseURL  = "https://api.openai.com"
	defaultUpstreamChatPath = "/v1/chat/completions"
	defaultStoreMaxEntries  = 1000
	defaultStoreTTL         = 3600 * time.Second
)

// Config holds process-level settings.
type Config struct {
	ListenAddr       string
	UpstreamBaseURL  string
	UpstreamChatPath string
	UpstreamAPIKey   string
	StoreMaxEntries  int
	StoreTTL         time.Duration
}

func LoadFromEnv() Config {
	return Config{
		ListenAddr:       getenv("LISTEN_ADDR", defaultListenAddr),
		UpstreamBaseURL:  getenv("UPSTREAM_BASE_URL", defaultUpstreamBaseURL),
		UpstreamChatPath: getenv("UPSTREAM_CHAT_PATH", defaultUpstreamChatPath),
		UpstreamAPIKey:   os.Getenv("UPSTREAM_API_KEY"),
		StoreMaxEntries:  getenvInt("STORE_MAX_ENTRIES", defaultStoreMaxEntries),
		StoreTTL:         getenvDuration("STORE_TTL", defaultStoreTTL),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}
