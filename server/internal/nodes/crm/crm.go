// Package crm provides CRM and marketing integration nodes: HubSpot, SendGrid,
// Pipedrive (declarative REST) and Mailchimp (native, due to server-derived URL).
package crm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/rest"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

func sp(name, label string, required bool) schema.ParamSchema {
	return schema.ParamSchema{Name: name, Label: label, Type: "string", Required: required}
}
func ip(name, label string, def int) schema.ParamSchema {
	return schema.ParamSchema{Name: name, Label: label, Type: "number", Default: def}
}
func jp(name, label string) schema.ParamSchema {
	return schema.ParamSchema{Name: name, Label: label, Type: "json"}
}

// Nodes returns the full CRM/marketing node pack.
func Nodes() []schema.NodeDefinition {
	return []schema.NodeDefinition{
		HubSpot().Build(),
		SendGrid().Build(),
		Pipedrive().Build(),
		Mailchimp(),
	}
}

// ── HubSpot ───────────────────────────────────────────────────────────────────

func HubSpot() rest.Node {
	contactID := sp("contactId", "Contact ID", true)
	companyID := sp("companyId", "Company ID", true)
	dealID := sp("dealId", "Deal ID", true)
	body := jp("body", "Body (JSON: {properties:{...}})")
	query := jp("filterBody", "Search/Filter (JSON)")
	limit := ip("limit", "Max results", 100)
	return rest.Node{
		Type: "crm.hubspot", Label: "HubSpot", Group: "integration", Icon: "Building",
		Description: "Manage HubSpot CRM contacts, companies, and deals.",
		BaseURL:     "https://api.hubapi.com",
		CredType:    "hubspotApi",
		Auth:        rest.Auth{Kind: "header", Header: "Authorization", Prefix: "Bearer ", ValueField: "accessToken"},
		Ops: []rest.Op{
			{Resource: "contact", Name: "list", Label: "List Contacts", Method: "GET",
				Path: "/crm/v3/objects/contacts", ItemsPath: "results",
				Query: map[string]string{"limit": "limit"},
				Params: []schema.ParamSchema{limit}},
			{Resource: "contact", Name: "get", Label: "Get Contact", Method: "GET",
				Path: "/crm/v3/objects/contacts/{contactId}",
				Params: []schema.ParamSchema{contactID}},
			{Resource: "contact", Name: "create", Label: "Create Contact", Method: "POST",
				Path: "/crm/v3/objects/contacts", BodyParam: "body",
				Params: []schema.ParamSchema{body}},
			{Resource: "contact", Name: "update", Label: "Update Contact", Method: "PATCH",
				Path: "/crm/v3/objects/contacts/{contactId}", BodyParam: "body",
				Params: []schema.ParamSchema{contactID, body}},
			{Resource: "contact", Name: "delete", Label: "Delete Contact", Method: "DELETE",
				Path: "/crm/v3/objects/contacts/{contactId}",
				Params: []schema.ParamSchema{contactID}},
			{Resource: "contact", Name: "search", Label: "Search Contacts", Method: "POST",
				Path: "/crm/v3/objects/contacts/search", BodyParam: "filterBody", ItemsPath: "results",
				Params: []schema.ParamSchema{query}},
			{Resource: "company", Name: "list", Label: "List Companies", Method: "GET",
				Path: "/crm/v3/objects/companies", ItemsPath: "results",
				Query: map[string]string{"limit": "limit"},
				Params: []schema.ParamSchema{limit}},
			{Resource: "company", Name: "get", Label: "Get Company", Method: "GET",
				Path: "/crm/v3/objects/companies/{companyId}",
				Params: []schema.ParamSchema{companyID}},
			{Resource: "company", Name: "create", Label: "Create Company", Method: "POST",
				Path: "/crm/v3/objects/companies", BodyParam: "body",
				Params: []schema.ParamSchema{body}},
			{Resource: "company", Name: "update", Label: "Update Company", Method: "PATCH",
				Path: "/crm/v3/objects/companies/{companyId}", BodyParam: "body",
				Params: []schema.ParamSchema{companyID, body}},
			{Resource: "deal", Name: "list", Label: "List Deals", Method: "GET",
				Path: "/crm/v3/objects/deals", ItemsPath: "results",
				Query: map[string]string{"limit": "limit"},
				Params: []schema.ParamSchema{limit}},
			{Resource: "deal", Name: "get", Label: "Get Deal", Method: "GET",
				Path: "/crm/v3/objects/deals/{dealId}",
				Params: []schema.ParamSchema{dealID}},
			{Resource: "deal", Name: "create", Label: "Create Deal", Method: "POST",
				Path: "/crm/v3/objects/deals", BodyParam: "body",
				Params: []schema.ParamSchema{body}},
			{Resource: "deal", Name: "update", Label: "Update Deal", Method: "PATCH",
				Path: "/crm/v3/objects/deals/{dealId}", BodyParam: "body",
				Params: []schema.ParamSchema{dealID, body}},
		},
	}
}

// ── SendGrid ──────────────────────────────────────────────────────────────────

