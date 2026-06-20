package microsoft

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

func oauthCtx(params map[string]any, client *http.Client) *schema.ExecContext {
	return &schema.ExecContext{
		Params:           params,
		RawParam:         func(n string) any { return params[n] },
		AuthorizedClient: func(string) (*http.Client, error) { return client, nil },
		Log:              func(string, any) {},
	}
}

func TestOutlookListMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/me/messages" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"value": []any{map[string]any{"id": "m1"}, map[string]any{"id": "m2"}},
			})
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := Outlook(srv.URL).Build()
	res, err := def.Execute(oauthCtx(map[string]any{"operation": "message:list", "credential": "c1"}, srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	if out := res.Outputs["main"]; len(out) != 2 || out[0].JSON["id"] != "m1" {
		t.Fatalf("messages: %+v", res.Outputs["main"])
	}
}

func TestTeamsSendChannelMessage(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "msg1"})
	}))
	defer srv.Close()

	def := Teams(srv.URL).Build()
	ctx := oauthCtx(map[string]any{
		"operation": "channelMessage:send", "teamId": "T1", "channelId": "C1",
		"body":      map[string]any{"body": map[string]any{"content": "hi"}},
		"credential": "c1",
	}, srv.Client())
	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/teams/T1/channels/C1/messages" {
		t.Fatalf("path: %s", gotPath)
	}
	if gotBody != `{"body":{"content":"hi"}}` {
		t.Fatalf("body: %s", gotBody)
	}
	if res.Outputs["main"][0].JSON["id"] != "msg1" {
		t.Fatalf("output: %+v", res.Outputs["main"])
	}
}

func TestNodesRegisterable(t *testing.T) {
	nodes := Nodes()
	if len(nodes) != 6 {
		t.Fatalf("expected 6 microsoft nodes, got %d", len(nodes))
	}
	for _, n := range nodes {
		if n.Params[0].Type != "credential" || n.Params[1].Name != "operation" {
			t.Fatalf("%s: unexpected params %+v", n.Type, n.Params[:2])
		}
	}
}
