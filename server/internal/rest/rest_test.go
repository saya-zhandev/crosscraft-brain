package rest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"encoding/json"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

// newCtx builds a minimal ExecContext for tests (no engine needed).
func newCtx(params, cred map[string]any) *schema.ExecContext {
	return &schema.ExecContext{
		Params:     params,
		RawParam:   func(name string) any { return params[name] },
		Credential: func(string) (map[string]any, error) { return cred, nil },
		Log:        func(string, any) {},
	}
}

// TestDeclarativeNode proves a data-defined node turns into a working NodeDefinition:
// path interpolation, header auth, JSON body, and response→items mapping.
func TestDeclarativeNode(t *testing.T) {
	var gotAuth, gotPath, gotMethod, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath, gotMethod = r.Header.Get("Authorization"), r.URL.Path, r.Method
		if r.Body != nil {
			b, _ := io.ReadAll(r.Body)
			gotBody = string(b)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/things/42":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "42", "name": "widget"})
		case r.Method == http.MethodPost && r.URL.Path == "/things":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "99", "created": true})
		default:
			http.Error(w, "nope", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	n := Node{
		Type: "test.things", Label: "Things", BaseURL: srv.URL, CredType: "httpHeaderAuth",
		Auth: Auth{Kind: "header", Header: "Authorization", Prefix: "Bearer ", ValueField: "value"},
		Ops: []Op{
			{Resource: "thing", Name: "get", Label: "Get", Method: "GET", Path: "/things/{id}",
				Params: []schema.ParamSchema{{Name: "id", Label: "ID", Type: "string", Required: true}}},
			{Resource: "thing", Name: "create", Label: "Create", Method: "POST", Path: "/things", BodyParam: "data",
				Params: []schema.ParamSchema{{Name: "data", Label: "Data", Type: "json"}}},
		},
	}
	def := n.Build()

	// the credential + operation params should be generated
	if def.Params[0].Type != "credential" || def.Params[1].Name != "operation" {
		t.Fatalf("generated params: %+v", def.Params)
	}

	// GET /things/42 with Bearer auth
	res, err := def.Execute(newCtx(map[string]any{"operation": "thing:get", "id": "42"}, map[string]any{"value": "secret"}))
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != "GET" || gotPath != "/things/42" {
		t.Fatalf("request: %s %s", gotMethod, gotPath)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("auth header: %q", gotAuth)
	}
	if out := res.Outputs["main"]; len(out) != 1 || out[0].JSON["name"] != "widget" {
		t.Fatalf("get output: %+v", out)
	}

	// POST /things with a JSON body
	res2, err := def.Execute(newCtx(map[string]any{"operation": "thing:create", "data": map[string]any{"name": "new"}}, map[string]any{"value": "secret"}))
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != "POST" || gotPath != "/things" || gotBody != `{"name":"new"}` {
		t.Fatalf("request: %s %s body=%s", gotMethod, gotPath, gotBody)
	}
	if res2.Outputs["main"][0].JSON["created"] != true {
		t.Fatalf("post output: %+v", res2.Outputs["main"])
	}
}
