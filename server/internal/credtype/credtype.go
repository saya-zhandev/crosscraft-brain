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
	// GrantType: "" / "authorization_code" (user redirect flow) or
	// "client_credentials" (server-to-server; no AuthURL, no user step).
	GrantType string `json:"grantType,omitempty"`
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
		Type{
			Name: "adobeSignApi", DisplayName: "Adobe Acrobat Sign (Integration Key)",
			Fields: []Field{
				{Name: "accessToken", Label: "Integration Key / Access Token", Type: "password", Required: true},
			},
		},
		Type{
			Name: "adobeOAuth2Api", DisplayName: "Adobe IMS (Server-to-Server)", Fields: clientFields,
			OAuth2: &OAuth2{TokenURL: "https://ims-na1.adobelogin.com/ims/token/v3", GrantType: "client_credentials"},
		},
		// ── Communication ───────────────────────────────────────────────────────
		Type{Name: "slackApi", DisplayName: "Slack (Bot Token)", Fields: []Field{
			{Name: "accessToken", Label: "Bot Token (xoxb-...)", Type: "password", Required: true},
		}},
		Type{Name: "discordApi", DisplayName: "Discord (Bot Token)", Fields: []Field{
			{Name: "accessToken", Label: "Bot Token", Type: "password", Required: true},
		}},
		Type{Name: "telegramApi", DisplayName: "Telegram Bot API", Fields: []Field{
			{Name: "accessToken", Label: "Bot Token", Type: "password", Required: true},
		}},
		Type{Name: "twilioApi", DisplayName: "Twilio", Fields: []Field{
			{Name: "accountSid", Label: "Account SID", Type: "string", Required: true},
			{Name: "authToken", Label: "Auth Token", Type: "password", Required: true},
		}},
		// ── Productivity / PM ────────────────────────────────────────────────────
		Type{Name: "notionApi", DisplayName: "Notion (Integration Token)", Fields: []Field{
			{Name: "accessToken", Label: "Internal Integration Token (secret_...)", Type: "password", Required: true},
		}},
		Type{Name: "airtableTokenApi", DisplayName: "Airtable (Personal Access Token)", Fields: []Field{
			{Name: "accessToken", Label: "Personal Access Token (pat...)", Type: "password", Required: true},
		}},
		Type{Name: "linearApi", DisplayName: "Linear API Key", Fields: []Field{
			{Name: "accessToken", Label: "API Key", Type: "password", Required: true},
		}},
		Type{Name: "todoistApi", DisplayName: "Todoist API Token", Fields: []Field{
			{Name: "accessToken", Label: "API Token", Type: "password", Required: true},
		}},
		Type{Name: "asanaApi", DisplayName: "Asana (Personal Access Token)", Fields: []Field{
			{Name: "accessToken", Label: "Personal Access Token", Type: "password", Required: true},
		}},
		Type{Name: "clickUpApi", DisplayName: "ClickUp API Token", Fields: []Field{
			{Name: "accessToken", Label: "API Token", Type: "password", Required: true},
		}},
		Type{Name: "jiraCloudApi", DisplayName: "Jira Cloud (Email + API Token)", Fields: []Field{
			{Name: "email", Label: "Account Email", Type: "string", Required: true},
			{Name: "apiToken", Label: "API Token", Type: "password", Required: true},
			{Name: "subdomain", Label: "Subdomain (e.g. mycompany)", Type: "string", Required: true},
		}},
		Type{Name: "trelloApi", DisplayName: "Trello (API Key + Token)", Fields: []Field{
			{Name: "apiKey", Label: "API Key", Type: "string", Required: true},
			{Name: "accessToken", Label: "Token", Type: "password", Required: true},
		}},
		// ── CRM / Marketing ──────────────────────────────────────────────────────
		Type{Name: "hubspotApi", DisplayName: "HubSpot (Private App Token)", Fields: []Field{
			{Name: "accessToken", Label: "Private App Access Token", Type: "password", Required: true},
		}},
		Type{Name: "mailchimpApi", DisplayName: "Mailchimp", Fields: []Field{
			{Name: "accessToken", Label: "API Key", Type: "password", Required: true},
			{Name: "server", Label: "Server Prefix (e.g. us1)", Type: "string", Required: true},
		}},
		Type{Name: "sendgridApi", DisplayName: "SendGrid API Key", Fields: []Field{
			{Name: "accessToken", Label: "API Key (SG....)", Type: "password", Required: true},
		}},
		Type{Name: "pipedriveApi", DisplayName: "Pipedrive API Token", Fields: []Field{
			{Name: "accessToken", Label: "API Token", Type: "password", Required: true},
		}},
		// ── Payments / Commerce ───────────────────────────────────────────────────
		Type{Name: "stripeApi", DisplayName: "Stripe Secret Key", Fields: []Field{
			{Name: "accessToken", Label: "Secret Key (sk_live_... or sk_test_...)", Type: "password", Required: true},
		}},
		// ── Dev / DevOps ──────────────────────────────────────────────────────────
		Type{Name: "githubApi", DisplayName: "GitHub (Personal Access Token)", Fields: []Field{
			{Name: "accessToken", Label: "Personal Access Token", Type: "password", Required: true},
		}},
		Type{Name: "gitlabApi", DisplayName: "GitLab (Personal Access Token)", Fields: []Field{
			{Name: "accessToken", Label: "Personal Access Token", Type: "password", Required: true},
			{Name: "baseUrl", Label: "Base URL (default: gitlab.com)", Type: "string", Placeholder: "https://gitlab.com"},
		}},
		Type{Name: "sentryApi", DisplayName: "Sentry Auth Token", Fields: []Field{
			{Name: "accessToken", Label: "Auth Token", Type: "password", Required: true},
		}},
	)
}
