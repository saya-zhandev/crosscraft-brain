// Command crosscraft is the single-binary backend: it serves the REST + SSE API
// (and, later, the embedded SPA) against Postgres. See BUILD.md.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/api"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/auth"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/credtype"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/crypto"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/engine"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/llm"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/nodes/adobe"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/nodes/ai"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/nodes/comm"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/nodes/core"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/nodes/crm"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/nodes/database"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/nodes/dev"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/nodes/google"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/nodes/microsoft"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/nodes/payments"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/nodes/productivity"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/oauth"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/registry"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/scheduler"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/store"
	"github.com/CrossCraftAI/crosscraft-brain/server/web"
)

func main() {
	// --port flag overrides the PORT env var.
	portFlag := flag.String("port", "", "HTTP listen port (overrides PORT env var)")
	flag.Parse()

	port := *portFlag
	if port == "" {
		port = env("PORT", "8080")
	}

	dsn := env("DATABASE_URL", "postgres://crosscraft:crosscraft@localhost:5433/crosscraft")
	secret := env("CREDENTIALS_SECRET", strings.Repeat("0", 64))
	if secret == strings.Repeat("0", 64) {
		log.Println("⚠ SECURITY: CREDENTIALS_SECRET is the default all-zeros key — credentials are NOT securely encrypted. Set a 64-char hex key in production.")
	}

	ctx := context.Background()

	// Retry Postgres connection for up to 30 s — avoids a hard crash when Docker
	// Compose starts the server before Postgres finishes its first init.
	var pool *pgxpool.Pool
	var err error
	for attempt := 1; attempt <= 30; attempt++ {
		pool, err = pgxpool.New(ctx, dsn)
		if err == nil {
			err = pool.Ping(ctx)
		}
		if err == nil {
			break
		}
		if pool != nil {
			pool.Close()
			pool = nil
		}
		if attempt == 30 {
			log.Fatalf("Postgres not reachable after 30 attempts (%s): %v\n\nIs Postgres running? Try:\n  docker compose up -d postgres", dsn, err)
		}
		log.Printf("Waiting for Postgres at %s... (%d/30)", dsn, attempt)
		time.Sleep(1 * time.Second)
	}
	defer pool.Close()

	cipher, err := crypto.New(secret)
	if err != nil {
		log.Fatalf("credentials: %v", err)
	}

	llmClient := llm.New()
	reg := registry.New().
		Register(core.Nodes...).
		Register(ai.Nodes(llmClient)...).
		Register(google.Nodes()...).
		Register(microsoft.Nodes()...).
		Register(adobe.Nodes()...).
		Register(comm.Nodes()...).
		Register(productivity.Nodes()...).
		Register(crm.Nodes()...).
		Register(payments.Nodes()...).
		Register(dev.Nodes()...).
		Register(database.PostgresNode())
	st := store.New(pool, cipher)
	eng := engine.New(reg, st)
	// Bounded async pool: caps concurrently-executing workflows and recovers any
	// runs left 'running' by a previous process (durability across restart).
	eng.StartWorkers(ctx, 8, 256)

	// Credential types + OAuth2 flow. The engine uses the oauth service to mint
	// authenticated HTTP clients for integration (REST) nodes.
	credTypes := credtype.Default()
	oauthSvc := oauth.New(st, credTypes, env("PUBLIC_BASE_URL", "http://localhost:"+port))
	eng.SetClientProvider(oauthSvc)

	// Fire schedule/cron triggers on active workflows.
	scheduler.New(st, eng).Start(ctx)

	// API key auth for mobile / third-party clients (optional — keys are opt-in).
	authSvc := auth.New(pool)
	handler := api.NewRouter(reg, st, eng, llmClient, web.FS(), oauthSvc, credTypes, authSvc)

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Graceful shutdown on Ctrl+C (SIGINT) / SIGTERM.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("Received %v, shutting down gracefully...", sig)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}
	}()

	log.Printf("crosscraft Go backend listening on :%s (db ok)", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		if strings.Contains(err.Error(), "address already in use") ||
			strings.Contains(err.Error(), "Only one usage") {
			log.Fatalf("Port :%s is already in use. Is another server instance running?\nStop it first, or use: --port 8081  (or set PORT=8081)", port)
		}
		log.Fatalf("server: %v", err)
	}
	log.Println("Server stopped.")
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
