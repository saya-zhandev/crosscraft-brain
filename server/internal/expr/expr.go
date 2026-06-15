// Package expr is the {{ }} expression evaluator and the Code-node runner.
//
// It is the one place JavaScript still runs in the Go backend: expressions and
// the user Code node are executed in goja (a pure-Go ECMAScript interpreter), so
// the authoring/operator UX from the TypeScript engine is preserved verbatim.
// Everything else in the engine is native Go.
//
// Scope exposed to expressions: $json, $input, $trigger, $node('id'), $now, plus
// goja's built-in JSON and Math. Mirrors packages/engine/src/expression.ts.
package expr

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/dop251/goja"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

// Scope is the data visible to an expression for one item.
type Scope struct {
	JSON    map[string]any
	Input   []schema.Item
	Trigger []schema.Item
	Node    func(id string) []schema.Item
}

var exprRE = regexp.MustCompile(`\{\{([\s\S]+?)\}\}`)
var wholeRE = regexp.MustCompile(`^\s*\{\{([\s\S]+?)\}\}\s*$`)

// itemsToJS converts []Item into a JS-friendly []any of {json, binary} maps so
// expressions can use $input[0].json.field exactly as in the TS engine.
func itemsToJS(items []schema.Item) []any {
	out := make([]any, len(items))
	for i, it := range items {
		m := map[string]any{"json": it.JSON}
		if it.Binary != nil {
			m["binary"] = it.Binary
		}
		out[i] = m
	}
	return out
}

func newVM(sc Scope) (*goja.Runtime, error) {
	vm := goja.New()
	now, err := vm.RunString("new Date()")
	if err != nil {
		return nil, err
	}
	if sc.JSON == nil {
		sc.JSON = map[string]any{} // match TS: $json defaults to {}
	}
	if err := vm.Set("$json", sc.JSON); err != nil {
		return nil, err
	}
	if err := vm.Set("$input", itemsToJS(sc.Input)); err != nil {
		return nil, err
	}
	if err := vm.Set("$trigger", itemsToJS(sc.Trigger)); err != nil {
		return nil, err
	}
	if err := vm.Set("$now", now); err != nil {
		return nil, err
	}
	node := sc.Node
	if node == nil {
		node = func(string) []schema.Item { return nil }
	}
	if err := vm.Set("$node", func(idArg string) []any { return itemsToJS(node(idArg)) }); err != nil {
		return nil, err
	}
	return vm, nil
}

// evalExpr runs a single JS expression against the scope and returns the value.
//
// NOTE: a fresh goja.Runtime per evaluation is used for the spike (simple and
// correct). The scale path (BUILD.md) is a sync.Pool of runtimes + a compiled
// *goja.Program cache keyed by source, plus an interrupt-based timeout.
func evalExpr(code string, sc Scope) (any, error) {
	vm, err := newVM(sc)
	if err != nil {
		return nil, err
	}
	v, err := vm.RunString("(" + code + ")")
	if err != nil {
		return nil, err
	}
	return v.Export(), nil
}

// ResolveValue resolves one param value against the scope. Non-strings and
// strings without {{ pass through untouched. A whole-string expression returns
// the raw typed value; embedded expressions are stringified and interpolated.
func ResolveValue(value any, sc Scope) (any, error) {
	s, ok := value.(string)
	if !ok || !strings.Contains(s, "{{") {
		return value, nil
	}

	if m := wholeRE.FindStringSubmatch(s); m != nil {
		out, err := evalExpr(m[1], sc)
		if err != nil {
			return nil, fmt.Errorf("expression error in %q: %w", s, err)
		}
		return out, nil
	}

	var firstErr error
	res := exprRE.ReplaceAllStringFunc(s, func(match string) string {
		code := exprRE.FindStringSubmatch(match)[1]
		out, err := evalExpr(code, sc)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("expression error in %q: %w", s, err)
			}
			return ""
		}
		return stringify(out)
	})
	if firstErr != nil {
		return nil, firstErr
	}
	return res, nil
}

// ResolveParams resolves every param value against the scope.
func ResolveParams(params map[string]any, sc Scope) (map[string]any, error) {
	out := make(map[string]any, len(params))
	for k, v := range params {
		rv, err := ResolveValue(v, sc)
		if err != nil {
			return nil, err
		}
		out[k] = rv
	}
	return out, nil
}

func stringify(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case map[string]any, []any:
		// Mirror JSON.stringify for object/array interpolation.
		b, err := jsonMarshal(t)
		if err != nil {
			return fmt.Sprintf("%v", t)
		}
		return b
	default:
		return fmt.Sprintf("%v", t)
	}
}
