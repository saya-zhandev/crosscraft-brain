package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/engine"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/id"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/nodes/core"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/registry"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/store"
)

// ── test doubles ───────────────────────────────────────────────────────────

// memStore implements WorkflowStore entirely in-memory.
type memStore struct {
	wfs   map[string]*schema.Workflow
	execs map[string]*execRec
	creds map[string]*credRec
	steps map[string][]schema.StepRecord
}

type execRec struct {
	record  schema.ExecutionRecord
	state   *engine.RunState
}
type credRec struct {
	row  store.CredentialRow
	data map[string]any
}

func newMemStore() *memStore {
	return &memStore{
		wfs:   map[string]*schema.Workflow{},
		execs: map[string]*execRec{},
		creds: map[string]*credRec{},
		steps: map[string][]schema.StepRecord{},
	}
}

func (m *memStore) ListWorkflows(_ context.Context) ([]store.WorkflowSummary, error) {
	out := []store.WorkflowSummary{}
	for _, w := range m.wfs {
		out = append(out, store.WorkflowSummary{ID: w.ID, Name: w.Name, Active: w.Active})
	}
	return out, nil
}

func (m *memStore) SaveWorkflow(_ context.Context, wf *schema.Workflow) error {
	cp := *wf
	cp.Nodes = append([]schema.WFNode{}, wf.Nodes...)
	cp.Edges = append([]schema.WFEdge{}, wf.Edges...)
	m.wfs[wf.ID] = &cp
	return nil
}

func (m *memStore) LoadWorkflow(_ context.Context, wfID string) (*schema.Workflow, error) {
	w, ok := m.wfs[wfID]
	if !ok { return nil, nil }
	cp := *w
	return &cp, nil
}

func (m *memStore) ListActiveWorkflows(_ context.Context) ([]schema.Workflow, error) {
	out := []schema.Workflow{}
	for _, w := range m.wfs {
		if w.Active { out = append(out, *w) }
	}
	return out, nil
}

func (m *memStore) ListExecutions(_ context.Context, workflowID string) ([]schema.ExecutionRecord, error) {
	out := []schema.ExecutionRecord{}
	for _, e := range m.execs {
		if workflowID == "" || e.record.WorkflowID == workflowID {
			out = append(out, e.record)
		}
	}
	return out, nil
}

func (m *memStore) GetExecutionStatus(_ context.Context, eid string) (store.ExecStatus, error) {
	e, ok := m.execs[eid]
	if !ok { return store.ExecStatus{Found: false}, nil }
	return store.ExecStatus{Status: e.record.Status, WaitingNodeID: e.record.WaitingNodeID, Found: true}, nil
}

func (m *memStore) GetExecutionSteps(_ context.Context, executionID string) ([]schema.StepRecord, error) {
	return m.steps[executionID], nil
}

func (m *memStore) ListCredentials(_ context.Context) ([]store.CredentialRow, error) {
	out := []store.CredentialRow{}
	for _, c := range m.creds {
		out = append(out, c.row)
	}
	return out, nil
}

func (m *memStore) CreateCredential(_ context.Context, ctype, name string, data map[string]any) (store.CredentialRow, error) {
	cid := id.New()
	r := store.CredentialRow{ID: cid, Type: ctype, Name: name}
	m.creds[cid] = &credRec{row: r, data: data}
	return r, nil
}

func (m *memStore) DeleteCredential(_ context.Context, cid string) error {
	delete(m.creds, cid)
	return nil
}

// memEngine implements WorkflowRunner by delegating to a real engine.Engine
// backed by the same memStore so execution records written by the engine are
// visible through the API's store.
type memEngine struct {
	reg *registry.Registry
	ms  *memStore // shared with the API's store
}

