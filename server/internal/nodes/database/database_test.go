package database

import (
	"context"
	"os"
	"testing"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/credtype"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresNodeDefinition(t *testing.T) {
	def := PostgresNode()
	if def.Type != "database.postgres" {
		t.Fatalf("unexpected type: %s", def.Type)
	}
	if len(def.Params) == 0 {
		t.Fatal("expected params to be defined")
	}
	if def.Execute == nil {
		t.Fatal("expected execute function to be set")
	}
	if len(def.Outputs) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(def.Outputs))
	}
}

func TestPostgresNodeUsesRegisteredCredentialType(t *testing.T) {
	reg := credtype.Default()
	credType, ok := reg.Get("postgres")
	if !ok {
		t.Fatal("expected postgres credential type to be registered")
	}
	if credType.DisplayName == "" {
		t.Fatal("expected postgres credential type to have a display name")
	}
	if len(credType.Fields) == 0 {
		t.Fatal("expected postgres credential fields to be defined")
	}
}

func TestResolvePostgresDSNUsesDatabaseURLFallback(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://app:secret@db:5432/appdb")
	ctx := &schema.ExecContext{}
	dsn, err := resolvePostgresDSN(ctx)
	if err != nil {
		t.Fatalf("expected DATABASE_URL fallback, got error: %v", err)
	}
	if dsn != os.Getenv("DATABASE_URL") {
		t.Fatalf("expected fallback DSN %q, got %q", os.Getenv("DATABASE_URL"), dsn)
	}
}

func TestExecutePostgresRequiresCredential(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	ctx := &schema.ExecContext{
		Params: map[string]any{"query": "select 1", "operation": "query:many"},
		Credential: func(paramName string) (map[string]any, error) {
			return nil, nil
		},
	}
	_, err := executePostgres(ctx)
	if err == nil {
		t.Fatal("expected error when credential and DATABASE_URL are missing")
	}
}

func TestGetOrCreatePostgresPoolCachesByDSN(t *testing.T) {
	originalFactory := newPostgresPool
	postgresPoolMu.Lock()
	postgresPoolCache = map[string]*pgxpool.Pool{}
	postgresPoolMu.Unlock()
	t.Cleanup(func() {
		newPostgresPool = originalFactory
		postgresPoolMu.Lock()
		postgresPoolCache = map[string]*pgxpool.Pool{}
		postgresPoolMu.Unlock()
	})

	var createCalls int
	newPostgresPool = func(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
		createCalls++
		return &pgxpool.Pool{}, nil
	}

	pool1, err := getOrCreatePostgresPool(context.Background(), "postgres://example")
	if err != nil {
		t.Fatalf("expected first pool creation to succeed: %v", err)
	}
	pool2, err := getOrCreatePostgresPool(context.Background(), "postgres://example")
	if err != nil {
		t.Fatalf("expected cached pool reuse to succeed: %v", err)
	}

	if createCalls != 1 {
		t.Fatalf("expected one pool creation, got %d", createCalls)
	}
	if pool1 != pool2 {
		t.Fatal("expected the same pool instance to be reused for the same DSN")
	}
}
