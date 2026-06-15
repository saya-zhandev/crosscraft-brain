package expr

import (
	"encoding/json"
	"fmt"

	"github.com/dop251/goja"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

// RunCode executes the user Code node: wraps the source as a function receiving
// `items` and `$trigger`, runs it in goja, and normalizes the return to []Item.
// Mirrors the core.code node from packages/nodes-core/src/index.ts.
func RunCode(src string, items, trigger []schema.Item) ([]schema.Item, error) {
	vm := goja.New()
	fnVal, err := vm.RunString("(function(items,$trigger){\"use strict\";\n" + src + "\n})")
	if err != nil {
		return nil, err
	}
	fn, ok := goja.AssertFunction(fnVal)
	if !ok {
		return nil, fmt.Errorf("code node: source did not compile to a function")
	}
	res, err := fn(goja.Undefined(), vm.ToValue(itemsToJS(items)), vm.ToValue(itemsToJS(trigger)))
	if err != nil {
		return nil, err
	}
	return jsToItems(res.Export()), nil
}

// jsToItems normalizes a value exported from goja into []Item. An array maps
// element-wise; a single value becomes one item; nil becomes an empty slice.
func jsToItems(v any) []schema.Item {
	var arr []any
	switch t := v.(type) {
	case nil:
		return []schema.Item{}
	case []any:
		arr = t
	default:
		arr = []any{t}
	}
	out := make([]schema.Item, 0, len(arr))
	for _, e := range arr {
		out = append(out, toItem(e))
	}
	return out
}

// toItem mirrors the TS rule: an object with a `json` property is treated as an
// Item; any other object becomes that item's json; a primitive is wrapped.
func toItem(e any) schema.Item {
	if m, ok := e.(map[string]any); ok {
		if j, has := m["json"]; has {
			if jm, ok := j.(map[string]any); ok {
				return schema.Item{JSON: jm}
			}
		}
		return schema.Item{JSON: m}
	}
	return schema.Item{JSON: map[string]any{"value": e}}
}

func jsonMarshal(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
