// Package store is the Postgres persistence (pgx) for workflows, executions,
// steps and credentials. It implements engine.Store and adds the read queries
// the HTTP API needs. SQL is a 1:1 port of packages/engine/src/store.ts against
// the unchanged db/schema.sql.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/crypto"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/engine"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/id"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

// Store is a pgx-backed persistence layer.
type Store struct {
	pool   *pgxpool.Pool
	cipher *crypto.Cipher
}

// New constructs a Store.
func New(pool *pgxpool.Pool, cipher *crypto.Cipher) *Store {
	return &Store{pool: pool, cipher: cipher}
}

// Ensure Store satisfies the engine's persistence contract.
var _ engine.Store = (*Store)(nil)

// ---- workflows -------------------------------------------------------------

// WorkflowSummary is the list view (id, name, active only).
type WorkflowSummary struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

// SaveWorkflow inserts or updates a workflow (graph stored as JSON of the whole Workflow).
func (s *Store) SaveWorkflow(ctx context.Context, wf *schema.Workflow) error {
	g, err := json.Marshal(wf)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO workflows (id, name, graph, active, version, updated_at)
		 VALUES ($1,$2,$3,$4,1, now())
		 ON CONFLICT (id) DO UPDATE SET name=$2, graph=$3, active=$4,
		   version=workflows.version+1, updated_at=now()`,
		wf.ID, wf.Name, string(g), wf.Active)
	return err
}

// LoadWorkflow returns the full workflow graph, or nil if not found.
func (s *Store) LoadWorkflow(ctx context.Context, wfID string) (*schema.Workflow, error) {
	var graph []byte
	err := s.pool.QueryRow(ctx, `SELECT graph FROM workflows WHERE id=$1`, wfID).Scan(&graph)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var wf schema.Workflow
	if err := json.Unmarshal(graph, &wf); err != nil {
		return nil, err
	}
	return &wf, nil
}

// ListWorkflows returns workflow summaries, newest first.
func (s *Store) ListWorkflows(ctx context.Context) ([]WorkflowSummary, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, active FROM workflows ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkflowSummary{}
	for rows.Next() {
		var w WorkflowSummary
		if err := rows.Scan(&w.ID, &w.Name, &w.Active); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// ListActiveWorkflows returns full graphs of active workflows (for webhook routing).
func (s *Store) ListActiveWorkflows(ctx context.Context) ([]schema.Workflow, error) {
	rows, err := s.pool.Query(ctx, `SELECT graph FROM workflows WHERE active=true`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []schema.Workflow{}
	for rows.Next() {
		var g []byte
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		var wf schema.Workflow
		if err := json.Unmarshal(g, &wf); err != nil {
			return nil, err
		}
		out = append(out, wf)
	}
	return out, rows.Err()
}

// ---- executions (engine.Store) --------------------------------------------

// CreateExecution inserts a new running execution and returns its id.
func (s *Store) CreateExecution(ctx context.Context, workflowID string) (string, error) {
	eid := id.New()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO executions (id, workflow_id, status, started_at) VALUES ($1,$2,'running',now())`,
		eid, workflowID)
	return eid, err
}

// SetWaiting marks an execution waiting and persists its run state.
func (s *Store) SetWaiting(ctx context.Context, executionID, waitingNodeID, resumeToken string, state *engine.RunState) error {
	st, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`UPDATE executions SET status='waiting', waiting_node_id=$2, resume_token=$3, state=$4 WHERE id=$1`,
		executionID, waitingNodeID, resumeToken, string(st))
	return err
}

// SaveState persists the run state (called after each completed node).
func (s *Store) SaveState(ctx context.Context, executionID string, state *engine.RunState) error {
	st, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE executions SET state=$2 WHERE id=$1`, executionID, string(st))
	return err
}

// FinishExecution marks an execution done and clears resume fields.
func (s *Store) FinishExecution(ctx context.Context, executionID, status string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE executions SET status=$2, finished_at=now(), waiting_node_id=NULL, resume_token=NULL WHERE id=$1`,
		executionID, status)
	return err
}

// LoadExecution returns an execution row plus its saved run state, or nil.
func (s *Store) LoadExecution(ctx context.Context, eid string) (*engine.LoadedExecution, error) {
	var (
		wfID, status     string
		resume, waiting  *string
		state            []byte
		started          time.Time
		finished         *time.Time
	)
	err := s.pool.QueryRow(ctx,
		`SELECT workflow_id, status, resume_token, waiting_node_id, state, started_at, finished_at
		 FROM executions WHERE id=$1`, eid).
		Scan(&wfID, &status, &resume, &waiting, &state, &started, &finished)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	le := &engine.LoadedExecution{
		ExecutionRecord: schema.ExecutionRecord{
			ID: eid, WorkflowID: wfID, Status: status,
			ResumeToken: resume, WaitingNodeID: waiting,
			StartedAt: started.UTC().Format(time.RFC3339), FinishedAt: fmtTimePtr(finished),
		},
	}
	if len(state) > 0 {
		var rs engine.RunState
		if err := json.Unmarshal(state, &rs); err != nil {
			return nil, err
		}
		le.State = &rs
	}
	return le, nil
}

// ExecStatus is the light status used by the SSE poll loop.
type ExecStatus struct {
	Status        string
	WaitingNodeID *string
	Found         bool
}

// GetExecutionStatus reads only status + waiting node (cheap; polled by SSE).
func (s *Store) GetExecutionStatus(ctx context.Context, eid string) (ExecStatus, error) {
	var st ExecStatus
	err := s.pool.QueryRow(ctx, `SELECT status, waiting_node_id FROM executions WHERE id=$1`, eid).
		Scan(&st.Status, &st.WaitingNodeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ExecStatus{Found: false}, nil
	}
	if err != nil {
		return ExecStatus{}, err
	}
	st.Found = true
	return st, nil
}