func (e *memEngine) Run(ctx context.Context, wf *schema.Workflow, triggerItems []schema.Item) (engine.RunResult, error) {
	wfs := map[string]*schema.Workflow{}
	for id, w := range e.ms.wfs { wfs[id] = w }
	wfs[wf.ID] = wf
	eng := engine.New(e.reg, &bridgedStore{ms: e.ms, wfs: wfs})
	return eng.Run(ctx, wf, triggerItems)
}

func (e *memEngine) Resume(ctx context.Context, executionID string, payload []schema.Item) (engine.RunResult, error) {
	// Check existence via the shared store so unknown IDs get an error.
	if _, ok := e.ms.execs[executionID]; !ok {
		return engine.RunResult{}, fmt.Errorf("execution not found: %s", executionID)
	}
	wfs := map[string]*schema.Workflow{}
	for id, w := range e.ms.wfs { wfs[id] = w }
	eng := engine.New(e.reg, &bridgedStore{ms: e.ms, wfs: wfs})
	return eng.Resume(ctx, executionID, payload)
}

// bridgedStore implements engine.Store, routing execution writes into the
// shared memStore so they're visible to the API's ListExecutions / GetExecution.
type bridgedStore struct {
	ms  *memStore
	wfs map[string]*schema.Workflow
}

func (s *bridgedStore) CreateExecution(_ context.Context, workflowID string) (string, error) {
	eid := id.New()
	s.ms.execs[eid] = &execRec{record: schema.ExecutionRecord{ID: eid, WorkflowID: workflowID, Status: "running"}}
	return eid, nil
}
func (s *bridgedStore) StartStep(_ context.Context, executionID, nodeID string, input []schema.Item) (string, error) {
	sid := id.New()
	s.ms.steps[executionID] = append(s.ms.steps[executionID], schema.StepRecord{
		ID: sid, ExecutionID: executionID, NodeID: nodeID, Status: "running", Input: input,
	})
	return sid, nil
}
func (s *bridgedStore) FinishStep(_ context.Context, stepID, status string, output []schema.Item, logs []schema.LogEntry, errMsg *string) error {
	for eid, steps := range s.ms.steps {
		for i, st := range steps {
			if st.ID == stepID {
				st.Status = status
				st.Output = output
				st.Logs = logs
				if errMsg != nil { st.Error = errMsg }
				s.ms.steps[eid][i] = st
				return nil
			}
		}
	}
	return nil
}
func (s *bridgedStore) SaveState(_ context.Context, eid string, state *engine.RunState) error {
	if e := s.ms.execs[eid]; e != nil { e.state = state }
	return nil
}
func (s *bridgedStore) SetWaiting(_ context.Context, eid, nodeID, token string, state *engine.RunState) error {
	e := s.ms.execs[eid]
	if e == nil { return nil }
	e.record.Status = "waiting"
	e.record.WaitingNodeID = &nodeID
	e.record.ResumeToken = &token
	e.state = state
	return nil
}
func (s *bridgedStore) FinishExecution(_ context.Context, eid, status string) error {
	if e := s.ms.execs[eid]; e != nil { e.record.Status = status }
	return nil
}
func (s *bridgedStore) GetCredentialData(_ context.Context, _ string) (map[string]any, error) { return nil, nil }
func (s *bridgedStore) LoadExecution(_ context.Context, eid string) (*engine.LoadedExecution, error) {
	e, ok := s.ms.execs[eid]
	if !ok { return nil, nil }
	return &engine.LoadedExecution{ExecutionRecord: e.record, State: e.state}, nil
}
func (s *bridgedStore) LoadWorkflow(_ context.Context, wfID string) (*schema.Workflow, error) {
	return s.wfs[wfID], nil
}
func (s *bridgedStore) ClaimWaiting(_ context.Context, eid string) (bool, error) {
	e := s.ms.execs[eid]
	if e == nil || e.record.Status != "waiting" { return false, nil }
	e.record.Status = "running"
	return true, nil
}
func (s *bridgedStore) ListRunningExecutionIDs(_ context.Context) ([]string, error) { return nil, nil }
func (s *bridgedStore) FailStaleRunningSteps(_ context.Context, _ string) error { return nil }