func SendGrid() rest.Node {
	body := jp("body", "Email Body (JSON: {personalizations, from, subject, content})")
	listID := sp("listId", "Contact List ID", false)
	contactID := sp("contactId", "Contact ID", true)
	return rest.Node{
		Type: "crm.sendgrid", Label: "SendGrid", Group: "integration", Icon: "MailCheck",
		Description: "Send transactional email and manage contacts via SendGrid.",
		BaseURL:     "https://api.sendgrid.com/v3",
		CredType:    "sendgridApi",
		Auth:        rest.Auth{Kind: "header", Header: "Authorization", Prefix: "Bearer ", ValueField: "accessToken"},
		Ops: []rest.Op{
			{Resource: "email", Name: "send", Label: "Send Email", Method: "POST",
				Path: "/mail/send", BodyParam: "body",
				Params: []schema.ParamSchema{body}},
			{Resource: "contact", Name: "search", Label: "Search Contacts", Method: "POST",
				Path: "/marketing/contacts/search", BodyParam: "body", ItemsPath: "result",
				Params: []schema.ParamSchema{body}},
			{Resource: "contact", Name: "upsert", Label: "Upsert Contacts", Method: "PUT",
				Path: "/marketing/contacts", BodyParam: "body",
				Params: []schema.ParamSchema{body}},
			{Resource: "contact", Name: "delete", Label: "Delete Contacts", Method: "DELETE",
				Path: "/marketing/contacts",
				Query: map[string]string{"ids": "contactId"},
				Params: []schema.ParamSchema{contactID}},
			{Resource: "list", Name: "list", Label: "List Contact Lists", Method: "GET",
				Path: "/marketing/lists", ItemsPath: "result"},
			{Resource: "list", Name: "get", Label: "Get Contact List", Method: "GET",
				Path: "/marketing/lists/{listId}",
				Params: []schema.ParamSchema{listID}},
		},
	}
}

// ── Pipedrive ─────────────────────────────────────────────────────────────────

func Pipedrive() rest.Node {
	personID := sp("personId", "Person ID", true)
	dealID := sp("dealId", "Deal ID", true)
	orgID := sp("orgId", "Organization ID", true)
	body := jp("body", "Body (JSON)")
	limit := ip("limit", "Max results", 100)
	filter := sp("filter_id", "Filter ID", false)
	return rest.Node{
		Type: "crm.pipedrive", Label: "Pipedrive", Group: "integration", Icon: "FunnelIcon",
		Description: "Manage Pipedrive persons, deals, and organizations.",
		BaseURL:     "https://api.pipedrive.com/v1",
		CredType:    "pipedriveApi",
		Auth:        rest.Auth{Kind: "header", Header: "Authorization", Prefix: "Bearer ", ValueField: "accessToken"},
		Ops: []rest.Op{
			{Resource: "person", Name: "list", Label: "List Persons", Method: "GET",
				Path: "/persons", ItemsPath: "data",
				Query: map[string]string{"limit": "limit", "filter_id": "filter_id"},
				Params: []schema.ParamSchema{limit, filter}},
			{Resource: "person", Name: "get", Label: "Get Person", Method: "GET",
				Path: "/persons/{personId}", ItemsPath: "data",
				Params: []schema.ParamSchema{personID}},
			{Resource: "person", Name: "create", Label: "Create Person", Method: "POST",
				Path: "/persons", BodyParam: "body", ItemsPath: "data",
				Params: []schema.ParamSchema{body}},
			{Resource: "person", Name: "update", Label: "Update Person", Method: "PUT",
				Path: "/persons/{personId}", BodyParam: "body", ItemsPath: "data",
				Params: []schema.ParamSchema{personID, body}},
			{Resource: "person", Name: "delete", Label: "Delete Person", Method: "DELETE",
				Path: "/persons/{personId}",
				Params: []schema.ParamSchema{personID}},
			{Resource: "deal", Name: "list", Label: "List Deals", Method: "GET",
				Path: "/deals", ItemsPath: "data",
				Query: map[string]string{"limit": "limit", "filter_id": "filter_id"},
				Params: []schema.ParamSchema{limit, filter}},
			{Resource: "deal", Name: "get", Label: "Get Deal", Method: "GET",
				Path: "/deals/{dealId}", ItemsPath: "data",
				Params: []schema.ParamSchema{dealID}},
			{Resource: "deal", Name: "create", Label: "Create Deal", Method: "POST",
				Path: "/deals", BodyParam: "body", ItemsPath: "data",
				Params: []schema.ParamSchema{body}},
			{Resource: "deal", Name: "update", Label: "Update Deal", Method: "PUT",
				Path: "/deals/{dealId}", BodyParam: "body", ItemsPath: "data",
				Params: []schema.ParamSchema{dealID, body}},
			{Resource: "organization", Name: "list", Label: "List Organizations", Method: "GET",
				Path: "/organizations", ItemsPath: "data",
				Query: map[string]string{"limit": "limit"},
				Params: []schema.ParamSchema{limit}},
			{Resource: "organization", Name: "get", Label: "Get Organization", Method: "GET",
				Path: "/organizations/{orgId}", ItemsPath: "data",
				Params: []schema.ParamSchema{orgID}},
		},
	}
}

