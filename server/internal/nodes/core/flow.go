package core

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/expr"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

// Flow-control function nodes (routing). Registered onto core.Nodes via init.
func init() { Nodes = append(Nodes, flowNodes...) }

var flowNodes = []schema.NodeDefinition{switchNode, filterNode, mergeNode, noOpNode, stopAndErrorNode, compareDatasetsNode, executeWorkflowNode, errorTriggerNode}

var filterNode = schema.NodeDefinition{
	Type: "core.filter", Label: "Filter", Group: "flow", Icon: "Filter",
	Description: "Keep only the items whose condition is truthy.",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "condition", Label: "Condition ({{ }} expression)", Type: "expression", Required: true, Placeholder: "{{ $json.active }}"},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		raw := ctx.RawParam("condition")
		out := []schema.Item{}
		for _, item := range itemsOrEmpty(ctx.Input) {
			v, err := expr.ResolveValue(raw, scopeFor(item, ctx))
			if err != nil {
				return schema.NodeResult{}, err
			}
			if isTruthy(v) {
				out = append(out, item)
			}
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
	},
}

var switchNode = schema.NodeDefinition{
	Type: "core.switch", Label: "Switch", Group: "flow", Icon: "Split",
	Description: "Route each item to output 0–3 by the first matching rule, else default.",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "0"}, {ID: "1"}, {ID: "2"}, {ID: "3"}, {ID: "default"}},
	Params: []schema.ParamSchema{
		{Name: "rules", Label: `Rules (JSON: [{"condition":"{{ }}","output":0}])`, Type: "json", Default: []any{}},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		rules := asRules(ctx.RawParam("rules"))
		out := map[string][]schema.Item{"0": {}, "1": {}, "2": {}, "3": {}, "default": {}}
		for _, item := range itemsOrEmpty(ctx.Input) {
			routed := false
			for _, r := range rules {
				v, err := expr.ResolveValue(r.Condition, scopeFor(item, ctx))
				if err != nil {
					return schema.NodeResult{}, err
				}
				if isTruthy(v) {
					port := fmt.Sprintf("%d", r.Output)
					if _, ok := out[port]; !ok {
						port = "default"
					}
					out[port] = append(out[port], item)
					routed = true
					break
				}
			}
			if !routed {
				out["default"] = append(out["default"], item)
			}
		}
		return schema.NodeResult{Outputs: out}, nil
	},
}

var mergeNode = schema.NodeDefinition{
	Type: "core.merge", Label: "Merge", Group: "flow", Icon: "Merge",
	Description: "Combine items from multiple connected inputs into one stream (append).",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "main"}},
	Params:      []schema.ParamSchema{},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": ctx.Input}}, nil
	},
}

var noOpNode = schema.NodeDefinition{
	Type: "core.noOp", Label: "No Operation", Group: "flow", Icon: "Circle",
	Description: "Do nothing; pass items through unchanged.",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "main"}},
	Params:      []schema.ParamSchema{},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": ctx.Input}}, nil
	},
}

var stopAndErrorNode = schema.NodeDefinition{
	Type: "core.stopAndError", Label: "Stop and Error", Group: "flow", Icon: "OctagonX",
	Description: "Throw an error to stop the workflow.",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "message", Label: "Error message", Type: "expression", Required: true, Default: "Workflow stopped"},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		raw := ctx.RawParam("message")
		msg := "Workflow stopped"
		if len(ctx.Input) > 0 {
			if v, err := expr.ResolveValue(raw, scopeFor(ctx.Input[0], ctx)); err == nil && v != nil && fmt.Sprintf("%v", v) != "" {
				msg = fmt.Sprintf("%v", v)
			}
		} else if s, ok := raw.(string); ok && s != "" {
			msg = s
		}
		return schema.NodeResult{}, fmt.Errorf("%s", msg)
	},
}

// ── Compare Datasets ──────────────────────────────────────────────────────

