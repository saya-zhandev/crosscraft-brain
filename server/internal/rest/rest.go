// Package rest is the declarative node framework: define a REST integration as data
// (resources → operations) and Build() a schema.NodeDefinition. Authentication is
// attached from a credential — either an OAuth2 client (via ctx.AuthorizedClient) or
// an API-key header. This is the force-multiplier that lets one small description
// stand up a full integration node, à la n8n's declarative nodes.
package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

// Auth describes how to authenticate requests.
type Auth struct {
	Kind       string // "oauth2" | "header" | "none"
	Header     string // header name (kind "header")
	Prefix     string // value prefix, e.g. "Bearer "
	ValueField string // credential data field holding the secret (kind "header")
}

// Op is one operation (resource + verb).
type Op struct {
	Resource  string
	Name      string
	Label     string
	Method    string
	Path      string            // may contain {param} placeholders
	Query     map[string]string // query key -> param name, or "=literal"
	BodyParam string            // param whose JSON value becomes the request body
	ItemsPath string            // dot-path to an array in the response; "" = whole body
	Params    []schema.ParamSchema
}

func (o Op) key() string { return o.Resource + ":" + o.Name }

// Node is a declarative REST node.
type Node struct {
	Type        string
	Label       string
	Group       string
	Icon        string
	Description string
	BaseURL     string
	CredType    string // credentialType for the credential param
	Auth        Auth
	Ops         []Op
}

const credParam = "credential"

var httpClient = &http.Client{Timeout: 60 * time.Second}

// Build compiles the declarative node into a schema.NodeDefinition.
func (n Node) Build() schema.NodeDefinition {
	params := []schema.ParamSchema{}
	if n.Auth.Kind != "" && n.Auth.Kind != "none" {
		params = append(params, schema.ParamSchema{
			Name: credParam, Label: "Credential", Type: "credential",
			Required: true, CredentialType: n.CredType,
		})
	}

	opts := make([]schema.ParamOption, 0, len(n.Ops))
	byKey := map[string]Op{}
	for _, o := range n.Ops {
		byKey[o.key()] = o
		label := o.Label
		if label == "" {
			label = o.Name
		}
		opts = append(opts, schema.ParamOption{Label: o.Resource + ": " + label, Value: o.key()})
	}
	params = append(params, schema.ParamSchema{
		Name: "operation", Label: "Operation", Type: "select", Required: true, Options: opts,
	})

	// Per-op params, shown only when that operation is selected.
	for _, o := range n.Ops {
		for _, p := range o.Params {
			p.ShowWhen = &schema.ShowWhen{Param: "operation", Equals: []any{o.key()}}
			params = append(params, p)
		}
	}

	node := n
	return schema.NodeDefinition{
		Type:        n.Type,
		Label:       n.Label,
		Group:       groupOr(n.Group, "integration"),
		Icon:        n.Icon,
		Description: n.Description,
		Inputs:      []schema.Port{{ID: "main"}},
		Outputs:     []schema.Port{{ID: "main"}},
		Params:      params,
		Credentials: credList(n),
		Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
			return node.execute(ctx, byKey)
		},
	}
}

