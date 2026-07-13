package database

import (
	"context"
	"os"
	"testing"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/credtype"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
	"go.mongodb.org/mongo-driver/mongo"
)

func TestMongoNodeDefinition(t *testing.T) {
	def := MongoNode()
	if def.Type != "database.mongodb" {
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

	// Verify all 10 operations are present
	opCount := countOperations(def)
	if opCount != 10 {
		t.Fatalf("expected 10 operations, got %d", opCount)
	}
}

func countOperations(def schema.NodeDefinition) int {
	for _, p := range def.Params {
		if p.Name == "operation" {
			return len(p.Options)
		}
	}
	return 0
}

func TestMongoNodeUsesRegisteredCredentialType(t *testing.T) {
	reg := credtype.Default()
	credType, ok := reg.Get("mongodbApi")
	if !ok {
		t.Fatal("expected mongodbApi credential type to be registered")
	}
	if credType.DisplayName == "" {
		t.Fatal("expected mongodbApi credential type to have a display name")
	}
	if len(credType.Fields) == 0 {
		t.Fatal("expected mongodbApi credential fields to be defined")
	}
}

func TestResolveMongoURIRequiresCredential(t *testing.T) {
	ctx := &schema.ExecContext{
		Credential: func(paramName string) (map[string]any, error) {
			return nil, nil
		},
	}
	_, _, err := resolveMongoURI(ctx)
	if err == nil {
		t.Fatal("expected error when credential is missing")
	}
}

func TestResolveMongoURIFromConnectionString(t *testing.T) {
	ctx := &schema.ExecContext{
		Credential: func(paramName string) (map[string]any, error) {
			return map[string]any{
				"connectionString": "mongodb://user:pass@host:27017/testdb",
			}, nil
		},
	}
	uri, dbName, err := resolveMongoURI(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uri != "mongodb://user:pass@host:27017/testdb" {
		t.Fatalf("unexpected URI: %s", uri)
	}
	if dbName != "admin" {
		t.Fatalf("unexpected dbName: %s", dbName)
	}
}

func TestResolveMongoURIFromFields(t *testing.T) {
	ctx := &schema.ExecContext{
		Credential: func(paramName string) (map[string]any, error) {
			return map[string]any{
				"host":     "mongo.example.com",
				"port":     "27018",
				"user":     "admin",
				"password": "secret",
				"database": "mydb",
			}, nil
		},
	}
	uri, dbName, err := resolveMongoURI(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uri != "mongodb://admin:secret@mongo.example.com:27018/mydb" {
		t.Fatalf("unexpected URI: %s", uri)
	}
	if dbName != "mydb" {
		t.Fatalf("unexpected dbName: %s", dbName)
	}
}

func TestMongoExecuteRequiresCollection(t *testing.T) {
	os.Setenv("MONGODB_URI", "")
	ctx := &schema.ExecContext{
		Params: map[string]any{
			"operation": "find",
			// collection is missing
		},
		Credential: func(paramName string) (map[string]any, error) {
			return map[string]any{
				"connectionString": "mongodb://localhost:27017",
			}, nil
		},
	}
	_, err := executeMongo(ctx)
	if err == nil {
		t.Fatal("expected error when collection is missing")
	}
}

func TestMongoClientCachedByURI(t *testing.T) {
	originalFactory := newMongoClient
	mongoClientMu.Lock()
	mongoClientCache = make(map[string]*mongo.Client)
	mongoClientMu.Unlock()
	t.Cleanup(func() {
		newMongoClient = originalFactory
		mongoClientMu.Lock()
		mongoClientCache = make(map[string]*mongo.Client)
		mongoClientMu.Unlock()
	})

	var createCalls int
	newMongoClient = func(ctx context.Context, uri string) (*mongo.Client, error) {
		createCalls++
		return &mongo.Client{}, nil
	}

	client1, err := getOrCreateMongoClient(context.Background(), "mongodb://example:27017")
	if err != nil {
		t.Fatalf("expected first client creation to succeed: %v", err)
	}
	client2, err := getOrCreateMongoClient(context.Background(), "mongodb://example:27017")
	if err != nil {
		t.Fatalf("expected cached client reuse to succeed: %v", err)
	}

	if createCalls != 1 {
		t.Fatalf("expected one client creation, got %d", createCalls)
	}
	if client1 != client2 {
		t.Fatal("expected the same client instance to be reused for the same URI")
	}
}

func TestMongoAggregateRequiresPipeline(t *testing.T) {
	ctx := &schema.ExecContext{
		Params: map[string]any{
			"operation":  "aggregate",
			"collection": "test",
		},
		Credential: func(paramName string) (map[string]any, error) {
			return map[string]any{"connectionString": "mongodb://localhost:27017"}, nil
		},
	}
	_, err := executeMongo(ctx)
	if err == nil {
		t.Fatal("expected error when pipeline is missing or invalid")
	}
}

func TestDatabaseNodeCount(t *testing.T) {
	nodes := Nodes()
	if len(nodes) != 5 {
		t.Fatalf("expected 5 database nodes, got %d", len(nodes))
	}
	types := make(map[string]bool)
	for _, n := range nodes {
		types[n.Type] = true
	}
	for _, want := range []string{"database.postgres", "database.mongodb", "database.mysql", "database.redis", "database.supabase"} {
		if !types[want] {
			t.Fatalf("expected node type %q in Nodes()", want)
		}
	}
}
