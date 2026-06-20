// Package oauth runs the OAuth2 authorization-code flow for credentials and hands
// out auto-refreshing *http.Client s for nodes. Refreshed tokens are persisted back
// to the credential store so a long-lived workflow never goes stale.
package oauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/credtype"
)

// CredStore is the subset of the persistence layer oauth needs (satisfied by
// *store.Store). Defined here so oauth doesn't import the store package.
type CredStore interface {
	GetCredentialFull(ctx context.Context, id string) (ctype string, name string, data map[string]any, err error)
	UpdateCredentialData(ctx context.Context, id string, data map[string]any) error
}

// Service runs the OAuth2 flow and builds authenticated clients.
type Service struct {
	store   CredStore
	types   *credtype.Registry
	baseURL string
	mu      sync.Mutex
	states  map[string]stateEntry
}

type stateEntry struct {
	credID  string
	expires time.Time
}

// New constructs the service. baseURL is the public base (for the redirect URI).
func New(store CredStore, types *credtype.Registry, baseURL string) *Service {
	return &Service{
		store:   store,
		types:   types,
		baseURL: strings.TrimRight(baseURL, "/"),
		states:  map[string]stateEntry{},
	}
}

func (s *Service) config(ctype string, data map[string]any) (*oauth2.Config, *credtype.OAuth2, error) {
	t, ok := s.types.Get(ctype)
	if !ok || t.OAuth2 == nil {
		return nil, nil, fmt.Errorf("credential type %q is not OAuth2", ctype)
	}
	authURL := str(data["authUrl"], t.OAuth2.AuthURL)
	tokenURL := str(data["tokenUrl"], t.OAuth2.TokenURL)
	if authURL == "" || tokenURL == "" {
		return nil, nil, fmt.Errorf("OAuth2 endpoints not configured for %q", ctype)
	}
	scopes := t.OAuth2.Scopes
	if sc := str(data["scope"], ""); sc != "" {
		scopes = strings.Fields(sc)
	}
	cfg := &oauth2.Config{
		ClientID:     str(data["clientId"], ""),
		ClientSecret: str(data["clientSecret"], ""),
		Endpoint:     oauth2.Endpoint{AuthURL: authURL, TokenURL: tokenURL},
		RedirectURL:  s.baseURL + "/api/oauth2/callback",
		Scopes:       scopes,
	}
	return cfg, t.OAuth2, nil
}

// AuthURL builds the provider authorization URL for a credential and remembers the
// state for the callback.
func (s *Service) AuthURL(ctx context.Context, credID string) (string, error) {
	ctype, _, data, err := s.store.GetCredentialFull(ctx, credID)
	if err != nil {
		return "", err
	}
	cfg, spec, err := s.config(ctype, data)
	if err != nil {
		return "", err
	}
	if cfg.ClientID == "" {
		return "", fmt.Errorf("credential is missing clientId")
	}
	state := randHex(16)
	s.putState(state, credID)
	opts := []oauth2.AuthCodeOption{}
	for k, v := range spec.AuthParams {
		opts = append(opts, oauth2.SetAuthURLParam(k, v))
	}
	return cfg.AuthCodeURL(state, opts...), nil
}

// Exchange completes the flow: validates state, swaps the code for tokens, and
// stores them on the credential.
func (s *Service) Exchange(ctx context.Context, state, code string) error {
	credID, ok := s.takeState(state)
	if !ok {
		return fmt.Errorf("invalid or expired state")
	}
	if code == "" {
		return fmt.Errorf("missing authorization code")
	}
	ctype, _, data, err := s.store.GetCredentialFull(ctx, credID)
	if err != nil {
		return err
	}
	cfg, _, err := s.config(ctype, data)
	if err != nil {
		return err
	}
	tok, err := cfg.Exchange(ctx, code)
	if err != nil {
		return err
	}
	return s.persist(ctx, credID, tok)
}

