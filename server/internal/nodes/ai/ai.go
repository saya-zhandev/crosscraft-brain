// Package ai provides LLM action nodes usable inside any workflow. Native Go port
// of packages/nodes-ai/src/index.ts; calls go through internal/llm.
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/llm"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

func itemsOrEmpty(in []schema.Item) []schema.Item {
	if len(in) > 0 {
		return in
	}
	return []schema.Item{{JSON: map[string]any{}}}
}

// Nodes returns the AI node definitions, bound to the given LLM client.
func Nodes(c *llm.Client) []schema.NodeDefinition {
	summarize := schema.NodeDefinition{
		Type:        "ai.summarize",
		Label:       "AI Summarize",
		Group:       "ai",
		Icon:        "Sparkles",
		Description: "Summarize text with an LLM.",
		Inputs:      []schema.Port{{ID: "main"}},
		Outputs:     []schema.Port{{ID: "main"}},
		Params: []schema.ParamSchema{
			{Name: "text", Label: "Text", Type: "expression", Required: true, Placeholder: "{{ $json.body }}"},
			{Name: "maxWords", Label: "Max words", Type: "number", Default: 60},
		},
		Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
			out := []schema.Item{}
			for range itemsOrEmpty(ctx.Input) {
				maxWords := asInt(ctx.Params["maxWords"], 60)
				summary, err := c.Complete(context.Background(), llm.CompleteOpts{
					Model:  c.Models.Fast,
					System: fmt.Sprintf("Summarize the user's text in at most %d words. Output only the summary.", maxWords),
					Prompt: asString(ctx.Params["text"], ""),
				})
				if err != nil {
					return schema.NodeResult{}, err
				}
				out = append(out, schema.Item{JSON: map[string]any{"summary": summary}})
			}
			return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
		},
	}

	classify := schema.NodeDefinition{
		Type:        "ai.classify",
		Label:       "AI Classify",
		Group:       "ai",
		Icon:        "Tags",
		Description: "Classify text into one of the provided categories.",
		Inputs:      []schema.Port{{ID: "main"}},
		Outputs:     []schema.Port{{ID: "main"}},
		Params: []schema.ParamSchema{
			{Name: "text", Label: "Text", Type: "expression", Required: true},
			{Name: "categories", Label: "Categories (JSON array)", Type: "json", Default: []any{"urgent", "normal"}},
		},
		Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
			cats := asStringSlice(ctx.Params["categories"], []string{"a", "b"})
			out := []schema.Item{}
			for range itemsOrEmpty(ctx.Input) {
				res, err := c.Structured(context.Background(), llm.StructuredOpts{
					Model:    c.Models.Fast,
					System:   "Classify the text into exactly one category.",
					Prompt:   fmt.Sprintf("Categories: %s\n\nText:\n%s", strings.Join(cats, ", "), asString(ctx.Params["text"], "")),
					ToolName: "classify",
					Schema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"category":   map[string]any{"type": "string", "enum": cats},
							"confidence": map[string]any{"type": "number"},
						},
						"required": []string{"category"},
					},
				})
				if err != nil {
					return schema.NodeResult{}, err
				}
				out = append(out, schema.Item{JSON: map[string]any{"category": res["category"], "confidence": res["confidence"]}})
			}
			return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
		},
	}

	extract := schema.NodeDefinition{
		Type:        "ai.extract",
		Label:       "AI Extract",
		Group:       "ai",
		Icon:        "ScanText",
		Description: "Extract structured fields from text into a JSON object.",
		Inputs:      []schema.Port{{ID: "main"}},
		Outputs:     []schema.Port{{ID: "main"}},
		Params: []schema.ParamSchema{
			{Name: "text", Label: "Text", Type: "expression", Required: true},
			{Name: "fields", Label: `Fields to extract (JSON: { field: "description" })`, Type: "json",
				Default: map[string]any{"name": "person name", "amount": "dollar amount"}},
		},
		Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
			fields := asStringMap(ctx.Params["fields"])
			properties := map[string]any{}
			for k, desc := range fields {
				properties[k] = map[string]any{"type": "string", "description": desc}
			}
			out := []schema.Item{}
			for range itemsOrEmpty(ctx.Input) {
				res, err := c.Structured(context.Background(), llm.StructuredOpts{
					Model:    c.Models.Fast,
					System:   "Extract the requested fields from the text. Use empty string if not present.",
					Prompt:   asString(ctx.Params["text"], ""),
					ToolName: "extract",
					Schema:   map[string]any{"type": "object", "properties": properties},
				})
				if err != nil {
					return schema.NodeResult{}, err
				}
				out = append(out, schema.Item{JSON: res})
			}
			return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
		},
	}

	return []schema.NodeDefinition{summarize, classify, extract}
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

func asInt(v any, def int) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return int(i)
		}
	case string:
		var n int
		if _, err := fmt.Sscanf(t, "%d", &n); err == nil {
			return n
		}
	}
	return def
}

func asStringSlice(v any, def []string) []string {
	switch t := v.(type) {
	case []string:
		if len(t) > 0 {
			return t
		}
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			out = append(out, fmt.Sprintf("%v", e))
		}
		if len(out) > 0 {
			return out
		}
	}
	return def
}

func asStringMap(v any) map[string]string {
	out := map[string]string{}
	if m, ok := v.(map[string]any); ok {
		for k, val := range m {
			out[k] = fmt.Sprintf("%v", val)
		}
	}
	return out
}
