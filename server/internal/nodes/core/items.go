package core

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

// Item-list function nodes (transform). Registered onto core.Nodes via init.
func init() { Nodes = append(Nodes, itemNodes...) }

var itemNodes = []schema.NodeDefinition{splitOutNode, aggregateNode, limitNode, sortNode, removeDuplicatesNode, renameKeysNode}

var splitOutNode = schema.NodeDefinition{
	Type: "core.splitOut", Label: "Split Out", Group: "transform", Icon: "Rows3",
	Description: "Turn a list field into separate items (one per element).",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "field", Label: "Field to split out (an array)", Type: "string", Required: true, Placeholder: "items"},
		{Name: "destination", Label: "Destination field (for non-object elements)", Type: "string"},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		field := asString(ctx.Params["field"], "")
		if field == "" {
			return schema.NodeResult{}, fmt.Errorf("field is required")
		}
		dest := asString(ctx.Params["destination"], field)
		out := []schema.Item{}
		for _, item := range itemsOrEmpty(ctx.Input) {
			arr, ok := item.JSON[field].([]any)
			if !ok {
				continue
			}
			for _, e := range arr {
				if m, ok := e.(map[string]any); ok {
					out = append(out, schema.Item{JSON: m})
				} else {
					out = append(out, schema.Item{JSON: map[string]any{dest: e}})
				}
			}
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
	},
}

var aggregateNode = schema.NodeDefinition{
	Type: "core.aggregate", Label: "Aggregate", Group: "transform", Icon: "Combine",
	Description: "Combine all items into one — collect a field into a list, or all item data.",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "mode", Label: "Mode", Type: "select", Default: "allItemData", Options: []schema.ParamOption{
			{Label: "All item data → field", Value: "allItemData"},
			{Label: "A field → list", Value: "fieldToList"},
		}},
		{Name: "field", Label: "Field (for 'field → list')", Type: "string",
			ShowWhen: &schema.ShowWhen{Param: "mode", Equals: []any{"fieldToList"}}},
		{Name: "destination", Label: "Output field", Type: "string", Default: "data"},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		mode := asString(ctx.Params["mode"], "allItemData")
		dest := asString(ctx.Params["destination"], "data")
		if mode == "fieldToList" {
			field := asString(ctx.Params["field"], "")
			vals := []any{}
			for _, item := range ctx.Input {
				if v, ok := item.JSON[field]; ok {
					vals = append(vals, v)
				}
			}
			return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{JSON: map[string]any{dest: vals}}}}}, nil
		}
		all := []any{}
		for _, item := range ctx.Input {
			all = append(all, item.JSON)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{JSON: map[string]any{dest: all}}}}}, nil
	},
}

var limitNode = schema.NodeDefinition{
	Type: "core.limit", Label: "Limit", Group: "transform", Icon: "ListFilter",
	Description: "Keep only the first or last N items.",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "maxItems", Label: "Max items", Type: "number", Default: 1},
		{Name: "keep", Label: "Keep", Type: "select", Default: "first", Options: []schema.ParamOption{
			{Label: "First", Value: "first"}, {Label: "Last", Value: "last"},
		}},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		n := asInt(ctx.Params["maxItems"], 1)
		if n < 0 {
			n = 0
		}
		in := ctx.Input
		if n >= len(in) {
			return schema.NodeResult{Outputs: map[string][]schema.Item{"main": in}}, nil
		}
		var out []schema.Item
		if asString(ctx.Params["keep"], "first") == "last" {
			out = in[len(in)-n:]
		} else {
			out = in[:n]
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
	},
}

var sortNode = schema.NodeDefinition{
	Type: "core.sort", Label: "Sort", Group: "transform", Icon: "ArrowUpDown",
	Description: "Sort items by a field (ascending or descending).",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "field", Label: "Field", Type: "string", Required: true},
		{Name: "order", Label: "Order", Type: "select", Default: "asc", Options: []schema.ParamOption{
			{Label: "Ascending", Value: "asc"}, {Label: "Descending", Value: "desc"},
		}},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		field := asString(ctx.Params["field"], "")
		desc := asString(ctx.Params["order"], "asc") == "desc"
		out := append([]schema.Item{}, ctx.Input...)
		sort.SliceStable(out, func(i, j int) bool {
			c := compareValues(out[i].JSON[field], out[j].JSON[field])
			if desc {
				return c > 0
			}
			return c < 0
		})
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
	},
}

var removeDuplicatesNode = schema.NodeDefinition{
	Type: "core.removeDuplicates", Label: "Remove Duplicates", Group: "transform", Icon: "CopyMinus",
	Description: "Drop items with a duplicate key (by fields, or the whole item).",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "fields", Label: "Fields (comma-separated; empty = whole item)", Type: "string"},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		fields := []string{}
		for _, f := range strings.Split(asString(ctx.Params["fields"], ""), ",") {
			if f = strings.TrimSpace(f); f != "" {
				fields = append(fields, f)
			}
		}
		seen := map[string]bool{}
		out := []schema.Item{}
		for _, item := range itemsOrEmpty(ctx.Input) {
			var key string
			if len(fields) == 0 {
				b, _ := json.Marshal(item.JSON)
				key = string(b)
			} else {
				parts := make([]string, len(fields))
				for i, f := range fields {
					parts[i] = fmt.Sprintf("%v", item.JSON[f])
				}
				key = strings.Join(parts, "\x00")
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, item)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
	},
}

var renameKeysNode = schema.NodeDefinition{
	Type: "core.renameKeys", Label: "Rename Keys", Group: "transform", Icon: "PenLine",
	Description: "Rename fields on each item.",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "renames", Label: `Renames (JSON: {"oldKey":"newKey"})`, Type: "json", Default: map[string]any{}},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		renames := asObject(ctx.RawParam("renames"))
		out := make([]schema.Item, 0, len(itemsOrEmpty(ctx.Input)))
		for _, item := range itemsOrEmpty(ctx.Input) {
			m := map[string]any{}
			for k, v := range item.JSON {
				m[k] = v
			}
			for oldK, newKAny := range renames {
				newK, _ := newKAny.(string)
				if newK == "" {
					continue
				}
				if v, ok := m[oldK]; ok {
					m[newK] = v
					delete(m, oldK)
				}
			}
			out = append(out, schema.Item{JSON: m})
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
	},
}

// ---- shared helpers (transform/util) ---------------------------------------

func asInt(v any, def int) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	case string:
		var n int
		if _, err := fmt.Sscanf(t, "%d", &n); err == nil {
			return n
		}
	}
	return def
}

func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	}
	return 0, false
}

// compareValues orders two field values: numerically when both are numbers, else
// by string form. Returns -1, 0, or 1.
func compareValues(a, b any) int {
	if af, aok := toFloat(a); aok {
		if bf, bok := toFloat(b); bok {
			switch {
			case af < bf:
				return -1
			case af > bf:
				return 1
			default:
				return 0
			}
		}
	}
	return strings.Compare(fmt.Sprintf("%v", a), fmt.Sprintf("%v", b))
}