// ── Mailchimp (native) ────────────────────────────────────────────────────────
// URL is server-dependent: https://{server}.api.mailchimp.com/3.0
// Auth is HTTP Basic: anystring:{API_KEY}

func Mailchimp() schema.NodeDefinition {
	return schema.NodeDefinition{
		Type: "crm.mailchimp", Label: "Mailchimp", Group: "integration", Icon: "Mail",
		Description: "Manage Mailchimp audience members and campaigns.",
		Inputs:      []schema.Port{{ID: "main"}},
		Outputs:     []schema.Port{{ID: "main"}},
		Credentials: []string{"mailchimpApi"},
		Params: []schema.ParamSchema{
			{Name: "credential", Label: "Credential", Type: "credential", Required: true, CredentialType: "mailchimpApi"},
			{Name: "operation", Label: "Operation", Type: "select", Required: true, Options: []schema.ParamOption{
				{Label: "member: List", Value: "member:list"},
				{Label: "member: Get", Value: "member:get"},
				{Label: "member: Add/Update", Value: "member:upsert"},
				{Label: "member: Delete", Value: "member:delete"},
				{Label: "list: List Audiences", Value: "list:list"},
				{Label: "campaign: List", Value: "campaign:list"},
			}},
			{Name: "listId", Label: "Audience/List ID", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{"member:list", "member:get", "member:upsert", "member:delete"}}},
			{Name: "subscriberHash", Label: "Subscriber Hash (MD5 of email)", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{"member:get", "member:delete"}}},
			{Name: "body", Label: "Body (JSON: {email_address, status, merge_fields})", Type: "json",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{"member:upsert"}}},
			{Name: "count", Label: "Max results", Type: "number", Default: 100},
		},
		Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
			cred, err := ctx.Credential("credential")
			if err != nil {
				return schema.NodeResult{}, err
			}
			apiKey, _ := cred["accessToken"].(string)
			server, _ := cred["server"].(string)
			if apiKey == "" || server == "" {
				return schema.NodeResult{}, fmt.Errorf("mailchimp: API key and server prefix are required")
			}
			base := "https://" + server + ".api.mailchimp.com/3.0"
			op := asString(ctx.Params["operation"], "")
			listID := asString(ctx.Params["listId"], "")
			subHash := asString(ctx.Params["subscriberHash"], "")

			call := func(method, path string, body map[string]any) ([]schema.Item, error) {
				return mailchimpCall("anystring", apiKey, method, base+path, body)
			}

			switch op {
			case "member:list":
				items, err := call("GET", "/lists/"+listID+"/members", nil)
				return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, err
			case "member:get":
				items, err := call("GET", "/lists/"+listID+"/members/"+subHash, nil)
				return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, err
			case "member:upsert":
				body := asObject(ctx.RawParam("body"))
				items, err := call("PUT", "/lists/"+listID+"/members/"+subHash, body)
				return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, err
			case "member:delete":
				items, err := call("DELETE", "/lists/"+listID+"/members/"+subHash, nil)
				return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, err
			case "list:list":
				items, err := call("GET", "/lists", nil)
				return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, err
			case "campaign:list":
				items, err := call("GET", "/campaigns", nil)
				return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, err
			default:
				return schema.NodeResult{}, fmt.Errorf("mailchimp: unknown operation %q", op)
			}
		},
	}
}

func mailchimpCall(user, apiKey, method, apiURL string, body map[string]any) ([]schema.Item, error) {
	var reqBody io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, apiURL, reqBody)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(user, apiKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mailchimp: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("mailchimp: %d %s", resp.StatusCode, truncate(string(raw), 400))
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return []schema.Item{{JSON: map[string]any{"success": true}}}, nil
	}
	var root any
	if json.Unmarshal(raw, &root) != nil {
		return []schema.Item{{JSON: map[string]any{"raw": string(raw)}}}, nil
	}
	if m, ok := root.(map[string]any); ok {
		for _, listField := range []string{"members", "lists", "campaigns"} {
			if arr, ok := m[listField].([]any); ok {
				out := make([]schema.Item, 0, len(arr))
				for _, e := range arr {
					if im, ok := e.(map[string]any); ok {
						out = append(out, schema.Item{JSON: im})
					}
				}
				return out, nil
			}
		}
		return []schema.Item{{JSON: m}}, nil
	}
	return []schema.Item{{JSON: map[string]any{"success": true}}}, nil
}

// ── shared helpers ────────────────────────────────────────────────────────────

func asString(v any, def string) string {
	if v == nil {
		return def
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func asObject(v any) map[string]any {
	switch t := v.(type) {
	case map[string]any:
		return t
	case string:
		var m map[string]any
		if json.Unmarshal([]byte(t), &m) == nil {
			return m
		}
	}
	return map[string]any{}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// suppress unused import warnings
var _ = strings.TrimSpace
