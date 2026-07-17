package mobile

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/auth"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/engine"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/id"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/nodes/core"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/registry"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/store"

	crosscraft "github.com/CrossCraftAI/crosscraft-brain/server/internal/mobile/gen"
)

// ── test doubles ───────────────────────────────────────────────────────────

// fakeAuth is a minimal auth service for testing. "valid_key" is the only
// accepted key; anything else (including empty) returns nil.
type fakeAuth struct{}

func (f *fakeAuth) Validate(ctx context.Context, raw string) (*auth.APIKey, error) {
	if raw == "valid_key" {
		return &auth.APIKey{ID: "test-key-id", Name: "test", CreatedAt: time.Now().UTC().Format(time.RFC3339)}, nil
	}
	return nil, nil
}

// memStore implements the mobile Store interface entirely in-memory, mirroring
// the pattern used in api_test.go.
type memStore struct {
	wfs   map[string]*schema.Workflow
	execs map[string]*execRec
	creds map[string]map[string]any
	steps map[string][]schema.StepRecord
}

type execRec struct {
	record schema.ExecutionRecord
	state  *engine.RunState
}

func newMemStore() *memStore {
	return &memStore{
		wfs:   map[string]*schema.Workflow{},
		execs: map[string]*execRec{},
		creds: map[string]map[string]any{},
		steps: map[string][]schema.StepRecord{},
	}
}

// ── engine.Store interface ─────────────────────────────────────────────────

func (m *memStore) CreateExecution(_ context.Context, workflowID string) (string, error) {
	eid := id.New()
	m.execs[eid] = &execRec{record: schema.ExecutionRecord{ID: eid, WorkflowID: workflowID, Status: "running"}}
	return eid, nil
}

func (m *memStore) StartStep(_ context.Context, executionID, nodeID string, input []schema.Item) (string, error) {
	sid := id.New()
	m.steps[executionID] = append(m.steps[executionID], schema.StepRecord{
		ID: sid, ExecutionID: executionID, NodeID: nodeID, Status: "running", Input: input,
	})
	return sid, nil
}

func (m *memStore) FinishStep(_ context.Context, stepID, status string, output []schema.Item, logs []schema.LogEntry, errMsg *string) error {
	for eid, steps := range m.steps {
		for i, st := range steps {
			if st.ID == stepID {
				st.Status = status
				st.Output = output
				st.Logs = logs
				if errMsg != nil {
					st.Error = errMsg
				}
				m.steps[eid][i] = st
				return nil
			}
		}
	}
	return nil
}

func (m *memStore) SaveState(_ context.Context, eid string, state *engine.RunState) error {
	if e := m.execs[eid]; e != nil {
		e.state = state
	}
	return nil
}

func (m *memStore) SetWaiting(_ context.Context, eid, nodeID, token string, state *engine.RunState) error {
	e := m.execs[eid]
	if e == nil {
		return nil
	}
	e.record.Status = "waiting"
	e.record.WaitingNodeID = &nodeID
	e.record.ResumeToken = &token
	e.state = state
	return nil
}

func (m *memStore) FinishExecution(_ context.Context, eid, status string) error {
	if e := m.execs[eid]; e != nil {
		e.record.Status = status
	}
	return nil
}

func (m *memStore) GetCredentialData(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}

func (m *memStore) LoadExecution(_ context.Context, eid string) (*engine.LoadedExecution, error) {
	e, ok := m.execs[eid]
	if !ok {
		return nil, nil
	}
	return &engine.LoadedExecution{ExecutionRecord: e.record, State: e.state}, nil
}

func (m *memStore) LoadWorkflow(_ context.Context, wfID string) (*schema.Workflow, error) {
	return m.wfs[wfID], nil
}

func (m *memStore) ClaimWaiting(_ context.Context, eid string) (bool, error) {
	e := m.execs[eid]
	if e == nil || e.record.Status != "waiting" {
		return false, nil
	}
	e.record.Status = "running"
	return true, nil
}

func (m *memStore) ListRunningExecutionIDs(_ context.Context) ([]string, error) {
	return nil, nil
}

func (m *memStore) FailStaleRunningSteps(_ context.Context, _ string) error {
	return nil
}

// ── extra Store methods (mobile-specific) ──────────────────────────────────

func (m *memStore) ListActiveWorkflows(_ context.Context) ([]schema.Workflow, error) {
	out := []schema.Workflow{}
	for _, w := range m.wfs {
		if w.Active {
			out = append(out, *w)
		}
	}
	return out, nil
}

func (m *memStore) ListWorkflows(_ context.Context) ([]store.WorkflowSummary, error) {
	out := []store.WorkflowSummary{}
	for _, w := range m.wfs {
		out = append(out, store.WorkflowSummary{ID: w.ID, Name: w.Name, Active: w.Active})
	}
	return out, nil
}

