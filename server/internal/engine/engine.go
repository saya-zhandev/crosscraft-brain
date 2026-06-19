// Package engine is the topological run executor with durable suspend/resume.
//
// A run walks the graph from its trigger, executing each node when all its
// upstream sources are done. Node I/O is persisted per step (powers monitoring).
// A node may suspend the run (durable wait); state is saved and the run resumes
// when /api/resume/{executionId} is called. Direct port of
// packages/engine/src/engine.ts.
package engine

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/expr"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/id"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/registry"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

// Engine executes workflows against a registry and a persistence Store.
type Engine struct {
	reg   *registry.Registry
	store Store

	// Async bounded pool (enabled by StartWorkers). When async is false, Run and
	// Resume drive inline in the caller's goroutine and return the full result —
	// the mode used by unit tests.
	async  bool
	jobs   chan string
	mu     sync.Mutex
	active map[string]bool
}

// New constructs a synchronous Engine. Call StartWorkers to switch to the
// bounded async worker pool (used in production).
func New(reg *registry.Registry, store Store) *Engine {
	return &Engine{reg: reg, store: store, active: map[string]bool{}}
}

// StartWorkers switches the engine to async mode: runs are driven by a bounded
// pool of `workers` goroutines pulling from a `queue`-deep channel, capping the
// number of concurrently executing workflows. It also recovers executions left
// 'running' by a previous process (durability across restart).
func (e *Engine) StartWorkers(ctx context.Context, workers, queue int) {
	if workers < 1 {
		workers = 1
	}
	if queue < 1 {
		queue = 1
	}
	e.jobs = make(chan string, queue)
	e.async = true
	for i := 0; i < workers; i++ {
		go e.worker(ctx)
	}
	go e.recoverRunning(ctx)
}

// RunResult is the outcome of a run/resume up to the next terminal or suspend.
type RunResult struct {
	ExecutionID string                   `json:"executionId"`
	Status      string                   `json:"status"` // success|error|waiting
	Respond     *schema.RespondSpec      `json:"respond,omitempty"`
	Outputs     map[string][]schema.Item `json:"outputs,omitempty"`
	Error       string                   `json:"error,omitempty"`
}

// ---- graph helpers ---------------------------------------------------------

func incomingEdges(wf *schema.Workflow, nodeID string) []schema.WFEdge {
	var r []schema.WFEdge
	for _, e := range wf.Edges {
		if e.Target == nodeID {
			r = append(r, e)
		}
	}
	return r
}

func outgoingEdges(wf *schema.Workflow, nodeID string) []schema.WFEdge {
	var r []schema.WFEdge
	for _, e := range wf.Edges {
		if e.Source == nodeID {
			r = append(r, e)
		}
	}
	return r
}

func findNode(wf *schema.Workflow, nodeID string) *schema.WFNode {
	for i := range wf.Nodes {
		if wf.Nodes[i].ID == nodeID {
			return &wf.Nodes[i]
		}
	}
	return nil
}

// gatherInput collects a node's input items from upstream sources (or trigger).
func gatherInput(wf *schema.Workflow, nodeID string, state *RunState) []schema.Item {
	incoming := incomingEdges(wf, nodeID)
	if len(incoming) == 0 {
		return state.TriggerItems
	}
	items := []schema.Item{}
	for _, e := range incoming {
		port := e.SourceHandle
		if port == "" {
			port = schema.DefaultPort
		}
		if outs, ok := state.NodeOutputs[e.Source]; ok {
			items = append(items, outs[port]...)
		}
	}
	return items
}

func isReady(wf *schema.Workflow, nodeID string, visited map[string]bool) bool {
	for _, e := range incomingEdges(wf, nodeID) {
		if !visited[e.Source] {
			return false
		}
	}
	return true
}

func enqueueReadySuccessors(wf *schema.Workflow, nodeID string, visited map[string]bool, ready []string) []string {
	for _, e := range outgoingEdges(wf, nodeID) {
		if !visited[e.Target] && !contains(ready, e.Target) && isReady(wf, e.Target, visited) {
			ready = append(ready, e.Target)
		}
	}
	return ready
}

func flattenOutputs(outputs map[string][]schema.Item) []schema.Item {
	ports := make([]string, 0, len(outputs))
	for p := range outputs {
		ports = append(ports, p)
	}
	sort.Strings(ports) // deterministic order for monitoring
	var r []schema.Item
	for _, p := range ports {
		r = append(r, outputs[p]...)
	}
	return r
}

