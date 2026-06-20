// Package credtype declares credential types: what fields a credential needs and,
// for OAuth2 types, the provider endpoints. The canvas renders the form / "Connect"
// button from these descriptors; the oauth service uses the OAuth2 spec to run the
// authorization-code flow. Mirrors n8n's credential types.
package credtype

// Field is one input on a credential form.
type Field struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Type        string `json:"type"` // "string" | "password"
	Required    bool   `json:"required,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
}

// OAuth2 is the provider spec for an OAuth2 credential type. For the generic type
// the endpoints are empty and read from the credential's own data instead.
type OAuth2 struct {
	AuthURL    string            `json:"authUrl"`
	TokenURL   string            `json:"tokenUrl"`
	Scopes     []string          `json:"scopes,omitempty"`
	AuthParams map[string]string `json:"authParams,omitempty"` // extra authorize-URL params
}

// Type describes a credential type.
type Type struct {
	Name        string  `json:"name"`
	DisplayName string  `json:"displayName"`
	Fields      []Field `json:"fields"`
	OAuth2      *OAuth2 `json:"oauth2,omitempty"`
}

// IsOAuth2 reports whether this type uses the OAuth2 flow.
func (t Type) IsOAuth2() bool { return t.OAuth2 != nil }

// Registry is an in-memory set of credential types.
type Registry struct{ m map[string]Type }

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{m: map[string]Type{}} }

// Register adds types (chainable).
func (r *Registry) Register(ts ...Type) *Registry {
	for _, t := range ts {
		r.m[t.Name] = t
	}
	return r
}

// Get returns a type by name.
func (r *Registry) Get(name string) (Type, bool) { t, ok := r.m[name]; return t, ok }

// All returns every registered type (for GET /api/credential-types).
func (r *Registry) All() []Type {
	out := make([]Type, 0, len(r.m))
	for _, t := range r.m {
		out = append(out, t)
	}
	return out
}

var clientFields = []Field{
	{Name: "clientId", Label: "Client ID", Type: "string", Required: true},
	{Name: "clientSecret", Label: "Client Secret", Type: "password", Required: true},
	{Name: "scope", Label: "Scopes (space-separated)", Type: "string"},
}

// Default returns the built-in credential types.
func Default() *Registry {
	return NewRegistry().Register(
		Type{
			Name: "googleOAuth2Api", DisplayName: "Google OAuth2", Fields: clientFields,
			OAuth2: &OAuth2{
				AuthURL:    "https://accounts.google.com/o/oauth2/v2/auth",
				TokenURL:   "https://oauth2.googleapis.com/token",
				AuthParams: map[string]string{"access_type": "offline", "prompt": "consent"},
			},
		},
		Type{
			Name: "microsoftOAuth2Api", DisplayName: "Microsoft OAuth2", Fields: clientFields,
			OAuth2: &OAuth2{
				AuthURL:    "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
				TokenURL:   "https://login.microsoftonline.com/common/oauth2/v2.0/token",
				Scopes:     []string{"offline_access"},
				AuthParams: map[string]string{"prompt": "consent"},
			},
		},
		Type{
			Name: "oAuth2Api", DisplayName: "Generic OAuth2",
			Fields: append([]Field{
				{Name: "authUrl", Label: "Authorization URL", Type: "string", Required: true},
				{Name: "tokenUrl", Label: "Token URL", Type: "string", Required: true},
			}, clientFields...),
			OAuth2: &OAuth2{}, // endpoints come from the credential data
		},
		Type{
			Name: "httpHeaderAuth", DisplayName: "Header Auth (API key)",
			Fields: []Field{
				{Name: "name", Label: "Header Name", Type: "string", Required: true, Placeholder: "Authorization"},
				{Name: "value", Label: "Header Value", Type: "password", Required: true},
			},
		},
	)
}