func (n Node) execute(ctx *schema.ExecContext, byKey map[string]Op) (schema.NodeResult, error) {
	opKey, _ := ctx.Params["operation"].(string)
	op, ok := byKey[opKey]
	if !ok {
		return schema.NodeResult{}, fmt.Errorf("unknown operation %q", opKey)
	}

	path, err := interpolate(op.Path, ctx.Params)
	if err != nil {
		return schema.NodeResult{}, err
	}
	u := strings.TrimRight(n.BaseURL, "/") + path

	q := url.Values{}
	for key, pn := range op.Query {
		if strings.HasPrefix(pn, "=") {
			q.Set(key, pn[1:])
			continue
		}
		if v := ctx.Params[pn]; v != nil && fmt.Sprint(v) != "" {
			q.Set(key, fmt.Sprint(v))
		}
	}
	if len(q) > 0 {
		u += "?" + q.Encode()
	}

	var body io.Reader
	hasBody := false
	if op.BodyParam != "" {
		b, _ := json.Marshal(asObject(ctx.RawParam(op.BodyParam)))
		body = bytes.NewReader(b)
		hasBody = true
	}

	method := strings.ToUpper(op.Method)
	if method == "" {
		method = http.MethodGet
	}
	if ctx.Log != nil {
		ctx.Log(method+" "+u, nil)
	}

	req, err := http.NewRequestWithContext(context.Background(), method, u, body)
	if err != nil {
		return schema.NodeResult{}, err
	}
	if hasBody {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	client := httpClient
	switch n.Auth.Kind {
	case "oauth2":
		c, err := ctx.AuthorizedClient(credParam)
		if err != nil {
			return schema.NodeResult{}, err
		}
		client = c
	case "header":
		cred, err := ctx.Credential(credParam)
		if err != nil {
			return schema.NodeResult{}, err
		}
		if cred == nil {
			return schema.NodeResult{}, fmt.Errorf("credential not set")
		}
		val, _ := cred[n.Auth.ValueField].(string)
		req.Header.Set(n.Auth.Header, n.Auth.Prefix+val)
	}

	res, err := doWithRetry(client, req)
	if err != nil {
		return schema.NodeResult{}, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		return schema.NodeResult{}, fmt.Errorf("%s %s -> %d: %s", method, u, res.StatusCode, truncate(string(raw), 500))
	}

	items, err := mapResponse(raw, op.ItemsPath)
	if err != nil {
		return schema.NodeResult{}, err
	}
	return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, nil
}

// doWithRetry retries 429/5xx up to 3 times, honoring Retry-After. The request body
// is rewound via GetBody (set automatically for bytes.Reader bodies).
func doWithRetry(client *http.Client, req *http.Request) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 && req.GetBody != nil {
			if b, err := req.GetBody(); err == nil {
				req.Body = b
			}
		}
		res, err := client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(backoff(attempt, ""))
			continue
		}
		if res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500 {
			wait := backoff(attempt, res.Header.Get("Retry-After"))
			res.Body.Close()
			lastErr = fmt.Errorf("status %d", res.StatusCode)
			time.Sleep(wait)
			continue
		}
		return res, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("request failed")
	}
	return nil, lastErr
}

// ---- helpers ---------------------------------------------------------------

func interpolate(path string, params map[string]any) (string, error) {
	var b strings.Builder
	for {
		i := strings.IndexByte(path, '{')
		if i < 0 {
			b.WriteString(path)
			break
		}
		j := strings.IndexByte(path[i:], '}')
		if j < 0 {
			b.WriteString(path)
			break
		}
		j += i
		b.WriteString(path[:i])
		name := path[i+1 : j]
		v, ok := params[name]
		if !ok || fmt.Sprint(v) == "" {
			return "", fmt.Errorf("missing path param %q", name)
		}
		b.WriteString(url.PathEscape(fmt.Sprint(v)))
		path = path[j+1:]
	}
	return b.String(), nil
}

func mapResponse(raw []byte, itemsPath string) ([]schema.Item, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return []schema.Item{{JSON: map[string]any{}}}, nil
	}
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return []schema.Item{{JSON: map[string]any{"data": string(raw)}}}, nil
	}
	node := root
	if itemsPath != "" {
		node = getPath(root, itemsPath)
	}
	if arr, ok := node.([]any); ok {
		out := make([]schema.Item, 0, len(arr))
		for _, e := range arr {
			out = append(out, toItem(e))
		}
		return out, nil
	}
	return []schema.Item{toItem(node)}, nil
}

func toItem(v any) schema.Item {
	if m, ok := v.(map[string]any); ok {
		return schema.Item{JSON: m}
	}
	return schema.Item{JSON: map[string]any{"value": v}}
}

func getPath(root any, path string) any {
	cur := root
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[part]
	}
	return cur
}

func asObject(v any) map[string]any {
	switch t := v.(type) {
	case string:
		var m map[string]any
		if json.Unmarshal([]byte(t), &m) == nil {
			return m
		}
		return map[string]any{}
	case map[string]any:
		return t
	default:
		return map[string]any{}
	}
}

func backoff(attempt int, retryAfter string) time.Duration {
	if retryAfter != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil {
			return time.Duration(secs) * time.Second
		}
	}
	d := time.Duration(500*(1<<attempt)) * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

func groupOr(g, def string) string {
	if g == "" {
		return def
	}
	return g
}

func credList(n Node) []string {
	if n.Auth.Kind == "" || n.Auth.Kind == "none" {
		return nil
	}
	return []string{n.CredType}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