// ClientForCredential returns an *http.Client that injects (and refreshes) the
// credential's OAuth2 token; refreshed tokens are persisted. Implements
// engine.ClientProvider.
func (s *Service) ClientForCredential(ctx context.Context, credID string) (*http.Client, error) {
	ctype, _, data, err := s.store.GetCredentialFull(ctx, credID)
	if err != nil {
		return nil, err
	}
	t, ok := s.types.Get(ctype)
	if !ok || t.OAuth2 == nil {
		return nil, fmt.Errorf("credential type %q is not OAuth2", ctype)
	}
	// Server-to-server: fetch + auto-refresh tokens with no user/redirect step.
	if t.OAuth2.GrantType == "client_credentials" {
		ccfg := &clientcredentials.Config{
			ClientID:     str(data["clientId"], ""),
			ClientSecret: str(data["clientSecret"], ""),
			TokenURL:     str(data["tokenUrl"], t.OAuth2.TokenURL),
			Scopes:       scopesOf(data, t.OAuth2.Scopes),
			AuthStyle:    oauth2.AuthStyleInParams,
		}
		return ccfg.Client(ctx), nil
	}
	cfg, _, err := s.config(ctype, data)
	if err != nil {
		return nil, err
	}
	tok := tokenFromData(data)
	if tok.AccessToken == "" && tok.RefreshToken == "" {
		return nil, fmt.Errorf("credential %s is not connected (run the OAuth2 flow)", credID)
	}
	src := &persistSource{base: cfg.TokenSource(ctx, tok), svc: s, credID: credID, last: tok.AccessToken}
	return oauth2.NewClient(ctx, src), nil
}

func (s *Service) persist(ctx context.Context, credID string, tok *oauth2.Token) error {
	_, _, data, err := s.store.GetCredentialFull(ctx, credID)
	if err != nil {
		return err
	}
	if data == nil {
		data = map[string]any{}
	}
	data["access_token"] = tok.AccessToken
	if tok.RefreshToken != "" {
		data["refresh_token"] = tok.RefreshToken
	}
	if tok.TokenType != "" {
		data["token_type"] = tok.TokenType
	}
	if !tok.Expiry.IsZero() {
		data["expiry"] = tok.Expiry.UTC().Format(time.RFC3339)
	}
	return s.store.UpdateCredentialData(ctx, credID, data)
}

// ---- state map -------------------------------------------------------------

func (s *Service) putState(state, credID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, v := range s.states {
		if now.After(v.expires) {
			delete(s.states, k)
		}
	}
	s.states[state] = stateEntry{credID: credID, expires: now.Add(10 * time.Minute)}
}

func (s *Service) takeState(state string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.states[state]
	if !ok || time.Now().After(e.expires) {
		delete(s.states, state)
		return "", false
	}
	delete(s.states, state)
	return e.credID, true
}

// ---- persisting token source ----------------------------------------------

type persistSource struct {
	base   oauth2.TokenSource
	svc    *Service
	credID string
	last   string
}

func (p *persistSource) Token() (*oauth2.Token, error) {
	tok, err := p.base.Token()
	if err != nil {
		return nil, err
	}
	if tok.AccessToken != p.last {
		p.last = tok.AccessToken
		_ = p.svc.persist(context.Background(), p.credID, tok)
	}
	return tok, nil
}

// ---- helpers ---------------------------------------------------------------

func tokenFromData(data map[string]any) *oauth2.Token {
	tok := &oauth2.Token{
		AccessToken:  str(data["access_token"], ""),
		RefreshToken: str(data["refresh_token"], ""),
		TokenType:    str(data["token_type"], ""),
	}
	if exp := str(data["expiry"], ""); exp != "" {
		if t, err := time.Parse(time.RFC3339, exp); err == nil {
			tok.Expiry = t
		}
	}
	return tok
}

func str(v any, def string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}

func scopesOf(data map[string]any, def []string) []string {
	if sc := str(data["scope"], ""); sc != "" {
		return strings.Fields(sc)
	}
	return def
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
