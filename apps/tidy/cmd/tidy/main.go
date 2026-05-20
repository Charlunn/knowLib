// tidy worker — long-running HTTP service.
//   POST /run         — process inbox files (existing behavior).
//   POST /ai-action   — batch AI ops on existing notes (deep_tidy, polish_logic,
//                       rewrite, knowledge_check).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/charlunn/knowlib/tidy/internal/aiops"
	"github.com/charlunn/knowlib/tidy/internal/config"
	"github.com/charlunn/knowlib/tidy/internal/llm"
	"github.com/charlunn/knowlib/tidy/internal/tidy"
	"github.com/charlunn/knowlib/tidy/internal/vaultfs"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	engine := tidy.NewEngine(cfg)

	// Shared vaultfs / llm for ai-action endpoint.
	fs := vaultfs.New(cfg.VaultPath, cfg.InboxDir, cfg.NotesDir, cfg.AtlasDir, cfg.KnowlibDir)
	defaultLLM := llm.New(cfg.OpenAIBaseURL, cfg.OpenAIAPIKey, cfg.OpenAIModel)
	promptDir := filepath.Join(cfg.VaultPath, cfg.KnowlibDir, "prompts")
	aiEngine := aiops.New(fs, defaultLLM, promptDir)

	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	mux.HandleFunc("/run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var in struct {
			Paths     []string        `json:"paths"`
			All       bool            `json:"all"`
			Overrides *tidy.Overrides `json:"overrides,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
		defer cancel()
		out, err := engine.Run(ctx, in.Paths, in.All, in.Overrides)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})

	mux.HandleFunc("/ai-action", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var in aiops.Request
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
		defer cancel()
		out, err := aiEngine.Run(ctx, in)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})

	mux.HandleFunc("/ai-action/apply", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		// Body is the previously-returned response; we just apply its operations.
		var in aiops.Response
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		// Apply via a no-LLM fast path: synthesise a Request with PreviewOnly=false
		// and the pre-resolved operations. We bypass the LLM by going directly
		// through the engine's apply method (exposed via a thin wrapper).
		if err := aiEngine.ApplyOperations(in.Operations); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"applied":true}`))
	})

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go func() {
		log.Printf("tidy listening on %s", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()
	<-ctx.Done()
	log.Printf("shutting down")
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutCancel()
	_ = srv.Shutdown(shutCtx)
}
