package core

import (
	"fmt"
	"testing"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

func ctxFor(input []schema.Item, params map[string]any) *schema.ExecContext {
	return &schema.ExecContext{
		Input:    input,
		Params:   params,
		RawParam: func(n string) any { return params[n] },
		Log:      func(string, any) {},
	}
}

func items(ms ...map[string]any) []schema.Item {
	out := make([]schema.Item, len(ms))
	for i, m := range ms {
		out[i] = schema.Item{JSON: m}
	}
	return out
}

func mustExec(t *testing.T, def schema.NodeDefinition, ctx *schema.ExecContext) schema.NodeResult {
	t.Helper()
	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("%s: %v", def.Type, err)
	}
	return res
}

func TestFilter(t *testing.T) {
	res := mustExec(t, filterNode, ctxFor(
		items(map[string]any{"active": true}, map[string]any{"active": false}),
		map[string]any{"condition": "{{ $json.active }}"}))
	out := res.Outputs["main"]
	if len(out) != 1 || out[0].JSON["active"] != true {
		t.Fatalf("filter: %+v", out)
	}
}

func TestSwitch(t *testing.T) {
	rules := []any{
		map[string]any{"condition": "{{ $json.n == 1 }}", "output": 0},
		map[string]any{"condition": "{{ $json.n == 2 }}", "output": 1},
	}
	res := mustExec(t, switchNode, ctxFor(
		items(map[string]any{"n": 1}, map[string]any{"n": 2}, map[string]any{"n": 3}),
		map[string]any{"rules": rules}))
	if len(res.Outputs["0"]) != 1 || len(res.Outputs["1"]) != 1 || len(res.Outputs["default"]) != 1 {
		t.Fatalf("switch routing: 0=%d 1=%d default=%d",
			len(res.Outputs["0"]), len(res.Outputs["1"]), len(res.Outputs["default"]))
	}
}

func TestSplitOut(t *testing.T) {
	res := mustExec(t, splitOutNode, ctxFor(
		items(map[string]any{"items": []any{map[string]any{"a": 1}, map[string]any{"a": 2}}}),
		map[string]any{"field": "items"}))
	out := res.Outputs["main"]
	if len(out) != 2 || fmt.Sprint(out[0].JSON["a"]) != "1" {
		t.Fatalf("splitOut: %+v", out)
	}
}

func TestAggregateFieldToList(t *testing.T) {
	res := mustExec(t, aggregateNode, ctxFor(
		items(map[string]any{"a": 1}, map[string]any{"a": 2}),
		map[string]any{"mode": "fieldToList", "field": "a", "destination": "vals"}))
	out := res.Outputs["main"]
	vals, _ := out[0].JSON["vals"].([]any)
	if len(out) != 1 || len(vals) != 2 || fmt.Sprint(vals[0]) != "1" {
		t.Fatalf("aggregate: %+v", out)
	}
}

func TestLimit(t *testing.T) {
	res := mustExec(t, limitNode, ctxFor(
		items(map[string]any{"i": 1}, map[string]any{"i": 2}, map[string]any{"i": 3}),
		map[string]any{"maxItems": 2, "keep": "first"}))
	if out := res.Outputs["main"]; len(out) != 2 || out[0].JSON["i"] != 1 {
		t.Fatalf("limit: %+v", res.Outputs["main"])
	}
}

func TestSort(t *testing.T) {
	res := mustExec(t, sortNode, ctxFor(
		items(map[string]any{"v": 3}, map[string]any{"v": 1}, map[string]any{"v": 2}),
		map[string]any{"field": "v", "order": "asc"}))
	out := res.Outputs["main"]
	if fmt.Sprint(out[0].JSON["v"], out[1].JSON["v"], out[2].JSON["v"]) != "1 2 3" {
		t.Fatalf("sort: %+v", out)
	}
}

func TestRemoveDuplicates(t *testing.T) {
	res := mustExec(t, removeDuplicatesNode, ctxFor(
		items(map[string]any{"id": 1}, map[string]any{"id": 1}, map[string]any{"id": 2}),
		map[string]any{"fields": "id"}))
	if out := res.Outputs["main"]; len(out) != 2 {
		t.Fatalf("removeDuplicates: %+v", out)
	}
}

func TestRenameKeys(t *testing.T) {
	res := mustExec(t, renameKeysNode, ctxFor(
		items(map[string]any{"a": 1}),
		map[string]any{"renames": map[string]any{"a": "b"}}))
	m := res.Outputs["main"][0].JSON
	if m["b"] != 1 || m["a"] != nil {
		t.Fatalf("renameKeys: %+v", m)
	}
}

func TestCryptoSHA256(t *testing.T) {
	res := mustExec(t, cryptoNode, ctxFor(
		items(map[string]any{}),
		map[string]any{"action": "hash", "algorithm": "sha256", "value": "abc", "destination": "h"}))
	got := res.Outputs["main"][0].JSON["h"]
	want := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got != want {
		t.Fatalf("sha256(abc) = %v", got)
	}
}

func TestStopAndError(t *testing.T) {
	_, err := stopAndErrorNode.Execute(ctxFor(items(map[string]any{}), map[string]any{"message": "boom"}))
	if err == nil || err.Error() != "boom" {
		t.Fatalf("expected error 'boom', got %v", err)
	}
}

func TestDateTimeAdd(t *testing.T) {
	res := mustExec(t, dateTimeNode, ctxFor(
		items(map[string]any{}),
		map[string]any{"action": "add", "value": 0, "amount": 1, "unit": "hours", "destination": "dt"}))
	dt, _ := res.Outputs["main"][0].JSON["dt"].(map[string]any)
	if dt["iso"] != "1970-01-01T01:00:00Z" {
		t.Fatalf("dateTime add: %+v", dt)
	}
}
