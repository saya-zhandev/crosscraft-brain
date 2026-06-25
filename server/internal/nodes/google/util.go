package google

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Retry transport — wraps the OAuth2 HTTP client's transport with retry logic
// for transient failures (429 Too Many Requests and 5xx server errors).
// Pattern mirrors the REST framework's doWithRetry in rest.go:330-357.
// ---------------------------------------------------------------------------

// retryTransport wraps an http.RoundTripper and retries on 429/5xx up to 3
// times with exponential backoff, honoring the Retry-After header.
type retryTransport struct {
	base http.RoundTripper
}

func (rt *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		res, err := rt.base.RoundTrip(req)
		if err != nil {
			lastErr = err
			time.Sleep(backoffDuration(attempt, ""))
			continue
		}
		if res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500 {
			wait := backoffDuration(attempt, res.Header.Get("Retry-After"))
			res.Body.Close()
			lastErr = fmt.Errorf("status %d", res.StatusCode)
			time.Sleep(wait)
			continue
		}
		return res, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("request failed after retries")
	}
	return nil, lastErr
}

func backoffDuration(attempt int, retryAfter string) time.Duration {
	if retryAfter != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil {
			return time.Duration(secs) * time.Second
		}
	}
	d := time.Duration(500*math.Pow(2, float64(attempt))) * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// wrapWithRetry returns an *http.Client whose transport retries on transient
// errors. If the provided client already has a retry transport, it is returned
// unchanged.
func wrapWithRetry(client *http.Client) *http.Client {
	if client == nil {
		return client
	}
	if _, ok := client.Transport.(*retryTransport); ok {
		return client // already wrapped
	}
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &http.Client{
		Transport:     &retryTransport{base: transport},
		CheckRedirect: client.CheckRedirect,
		Jar:           client.Jar,
		Timeout:       client.Timeout,
	}
}

// ---------------------------------------------------------------------------
// Shared type-conversion helpers (used by all Google nodes)
// ---------------------------------------------------------------------------

// asObject coerces an any value to map[string]any.
func asObject(v any) map[string]any {
	switch t := v.(type) {
	case map[string]any:
		return t
	case string:
		var m map[string]any
		if json.Unmarshal([]byte(t), &m) == nil {
			return m
		}
		return map[string]any{}
	default:
		return map[string]any{}
	}
}

// toInt64 coerces common numeric/string representations to int64.
func toInt64(v any) (int64, bool) {
	switch t := v.(type) {
	case int64:
		return t, true
	case int:
		return int64(t), true
	case float64:
		return int64(t), true
	case string:
		if n, err := strconv.ParseInt(t, 10, 64); err == nil {
			return n, true
		}
	}
	return 0, false
}

// parseIntParam extracts an integer from a param value which may arrive as
// float64 (JSON number), int, int64, or string.
func parseIntParam(v any, defaultVal int) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	case string:
		if n, err := strconv.Atoi(t); err == nil {
			return n
		}
	}
	return defaultVal
}

// splitCSV splits a comma-separated string param value, trimming whitespace
// and dropping empty segments.
func splitCSV(params map[string]any, name string) []string {
	s, _ := params[name].(string)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