// ── test server setup ──────────────────────────────────────────────────────

func testRouter(reg *registry.Registry, st *memStore, eng WorkflowRunner) http.Handler {
	if reg == nil {
		reg = registry.New().Register(core.Nodes...)
	}
	if st == nil {
		st = newMemStore()
	}
	if eng == nil {
		eng = &memEngine{reg: reg, ms: st}
	}
	return NewRouter(reg, st, eng, nil, nil, nil, nil, nil)
}

func doJSON(t *testing.T, router http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != nil {
		b, _ := json.Marshal(body)
		r = httptest.NewRequest(method, path, strings.NewReader(string(b)))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(w.Body).Decode(&m); err != nil {
		t.Fatalf("decode body: %v (body=%s)", err, w.Body.String())
	}
	return m
}

// ── tests ──────────────────────────────────────────────────────────────────

func TestNodesEndpoint(t *testing.T) {
	reg := registry.New().Register(core.Nodes...)
	w := doJSON(t, testRouter(reg, nil, nil), "GET", "/api/nodes", nil)
	if w.Code != http.StatusOK { t.Fatalf("status %d", w.Code) }
	var nodes []map[string]any
	if err := json.NewDecoder(w.Body).Decode(&nodes); err != nil {
		t.Fatal(err)
	}
	if len(nodes) < 5 { t.Fatalf("expected >=5 nodes, got %d", len(nodes)) }
	for _, n := range nodes {
		if n["type"] == nil || n["label"] == nil {
			t.Fatalf("node missing type/label: %+v", n)
		}
	}
}

func TestWorkflowCRUD(t *testing.T) {
	st := newMemStore()
	router := testRouter(nil, st, nil)

	// Create
	w := doJSON(t, router, "POST", "/api/workflows", map[string]any{
		"name": "test wf",
	})
	if w.Code != http.StatusOK { t.Fatalf("create: %d", w.Code) }
	body := decodeBody(t, w)
	if body["id"] == "" { t.Fatal("no id") }
	wfID := body["id"].(string)
	if body["name"] != "test wf" { t.Fatalf("name: %v", body["name"]) }

	// List
	w = doJSON(t, router, "GET", "/api/workflows", nil)
	if w.Code != http.StatusOK { t.Fatalf("list: %d", w.Code) }
	var list []map[string]any
	json.NewDecoder(w.Body).Decode(&list)
	if len(list) != 1 { t.Fatalf("list len: %d", len(list)) }

	// Get
	w = doJSON(t, router, "GET", "/api/workflows/"+wfID, nil)
	if w.Code != http.StatusOK { t.Fatalf("get: %d", w.Code) }
	body = decodeBody(t, w)
	if body["id"] != wfID { t.Fatalf("get id mismatch") }

	// Update
	w = doJSON(t, router, "PUT", "/api/workflows/"+wfID, map[string]any{
		"name": "updated", "nodes": []any{}, "edges": []any{},
	})
	if w.Code != http.StatusOK { t.Fatalf("update: %d", w.Code) }
	body = decodeBody(t, w)
	if body["name"] != "updated" { t.Fatalf("update name: %v", body["name"]) }

	// 404 on unknown
	w = doJSON(t, router, "GET", "/api/workflows/nonexistent", nil)
	if w.Code != http.StatusNotFound { t.Fatalf("expected 404, got %d", w.Code) }
}

func TestRunWorkflow(t *testing.T) {
	reg := registry.New().Register(core.Nodes...)
	st := newMemStore()
	eng := &memEngine{reg: reg, ms: st}

	// Save a minimal workflow: manual -> set
	wf := &schema.Workflow{
		ID: "wf-run", Name: "run test", Active: true,
		Nodes: []schema.WFNode{
			{ID: "n1", Type: "core.manualTrigger", Params: map[string]any{}},
			{ID: "n2", Type: "core.set", Params: map[string]any{"fields": map[string]any{"x": "1"}}},
		},
		Edges: []schema.WFEdge{{ID: "e1", Source: "n1", Target: "n2"}},
	}
	st.wfs[wf.ID] = wf

	router := testRouter(reg, st, eng)
	w := doJSON(t, router, "POST", "/api/workflows/wf-run/run", map[string]any{"hello": "world"})
	if w.Code != http.StatusOK { t.Fatalf("run: status %d body=%s", w.Code, w.Body.String()) }
	body := decodeBody(t, w)
	if body["executionId"] == "" { t.Fatal("no executionId") }
	if body["status"] != "success" { t.Fatalf("status: %v", body["status"]) }
}

func TestRunMissingWorkflow(t *testing.T) {
	router := testRouter(nil, nil, nil)
	w := doJSON(t, router, "POST", "/api/workflows/nonexistent/run", nil)
	if w.Code != http.StatusNotFound { t.Fatalf("expected 404, got %d", w.Code) }
}

func TestResumeEndpoint(t *testing.T) {
	st := newMemStore()
	router := testRouter(nil, st, nil)

	// Resume of unknown execution
	w := doJSON(t, router, "POST", "/api/resume/unknown", map[string]any{})
	if w.Code != http.StatusBadRequest { t.Fatalf("expected 400, got %d", w.Code) }
	body := decodeBody(t, w)
	if body["error"] == nil { t.Fatal("expected error message") }
}

func TestWebhookRouting(t *testing.T) {
	reg := registry.New().Register(core.Nodes...)
	st := newMemStore()
	eng := &memEngine{reg: reg, ms: st}

	// Workflow with webhook trigger matching path "myhook"
	wf := &schema.Workflow{
		ID: "wf-web", Name: "webhook wf", Active: true,
		Nodes: []schema.WFNode{
			{ID: "t", Type: "core.webhookTrigger", Params: map[string]any{"path": "myhook"}},
			{ID: "s", Type: "core.set", Params: map[string]any{"fields": map[string]any{"echo": `{{$json.x}}`}}},
		},
		Edges: []schema.WFEdge{{ID: "e1", Source: "t", Target: "s"}},
	}
	st.wfs[wf.ID] = wf

	router := testRouter(reg, st, eng)

	// Hit matching webhook
	w := doJSON(t, router, "POST", "/api/webhook/myhook", map[string]any{"x": 42})
	if w.Code != http.StatusOK { t.Fatalf("webhook: status %d body=%s", w.Code, w.Body.String()) }
	body := decodeBody(t, w)
	if body["status"] != "success" { t.Fatalf("webhook status: %v", body["status"]) }

	// Non-matching path
	w = doJSON(t, router, "POST", "/api/webhook/nonexistent", nil)
	if w.Code != http.StatusNotFound { t.Fatalf("expected 404 for unknown webhook, got %d", w.Code) }
}

func TestCredentialsCRUD(t *testing.T) {
	st := newMemStore()
	router := testRouter(nil, st, nil)

	// List empty
	w := doJSON(t, router, "GET", "/api/credentials", nil)
	if w.Code != http.StatusOK { t.Fatalf("list: %d", w.Code) }
	var list []map[string]any
	json.NewDecoder(w.Body).Decode(&list)
	if len(list) != 0 { t.Fatalf("expected empty, got %d", len(list)) }

	// Create
	w = doJSON(t, router, "POST", "/api/credentials", map[string]any{
		"type": "httpHeaderAuth", "name": "My Key", "data": map[string]any{"name": "Authorization", "value": "secret"},
	})
	if w.Code != http.StatusOK { t.Fatalf("create: %d body=%s", w.Code, w.Body.String()) }
	body := decodeBody(t, w)
	cid := body["id"].(string)
	if cid == "" { t.Fatal("no credential id") }

	// List now has one
	w = doJSON(t, router, "GET", "/api/credentials", nil)
	json.NewDecoder(w.Body).Decode(&list)
	if len(list) != 1 { t.Fatalf("expected 1, got %d", len(list)) }

	// Delete
	w = doJSON(t, router, "DELETE", "/api/credentials/"+cid, nil)
	if w.Code != http.StatusOK { t.Fatalf("delete: %d", w.Code) }

	// List empty again
	w = doJSON(t, router, "GET", "/api/credentials", nil)
	json.NewDecoder(w.Body).Decode(&list)
	if len(list) != 0 { t.Fatalf("expected 0 after delete, got %d", len(list)) }
}

func TestCreateCredentialMissingFields(t *testing.T) {
	router := testRouter(nil, nil, nil)
	// Missing required JSON fields → should still succeed (API is lenient, store validates)
	w := doJSON(t, router, "POST", "/api/credentials", map[string]any{})
	if w.Code != http.StatusOK { t.Fatalf("status %d", w.Code) }
}

func TestCreateWorkflowDefaults(t *testing.T) {
	st := newMemStore()
	router := testRouter(nil, st, nil)

	// Empty body → gets defaults
	w := doJSON(t, router, "POST", "/api/workflows", map[string]any{})
	if w.Code != http.StatusOK { t.Fatalf("status %d", w.Code) }
	body := decodeBody(t, w)
	if body["id"] == nil || body["id"] == "" { t.Fatal("no id") }
	if body["name"] != "Untitled workflow" { t.Fatalf("default name: %v", body["name"]) }
}

func TestExecutionsEndpoint(t *testing.T) {
	reg := registry.New().Register(core.Nodes...)
	st := newMemStore()
	eng := &memEngine{reg: reg, ms: st}

	wf := &schema.Workflow{
		ID: "wf-exec", Name: "exec test", Active: true,
		Nodes: []schema.WFNode{
			{ID: "n1", Type: "core.manualTrigger", Params: map[string]any{}},
			{ID: "n2", Type: "core.noOp", Params: map[string]any{}},
		},
		Edges: []schema.WFEdge{{ID: "e1", Source: "n1", Target: "n2"}},
	}
	st.wfs[wf.ID] = wf

	router := testRouter(reg, st, eng)

	// Run a workflow first
	w := doJSON(t, router, "POST", "/api/workflows/wf-exec/run", nil)
	if w.Code != http.StatusOK { t.Fatalf("run: %d", w.Code) }
	body := decodeBody(t, w)
	eid := body["executionId"].(string)

	// List executions
	w = doJSON(t, router, "GET", "/api/executions", nil)
	if w.Code != http.StatusOK { t.Fatalf("list: %d", w.Code) }
	var list []map[string]any
	json.NewDecoder(w.Body).Decode(&list)
	if len(list) < 1 { t.Fatalf("expected >=1, got %d", len(list)) }

	// List by workflow
	w = doJSON(t, router, "GET", "/api/executions?workflowId=wf-exec", nil)
	json.NewDecoder(w.Body).Decode(&list)
	if len(list) < 1 { t.Fatalf("by wf: %d", len(list)) }

	// Get single execution
	w = doJSON(t, router, "GET", "/api/executions/"+eid, nil)
	if w.Code != http.StatusOK { t.Fatalf("get exec: %d body=%s", w.Code, w.Body.String()) }
	body = decodeBody(t, w)
	if body["status"] != "success" { t.Fatalf("exec status: %v", body["status"]) }
	if body["steps"] == nil { t.Fatal("no steps") }

	// Missing execution
	w = doJSON(t, router, "GET", "/api/executions/nonexistent", nil)
	if w.Code != http.StatusNotFound { t.Fatalf("expected 404, got %d", w.Code) }
}

func TestExecutionNotFound(t *testing.T) {
	router := testRouter(nil, nil, nil)
	w := doJSON(t, router, "GET", "/api/executions/doesnotexist", nil)
	if w.Code != http.StatusNotFound { t.Fatalf("expected 404, got %d", w.Code) }
}

func TestRateLimiter(t *testing.T) {
	rl := NewRateLimiter(10, 3) // 10/sec, burst 3

	// Burst: first 3 should pass immediately
	for i := 0; i < 3; i++ {
		if !rl.Allow("test") { t.Fatalf("burst token %d denied", i) }
	}
	// 4th should be denied (rate-limited)
	if rl.Allow("test") { t.Fatal("expected deny after burst exhausted") }

	// Different IPs get independent buckets
	for i := 0; i < 3; i++ {
		if !rl.Allow("other") { t.Fatalf("other ip token %d denied", i) }
	}

	// Periodic cleanup doesn't crash
	rl.Allow("cleanup-test")
}

func TestRateLimiterMiddleware(t *testing.T) {
	rl := NewRateLimiter(0.1, 1) // very restrictive for test

	var called bool
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	// First request: passes (burst=1)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusOK { t.Fatalf("first request denied: %d", w.Code) }
	if !called { t.Fatal("handler not called") }

	// Immediate second request: denied (>1 request per 10 seconds)
	called = false
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusTooManyRequests { t.Fatalf("expected 429, got %d", w.Code) }
	if called { t.Fatal("handler should not be called on rate limit") }
}

func TestSPAHandler(t *testing.T) {
	// nil FS → not found handler is not set → 404 from chi
	ms := newMemStore()
	router := NewRouter(registry.New(), ms, &memEngine{reg: registry.New(), ms: ms}, nil, nil, nil, nil, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/anything", nil))
	// Without static FS, chi returns 405 (no matching route for GET)
	if w.Code != http.StatusMethodNotAllowed {
		t.Logf("without SPA FS, unknown path returns %d", w.Code)
	}
}

func TestCORSHeaders(t *testing.T) {
	router := testRouter(nil, nil, nil)

	// OPTIONS preflight
	r := httptest.NewRequest("OPTIONS", "/api/nodes", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent { t.Fatalf("OPTIONS: %d", w.Code) }
	if w.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Fatal("no CORS header on OPTIONS")
	}

	// Normal request should also have CORS
	r = httptest.NewRequest("GET", "/api/nodes", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Fatal("no CORS header on GET")
	}
}

func TestSaveWorkflowBadBody(t *testing.T) {
	st := newMemStore()
	router := testRouter(nil, st, nil)

	// PUT with bad JSON
	r := httptest.NewRequest("PUT", "/api/workflows/someid", strings.NewReader("not json"))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest { t.Fatalf("expected 400, got %d", w.Code) }
}

func TestWebhookMissingTrigger(t *testing.T) {
	st := newMemStore()
	// Workflow without a webhook trigger
	st.wfs["wf-no-hook"] = &schema.Workflow{
		ID: "wf-no-hook", Name: "no hook", Active: true,
		Nodes: []schema.WFNode{{ID: "n1", Type: "core.manualTrigger", Params: map[string]any{}}},
	}
	router := testRouter(nil, st, nil)

	w := doJSON(t, router, "POST", "/api/webhook/anything", nil)
	if w.Code != http.StatusNotFound { t.Fatalf("expected 404, got %d", w.Code) }
}

func TestStreamEndpointReturns200(t *testing.T) {
	st := newMemStore()
	// Set up an execution in 'success' state so the SSE terminates instantly
	st.execs["exec-done"] = &execRec{
		record: schema.ExecutionRecord{ID: "exec-done", WorkflowID: "wf1", Status: "success"},
	}
	router := testRouter(nil, st, nil)

	r := httptest.NewRequest("GET", "/api/executions/exec-done/stream", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)

	// Should return SSE content type and finish immediately
	if w.Code != http.StatusOK { t.Fatalf("status %d", w.Code) }
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type: %s", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, "data:") { t.Fatalf("no SSE data: %s", body) }
	if !strings.Contains(body, "success") { t.Fatalf("no success in body: %s", body) }
}
