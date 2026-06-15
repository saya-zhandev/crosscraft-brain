package engine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/id"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/nodes/core"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/registry"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

// memStore is an in-memory Store for tests (no Postgres needed).
type memStore struct {
	wf    map[string]*schema.Workflow
	execs map[string]*LoadedExecution
	creds map[string]map[string]any
}

func newMemStore() *memStore {
	return &memStore{
		wf:    map[string]*schema.Workflow{},
		execs: map[string]*LoadedExecution{},
		creds: map[string]map[string]any{},
	}
}

func nowStr() string { return time.Now().UTC().Format(time.RFC3339) }

func (m *memStore) CreateExecution(_ context.Context, workflowID string) (string, error) {
	eid := id.New()
	m.execs[eid] = &LoadedExecution{ExecutionRecord: schema.ExecutionRecord{ID: eid, WorkflowID: workflowID, Status: "running", StartedAt: nowStr()}}
	return eid, nil
}
func (m *memStore) StartStep(_ context.Context, _, _ string, _ []schema.Item) (string, error) {
	return id.New(), nil
}
func (m *memStore) FinishStep(_ context.Context, _, _ string, _ []schema.Item, _ []schema.LogEntry, _ *string) error {
	return nil
}
func (m *memStore) SaveState(_ context.Context, eid string, state *RunState) error {
	if e := m.execs[eid]; e != nil {
		e.State = state
	}
	return nil
}
func (m *memStore) SetWaiting(_ context.Context, eid, waitingNodeID, token string, state *RunState) error {
	e := m.execs[eid]
	if e == nil {
		return nil
	}
	e.Status = "waiting"
	e.WaitingNodeID = &waitingNodeID
	e.ResumeToken = &token
	e.State = state
	return nil
}
func (m *memStore) FinishExecution(_ context.Context, eid, status string) error {
	if e := m.execs[eid]; e != nil {
		e.Status = status
	}
	return nil
}
func (m *memStore) GetCredentialData(_ context.Context, credID string) (map[string]any, error) {
	return m.creds[credID], nil
}
func (m *memStore) LoadExecution(_ context.Context, eid string) (*LoadedExecution, error) {
	return m.execs[eid], nil
}
func (m *memStore) LoadWorkflow(_ context.Context, wfID string) (*schema.Workflow, error) {
	return m.wf[wfID], nil
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestRunGojaWorkflow exercises the goja expression evaluator AND Code node
// end-to-end: manual -> set ({{ }}) -> if ({{ }}) -> code (JS).
func TestRunGojaWorkflow(t *testing.T) {
	reg := registry.New().Register(core.Nodes...)
	store := newMemStore()
	wf := &schema.Workflow{
		ID: "wf1", Name: "goja", Active: true,
		Nodes: []schema.WFNode{
			{ID: "n1", Type: "core.manualTrigger", Params: map[string]any{}},
			{ID: "n2", Type: "core.set", Params: map[string]any{"fields": map[string]any{
				"greeting": `{{ "hi " + $json.name }}`,
				"doubled":  `{{ $json.qty * 2 }}`,
			}}},
			{ID: "n3", Type: "core.if", Params: map[string]any{"condition": `{{ $json.doubled > 5 }}`}},
			{ID: "n4", Type: "core.code", Params: map[string]any{"code": `return items.map(function(i){ return { json: { ok: true, g: i.json.greeting, d: i.json.doubled } }; });`}},
		},
		Edges: []schema.WFEdge{
			{ID: "e1", Source: "n1", Target: "n2"},
			{ID: "e2", Source: "n2", Target: "n3"},
			{ID: "e3", Source: "n3", SourceHandle: "true", Target: "n4"},
		},
	}
	store.wf[wf.ID] = wf

	eng := New(reg, store)
	res, err := eng.Run(context.Background(), wf, []schema.Item{{JSON: map[string]any{"name": "bob", "qty": 4}}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "success" {
		t.Fatalf("status=%s error=%s", res.Status, res.Error)
	}
	got := mustJSON(t, res.Outputs["n4"])
	want := `[{"json":{"d":8,"g":"hi bob","ok":true}}]`
	if got != want {
		t.Fatalf("terminal output\n got: %s\nwant: %s", got, want)
	}
}

// TestSuspendResume exercises durable wait/resume: manual -> wait -> set.
func TestSuspendResume(t *testing.T) {
	reg := registry.New().Register(core.Nodes...)
	store := newMemStore()
	wf := &schema.Workflow{
		ID: "wf2", Name: "wait", Active: true,
		Nodes: []schema.WFNode{
			{ID: "a", Type: "core.manualTrigger", Params: map[string]any{}},
			{ID: "b", Type: "core.wait", Params: map[string]any{}},
			{ID: "c", Type: "core.set", Params: map[string]any{"fields": map[string]any{"echo": `{{ $json.resumed }}`}}},
		},
		Edges: []schema.WFEdge{
			{ID: "e1", Source: "a", Target: "b"},
			{ID: "e2", Source: "b", Target: "c"},
		},
	}
	store.wf[wf.ID] = wf

	eng := New(reg, store)
	res, err := eng.Run(context.Background(), wf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "waiting" {
		t.Fatalf("expected waiting, got %s (err=%s)", res.Status, res.Error)
	}

	res2, err := eng.Resume(context.Background(), res.ExecutionID, []schema.Item{{JSON: map[string]any{"resumed": true}}})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Status != "success" {
		t.Fatalf("status=%s error=%s", res2.Status, res2.Error)
	}
	got := mustJSON(t, res2.Outputs["c"])
	want := `[{"json":{"echo":true,"resumed":true}}]`
	if got != want {
		t.Fatalf("resumed output\n got: %s\nwant: %s", got, want)
	}
}
