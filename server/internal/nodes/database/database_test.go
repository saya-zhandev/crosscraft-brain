package database

import (
	"os"
	"testing"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/credtype"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
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