func (m *memStore) GetExecutionStatus(_ context.Context, eid string) (store.ExecStatus, error) {
	e, ok := m.execs[eid]
	if !ok {
		return store.ExecStatus{Found: false}, nil
	}
	return store.ExecStatus{Status: e.record.Status, WaitingNodeID: e.record.WaitingNodeID, Found: true}, nil
}

func (m *memStore) GetExecutionSteps(_ context.Context, executionID string) ([]schema.StepRecord, error) {
	return m.steps[executionID], nil
}

// ── test server setup ──────────────────────────────────────────────────────

// testServer creates a Server with in-memory doubles and a real engine.
func testServer(reg *registry.Registry, st *memStore) *Server {
	if reg == nil {
		reg = registry.New().Register(core.Nodes...)
	}
	if st == nil {
		st = newMemStore()
	}
	eng := engine.New(reg, st)
	return NewServer(st, eng, &fakeAuth{}, nil)
}

// ── TriggerWorkflow tests ──────────────────────────────────────────────────

func TestTriggerWorkflow_Success(t *testing.T) {
	st := newMemStore()
	wf := &schema.Workflow{
		ID: "wf-web", Name: "webhook wf", Active: true,
		Nodes: []schema.WFNode{
			{ID: "t", Type: "core.webhookTrigger", Params: map[string]any{"path": "myhook"}},
			{ID: "s", Type: "core.set", Params: map[string]any{"fields": map[string]any{"echo": `{{$json.x}}`}}},
		},
		Edges: []schema.WFEdge{{ID: "e1", Source: "t", Target: "s"}},
	}
	st.wfs[wf.ID] = wf

	srv := testServer(nil, st)

	resp, err := srv.TriggerWorkflow(context.Background(), &crosscraft.TriggerRequest{
		WebhookPath: "myhook",
		ApiKey:      "valid_key",
		Params:      map[string]string{"x": "42"},
	})
	if err != nil {
		t.Fatalf("TriggerWorkflow failed: %v", err)
	}
	if resp.ExecutionId == "" {
		t.Fatal("expected non-empty execution_id")
	}
	if resp.Status != "success" {
		t.Fatalf("expected status=success, got %q", resp.Status)
	}
}

func TestTriggerWorkflow_Unauthenticated(t *testing.T) {
	srv := testServer(nil, nil)

	_, err := srv.TriggerWorkflow(context.Background(), &crosscraft.TriggerRequest{
		WebhookPath: "myhook",
		ApiKey:      "",
	})
	if err == nil {
		t.Fatal("expected error for empty api_key")
	}
}

func TestTriggerWorkflow_InvalidKey(t *testing.T) {
	srv := testServer(nil, nil)

	_, err := srv.TriggerWorkflow(context.Background(), &crosscraft.TriggerRequest{
		WebhookPath: "myhook",
		ApiKey:      "bad_key",
	})
	if err == nil {
		t.Fatal("expected error for invalid api_key")
	}
}

func TestTriggerWorkflow_NotFound(t *testing.T) {
	srv := testServer(nil, nil)

	_, err := srv.TriggerWorkflow(context.Background(), &crosscraft.TriggerRequest{
		WebhookPath: "nonexistent",
		ApiKey:      "valid_key",
	})
	if err == nil {
		t.Fatal("expected NotFound error")
	}
}

func TestTriggerWorkflow_MissingPath(t *testing.T) {
	srv := testServer(nil, nil)

	_, err := srv.TriggerWorkflow(context.Background(), &crosscraft.TriggerRequest{
		ApiKey: "valid_key",
	})
	if err == nil {
		t.Fatal("expected InvalidArgument error for missing webhook_path")
	}
}

// ── ResumeExecution tests ──────────────────────────────────────────────────

func TestResumeExecution_NotFound(t *testing.T) {
	srv := testServer(nil, nil)

	_, err := srv.ResumeExecution(context.Background(), &crosscraft.ResumeRequest{
		ExecutionId: "nonexistent",
		ApiKey:      "valid_key",
	})
	if err == nil {
		t.Fatal("expected FailedPrecondition error for unknown execution")
	}
}

func TestResumeExecution_Unauthenticated(t *testing.T) {
	srv := testServer(nil, nil)

	_, err := srv.ResumeExecution(context.Background(), &crosscraft.ResumeRequest{
		ExecutionId: "some-id",
		ApiKey:      "",
	})
	if err == nil {
		t.Fatal("expected error for empty api_key")
	}
}

