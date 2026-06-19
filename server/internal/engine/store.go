package engine

import (
	"context"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

// RunState is the serialized run state stashed on the execution row so a waiting
// run can resume. Mirrors RunState in packages/engine/src/store.ts.
type RunState struct {
	TriggerItems []schema.Item                       `json:"triggerItems"`
	NodeOutputs  map[string]map[string][]schema.Item `json:"nodeOutputs"`
	Visited      []string                            `json:"visited"`
}

// LoadedExecution is an execution row plus its (possibly nil) saved run state.
type LoadedExecution struct {
	schema.ExecutionRecord
	State *RunState
}

// Store is the persistence the engine depends on. The Postgres implementation
// lives in internal/store; tests inject an in-memory fake. Defining the
// interface here (the consumer) keeps the engine decoupled and testable.
type Store interface {
	CreateExecution(ctx context.Context, workflowID string) (string, error)
	StartStep(ctx context.Context, executionID, nodeID string, input []schema.Item) (string, error)
	FinishStep(ctx context.Context, stepID, status string, output []schema.Item, logs []schema.LogEntry, errMsg *string) error
	SaveState(ctx context.Context, executionID string, state *RunState) error
	SetWaiting(ctx context.Context, executionID, waitingNodeID, resumeToken string, state *RunState) error
	FinishExecution(ctx context.Context, executionID, status string) error
	GetCredentialData(ctx context.Context, id string) (map[string]any, error)
	LoadExecution(ctx context.Context, id string) (*LoadedExecution, error)
	LoadWorkflow(ctx context.Context, id string) (*schema.Workflow, error)

	// ClaimWaiting atomically transitions a waiting execution to running and
	// reports whether THIS caller won the transition (guards double-resume).
	ClaimWaiting(ctx context.Context, executionID string) (bool, error)
	// ListRunningExecutionIDs returns executions left in 'running' — used on
	// startup to recover runs interrupted by a previous process.
	ListRunningExecutionIDs(ctx context.Context) ([]string, error)
	// FailStaleRunningSteps marks an execution's still-'running' step rows as
	// errored (cleanup before a recovered run continues).
	FailStaleRunningSteps(ctx context.Context, executionID string) error
}
