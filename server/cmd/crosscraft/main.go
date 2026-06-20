// Command crosscraft is the single-binary backend: it serves the REST + SSE API
// (and, later, the embedded SPA) against Postgres. See BUILD.md.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/api"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/credtype"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/crypto"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/engine"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/llm"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/nodes/ai"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/nodes/core"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/oauth"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/registry"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/store"
	"github.com/CrossCraftAI/crosscraft-brain/server/web"
)

func main() {
	dsn := env("DATABASE_URL", "postgres://crosscraft:crosscraft@localhost:5433/crosscraft")
	secret := env("CREDENTIALS_SECRET", strings.Repeat("0", 64))
	port := env("PORT", "8080")

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatalf("connect db: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("ping db (%s): %v", dsn, err)
	}

	cipher, err := crypto.New(secret)
	if err != nil {
		log.Fatalf("credentials: %v", err)
	}

	llmClient := llm.New()
	reg := registry.New().Register(core.Nodes...).Register(ai.Nodes(llmClient)...)
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

	handler := api.NewRouter(reg, st, eng, llmClient, web.FS(), oauthSvc, credTypes)

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("crosscraft Go backend listening on :%s (db ok)", port)
	log.Fatal(srv.ListenAndServe())
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