func terminalOutputs(wf *schema.Workflow, state *RunState) map[string][]schema.Item {
	out := map[string][]schema.Item{}
	for _, n := range wf.Nodes {
		produced, ok := state.NodeOutputs[n.ID]
		if ok && len(outgoingEdges(wf, n.ID)) == 0 {
			out[n.ID] = produced[schema.DefaultPort]
		}
	}
	return out
}

func (e *Engine) findTrigger(wf *schema.Workflow) (*schema.WFNode, error) {
	for i := range wf.Nodes {
		n := &wf.Nodes[i]
		if def, ok := e.reg.Get(n.Type); ok && def.IsTrigger {
			return n, nil
		}
	}
	for i := range wf.Nodes {
		n := &wf.Nodes[i]
		if len(incomingEdges(wf, n.ID)) == 0 {
			return n, nil
		}
	}
	return nil, fmt.Errorf("workflow has no trigger / root node")
}

// buildContext wires the ExecContext closures for one node execution. Returns an
// error if eager param/expression resolution fails (treated as a node error).
func (e *Engine) buildContext(
	ctx context.Context,
	wf *schema.Workflow,
	node *schema.WFNode,
	executionID string,
	state *RunState,
	inputItems []schema.Item,
	logs *[]schema.LogEntry,
) (*schema.ExecContext, error) {
	firstJSON := map[string]any{}
	if len(inputItems) > 0 && inputItems[0].JSON != nil {
		firstJSON = inputItems[0].JSON
	}
	upstream := func(idArg string) []schema.Item {
		if outs, ok := state.NodeOutputs[idArg]; ok {
			return outs[schema.DefaultPort]
		}
		return nil
	}
	sc := expr.Scope{JSON: firstJSON, Input: inputItems, Trigger: state.TriggerItems, Node: upstream}
	params, err := expr.ResolveParams(node.Params, sc)
	if err != nil {
		return nil, err
	}
	cc := &schema.ExecContext{
		Input:    inputItems,
		Params:   params,
		RawParam: func(name string) any { return node.Params[name] },
		Upstream: upstream,
		Credential: func(paramName string) (map[string]any, error) {
			v, ok := params[paramName].(string)
			if !ok || v == "" {
				return nil, nil
			}
			return e.store.GetCredentialData(ctx, v)
		},
		Trigger: state.TriggerItems,
		Log:     func(message string, data any) { *logs = append(*logs, schema.LogEntry{Message: message, Data: data}) },
		First: func() map[string]any {
			if len(inputItems) > 0 && inputItems[0].JSON != nil {
				return inputItems[0].JSON
			}
			return map[string]any{}
		},
		IDs: schema.ExecIDs{WorkflowID: wf.ID, ExecutionID: executionID, NodeID: node.ID},
	}
	return cc, nil
}

