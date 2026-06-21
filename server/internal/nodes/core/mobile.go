// Package core — mobile/client-enablement nodes.
//
//   - core.formTrigger        — accepts form POSTs; validates required fields
//   - core.webhookRespond     — suspends the run with a custom HTTP response to the caller
//   - core.pushNotification   — sends FCM/APNs push notifications to mobile devices
//
// These nodes, together with the api-key auth layer, let mobile apps drive and
// interact with workflows via simple HTTP calls.
package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

func init() {
	Nodes = append(Nodes, formTriggerNode, webhookRespondNode, pushNotificationNode)
}

// ── Form Trigger ────────────────────────────────────────────────────────────
//
// Like a webhook trigger, but validates required form fields and parses
// application/x-www-form-urlencoded in addition to JSON. Designed for mobile
// app form submissions.

var formTriggerNode = schema.NodeDefinition{
	Type:        "core.formTrigger",
	Label:       "Form Trigger",
	Group:       "trigger",
	Icon:        "FormInput",
	Description: "Start the workflow on a form POST. Validates required fields.",
	IsTrigger:   true,
	Inputs:      []schema.Port{},
	Outputs:     []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "path", Label: "Path", Type: "string", Required: true, Placeholder: "contact-form"},
		{Name: "requiredFields", Label: "Required fields (comma-separated)", Type: "string", Placeholder: "email,name"},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		out := ctx.Trigger
		if len(out) == 0 {
			out = []schema.Item{{JSON: map[string]any{}}}
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
	},
}

func validateForm(item schema.Item, requiredCSV string) error {
	if requiredCSV == "" {
		return nil
	}
	for _, f := range splitCSV(requiredCSV) {
		if _, ok := item.JSON[f]; !ok {
			return fmt.Errorf("missing required field: %s", f)
		}
	}
	return nil
}

// ── Webhook Respond ─────────────────────────────────────────────────────────
//
// Suspends the run while sending a structured HTTP response back to the webhook
// or form caller. A mobile app POSTs a form → workflow runs to this node →
// gets a custom JSON response → the workflow suspends, awaiting the next
// resume (e.g. from a subsequent user action).

var webhookRespondNode = schema.NodeDefinition{
	Type:        "core.webhookRespond",
	Label:       "Respond to Webhook",
	Group:       "flow",
	Icon:        "Reply",
	Description: "Return a custom HTTP response to the caller, then pause until resumed.",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "status", Label: "HTTP Status", Type: "number", Default: float64(200)},
		{Name: "body", Label: "Response Body (JSON)", Type: "json", Default: map[string]any{"ok": true}},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		status := asInt(ctx.Params["status"], 200)
		body := ctx.Params["body"]
		if body == nil {
			body = map[string]any{"ok": true}
		}
		return schema.NodeResult{Suspend: &schema.SuspendRequest{
			Kind: "webhook",
			Respond: &schema.RespondSpec{
				Status: status,
				Body:   body,
			},
		}}, nil
	},
}

// ── Push Notification ───────────────────────────────────────────────────────
//
// Sends a push notification via Firebase Cloud Messaging (FCM) HTTP v1 API.
// Supports both notification (display) and data (silent) messages.
// FCM reaches both Android and iOS devices.

