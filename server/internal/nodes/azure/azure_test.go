package azure

import (
	"testing"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

func TestBlobStorageNode(t *testing.T) {
	def := BlobStorage("https://test.blob.core.windows.net").Build()
	if def.Type != "azure.blobStorage" {
		t.Fatalf("unexpected type: %s", def.Type)
	}
	ops := collectOps(def)
	if len(ops) < 8 {
		t.Fatalf("expected at least 8 ops, got %d", len(ops))
	}
	for _, want := range []string{"container:list", "container:create", "container:delete", "blob:list", "blob:upload", "blob:download", "blob:delete", "blob:copy"} {
		if !ops[want] {
			t.Fatalf("expected operation %q", want)
		}
	}
}

func TestCosmosDBNode(t *testing.T) {
	def := CosmosDB("https://test.documents.azure.com").Build()
	ops := collectOps(def)
	if len(ops) < 9 {
		t.Fatalf("expected at least 9 ops, got %d", len(ops))
	}
	for _, want := range []string{"database:list", "database:create", "database:delete", "container:list", "container:create", "item:get", "item:create", "item:update", "item:delete", "item:query"} {
		if !ops[want] {
			t.Fatalf("expected operation %q", want)
		}
	}
}

func TestPowerBINode(t *testing.T) {
	def := PowerBI("https://api.powerbi.com/v1.0/myorg").Build()
	ops := collectOps(def)
	for _, want := range []string{"dataset:list", "dataset:get", "dataset:pushRows", "dataset:refresh", "report:list", "dashboard:list", "group:list"} {
		if !ops[want] {
			t.Fatalf("expected operation %q", want)
		}
	}
}

func TestDevOpsNode(t *testing.T) {
	def := DevOps("https://dev.azure.com/testorg").Build()
	ops := collectOps(def)
	if len(ops) < 10 {
		t.Fatalf("expected at least 10 ops, got %d", len(ops))
	}
	for _, want := range []string{"workItem:list", "workItem:create", "workItem:update", "workItem:delete", "pipeline:list", "pipeline:run", "repo:list", "pullRequest:list", "pullRequest:create"} {
		if !ops[want] {
			t.Fatalf("expected operation %q", want)
		}
	}
}

func TestOpenAINode(t *testing.T) {
	def := OpenAI("https://test.openai.azure.com").Build()
	ops := collectOps(def)
	for _, want := range []string{"completion:create", "chat:completion", "embedding:create", "image:generate", "audio:transcribe", "audio:translate"} {
		if !ops[want] {
			t.Fatalf("expected operation %q", want)
		}
	}
}

func TestAzureNodesRegistration(t *testing.T) {
	nodes := Nodes()
	if len(nodes) != 5 {
		t.Fatalf("expected 5 Azure nodes, got %d", len(nodes))
	}
	names := map[string]bool{}
	for _, n := range nodes {
		names[n.Type] = true
	}
	for _, want := range []string{
		"azure.blobStorage", "azure.cosmos",
		"azure.powerbi", "azure.devops", "azure.openai",
	} {
		if !names[want] {
			t.Fatalf("expected node %q to be registered", want)
		}
	}
}

func TestAzureSharedKeySigning(t *testing.T) {
	// Verify the signing function exists and is callable
	_ = AzureSignRequest
	_ = NewSignedTransport
}

func TestCosmosSigning(t *testing.T) {
	_ = cosmosSignRequest
	_ = cosmosDo
}

func collectOps(def schema.NodeDefinition) map[string]bool {
	ops := map[string]bool{}
	for _, p := range def.Params {
		if p.Name == "operation" {
			for _, opt := range p.Options {
				ops[opt.Value] = true
			}
		}
	}
	return ops
}