// drive is the core loop shared by Run and Resume. It mutates state.
func (e *Engine) drive(ctx context.Context, wf *schema.Workflow, executionID string, state *RunState, ready []string) (RunResult, error) {
	visited := map[string]bool{}
	for _, v := range state.Visited {
		visited[v] = true
	}

	for len(ready) > 0 {
		nodeID := ready[0]
		ready = ready[1:]
		if visited[nodeID] {
			continue
		}
		node := findNode(wf, nodeID)
		if node == nil {
			continue
		}

		def, ok := e.reg.Get(node.Type)
		if !ok {
			msg := "unknown node type: " + node.Type
			_ = e.store.FinishExecution(ctx, executionID, "error")
			return RunResult{ExecutionID: executionID, Status: "error", Error: msg}, nil
		}

		inputItems := gatherInput(wf, nodeID, state)

		// Prune untaken branches: a non-trigger node with connected inputs but
		// no items is skipped (and its successors considered).
		if !def.IsTrigger && len(incomingEdges(wf, nodeID)) > 0 && len(inputItems) == 0 {
			state.NodeOutputs[nodeID] = map[string][]schema.Item{schema.DefaultPort: {}}
			visited[nodeID] = true
			state.Visited = setKeys(visited)
			ready = enqueueReadySuccessors(wf, nodeID, visited, ready)
			continue
		}

		logs := []schema.LogEntry{}
		stepID, err := e.store.StartStep(ctx, executionID, nodeID, inputItems)
		if err != nil {
			return RunResult{}, err
		}

		var result schema.NodeResult
		cc, perr := e.buildContext(ctx, wf, node, executionID, state, inputItems, &logs)
		execErr := perr
		if execErr == nil {
			result, execErr = def.Execute(cc)
		}
		if execErr != nil {
			msg := execErr.Error()
			_ = e.store.FinishStep(ctx, stepID, "error", nil, logs, &msg)
			_ = e.store.FinishExecution(ctx, executionID, "error")
			return RunResult{ExecutionID: executionID, Status: "error", Error: msg}, nil
		}

		if result.Suspend != nil {
			logs = append(logs, schema.LogEntry{Message: "suspended; awaiting resume"})
			_ = e.store.FinishStep(ctx, stepID, "success", nil, logs, nil)
			state.Visited = setKeys(visited) // waiting node intentionally NOT visited
			resumeToken := id.New()
			if err := e.store.SetWaiting(ctx, executionID, nodeID, resumeToken, state); err != nil {
				return RunResult{}, err
			}
			return RunResult{ExecutionID: executionID, Status: "waiting", Respond: result.Suspend.Respond}, nil
		}

		state.NodeOutputs[nodeID] = result.Outputs
		visited[nodeID] = true
		state.Visited = setKeys(visited)
		if err := e.store.FinishStep(ctx, stepID, "success", flattenOutputs(result.Outputs), logs, nil); err != nil {
			return RunResult{}, err
		}
		if err := e.store.SaveState(ctx, executionID, state); err != nil {
			return RunResult{}, err
		}
		ready = enqueueReadySuccessors(wf, nodeID, visited, ready)
	}

	if err := e.store.FinishExecution(ctx, executionID, "success"); err != nil {
		return RunResult{}, err
	}
	return RunResult{ExecutionID: executionID, Status: "success", Outputs: terminalOutputs(wf, state)}, nil
}

// Run starts a new execution. In sync mode it drives to completion/suspend and
// returns the full result; in async mode it persists the seed state, enqueues the
// run on the bounded pool, and returns immediately with status "running".
func (e *Engine) Run(ctx context.Context, wf *schema.Workflow, triggerItems []schema.Item) (RunResult, error) {
	trigger, err := e.findTrigger(wf)
	if err != nil {
		return RunResult{}, err
	}
	if triggerItems == nil {
		triggerItems = []schema.Item{{JSON: map[string]any{}}}
	}
	executionID, err := e.store.CreateExecution(ctx, wf.ID)
	if err != nil {
		return RunResult{}, err
	}
	state := &RunState{
		TriggerItems: triggerItems,
		NodeOutputs:  map[string]map[string][]schema.Item{},
		Visited:      []string{},
	}
	if !e.async {
		return e.drive(ctx, wf, executionID, state, []string{trigger.ID})
	}
	// Persist the seed so a crash before the first node is still recoverable.
	if err := e.store.SaveState(ctx, executionID, state); err != nil {
		return RunResult{}, err
	}
	e.enqueue(executionID)
	return RunResult{ExecutionID: executionID, Status: "running"}, nil
}

// Resume continues a waiting execution with the payload that arrived (the resumed
// node's output). The waiting→running transition is an atomic compare-and-set, so
// duplicate/concurrent resumes are rejected — only the first wins.
func (e *Engine) Resume(ctx context.Context, executionID string, payload []schema.Item) (RunResult, error) {
	exec, err := e.store.LoadExecution(ctx, executionID)
	if err != nil {
		return RunResult{}, err
	}
	if exec == nil {
		return RunResult{}, fmt.Errorf("execution not found: %s", executionID)
	}
	if exec.Status != "waiting" || exec.State == nil || exec.WaitingNodeID == nil {
		return RunResult{}, fmt.Errorf("execution %s is not waiting (status=%s)", executionID, exec.Status)
	}
	// Atomic guard against double-resume: only one caller flips waiting→running.
	claimed, err := e.store.ClaimWaiting(ctx, executionID)
	if err != nil {
		return RunResult{}, err
	}
	if !claimed {
		return RunResult{}, fmt.Errorf("execution %s was already resumed", executionID)
	}

	state := exec.State
	if state.NodeOutputs == nil {
		state.NodeOutputs = map[string]map[string][]schema.Item{}
	}
	waitingNodeID := *exec.WaitingNodeID
	state.NodeOutputs[waitingNodeID] = map[string][]schema.Item{schema.DefaultPort: payload}

	visited := map[string]bool{}
	for _, v := range state.Visited {
		visited[v] = true
	}
	visited[waitingNodeID] = true
	state.Visited = setKeys(visited)
	if err := e.store.SaveState(ctx, executionID, state); err != nil {
		return RunResult{}, err
	}

	if !e.async {
		wf, err := e.store.LoadWorkflow(ctx, exec.WorkflowID)
		if err != nil {
			return RunResult{}, err
		}
		if wf == nil {
			return RunResult{}, fmt.Errorf("workflow not found: %s", exec.WorkflowID)
		}
		ready := enqueueReadySuccessors(wf, waitingNodeID, visited, []string{})
		return e.drive(ctx, wf, executionID, state, ready)
	}
	e.enqueue(executionID)
	return RunResult{ExecutionID: executionID, Status: "running"}, nil
}

