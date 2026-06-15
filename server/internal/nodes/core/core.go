// Package core provides the vertical-agnostic building-block nodes shipped with
// the skeleton. Native Go port of packages/nodes-core/src/index.ts. The user
// Code node and {{ }} expressions run via internal/expr (goja); everything else
// is plain Go.
package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/expr"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

func itemsOrEmpty(in []schema.Item) []schema.Item {
	if len(in) > 0 {
		return in
	}
	return []schema.Item{{JSON: map[string]any{}}}
}

func scopeFor(item schema.Item, ctx *schema.ExecContext) expr.Scope {
	j := item.JSON
	if j == nil {
		j = map[string]any{}
	}
	return expr.Scope{JSON: j, Input: ctx.Input, Trigger: ctx.Trigger, Node: ctx.Upstream}
}

var manualTrigger = schema.NodeDefinition{
	Type:        "core.manualTrigger",
	Label:       "Manual Trigger",
	Group:       "trigger",
	Icon:        "Play",
	Description: "Starts the workflow when you click Run.",
	IsTrigger:   true,
	Inputs:      []schema.Port{},
	Outputs:     []schema.Port{{ID: "main"}},
	Params:      []schema.ParamSchema{},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		out := ctx.Trigger
		if len(out) == 0 {
			out = []schema.Item{{JSON: map[string]any{}}}
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
	},
}

var webhookTrigger = schema.NodeDefinition{
	Type:        "core.webhookTrigger",
	Label:       "Webhook",
	Group:       "trigger",
	Icon:        "Webhook",
	Description: "Starts the workflow when POSTed to /api/webhook/{path}. Body becomes the item.",
	IsTrigger:   true,
	Inputs:      []schema.Port{},
	Outputs:     []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "path", Label: "Path", Type: "string", Required: true, Placeholder: "my-hook"},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		out := ctx.Trigger
		if len(out) == 0 {
			out = []schema.Item{{JSON: map[string]any{}}}
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
	},
}

var set = schema.NodeDefinition{
	Type:        "core.set",
	Label:       "Set Fields",
	Group:       "transform",
	Icon:        "PencilLine",
	Description: "Merge a set of (expression-aware) fields onto each item.",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "fields", Label: "Fields (JSON object; values may use {{ }})", Type: "json", Default: map[string]any{}},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		fields := asObject(ctx.RawParam("fields"))
		in := itemsOrEmpty(ctx.Input)
		out := make([]schema.Item, 0, len(in))
		for _, item := range in {
			sc := scopeFor(item, ctx)
			merged := map[string]any{}
			for k, v := range item.JSON {
				merged[k] = v
			}
			for k, v := range fields {
				rv, err := expr.ResolveValue(v, sc)
				if err != nil {
					return schema.NodeResult{}, err
				}
				merged[k] = rv
			}
			out = append(out, schema.Item{JSON: merged})
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
	},
}

var ifNode = schema.NodeDefinition{
	Type:        "core.if",
	Label:       "If",
	Group:       "flow",
	Icon:        "GitBranch",
	Description: "Route each item to true/false based on a condition expression.",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "true", Label: "true"}, {ID: "false", Label: "false"}},
	Params: []schema.ParamSchema{
		{Name: "condition", Label: "Condition ({{ }} expression, must be truthy/falsy)", Type: "expression", Required: true, Placeholder: "{{ $json.amount > 100 }}"},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		t := []schema.Item{}
		f := []schema.Item{}
		raw := ctx.RawParam("condition")
		for _, item := range itemsOrEmpty(ctx.Input) {
			val, err := expr.ResolveValue(raw, scopeFor(item, ctx))
			if err != nil {
				return schema.NodeResult{}, err
			}
			if isTruthy(val) {
				t = append(t, item)
			} else {
				f = append(f, item)
			}
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"true": t, "false": f}}, nil
	},
}

