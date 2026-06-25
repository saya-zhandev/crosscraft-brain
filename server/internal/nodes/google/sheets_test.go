package google

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

func TestSheetsToObjectsMapsHeaderRow(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		// The SDK constructs URLs with a /v4/ version prefix baked into the
		// method templates; the base URL is just scheme+host.
		if r.Method == http.MethodGet && r.URL.Path == "/v4/spreadsheets/SHEET1/values/Sheet1!A1:C3" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"range": "Sheet1!A1:C3",
				"values": []any{
					[]any{"name", "age"},
					[]any{"Ada", "36"},
					[]any{"Grace", "45"},
				},
			})
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := SheetsNode(srv.URL)
	called := false
	ctx := oauthCtx(map[string]any{
		"operation":     "values:toObjects",
		"spreadsheetId": "SHEET1",
		"range":         "Sheet1!A1:C3",
		"credential":    "c1",
	}, srv.Client(), &called)

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v (path=%s)", err, gotPath)
	}
	if !called {
		t.Fatal("expected the OAuth2 client to be used")
	}
	if len(res.Outputs["main"]) != 2 {
		t.Fatalf("expected 2 mapped rows, got %d", len(res.Outputs["main"]))
	}
	if got := res.Outputs["main"][0].JSON["name"]; got != "Ada" {
		t.Fatalf("expected first row name to be Ada, got %#v", got)
	}
}

func TestSheetsDeleteRowsUsesBatchUpdate(t *testing.T) {
	var gotMethod, gotPath string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if r.Method == http.MethodGet && r.URL.Path == "/v4/spreadsheets/SHEET1" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"sheets": []any{map[string]any{"properties": map[string]any{"sheetId": 7, "title": "Sheet1"}}}})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/v4/spreadsheets/SHEET1:batchUpdate" {
			_ = json.NewDecoder(r.Body).Decode(&body)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"replies": []any{map[string]any{"deleteDimension": map[string]any{"status": map[string]any{"code": 200}}}}})
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := SheetsNode(srv.URL)
	called := false
	ctx := oauthCtx(map[string]any{
		"operation":     "values:deleteRows",
		"spreadsheetId": "SHEET1",
		"range":         "Sheet1!A2:A4",
		"count":         float64(2),
		"credential":    "c1",
	}, srv.Client(), &called)

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/v4/spreadsheets/SHEET1:batchUpdate" {
		t.Fatalf("unexpected batch update call: %s %s", gotMethod, gotPath)
	}
	if body == nil || len(body["requests"].([]any)) == 0 {
		t.Fatalf("expected a batch update request payload, got %#v", body)
	}
	if res.Outputs["main"][0].JSON["deleted"] != true {
		t.Fatalf("expected delete result to be reported: %#v", res.Outputs["main"][0].JSON)
	}
}

func TestSheetsRowAddedTriggerUsesCursor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v4/spreadsheets/SHEET1/values/Sheet1!A1:A5" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"range":  "Sheet1!A1:A5",
				"values": []any{[]any{"one"}, []any{"two"}, []any{"three"}},
			})
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := SheetsNode(srv.URL)
	called := false
	ctx := oauthCtx(map[string]any{
		"operation":     "trigger:rowAdded",
		"spreadsheetId": "SHEET1",
		"range":         "Sheet1!A1:A5",
		"credential":    "c1",
	}, srv.Client(), &called)
	ctx.State = map[string]any{}

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	// 3 rows in the mock; first poll emits all of them and advances cursor to 3.
	if len(res.Outputs["main"]) != 3 {
		t.Fatalf("expected 3 rows on first poll, got %d", len(res.Outputs["main"]))
	}
	if got := ctx.State["sheets:cursor:SHEET1:Sheet1!A1:A5"]; got != int64(3) {
		t.Fatalf("expected cursor to be advanced to 3, got %#v", got)
	}
}

func TestSheetsRowUpdatedTriggerDetectsChanges(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v4/spreadsheets/SHEET1/values/Sheet1!A1:A3" {
			w.Header().Set("Content-Type", "application/json")
			callCount++
			if callCount == 1 {
				// First poll: two rows.
				_ = json.NewEncoder(w).Encode(map[string]any{
					"range":  "Sheet1!A1:A3",
					"values": []any{[]any{"alpha"}, []any{"beta"}},
				})
			} else {
				// Second poll: first row changed, second same, third added.
				_ = json.NewEncoder(w).Encode(map[string]any{
					"range":  "Sheet1!A1:A3",
					"values": []any{[]any{"alpha-v2"}, []any{"beta"}, []any{"gamma"}},
				})
			}
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := SheetsNode(srv.URL)
	called := false
	ctx := oauthCtx(map[string]any{
		"operation":     "trigger:rowUpdated",
		"spreadsheetId": "SHEET1",
		"range":         "Sheet1!A1:A3",
		"credential":    "c1",
	}, srv.Client(), &called)
	ctx.State = map[string]any{}

	// Poll 1 — all rows are "new" (no stored hashes yet).
	res1, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("poll 1: %v", err)
	}
	if len(res1.Outputs["main"]) != 2 {
		t.Fatalf("poll 1: expected 2 new rows, got %d", len(res1.Outputs["main"]))
	}

	// Reset the rate-limit key so second poll executes immediately.
	delete(ctx.State, "sheets:lastPoll:trigger:rowUpdated:SHEET1:Sheet1!A1:A3")

	// Poll 2 — col 1 changed (alpha→alpha-v2) + one new row (gamma).
	res2, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("poll 2: %v", err)
	}
	if len(res2.Outputs["main"]) != 2 {
		t.Fatalf("poll 2: expected 2 rows (1 changed + 1 new), got %d", len(res2.Outputs["main"]))
	}
}

