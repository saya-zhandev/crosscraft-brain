package core

import (
	"encoding/json"
	"fmt"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/expr"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

// Flow-control function nodes (routing). Registered onto core.Nodes via init.
func init() { Nodes = append(Nodes, flowNodes...) }

var flowNodes = []schema.NodeDefinition{switchNode, filterNode, mergeNode, noOpNode, stopAndErrorNode}

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
