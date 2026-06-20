package core

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

func binItem(prop, content string) schema.Item {
	return schema.Item{
		JSON:   map[string]any{},
		Binary: map[string]schema.BinaryRef{prop: {Data: base64.StdEncoding.EncodeToString([]byte(content)), MimeType: "text/plain"}},
	}
}

func TestExtractFromFileCSV(t *testing.T) {
	res := mustExec(t, extractFromFileNode, ctxFor(
		[]schema.Item{binItem("data", "a,b\n1,2\n3,4\n")},
		map[string]any{"format": "csv", "binaryProperty": "data", "hasHeader": true}))
	out := res.Outputs["main"]
	if len(out) != 2 || out[0].JSON["a"] != "1" || out[1].JSON["b"] != "4" {
		t.Fatalf("extract CSV: %+v", out)
	}
}

func TestExtractFromFileJSON(t *testing.T) {
	res := mustExec(t, extractFromFileNode, ctxFor(
		[]schema.Item{binItem("data", `[{"x":1},{"x":2}]`)},
		map[string]any{"format": "json", "binaryProperty": "data"}))
	if out := res.Outputs["main"]; len(out) != 2 || out[0].JSON["x"] == nil {
		t.Fatalf("extract JSON: %+v", res.Outputs["main"])
	}
}

func TestConvertToFileJSONRoundTrip(t *testing.T) {
	res := mustExec(t, convertToFileNode, ctxFor(
		items(map[string]any{"x": 1}, map[string]any{"x": 2}),
		map[string]any{"format": "json", "binaryProperty": "out", "fileName": "rows.json"}))
	item := res.Outputs["main"][0]
	ref, ok := item.Binary["out"]
	if !ok || ref.FileName != "rows.json" {
		t.Fatalf("convert: missing binary or name: %+v", item)
	}
	raw, _ := base64.StdEncoding.DecodeString(ref.Data)
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err != nil || len(arr) != 2 {
		t.Fatalf("convert JSON content: %s (%v)", raw, err)
	}
}

func TestConvertToFileCSV(t *testing.T) {
	res := mustExec(t, convertToFileNode, ctxFor(
		items(map[string]any{"a": 1, "b": 2}, map[string]any{"a": 3, "b": 4}),
		map[string]any{"format": "csv"}))
	ref := res.Outputs["main"][0].Binary["data"]
	raw, _ := base64.StdEncoding.DecodeString(ref.Data)
	// header is union of keys, sorted: "a,b"
	want := "a,b\n1,2\n3,4\n"
	if string(raw) != want {
		t.Fatalf("convert CSV:\n got %q\nwant %q", string(raw), want)
	}
}