func TestResumeExecution_InvalidPayload(t *testing.T) {
	srv := testServer(nil, nil)

	_, err := srv.ResumeExecution(context.Background(), &crosscraft.ResumeRequest{
		ExecutionId: "some-id",
		ApiKey:      "valid_key",
		PayloadJson: "not valid json",
	})
	if err == nil {
		t.Fatal("expected InvalidArgument error for bad JSON payload")
	}
}

// ── StreamExecution tests ──────────────────────────────────────────────────

// fakeStream implements grpc.ServerStreamingServer[crosscraft.ExecutionEvent]
// for testing, collecting sent events.
type fakeStream struct {
	grpc.ServerStream
	ctx    context.Context
	events []*crosscraft.ExecutionEvent
}

func (f *fakeStream) Send(event *crosscraft.ExecutionEvent) error {
	f.events = append(f.events, event)
	return nil
}

func (f *fakeStream) Context() context.Context {
	if f.ctx == nil {
		return context.Background()
	}
	return f.ctx
}

func (f *fakeStream) SetHeader(metadata.MD) error  { return nil }
func (f *fakeStream) SendHeader(metadata.MD) error { return nil }
func (f *fakeStream) SetTrailer(metadata.MD)       {}
func (f *fakeStream) SendMsg(m interface{}) error    { return nil }
func (f *fakeStream) RecvMsg(m interface{}) error    { return nil }

func TestStreamExecution_Success(t *testing.T) {
	st := newMemStore()
	// Set up an already-finished execution.
	st.execs["exec-done"] = &execRec{
		record: schema.ExecutionRecord{ID: "exec-done", WorkflowID: "wf1", Status: "success"},
	}
	srv := testServer(nil, st)

	stream := &fakeStream{ctx: context.Background()}
	err := srv.StreamExecution(&crosscraft.StreamRequest{
		ExecutionId: "exec-done",
		ApiKey:      "valid_key",
	}, stream)
	if err != nil {
		t.Fatalf("StreamExecution failed: %v", err)
	}
	if len(stream.events) < 1 {
		t.Fatal("expected at least one event")
	}
	last := stream.events[len(stream.events)-1]
	if last.Status != "success" {
		t.Fatalf("expected status=success, got %q", last.Status)
	}
}

func TestStreamExecution_Unauthenticated(t *testing.T) {
	srv := testServer(nil, nil)

	stream := &fakeStream{ctx: context.Background()}
	err := srv.StreamExecution(&crosscraft.StreamRequest{
		ExecutionId: "some-id",
		ApiKey:      "",
	}, stream)
	if err == nil {
		t.Fatal("expected error for empty api_key")
	}
}

func TestStreamExecution_NotFound(t *testing.T) {
	srv := testServer(nil, nil)

	stream := &fakeStream{ctx: context.Background()}
	err := srv.StreamExecution(&crosscraft.StreamRequest{
		ExecutionId: "nonexistent",
		ApiKey:      "valid_key",
	}, stream)
	if err == nil {
		t.Fatal("expected NotFound error")
	}
}

func TestStreamExecution_MissingID(t *testing.T) {
	srv := testServer(nil, nil)

	stream := &fakeStream{ctx: context.Background()}
	err := srv.StreamExecution(&crosscraft.StreamRequest{
		ApiKey: "valid_key",
	}, stream)
	if err == nil {
		t.Fatal("expected InvalidArgument for missing execution_id")
	}
}

// ── GetExecution tests ─────────────────────────────────────────────────────

func TestGetExecution_Success(t *testing.T) {
	st := newMemStore()
	st.execs["exec-1"] = &execRec{
		record: schema.ExecutionRecord{ID: "exec-1", WorkflowID: "wf1", Status: "success"},
	}
	st.steps["exec-1"] = []schema.StepRecord{
		{ID: "step-1", ExecutionID: "exec-1", NodeID: "n1", Status: "success"},
	}
	srv := testServer(nil, st)

	resp, err := srv.GetExecution(context.Background(), &crosscraft.GetExecutionRequest{
		ExecutionId: "exec-1",
		ApiKey:      "valid_key",
	})
	if err != nil {
		t.Fatalf("GetExecution failed: %v", err)
	}
	if resp.Status != "success" {
		t.Fatalf("expected status=success, got %q", resp.Status)
	}
	if resp.StepsJson == "" || resp.StepsJson == "[]" {
		t.Fatalf("expected non-empty steps_json, got %q", resp.StepsJson)
	}
}

func TestGetExecution_NotFound(t *testing.T) {
	srv := testServer(nil, nil)

	_, err := srv.GetExecution(context.Background(), &crosscraft.GetExecutionRequest{
		ExecutionId: "nonexistent",
		ApiKey:      "valid_key",
	})
	if err == nil {
		t.Fatal("expected NotFound error")
	}
}

