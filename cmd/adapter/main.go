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

package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/Kurok1/openai-responses-adapter/internal/config"
	"github.com/Kurok1/openai-responses-adapter/internal/httpserver"
	"github.com/Kurok1/openai-responses-adapter/internal/mcp"
	"github.com/Kurok1/openai-responses-adapter/internal/state"
	"github.com/Kurok1/openai-responses-adapter/internal/upstream"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cfg := config.LoadFromEnv()
	log.Printf("starting adapter version=%s commit=%s date=%s", version, commit, date)

	client := upstream.NewClient(cfg)
	store := state.NewMemoryStore(cfg.StoreMaxEntries, cfg.StoreTTL)
	mcpManager, err := mcp.LoadManagerFromFile(cfg.MCPConfigPath)
	if err != nil {
		log.Fatalf("load mcp manager: %v", err)
	}
	if mcpManager != nil {
		log.Printf("mcp manager enabled config=%s", cfg.MCPConfigPath)
	}
	handler := httpserver.NewHandlerWithMCP(cfg, client, store, mcpManager)

	srv := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("adapter listening on %s", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen and serve: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()
	log.Printf("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("server shutdown: %v", err)
	}
}