var pushNotificationNode = schema.NodeDefinition{
	Type:        "core.pushNotification",
	Label:       "Push Notification",
	Group:       "integration",
	Icon:        "Bell",
	Description: "Send a push notification to mobile devices via FCM (Firebase Cloud Messaging).",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "deviceToken", Label: "Device Token (FCM registration token)", Type: "expression", Required: true,
			Placeholder: "{{ $json.fcmToken }}"},
		{Name: "title", Label: "Title", Type: "expression", Placeholder: "New scan recorded"},
		{Name: "body", Label: "Body", Type: "expression", Placeholder: "Code: {{ $json.code }}"},
		{Name: "data", Label: "Data payload (JSON, for silent/background)", Type: "json", Default: map[string]any{}},
	},
	Credentials: []string{"fcmServiceAccount"},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		deviceToken := asString(ctx.Params["deviceToken"], "")
		if deviceToken == "" {
			return schema.NodeResult{}, fmt.Errorf("pushNotification: deviceToken is required")
		}
		title := asString(ctx.Params["title"], "")
		body := asString(ctx.Params["body"], "")

		cred, err := ctx.Credential("credential")
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("pushNotification: credential: %w", err)
		}
		if cred == nil {
			return schema.NodeResult{}, fmt.Errorf("pushNotification: FCM service account credential is required")
		}

		// FCM HTTP v1 requires an OAuth2 access token. The credential either has a
		// pre-fetched access_token (if obtained via client_credentials) or a service
		// account JSON that we use to get one.
		accessToken := asString(cred["access_token"], "")
		if accessToken == "" {
			// Attempt service-account key exchange inline (simple JWT assertion).
			saJSON := asString(cred["service_account_json"], "")
			if saJSON == "" {
				// Try to read the raw credential data as the SA JSON itself.
				if b, err := json.Marshal(cred); err == nil {
					saJSON = string(b)
				}
			}
			if saJSON != "" {
				tok, err := fetchFCMAccessToken(saJSON)
				if err != nil {
					return schema.NodeResult{}, fmt.Errorf("pushNotification: FCM auth: %w", err)
				}
				accessToken = tok
			}
		}
		if accessToken == "" {
			return schema.NodeResult{}, fmt.Errorf("pushNotification: no FCM access token or service account configured")
		}

		projectID := asString(cred["project_id"], "")
		if projectID == "" {
			return schema.NodeResult{}, fmt.Errorf("pushNotification: FCM project_id is required in credential")
		}

		// Build FCM v1 message
		msg := map[string]any{
			"message": map[string]any{
				"token": deviceToken,
			},
		}
		if title != "" || body != "" {
			msg["message"].(map[string]any)["notification"] = map[string]any{
				"title": title,
				"body":  body,
			}
		}
		if data := asObject(ctx.RawParam("data")); len(data) > 0 {
			msg["message"].(map[string]any)["data"] = data
		}

		reqBody, _ := json.Marshal(msg)
		url := fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", projectID)
		req, err := http.NewRequest("POST", url, bytes.NewReader(reqBody))
		if err != nil {
			return schema.NodeResult{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+accessToken)

		client := &http.Client{Timeout: 15 * time.Second}
		res, err := client.Do(req)
		if err != nil {
			return schema.NodeResult{}, err
		}
		defer res.Body.Close()
		respBody, _ := io.ReadAll(res.Body)

		if res.StatusCode >= 400 {
			return schema.NodeResult{}, fmt.Errorf("pushNotification: FCM error %d: %s", res.StatusCode, truncateStr(string(respBody), 300))
		}

		ctx.Log("push sent to "+deviceToken, map[string]any{"title": title, "status": res.StatusCode})

		out := []schema.Item{{
			JSON: map[string]any{
				"sent":        true,
				"deviceToken": deviceToken,
				"title":       title,
				"response":    string(respBody),
			},
		}}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
	},
}

// fetchFCMAccessToken gets an OAuth2 access token for FCM using a service
// account JSON key (JWT bearer assertion against
// https://oauth2.googleapis.com/token).
func fetchFCMAccessToken(saJSON string) (string, error) {
	var sa struct {
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
		TokenURI    string `json:"token_uri"`
	}
	if err := json.Unmarshal([]byte(saJSON), &sa); err != nil {
		return "", fmt.Errorf("invalid service account JSON: %w", err)
	}
	if sa.ClientEmail == "" || sa.PrivateKey == "" {
		return "", fmt.Errorf("service account JSON missing client_email or private_key")
	}
	tokenURI := sa.TokenURI
	if tokenURI == "" {
		tokenURI = "https://oauth2.googleapis.com/token"
	}

	// Build JWT assertion
	header := base64urlEncode([]byte(`{"alg":"RS256","typ":"JWT"}`))
	now := time.Now()
	claimSet := map[string]any{
		"iss":   sa.ClientEmail,
		"scope": "https://www.googleapis.com/auth/firebase.messaging",
		"aud":   tokenURI,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	}
	claimBytes, _ := json.Marshal(claimSet)
	assertion := header + "." + base64urlEncode(claimBytes)

	// Sign with RSA private key
	sig, err := signRSASHA256([]byte(sa.PrivateKey), []byte(assertion))
	if err != nil {
		return "", fmt.Errorf("signing JWT: %w", err)
	}
	assertion += "." + base64urlEncode(sig)

	// Exchange assertion for access token
	form := bytes.NewBufferString("grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer&assertion=" + assertion)
	req, _ := http.NewRequest("POST", tokenURI, form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		return "", fmt.Errorf("FCM token exchange: %d: %s", res.StatusCode, truncateStr(string(body), 200))
	}
	var tresp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tresp); err != nil {
		return "", fmt.Errorf("parsing token response: %w", err)
	}
	return tresp.AccessToken, nil
}

// ── helpers ─────────────────────────────────────────────────────────────────

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func truncateStr(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
