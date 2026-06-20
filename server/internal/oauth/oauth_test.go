package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/credtype"
)

// fakeStore is an in-memory CredStore for tests.
type fakeStore struct {
	mu    sync.Mutex
	ctype string
	data  map[string]any
}

func (f *fakeStore) GetCredentialFull(_ context.Context, _ string) (string, string, map[string]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := map[string]any{}
	for k, v := range f.data {
		cp[k] = v
	}
	return f.ctype, "cred", cp, nil
}

func (f *fakeStore) UpdateCredentialData(_ context.Context, _ string, data map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data = data
	return nil
}

// TestAuthURLAndExchange drives the flow against a fake token server: building the
// authorize URL, exchanging a code (state is single-use), and storing tokens.
func TestAuthURLAndExchange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"at-123","refresh_token":"rt-456","token_type":"Bearer","expires_in":3600}`))
			return
		}
		http.Error(w, "no", http.StatusNotFound)
	}))
	defer srv.Close()

	types := credtype.NewRegistry().Register(credtype.Type{
		Name: "testOAuth2", DisplayName: "Test",
		OAuth2: &credtype.OAuth2{
			AuthURL: srv.URL + "/auth", TokenURL: srv.URL + "/token",
			Scopes: []string{"a", "b"}, AuthParams: map[string]string{"prompt": "consent"},
		},
	})
	store := &fakeStore{ctype: "testOAuth2", data: map[string]any{"clientId": "cid", "clientSecret": "csec"}}
	svc := New(store, types, "http://localhost:8080")

	au, err := svc.AuthURL(context.Background(), "c1")
	if err != nil {
		t.Fatal(err)
	}
	pu, _ := url.Parse(au)
	qs := pu.Query()
	if qs.Get("client_id") != "cid" {
		t.Fatalf("client_id: %s", qs.Get("client_id"))
	}
	if qs.Get("redirect_uri") != "http://localhost:8080/api/oauth2/callback" {
		t.Fatalf("redirect_uri: %s", qs.Get("redirect_uri"))
	}
	if qs.Get("prompt") != "consent" {
		t.Fatalf("missing auth param prompt")
	}
	if !strings.Contains(qs.Get("scope"), "a") {
		t.Fatalf("scope: %s", qs.Get("scope"))
	}
	state := qs.Get("state")
	if state == "" {
		t.Fatal("missing state")
	}

	if err := svc.Exchange(context.Background(), state, "thecode"); err != nil {
		t.Fatal(err)
	}
	if store.data["access_token"] != "at-123" || store.data["refresh_token"] != "rt-456" {
		t.Fatalf("tokens not stored: %+v", store.data)
	}
	// state is single-use
	if err := svc.Exchange(context.Background(), state, "thecode"); err == nil {
		t.Fatal("expected reused state to be rejected")
	}

	// a connected credential yields a client
	if _, err := svc.ClientForCredential(context.Background(), "c1"); err != nil {
		t.Fatalf("client: %v", err)
	}
}