func TestSheetsNodeIncludesNewOperations(t *testing.T) {
	def := SheetsNode("https://example.test/")
	ops := []string{}
	for _, p := range def.Params {
		if p.Name == "operation" {
			for _, opt := range p.Options {
				ops = append(ops, opt.Value)
			}
		}
	}
	for _, want := range []string{"values:toObjects", "values:deleteRows", "values:deleteColumns", "spreadsheet:delete", "trigger:rowAdded", "trigger:rowUpdated"} {
		found := false
		for _, got := range ops {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected operation %q to be registered", want)
		}
	}
}

// TestSheetsSpreadsheetCreateRejectsMissingTitle verifies the panic fix:
// spreadsheet:create without a title returns an error instead of crashing.
func TestSheetsSpreadsheetCreateRejectsMissingTitle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer srv.Close()

	def := SheetsNode(srv.URL)
	called := false

	// No "body" param at all — asObject returns empty map, title should be "".
	ctx := oauthCtx(map[string]any{
		"operation":  "spreadsheet:create",
		"credential": "c1",
	}, srv.Client(), &called)

	_, err := def.Execute(ctx)
	if err == nil {
		t.Fatal("expected an error for missing body.title, got nil")
	}
	if err.Error() == "" || !contains(err.Error(), "title") {
		t.Fatalf("expected error about missing title, got: %v", err)
	}
}

// TestSheetsSpreadsheetCreateRequiresSpreadsheetId ensures operations that need
// spreadsheetId fail with a clear message when it is missing.
func TestSheetsSpreadsheetGetRequiresSpreadsheetId(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer srv.Close()

	def := SheetsNode(srv.URL)
	called := false
	ctx := oauthCtx(map[string]any{
		"operation":  "spreadsheet:get",
		"credential": "c1",
		// spreadsheetId intentionally omitted
	}, srv.Client(), &called)

	_, err := def.Execute(ctx)
	if err == nil {
		t.Fatal("expected an error for missing spreadsheetId, got nil")
	}
}

// TestSheetsDeleteColumnsUsesParsedRange verifies the A1-range parsing fix:
// "Sheet1!B2:C5" → delete columns starting at B (0-based index 1).
func TestSheetsDeleteColumnsUsesParsedRange(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v4/spreadsheets/SHEET1" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"sheets": []any{map[string]any{"properties": map[string]any{"sheetId": 7, "title": "Sheet1"}}}})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/v4/spreadsheets/SHEET1:batchUpdate" {
			_ = json.NewDecoder(r.Body).Decode(&body)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"replies": []any{map[string]any{"deleteDimension": map[string]any{"status": map[string]any{"code": 200}}}}})
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := SheetsNode(srv.URL)
	called := false
	ctx := oauthCtx(map[string]any{
		"operation":     "values:deleteColumns",
		"spreadsheetId": "SHEET1",
		"range":         "Sheet1!B2:C5",
		"count":         float64(3),
		"credential":    "c1",
	}, srv.Client(), &called)

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Outputs["main"][0].JSON["deleted"] != true {
		t.Fatalf("expected deleted=true, got %#v", res.Outputs["main"][0].JSON)
	}
	// Verify the parsed start index: column B = 1 (0-based).
	reqs := body["requests"].([]any)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 batch update request, got %d", len(reqs))
	}
	req := reqs[0].(map[string]any)
	dd := req["deleteDimension"].(map[string]any)
	rng := dd["range"].(map[string]any)
	if dim, _ := rng["dimension"].(string); dim != "COLUMNS" {
		t.Fatalf("expected dimension COLUMNS, got %q", dim)
	}
	start := rng["startIndex"].(float64)
	if start != 1 { // column B → 0-based index 1
		t.Fatalf("expected startIndex=1 for column B, got %v", start)
	}
	end := rng["endIndex"].(float64)
	if end != 4 { // startIndex(1) + count(3) = 4
		t.Fatalf("expected endIndex=4, got %v", end)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

var _ = schema.ExecContext{}
