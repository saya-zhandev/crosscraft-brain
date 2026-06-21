package engine

import (
	"context"
	"fmt"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

// runSubWorkflow executes a sub-workflow synchronously (bypassing the async pool)
// and returns its terminal output items. Used by the Execute Workflow node via
// ExecContext.RunSubWorkflow.
func (e *Engine) runSubWorkflow(ctx context.Context, wfID string, items []schema.Item) ([]schema.Item, error) {
	subWF, err := e.store.LoadWorkflow(ctx, wfID)
	if err != nil || subWF == nil {
		return nil, fmt.Errorf("sub-workflow %q not found", wfID)
	}
	trigger, err := e.findTrigger(subWF)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []schema.Item{{JSON: map[string]any{}}}
	}
	executionID, err := e.store.CreateExecution(ctx, subWF.ID)
	if err != nil {
		return nil, err
	}
	state := &RunState{
		TriggerItems: items,
		NodeOutputs:  map[string]map[string][]schema.Item{},
		Visited:      []string{},
	}
	res, err := e.drive(ctx, subWF, executionID, state, []string{trigger.ID})
	if err != nil {
		return nil, err
	}
	if res.Status == "error" {
		return nil, fmt.Errorf("sub-workflow: %s", res.Error)
	}
	if res.Status == "waiting" {
		return nil, fmt.Errorf("sub-workflow suspended; async sub-workflows not supported")
	}
	all := []schema.Item{}
	for _, its := range res.Outputs {
		all = append(all, its...)
	}
	return all, nil
}