// ListExecutions returns recent executions (optionally filtered by workflow).
func (s *Store) ListExecutions(ctx context.Context, workflowID string) ([]schema.ExecutionRecord, error) {
	var rows pgx.Rows
	var err error
	const cols = `SELECT id, workflow_id, status, resume_token, waiting_node_id, started_at, finished_at FROM executions`
	if workflowID != "" {
		rows, err = s.pool.Query(ctx, cols+` WHERE workflow_id=$1 ORDER BY started_at DESC LIMIT 100`, workflowID)
	} else {
		rows, err = s.pool.Query(ctx, cols+` ORDER BY started_at DESC LIMIT 100`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []schema.ExecutionRecord{}
	for rows.Next() {
		var r schema.ExecutionRecord
		var started time.Time
		var finished *time.Time
		if err := rows.Scan(&r.ID, &r.WorkflowID, &r.Status, &r.ResumeToken, &r.WaitingNodeID, &started, &finished); err != nil {
			return nil, err
		}
		r.StartedAt = started.UTC().Format(time.RFC3339)
		r.FinishedAt = fmtTimePtr(finished)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---- steps (engine.Store + monitoring) ------------------------------------

// StartStep inserts a running step and returns its id.
func (s *Store) StartStep(ctx context.Context, executionID, nodeID string, input []schema.Item) (string, error) {
	sid := id.New()
	in, err := json.Marshal(orEmptyItems(input))
	if err != nil {
		return "", err
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO execution_steps (id, execution_id, node_id, status, input, started_at)
		 VALUES ($1,$2,$3,'running',$4, now())`,
		sid, executionID, nodeID, string(in))
	return sid, err
}

// FinishStep records a step's result.
func (s *Store) FinishStep(ctx context.Context, stepID, status string, output []schema.Item, logs []schema.LogEntry, errMsg *string) error {
	out, err := json.Marshal(orEmptyItems(output))
	if err != nil {
		return err
	}
	lg, err := json.Marshal(orEmptyLogs(logs))
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`UPDATE execution_steps SET status=$2, output=$3, logs=$4, error=$5, finished_at=now() WHERE id=$1`,
		stepID, status, string(out), string(lg), errMsg)
	return err
}

// GetExecutionSteps returns a run's steps in start order.
func (s *Store) GetExecutionSteps(ctx context.Context, executionID string) ([]schema.StepRecord, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, execution_id, node_id, status, input, output, logs, error, started_at, finished_at
		 FROM execution_steps WHERE execution_id=$1 ORDER BY started_at ASC`, executionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []schema.StepRecord{}
	for rows.Next() {
		var r schema.StepRecord
		var input, output, logs []byte
		var started time.Time
		var finished *time.Time
		if err := rows.Scan(&r.ID, &r.ExecutionID, &r.NodeID, &r.Status, &input, &output, &logs, &r.Error, &started, &finished); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(input, &r.Input)
		_ = json.Unmarshal(output, &r.Output)
		_ = json.Unmarshal(logs, &r.Logs)
		if r.Input == nil {
			r.Input = []schema.Item{}
		}
		if r.Output == nil {
			r.Output = []schema.Item{}
		}
		r.StartedAt = started.UTC().Format(time.RFC3339)
		r.FinishedAt = fmtTimePtr(finished)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---- credentials -----------------------------------------------------------

// CredentialRow is the safe (no secret) credential view.
type CredentialRow struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`
}

// ListCredentials returns credentials without their decrypted data.
func (s *Store) ListCredentials(ctx context.Context) ([]CredentialRow, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, type, name FROM credentials ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CredentialRow{}
	for rows.Next() {
		var c CredentialRow
		if err := rows.Scan(&c.ID, &c.Type, &c.Name); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CreateCredential encrypts and stores a credential, returning its safe view.
func (s *Store) CreateCredential(ctx context.Context, ctype, name string, data map[string]any) (CredentialRow, error) {
	cid := id.New()
	b, err := json.Marshal(data)
	if err != nil {
		return CredentialRow{}, err
	}
	enc, err := s.cipher.Encrypt(string(b))
	if err != nil {
		return CredentialRow{}, err
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO credentials (id, type, name, data_encrypted, created_at) VALUES ($1,$2,$3,$4, now())`,
		cid, ctype, name, enc)
	if err != nil {
		return CredentialRow{}, err
	}
	return CredentialRow{ID: cid, Type: ctype, Name: name}, nil
}

// DeleteCredential removes a credential.
func (s *Store) DeleteCredential(ctx context.Context, cid string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM credentials WHERE id=$1`, cid)
	return err
}

// GetCredentialData decrypts and returns a credential's data (engine.Store).
func (s *Store) GetCredentialData(ctx context.Context, credID string) (map[string]any, error) {
	var enc string
	err := s.pool.QueryRow(ctx, `SELECT data_encrypted FROM credentials WHERE id=$1`, credID).Scan(&enc)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	plain, err := s.cipher.Decrypt(enc)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(plain), &m); err != nil {
		return nil, err
	}
	return m, nil
}

// ---- helpers ---------------------------------------------------------------

func fmtTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func orEmptyItems(in []schema.Item) []schema.Item {
	if in == nil {
		return []schema.Item{}
	}
	return in
}

func orEmptyLogs(in []schema.LogEntry) []schema.LogEntry {
	if in == nil {
		return []schema.LogEntry{}
	}
	return in
}
