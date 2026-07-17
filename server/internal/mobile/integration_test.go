package mobile

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/engine"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/nodes/core"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/registry"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"

	crosscraft "github.com/CrossCraftAI/crosscraft-brain/server/internal/mobile/gen"
)

// bufSize for the in-memory listener.
const bufSize = 1024 * 1024

// setupGRPC creates an in-process gRPC server+client pair over bufconn.
// Returns the client and a teardown function.
func setupGRPC(t *testing.T, st *memStore) (crosscraft.CrossCraftMobileClient, func()) {
	t.Helper()

	reg := registry.New().Register(core.Nodes...)
	eng := engine.New(reg, st)
	srv := NewServer(st, eng, &fakeAuth{}, nil)

	grpcSrv := grpc.NewServer()
	crosscraft.RegisterCrossCraftMobileServer(grpcSrv, srv)

	lis := bufconn.Listen(bufSize)
	go func() {
		if err := grpcSrv.Serve(lis); err != nil {
			// server stopped — expected during cleanup
		}
	}()

	dialer := func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	}

	conn, err := grpc.NewClient("passthrough://bufconn",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to dial bufconn: %v", err)
	}

	client := crosscraft.NewCrossCraftMobileClient(conn)

	cleanup := func() {
		conn.Close()
		grpcSrv.Stop()
	}

	return client, cleanup
}

func TestIntegration_TriggerAndGetExecution(t *testing.T) {
	st := newMemStore()
	st.wfs["wf-1"] = &schema.Workflow{
		ID: "wf-1", Name: "Integration Test", Active: true,
		Nodes: []schema.WFNode{
			{ID: "t", Type: "core.webhookTrigger", Params: map[string]any{"path": "test-hook"}},
			{ID: "s", Type: "core.set", Params: map[string]any{"fields": map[string]any{"echo": `{{$json.x}}`}}},
		},
		Edges: []schema.WFEdge{{ID: "e1", Source: "t", Target: "s"}},
	}

	client, cleanup := setupGRPC(t, st)
	defer cleanup()

	ctx := context.Background()

	// 1. Trigger a workflow via webhook path
	tr, err := client.TriggerWorkflow(ctx, &crosscraft.TriggerRequest{
		WebhookPath: "test-hook",
		ApiKey:      "valid_key",
		Params:      map[string]string{"x": "42"},
	})
	if err != nil {
		t.Fatalf("TriggerWorkflow failed: %v", err)
	}
	if tr.ExecutionId == "" {
		t.Fatal("no execution_id returned")
	}
	if tr.Status != "success" {
		t.Fatalf("expected status=success, got %q", tr.Status)
	}
	t.Logf("TriggerWorkflow OK — executionId=%s, status=%s", tr.ExecutionId, tr.Status)

	// 2. Get the execution
	ge, err := client.GetExecution(ctx, &crosscraft.GetExecutionRequest{
		ExecutionId: tr.ExecutionId,
		ApiKey:      "valid_key",
	})
	if err != nil {
		t.Fatalf("GetExecution failed: %v", err)
	}
	if ge.Status != "success" {
		t.Fatalf("expected status=success, got %q", ge.Status)
	}

	var steps []map[string]any
	if err := json.Unmarshal([]byte(ge.StepsJson), &steps); err != nil {
		t.Fatalf("failed to parse steps_json: %v", err)
	}
	if len(steps) == 0 {
		t.Fatal("expected at least 1 step")
	}
	t.Logf("GetExecution OK — %d steps", len(steps))
	for _, s := range steps {
		t.Logf("  step: nodeId=%v status=%v", s["nodeId"], s["status"])
	}
}

func TestIntegration_StreamExecution(t *testing.T) {
	st := newMemStore()
	st.wfs["wf-stream"] = &schema.Workflow{
		ID: "wf-stream", Name: "Stream Test", Active: true,
		Nodes: []schema.WFNode{
			{ID: "t", Type: "core.webhookTrigger", Params: map[string]any{"path": "stream-test"}},
			{ID: "s", Type: "core.set", Params: map[string]any{"fields": map[string]any{"done": "true"}}},
		},
		Edges: []schema.WFEdge{{ID: "e1", Source: "t", Target: "s"}},
	}

	client, cleanup := setupGRPC(t, st)
	defer cleanup()

	ctx := context.Background()

	// Trigger first so we have an execution to stream
	tr, err := client.TriggerWorkflow(ctx, &crosscraft.TriggerRequest{
		WebhookPath: "stream-test",
		ApiKey:      "valid_key",
	})
	if err != nil {
		t.Fatalf("TriggerWorkflow failed: %v", err)
	}

	// Stream the execution
	stream, err := client.StreamExecution(ctx, &crosscraft.StreamRequest{
		ExecutionId: tr.ExecutionId,
		ApiKey:      "valid_key",
	})
	if err != nil {
		t.Fatalf("StreamExecution failed: %v", err)
	}

	var events []*crosscraft.ExecutionEvent
	for {
		ev, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("stream recv error: %v", err)
		}
		events = append(events, ev)
	}

	if len(events) == 0 {
		t.Fatal("expected at least 1 stream event")
	}
	last := events[len(events)-1]
	if last.Status != "success" {
		t.Fatalf("expected final status=success, got %q", last.Status)
	}
	t.Logf("StreamExecution OK — received %d events, final status=%s", len(events), last.Status)
	for i, ev := range events {
		t.Logf("  event %d: status=%s, steps=%d", i+1, ev.Status, len(ev.Steps))
	}
}

func TestIntegration_ListWorkflows(t *testing.T) {
	st := newMemStore()
	st.wfs["wf-a"] = &schema.Workflow{ID: "wf-a", Name: "Alpha", Active: true}
	st.wfs["wf-b"] = &schema.Workflow{ID: "wf-b", Name: "Beta", Active: false}

	client, cleanup := setupGRPC(t, st)
	defer cleanup()

	resp, err := client.ListWorkflows(context.Background(), &crosscraft.ListWorkflowsRequest{
		ApiKey: "valid_key",
	})
	if err != nil {
		t.Fatalf("ListWorkflows failed: %v", err)
	}

	var parsed []map[string]any
	if err := json.Unmarshal([]byte(resp.WorkflowsJson), &parsed); err != nil {
		t.Fatalf("failed to parse workflows_json: %v", err)
	}
	if len(parsed) != 2 {
		t.Fatalf("expected 2 workflows, got %d", len(parsed))
	}
	t.Logf("ListWorkflows OK — %d workflows: %v / %v", len(parsed), parsed[0]["name"], parsed[1]["name"])
}

func TestIntegration_AuthRejected(t *testing.T) {
	st := newMemStore()
	client, cleanup := setupGRPC(t, st)
	defer cleanup()

	_, err := client.ListWorkflows(context.Background(), &crosscraft.ListWorkflowsRequest{
		ApiKey: "",
	})
	if err == nil {
		t.Fatal("expected gRPC error for empty api_key")
	}
	t.Logf("Auth rejected OK — error: %v", err)
}

func TestIntegration_NotFound(t *testing.T) {
	st := newMemStore()
	client, cleanup := setupGRPC(t, st)
	defer cleanup()

	_, err := client.GetExecution(context.Background(), &crosscraft.GetExecutionRequest{
		ExecutionId: "does-not-exist",
		ApiKey:      "valid_key",
	})
	if err == nil {
		t.Fatal("expected NotFound error")
	}
	t.Logf("NotFound OK — error: %v", err)
}
