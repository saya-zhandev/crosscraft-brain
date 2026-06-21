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

// PaginationStyle selects the pagination strategy for list endpoints.
type PaginationStyle string

const (
	PaginateNone      PaginationStyle = ""
	PaginateCursor    PaginationStyle = "cursor"
	PaginatePageToken PaginationStyle = "pageToken"
	PaginateOffset    PaginationStyle = "offset"
)

// Pagination configures automatic page-walking for a list operation.
type Pagination struct {
	Style         PaginationStyle // cursor, pageToken, or offset
	NextTokenPath string          // dot-path to the next-page token in the response body
	TokenParam    string          // query param for the token (default: "pageToken")
	OffsetParam   string          // query param for the offset (default: "offset")
	LimitParam    string          // query param for page size (default: "limit")
	Limit         int             // default page size (0 = use API default)
}

// Op is one operation (resource + verb).
type Op struct {
	Resource   string
	Name       string
	Label      string
	Method     string
	Path       string            // may contain {param} placeholders
	Query      map[string]string // query key -> param name, or "=literal"
	BodyParam  string            // param whose JSON value becomes the request body
	ItemsPath  string            // dot-path to an array in the response; "" = whole body
	Params     []schema.ParamSchema
	Pagination *Pagination // if set, walk pages and accumulate results
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
	// BaseURLParam, if set, adds a string param of this name (defaulting to
	// BaseURL) so the user can override the endpoint at runtime — e.g. an
	// Acrobat Sign account shard or a self-hosted instance.
	BaseURLParam string
	CredType     string            // credentialType for the credential param
	Auth         Auth
	Headers      map[string]string // extra fixed request headers (e.g. API version)
	Ops          []Op
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
	if n.BaseURLParam != "" {
		params = append(params, schema.ParamSchema{
			Name: n.BaseURLParam, Label: "API base URL", Type: "string", Default: n.BaseURL,
		})
	}
	params = append(params, schema.ParamSchema{
		Name: "operation", Label: "Operation", Type: "select", Required: true, Options: opts,
	})

	// Per-op params, shown only when their operation(s) are selected. Params shared
	// by several ops (same Name) are emitted once with a combined showWhen.
	paramIndex := map[string]int{}
	for _, o := range n.Ops {
		for _, p := range o.Params {
			if idx, ok := paramIndex[p.Name]; ok {
				if sw := params[idx].ShowWhen; sw != nil {
					sw.Equals = append(sw.Equals, o.key())
				}
				continue
			}
			pp := p
			pp.ShowWhen = &schema.ShowWhen{Param: "operation", Equals: []any{o.key()}}
			params = append(params, pp)
			paramIndex[p.Name] = len(params) - 1
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
	base := n.BaseURL
	if n.BaseURLParam != "" {
		if v, ok := ctx.Params[n.BaseURLParam].(string); ok && v != "" {
			base = v
		}
	}
	baseURL := strings.TrimRight(base, "/")

	method := strings.ToUpper(op.Method)
	if method == "" {
		method = http.MethodGet
	}

	var bodyBytes []byte
	hasBody := false
	if op.BodyParam != "" {
		bodyBytes, _ = json.Marshal(asObject(ctx.RawParam(op.BodyParam)))
		hasBody = true
	}

	// Build auth'd client once
	client := httpClient
	headerCredVal := ""
	if n.Auth.Kind == "oauth2" {
		c, err := ctx.AuthorizedClient(credParam)
		if err != nil {
			return schema.NodeResult{}, err
		}
		client = c
	} else if n.Auth.Kind == "header" {
		cred, err := ctx.Credential(credParam)
		if err != nil {
			return schema.NodeResult{}, err
		}
		if cred == nil {
			return schema.NodeResult{}, fmt.Errorf("credential not set")
		}
		headerCredVal, _ = cred[n.Auth.ValueField].(string)
	}

	// Pagination loop
	allItems := []schema.Item{}
	maxPages := 50
	pageQuery := url.Values{}
	offset := 0
	for page := 0; page < maxPages; page++ {
		// Build URL with query params (including pagination tokens)
		q := url.Values{}
		for k, v := range pageQuery {
			q[k] = v
		}
		for key, pn := range op.Query {
			if strings.HasPrefix(pn, "=") {
				q.Set(key, pn[1:])
				continue
			}
			if v := ctx.Params[pn]; v != nil && fmt.Sprint(v) != "" {
				q.Set(key, fmt.Sprint(v))
			}
		}
		u := baseURL + path
		if len(q) > 0 {
			u += "?" + q.Encode()
		}

		if ctx.Log != nil {
			ctx.Log(method+" "+u, nil)
		}

		reqCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		var reqBody io.Reader
		if hasBody {
			reqBody = bytes.NewReader(bodyBytes)
		}
		req, err := http.NewRequestWithContext(reqCtx, method, u, reqBody)
		if err != nil {
			cancel()
			return schema.NodeResult{}, err
		}
		if hasBody {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Accept", "application/json")
		for k, v := range n.Headers {
			req.Header.Set(k, v)
		}
		if n.Auth.Kind == "header" && headerCredVal != "" {
			req.Header.Set(n.Auth.Header, n.Auth.Prefix+headerCredVal)
		}

		res, err := doWithRetry(client, req)
		cancel()
		if err != nil {
			return schema.NodeResult{}, err
		}
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode >= 400 {
			return schema.NodeResult{}, fmt.Errorf("%s %s -> %d: %s", method, u, res.StatusCode, truncate(string(raw), 500))
		}

		items, err := mapResponse(raw, op.ItemsPath)
		if err != nil {
			return schema.NodeResult{}, err
		}
		allItems = append(allItems, items...)

		// Check for next page
		if op.Pagination == nil || op.Pagination.Style == PaginateNone {
			break
		}
		nextToken := ""
		switch op.Pagination.Style {
		case PaginateCursor, PaginatePageToken:
			tokenPath := op.Pagination.NextTokenPath
			if tokenPath == "" {
				tokenPath = "nextPageToken"
			}
			tokenParam := op.Pagination.TokenParam
			if tokenParam == "" {
				tokenParam = "pageToken"
			}
			if tv := getPath(parseRaw(raw), tokenPath); tv != nil {
				nextToken = fmt.Sprint(tv)
			}
			if nextToken == "" {
				page = maxPages // stop
			} else {
				pageQuery = url.Values{tokenParam: {nextToken}}
			}
		case PaginateOffset:
			if len(items) == 0 {
				page = maxPages // stop
			}
			offset += len(items)
			offsetParam := op.Pagination.OffsetParam
			if offsetParam == "" {
				offsetParam = "offset"
			}
			limitParam := op.Pagination.LimitParam
			if limitParam == "" {
				limitParam = "limit"
			}
			lim := op.Pagination.Limit
			if lim == 0 {
				lim = 100
			}
			pageQuery = url.Values{offsetParam: {strconv.Itoa(offset)}, limitParam: {strconv.Itoa(lim)}}
		}
		if page >= maxPages-1 {
			break
		}
	}

	return schema.NodeResult{Outputs: map[string][]schema.Item{"main": allItems}}, nil
}

// parseRaw unmarshals raw JSON into an any for getPath traversal.
func parseRaw(raw []byte) any {
	var v any
	json.Unmarshal(raw, &v)
	return v
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