// ---- async worker pool -----------------------------------------------------

func (e *Engine) enqueue(executionID string) { e.jobs <- executionID }

func (e *Engine) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case eid := <-e.jobs:
			e.process(ctx, eid)
		}
	}
}

// claim ensures only one worker drives a given execution at a time (so a recovery
// re-enqueue and a fresh enqueue can't double-drive the same run).
func (e *Engine) claim(executionID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.active[executionID] {
		return false
	}
	e.active[executionID] = true
	return true
}

func (e *Engine) release(executionID string) {
	e.mu.Lock()
	delete(e.active, executionID)
	e.mu.Unlock()
}

// process drives one execution from its persisted state. It unifies fresh runs,
// resumes, and crash recovery: the ready frontier is recomputed from `visited`.
func (e *Engine) process(ctx context.Context, executionID string) {
	if !e.claim(executionID) {
		return // another worker already owns it
	}
	defer e.release(executionID)

	exec, err := e.store.LoadExecution(ctx, executionID)
	if err != nil || exec == nil || exec.Status != "running" {
		return
	}
	wf, err := e.store.LoadWorkflow(ctx, exec.WorkflowID)
	if err != nil || wf == nil {
		_ = e.store.FinishExecution(ctx, executionID, "error")
		return
	}

	state := exec.State
	if state == nil {
		state = &RunState{NodeOutputs: map[string]map[string][]schema.Item{}}
	}
	if state.NodeOutputs == nil {
		state.NodeOutputs = map[string]map[string][]schema.Item{}
	}
	if state.TriggerItems == nil {
		state.TriggerItems = []schema.Item{{JSON: map[string]any{}}}
	}

	visited := map[string]bool{}
	for _, v := range state.Visited {
		visited[v] = true
	}

	var ready []string
	if len(visited) == 0 {
		trigger, err := e.findTrigger(wf)
		if err != nil {
			_ = e.store.FinishExecution(ctx, executionID, "error")
			return
		}
		ready = []string{trigger.ID}
	} else {
		ready = e.frontier(wf, visited)
	}
	if _, err := e.drive(ctx, wf, executionID, state, ready); err != nil {
		log.Printf("execution %s: drive error: %v", executionID, err)
	}
}

// frontier returns not-yet-visited nodes whose upstream sources are all visited —
// the set ready to run when continuing a partially-executed graph.
func (e *Engine) frontier(wf *schema.Workflow, visited map[string]bool) []string {
	ready := []string{}
	for nodeID := range visited {
		ready = enqueueReadySuccessors(wf, nodeID, visited, ready)
	}
	return ready
}

// recoverRunning re-enqueues executions left 'running' by a previous process.
// Per-node state was checkpointed after each step, so they continue from the last
// checkpoint; stale 'running' step rows are failed first. The interrupted node may
// re-run (at-least-once).
func (e *Engine) recoverRunning(ctx context.Context) {
	ids, err := e.store.ListRunningExecutionIDs(ctx)
	if err != nil {
		log.Printf("recovery: list running executions: %v", err)
		return
	}
	if len(ids) > 0 {
		log.Printf("recovery: re-enqueuing %d interrupted execution(s)", len(ids))
	}
	for _, eid := range ids {
		_ = e.store.FailStaleRunningSteps(ctx, eid)
		e.enqueue(eid)
	}
}

// ---- small helpers ---------------------------------------------------------

func contains(s []string, x string) bool {
	for _, v := range s {
		if v == x {
			return true
		}
	}
	return false
}

func setKeys(m map[string]bool) []string {
	r := make([]string, 0, len(m))
	for k, v := range m {
		if v {
			r = append(r, k)
		}
	}
	return r
}