var compareDatasetsNode = schema.NodeDefinition{
	Type: "core.compareDatasets", Label: "Compare Datasets", Group: "flow", Icon: "GitCompare",
	Description: "Compare items from two inputs by a key field. Output only-in-A, only-in-B, same, and different.",
	Inputs:  []schema.Port{{ID: "inputA"}, {ID: "inputB"}},
	Outputs: []schema.Port{{ID: "inAOnly"}, {ID: "inBOnly"}, {ID: "same"}, {ID: "different"}},
	Params: []schema.ParamSchema{
		{Name: "keyField", Label: "Key field to match on", Type: "string", Required: true, Placeholder: "id", Default: "id"},
		{Name: "compareFields", Label: "Fields to compare (comma-separated, empty=all)", Type: "string", Placeholder: "name,price"},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		keyField := asString(ctx.Params["keyField"], "id")
		compareCSV := asString(ctx.Params["compareFields"], "")

		// Items arrive from both inputs concatenated in ctx.Input. Users tag
		// items with "_dataset": "a" or "_dataset": "b" via a Set node upstream.
		in := itemsOrEmpty(ctx.Input)
		datasetA := []schema.Item{}
		datasetB := []schema.Item{}
		for _, it := range in {
			if v, ok := it.JSON["_dataset"].(string); ok && v == "b" {
				datasetB = append(datasetB, it)
			} else {
				datasetA = append(datasetA, it)
			}
		}

		// Index B by key
		bByKey := map[string]schema.Item{}
		for _, it := range datasetB {
			k := fmt.Sprintf("%v", it.JSON[keyField])
			bByKey[k] = it
		}

		inAOnly := []schema.Item{}
		inBOnly := []schema.Item{}
		same := []schema.Item{}
		different := []schema.Item{}
		seenB := map[string]bool{}

		for _, a := range datasetA {
			ak := fmt.Sprintf("%v", a.JSON[keyField])
			b, ok := bByKey[ak]
			if !ok {
				inAOnly = append(inAOnly, a)
				continue
			}
			seenB[ak] = true
			if fieldsEqual(a, b, compareCSV) {
				merged := map[string]any{}
				for k, v := range a.JSON {
					merged[k] = v
				}
				merged["_compared"] = "same"
				same = append(same, schema.Item{JSON: merged})
			} else {
				merged := map[string]any{}
				for k, v := range a.JSON {
					merged[k] = v
				}
				merged["_compared"] = "different"
				for k, vb := range b.JSON {
					merged["_b_"+k] = vb
				}
				different = append(different, schema.Item{JSON: merged})
			}
		}
		for _, b := range datasetB {
			bk := fmt.Sprintf("%v", b.JSON[keyField])
			if !seenB[bk] {
				inBOnly = append(inBOnly, b)
			}
		}

		return schema.NodeResult{Outputs: map[string][]schema.Item{
			"inAOnly": inAOnly, "inBOnly": inBOnly,
			"same": same, "different": different,
		}}, nil
	},
}

func fieldsEqual(a, b schema.Item, fieldsCSV string) bool {
	if fieldsCSV == "" {
		// Compare all shared keys
		for k, va := range a.JSON {
			if k == "_dataset" {
				continue
			}
			vb, ok := b.JSON[k]
			if !ok || fmt.Sprintf("%v", va) != fmt.Sprintf("%v", vb) {
				return false
			}
		}
		return true
	}
	for _, f := range splitCSV(fieldsCSV) {
		va := fmt.Sprintf("%v", a.JSON[f])
		vb := fmt.Sprintf("%v", b.JSON[f])
		if va != vb {
			return false
		}
	}
	return true
}

// ── Execute Workflow ───────────────────────────────────────────────────────

var executeWorkflowNode = schema.NodeDefinition{
	Type: "core.executeWorkflow", Label: "Execute Workflow", Group: "flow", Icon: "PlaySquare",
	Description: "Run another workflow with the current items as input and return its output.",
	Inputs:  []schema.Port{{ID: "main"}},
	Outputs: []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "workflowId", Label: "Workflow ID", Type: "string", Required: true, Placeholder: "The target workflow's ID"},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		wfID := asString(ctx.Params["workflowId"], "")
		if wfID == "" {
			return schema.NodeResult{}, fmt.Errorf("executeWorkflow: workflowId is required")
		}
		if ctx.RunSubWorkflow == nil {
			return schema.NodeResult{}, fmt.Errorf("executeWorkflow: engine does not support sub-workflows")
		}
		items := itemsOrEmpty(ctx.Input)
		out, err := ctx.RunSubWorkflow(context.Background(), wfID, items)
		if err != nil {
			return schema.NodeResult{}, err
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
	},
}

// ── Error Trigger ──────────────────────────────────────────────────────────

var errorTriggerNode = schema.NodeDefinition{
	Type: "core.errorTrigger", Label: "Error Trigger", Group: "trigger", Icon: "AlertTriangle",
	Description: "Start the workflow when another workflow errors. Receives error info (workflowId, executionId, nodeId, message).",
	IsTrigger: true,
	Inputs:    []schema.Port{},
	Outputs:   []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "workflowId", Label: "Source Workflow ID (empty = any)", Type: "string", Placeholder: "Leave empty to catch all errors"},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		out := ctx.Trigger
		if len(out) == 0 {
			out = []schema.Item{{JSON: map[string]any{"error": "triggered without payload"}}}
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
	},
}

// rule is one Switch routing rule.
type rule struct {
	Condition string
	Output    int
}

func asRules(v any) []rule {
	var arr []any
	switch t := v.(type) {
	case []any:
		arr = t
	case string:
		_ = json.Unmarshal([]byte(t), &arr)
	}
	out := []rule{}
	for _, e := range arr {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		cond, _ := m["condition"].(string)
		outIdx := 0
		switch o := m["output"].(type) {
		case float64:
			outIdx = int(o)
		case int:
			outIdx = o
		case string:
			_, _ = fmt.Sscanf(o, "%d", &outIdx)
		}
		out = append(out, rule{Condition: cond, Output: outIdx})
	}
	return out
}
