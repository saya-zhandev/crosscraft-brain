package adobe

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

// TestAcrobatSignListWithBaseOverride checks header (Bearer) auth, the ItemsPath
// mapping, and the per-node baseUrl override (a production base is overridden to
// the test server).
func TestAcrobatSignListWithBaseOverride(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.Method == http.MethodGet && r.URL.Path == "/agreements" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"userAgreementList": []any{map[string]any{"id": "a1"}, map[string]any{"id": "a2"}},
			})
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := AcrobatSign("https://api.na1.adobesign.com/api/rest/v6").Build()
	ctx := &schema.ExecContext{
		Params:     map[string]any{"operation": "agreement:list", "baseUrl": srv.URL, "credential": "c1"},
		RawParam:   func(n string) any { return nil },
		Credential: func(string) (map[string]any, error) { return map[string]any{"accessToken": "KEY123"}, nil },
		Log:        func(string, any) {},
	}
	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer KEY123" {
		t.Fatalf("auth header: %q", gotAuth)
	}
	if out := res.Outputs["main"]; len(out) != 2 || out[0].JSON["id"] != "a1" {
		t.Fatalf("agreements: %+v", res.Outputs["main"])
	}
}

func TestNodesRegisterable(t *testing.T) {
	nodes := Nodes()
	if len(nodes) != 1 {
		t.Fatalf("expected 1 adobe node, got %d", len(nodes))
	}
	if nodes[0].Params[0].Type != "credential" {
		t.Fatalf("first param should be credential: %+v", nodes[0].Params[0])
	}
}
