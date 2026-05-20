// knowlib-api: HTTP REST + MCP server, behind Caddy.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/charlunn/knowlib/api/internal/auth"
	"github.com/charlunn/knowlib/api/internal/autotidy"
	"github.com/charlunn/knowlib/api/internal/config"
	"github.com/charlunn/knowlib/api/internal/handlers"
	"github.com/charlunn/knowlib/api/internal/httpx"
	"github.com/charlunn/knowlib/api/internal/mcpsrv"
	"github.com/charlunn/knowlib/api/internal/store"
	"github.com/charlunn/knowlib/api/internal/vault"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	st, err := store.Open(cfg.StatePath, []byte(cfg.JWTSecret))
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	v := vault.New(cfg.VaultPath, cfg.InboxDir, cfg.NotesDir, cfg.AtlasDir, cfg.KnowlibDir)
	embed := httpx.NewEmbedClient(cfg.EmbedBaseURL, cfg.EmbedModel)
	qdrant := httpx.NewQdrantClient(cfg.QdrantURL, cfg.QdrantCollection)
	tidy := httpx.NewTidyClient(cfg.TidyServiceURL)

	deps := &handlers.Deps{
		Cfg:    cfg,
		Vault:  v,
		Store:  st,
		Embed:  embed,
		Qdrant: qdrant,
		Tidy:   tidy,
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	// Public: login is rate-limited but unauthenticated.
	r.Post("/api/auth/login", handlers.Login(deps))
	r.Post("/api/auth/logout", handlers.Logout(deps))

	// Authenticated routes — accept session JWT (cookie or Bearer) or API token.
	r.Group(func(g chi.Router) {
		g.Use(auth.RequireAuth(cfg.JWTSecret, st))

		g.Post("/api/capture", handlers.Capture(deps))
		g.Get("/api/inbox", handlers.Inbox(deps))
		g.Get("/api/list", handlers.List(deps))
		g.Get("/api/note", handlers.Note(deps))
		g.Get("/api/search", handlers.Search(deps))
		g.Post("/api/tidy", handlers.Tidy(deps))
		g.Post("/api/ai-action", handlers.AIAction(deps))
		g.Post("/api/ai-action/apply", handlers.AIActionApply(deps))

		g.Get("/api/settings", handlers.GetSettings(deps))
		g.Put("/api/settings", handlers.PutSettings(deps))
		g.Post("/api/settings/test-llm", handlers.TestLLM(deps))

		g.Get("/api/tokens", handlers.ListTokens(deps))
		g.Post("/api/tokens", handlers.CreateToken(deps))
		g.Delete("/api/tokens/{id}", handlers.RevokeToken(deps))

		g.Get("/api/skill-bundle.zip", handlers.SkillBundle(deps))

		// MCP — same auth (typically API token).
		mcp := mcpsrv.New(deps)
		g.Method("GET", "/mcp", mcp)
		g.Method("POST", "/mcp", mcp)
	})

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Start auto-tidy scheduler.
	sched := autotidy.New(cfg, st, v, tidy)
	sched.Start()
	defer sched.Stop()

	// Graceful shutdown so a docker stop doesn't abort an in-flight tidy call.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go func() {
		log.Printf("knowlib-api listening on %s", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()
	<-ctx.Done()
	log.Printf("shutting down")
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutCancel()
	_ = srv.Shutdown(shutCtx)
}