func TestGetExecution_Unauthenticated(t *testing.T) {
	srv := testServer(nil, nil)

	_, err := srv.GetExecution(context.Background(), &crosscraft.GetExecutionRequest{
		ExecutionId: "some-id",
		ApiKey:      "",
	})
	if err == nil {
		t.Fatal("expected error for empty api_key")
	}
}

// ── RegisterDevice tests ───────────────────────────────────────────────────

func TestRegisterDevice_Unauthenticated(t *testing.T) {
	srv := testServer(nil, nil)

	_, err := srv.RegisterDevice(context.Background(), &crosscraft.RegisterDeviceRequest{
		ApiKey:      "",
		DeviceToken: "token123",
		Platform:    "ios",
	})
	if err == nil {
		t.Fatal("expected error for empty api_key")
	}
}

func TestRegisterDevice_MissingToken(t *testing.T) {
	srv := testServer(nil, nil)

	_, err := srv.RegisterDevice(context.Background(), &crosscraft.RegisterDeviceRequest{
		ApiKey:  "valid_key",
		Platform: "ios",
	})
	if err == nil {
		t.Fatal("expected InvalidArgument for missing device_token")
	}
}

func TestRegisterDevice_MissingPlatform(t *testing.T) {
	srv := testServer(nil, nil)

	_, err := srv.RegisterDevice(context.Background(), &crosscraft.RegisterDeviceRequest{
		ApiKey:      "valid_key",
		DeviceToken: "token123",
	})
	if err == nil {
		t.Fatal("expected InvalidArgument for missing platform")
	}
}

// ── ListWorkflows tests ────────────────────────────────────────────────────

func TestListWorkflows_Success(t *testing.T) {
	st := newMemStore()
	st.wfs["wf-1"] = &schema.Workflow{ID: "wf-1", Name: "First", Active: true}
	st.wfs["wf-2"] = &schema.Workflow{ID: "wf-2", Name: "Second", Active: false}
	srv := testServer(nil, st)

	resp, err := srv.ListWorkflows(context.Background(), &crosscraft.ListWorkflowsRequest{
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
}

func TestListWorkflows_Empty(t *testing.T) {
	srv := testServer(nil, nil)

	resp, err := srv.ListWorkflows(context.Background(), &crosscraft.ListWorkflowsRequest{
		ApiKey: "valid_key",
	})
	if err != nil {
		t.Fatalf("ListWorkflows failed: %v", err)
	}
	if resp.WorkflowsJson != "[]" {
		t.Fatalf("expected empty array, got %s", resp.WorkflowsJson)
	}
}

func TestListWorkflows_Unauthenticated(t *testing.T) {
	srv := testServer(nil, nil)

	_, err := srv.ListWorkflows(context.Background(), &crosscraft.ListWorkflowsRequest{
		ApiKey: "",
	})
	if err == nil {
		t.Fatal("expected error for empty api_key")
	}
}

// ── All RPCs require auth ──────────────────────────────────────────────────

func TestAllRPCsRequireAuth(t *testing.T) {
	srv := testServer(nil, nil)

	tests := []struct {
		name string
		fn   func() error
	}{
		{
			"TriggerWorkflow",
			func() error {
				_, err := srv.TriggerWorkflow(context.Background(), &crosscraft.TriggerRequest{WebhookPath: "x", ApiKey: ""})
				return err
			},
		},
		{
			"ResumeExecution",
			func() error {
				_, err := srv.ResumeExecution(context.Background(), &crosscraft.ResumeRequest{ExecutionId: "x", ApiKey: ""})
				return err
			},
		},
		{
			"StreamExecution",
			func() error {
				return srv.StreamExecution(&crosscraft.StreamRequest{ExecutionId: "x", ApiKey: ""}, &fakeStream{ctx: context.Background()})
			},
		},
		{
			"GetExecution",
			func() error {
				_, err := srv.GetExecution(context.Background(), &crosscraft.GetExecutionRequest{ExecutionId: "x", ApiKey: ""})
				return err
			},
		},
		{
			"RegisterDevice",
			func() error {
				_, err := srv.RegisterDevice(context.Background(), &crosscraft.RegisterDeviceRequest{ApiKey: "", DeviceToken: "t", Platform: "ios"})
				return err
			},
		},
		{
			"ListWorkflows",
			func() error {
				_, err := srv.ListWorkflows(context.Background(), &crosscraft.ListWorkflowsRequest{ApiKey: ""})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.fn(); err == nil {
				t.Errorf("%s with empty api_key should return error", tt.name)
			}
		})
	}
}
