// Package llm is the provider abstraction for AI nodes + copilot. It talks the
// Anthropic Messages API directly over HTTP, so any Anthropic-Messages-compatible
// endpoint works via env (e.g. DeepSeek's /anthropic). Port of
// packages/nodes-ai/src/llm.ts.
//
//	AI_BASE_URL   override base URL (else ANTHROPIC_BASE_URL, else Anthropic default)
//	AI_API_KEY    override key       (else ANTHROPIC_API_KEY)
//	AI_MODEL_FAST / AI_MODEL_SMART   override the model ids
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	anthropicVersion = "2023-06-01"
	defaultBaseURL   = "https://api.anthropic.com"
)

// Models holds the configured model ids.
type Models struct {
	Fast  string // cheap in-node AI
	Smart string // copilot / heavier reasoning
}

// Client is an Anthropic Messages HTTP client.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
	Models  Models
}

// New builds a client from env (key may be empty → AI features disabled).
func New() *Client {
	base := firstNonEmpty(os.Getenv("AI_BASE_URL"), os.Getenv("ANTHROPIC_BASE_URL"), defaultBaseURL)
	return &Client{
		baseURL: strings.TrimRight(base, "/"),
		apiKey:  firstNonEmpty(os.Getenv("AI_API_KEY"), os.Getenv("ANTHROPIC_API_KEY")),
		http:    &http.Client{Timeout: 60 * time.Second},
		Models: Models{
			Fast:  firstNonEmpty(os.Getenv("AI_MODEL_FAST"), "claude-haiku-4-5"),
			Smart: firstNonEmpty(os.Getenv("AI_MODEL_SMART"), "claude-sonnet-4-6"),
		},
	}
}

// Enabled reports whether an API key is configured.
func (c *Client) Enabled() bool { return c.apiKey != "" }

type message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type toolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

type messagesRequest struct {
	Model      string      `json:"model"`
	MaxTokens  int         `json:"max_tokens"`
	System     string      `json:"system,omitempty"`
	Messages   []message   `json:"messages"`
	Tools      []tool      `json:"tools,omitempty"`
	ToolChoice *toolChoice `json:"tool_choice,omitempty"`
}

type contentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type messagesResponse struct {
	Content []contentBlock `json:"content"`
	Error   *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *Client) do(ctx context.Context, body messagesRequest) (*messagesResponse, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("no AI key set (AI_API_KEY or ANTHROPIC_API_KEY) — AI features are disabled")
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("AI request failed (%d): %s", res.StatusCode, string(data))
	}
	var mr messagesResponse
	if err := json.Unmarshal(data, &mr); err != nil {
		return nil, err
	}
	return &mr, nil
}

// CompleteOpts configures a plain-text completion.
type CompleteOpts struct {
	System    string
	Prompt    string
	Model     string
	MaxTokens int
}

// Complete returns the concatenated text of a completion.
func (c *Client) Complete(ctx context.Context, opts CompleteOpts) (string, error) {
	model := firstNonEmpty(opts.Model, c.Models.Fast)
	maxTokens := opts.MaxTokens
	if maxTokens == 0 {
		maxTokens = 1024
	}
	mr, err := c.do(ctx, messagesRequest{
		Model: model, MaxTokens: maxTokens, System: opts.System,
		Messages: []message{{Role: "user", Content: opts.Prompt}},
	})
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, b := range mr.Content {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return sb.String(), nil
}

// StructuredOpts configures a forced-tool-call structured output.
type StructuredOpts struct {
	System    string
	Prompt    string
	ToolName  string
	Schema    map[string]any
	Model     string
	MaxTokens int
}

// Structured forces a single tool call and returns its input object.
func (c *Client) Structured(ctx context.Context, opts StructuredOpts) (map[string]any, error) {
	model := firstNonEmpty(opts.Model, c.Models.Smart)
	maxTokens := opts.MaxTokens
	if maxTokens == 0 {
		maxTokens = 2048
	}
	mr, err := c.do(ctx, messagesRequest{
		Model: model, MaxTokens: maxTokens, System: opts.System,
		Tools:      []tool{{Name: opts.ToolName, Description: "Return the result in this structure.", InputSchema: opts.Schema}},
		ToolChoice: &toolChoice{Type: "tool", Name: opts.ToolName},
		Messages:   []message{{Role: "user", Content: opts.Prompt}},
	})
	if err != nil {
		return nil, err
	}
	for _, b := range mr.Content {
		if b.Type == "tool_use" {
			var m map[string]any
			if err := json.Unmarshal(b.Input, &m); err != nil {
				return nil, err
			}
			return m, nil
		}
	}
	return nil, fmt.Errorf("model did not return a tool call")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