var httpNode = schema.NodeDefinition{
	Type:        "core.http",
	Label:       "HTTP Request",
	Group:       "integration",
	Icon:        "Globe",
	Description: "Call an external HTTP API.",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "method", Label: "Method", Type: "select", Default: "GET", Options: []schema.ParamOption{
			{Label: "GET", Value: "GET"}, {Label: "POST", Value: "POST"}, {Label: "PUT", Value: "PUT"}, {Label: "PATCH", Value: "PATCH"}, {Label: "DELETE", Value: "DELETE"},
		}},
		{Name: "url", Label: "URL", Type: "expression", Required: true, Placeholder: "https://api.example.com"},
		{Name: "headers", Label: "Headers (JSON)", Type: "json", Default: map[string]any{}},
		{Name: "body", Label: "Body (JSON)", Type: "json", Default: map[string]any{}},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		method := strings.ToUpper(asString(ctx.Params["method"], "GET"))
		url := asString(ctx.Params["url"], "")
		if url == "" {
			return schema.NodeResult{}, fmt.Errorf("HTTP node: URL is required")
		}
		headers := asObject(ctx.Params["headers"])
		reqHeaders := map[string]string{}
		for k, v := range headers {
			reqHeaders[strings.ToLower(k)] = fmt.Sprintf("%v", v)
		}
		var body io.Reader
		if method != "GET" && method != "DELETE" {
			b, _ := json.Marshal(asObject(ctx.Params["body"]))
			body = bytes.NewReader(b)
			if _, ok := reqHeaders["content-type"]; !ok {
				reqHeaders["content-type"] = "application/json"
			}
		}
		ctx.Log(method+" "+url, nil)
		req, err := http.NewRequest(method, url, body)
		if err != nil {
			return schema.NodeResult{}, err
		}
		for k, v := range reqHeaders {
			req.Header.Set(k, v)
		}
		res, err := httpClient.Do(req)
		if err != nil {
			return schema.NodeResult{}, err
		}
		defer res.Body.Close()
		text, _ := io.ReadAll(res.Body)
		var data any
		if json.Unmarshal(text, &data) != nil {
			data = string(text)
		}
		item := schema.Item{JSON: map[string]any{
			"status": res.StatusCode,
			"ok":     res.StatusCode >= 200 && res.StatusCode < 300,
			"body":   data,
		}}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {item}}}, nil
	},
}

var code = schema.NodeDefinition{
	Type:        "core.code",
	Label:       "Code",
	Group:       "flow",
	Icon:        "Code",
	Description: "Run JavaScript. Receives `items` (array), must return an array of items.",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "code", Label: "JavaScript", Type: "expression", Default: "return items;", Description: "Vars: items, $trigger. Return Item[] or plain objects."},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		src := asString(ctx.RawParam("code"), "return items;")
		out, err := expr.RunCode(src, itemsOrEmpty(ctx.Input), ctx.Trigger)
		if err != nil {
			return schema.NodeResult{}, err
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
	},
}

var wait = schema.NodeDefinition{
	Type:        "core.wait",
	Label:       "Wait for Webhook",
	Group:       "flow",
	Icon:        "Clock",
	Description: "Pause the run until POST /api/resume/{executionId} is called. The next item is the resume payload.",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "main"}},
	Params:      []schema.ParamSchema{},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		return schema.NodeResult{Suspend: &schema.SuspendRequest{
			Kind:    "webhook",
			Respond: &schema.RespondSpec{Body: map[string]any{"executionId": ctx.IDs.ExecutionID, "status": "waiting"}},
		}}, nil
	},
}

// Nodes is the list of core node definitions registered with the engine.
var Nodes = []schema.NodeDefinition{
	manualTrigger,
	webhookTrigger,
	set,
	ifNode,
	httpNode,
	code,
	wait,
}

// ---- helpers ---------------------------------------------------------------

func asString(v any, def string) string {
	if v == nil {
		return def
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func asObject(v any) map[string]any {
	switch t := v.(type) {
	case string:
		var m map[string]any
		if json.Unmarshal([]byte(t), &m) == nil {
			return m
		}
		return map[string]any{}
	case map[string]any:
		return t
	default:
		return map[string]any{}
	}
}

// isTruthy mirrors JavaScript truthiness for values exported from goja.
func isTruthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case int:
		return t != 0
	case int64:
		return t != 0
	case float64:
		return t != 0 && !math.IsNaN(t)
	default:
		return true
	}
}
