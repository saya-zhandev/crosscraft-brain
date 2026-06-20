package google

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

// oauthCtx builds an ExecContext whose AuthorizedClient returns the given client
// (standing in for the OAuth2 client the engine would inject).
func oauthCtx(params map[string]any, client *http.Client, called *bool) *schema.ExecContext {
	return &schema.ExecContext{
		Params:   params,
		RawParam: func(n string) any { return params[n] },
		AuthorizedClient: func(string) (*http.Client, error) {
			*called = true
			return client, nil
		},
		Log: func(string, any) {},
	}
}

func TestSheetsGetValues(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method == http.MethodGet && r.URL.Path == "/spreadsheets/SHEET1/values/Sheet1!A1:B2" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"range": "Sheet1!A1:B2", "values": []any{[]any{"a", "b"}, []any{"c", "d"}},
			})
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := Sheets(srv.URL).Build()
	called := false
	ctx := oauthCtx(map[string]any{
		"operation": "values:get", "spreadsheetId": "SHEET1", "range": "Sheet1!A1:B2", "credential": "c1",
	}, srv.Client(), &called)

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v (path=%s)", err, gotPath)
	}
	if !called {
		t.Fatal("expected the OAuth2 client to be used")
	}
	if out := res.Outputs["main"]; len(out) != 2 {
		t.Fatalf("expected 2 rows, got %d: %+v", len(out), out)
	}
}

func TestCalendarListEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/calendars/primary/events" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []any{map[string]any{"id": "e1"}, map[string]any{"id": "e2"}},
			})
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := Calendar(srv.URL).Build()
	called := false
	ctx := oauthCtx(map[string]any{"operation": "event:list", "calendarId": "primary", "credential": "c1"}, srv.Client(), &called)

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	out := res.Outputs["main"]
	if len(out) != 2 || out[0].JSON["id"] != "e1" {
		t.Fatalf("events: %+v", out)
	}
}

// TestNodesRegisterable ensures the whole pack builds without panicking and each
// node exposes the credential + operation params.
func TestNodesRegisterable(t *testing.T) {
	nodes := Nodes()
	if len(nodes) != 6 {
		t.Fatalf("expected 6 google nodes, got %d", len(nodes))
	}
	for _, n := range nodes {
		if len(n.Params) == 0 || n.Params[0].Type != "credential" {
			t.Fatalf("%s: first param should be credential, got %+v", n.Type, n.Params)
		}
	}
}
