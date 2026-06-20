// Package productivity provides project-management and productivity integration nodes:
// Notion, Airtable, Linear, Todoist, Asana, ClickUp (declarative REST) and
// Jira and Trello (native, due to non-standard auth).
package productivity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
func ep(name, label, placeholder string) schema.ParamSchema {
	return schema.ParamSchema{Name: name, Label: label, Type: "expression", Placeholder: placeholder}
}

// Nodes returns the full productivity node pack.
func Nodes() []schema.NodeDefinition {
	return []schema.NodeDefinition{
		Notion().Build(),
		Airtable().Build(),
		Linear().Build(),
		Todoist().Build(),
		Asana().Build(),
		ClickUp().Build(),
		Jira(),
		Trello(),
	}
}

// ── Notion ────────────────────────────────────────────────────────────────────

func Notion() rest.Node {
	pageID := sp("pageId", "Page ID", true)
	dbID := sp("databaseId", "Database ID", true)
	blockID := sp("blockId", "Block ID", true)
	body := jp("body", "Body (JSON)")
	filter := jp("filter", "Filter (JSON)")
	limit := ip("pageSize", "Page size", 100)
	return rest.Node{
		Type: "productivity.notion", Label: "Notion", Group: "integration", Icon: "BookOpen",
		Description: "Read and write Notion pages, databases, and blocks.",
		BaseURL:     "https://api.notion.com/v1",
		CredType:    "notionApi",
		Headers:     map[string]string{"Notion-Version": "2022-06-28"},
		Auth:        rest.Auth{Kind: "header", Header: "Authorization", Prefix: "Bearer ", ValueField: "accessToken"},
		Ops: []rest.Op{
			{Resource: "page", Name: "get", Label: "Get Page", Method: "GET",
				Path: "/pages/{pageId}", Params: []schema.ParamSchema{pageID}},
			{Resource: "page", Name: "create", Label: "Create Page", Method: "POST",
				Path: "/pages", BodyParam: "body", Params: []schema.ParamSchema{body}},
			{Resource: "page", Name: "update", Label: "Update Page Properties", Method: "PATCH",
				Path: "/pages/{pageId}", BodyParam: "body", Params: []schema.ParamSchema{pageID, body}},
			{Resource: "database", Name: "get", Label: "Get Database", Method: "GET",
				Path: "/databases/{databaseId}", Params: []schema.ParamSchema{dbID}},
			{Resource: "database", Name: "query", Label: "Query Database", Method: "POST",
				Path: "/databases/{databaseId}/query", BodyParam: "filter", ItemsPath: "results",
				Query: map[string]string{"page_size": "pageSize"},
				Params: []schema.ParamSchema{dbID, filter, limit}},
			{Resource: "block", Name: "listChildren", Label: "List Block Children", Method: "GET",
				Path: "/blocks/{blockId}/children", ItemsPath: "results",
				Params: []schema.ParamSchema{blockID}},
			{Resource: "block", Name: "appendChildren", Label: "Append Block Children", Method: "PATCH",
				Path: "/blocks/{blockId}/children", BodyParam: "body",
				Params: []schema.ParamSchema{blockID, body}},
			{Resource: "user", Name: "list", Label: "List Users", Method: "GET",
				Path: "/users", ItemsPath: "results"},
			{Resource: "search", Name: "search", Label: "Search", Method: "POST",
				Path: "/search", BodyParam: "body", ItemsPath: "results",
				Params: []schema.ParamSchema{body}},
		},
	}
}

// ── Airtable ──────────────────────────────────────────────────────────────────

func Airtable() rest.Node {
	baseID := sp("baseId", "Base ID (appXXX)", true)
	tableID := sp("tableId", "Table Name or ID", true)
	recID := sp("recordId", "Record ID (recXXX)", true)
	body := jp("body", "Body (JSON: {fields:{...}})")
	filter := sp("filterByFormula", "Filter by Formula", false)
	limit := ip("maxRecords", "Max records", 100)
	return rest.Node{
		Type: "productivity.airtable", Label: "Airtable", Group: "integration", Icon: "Table2",
		Description: "Create, read, update, and delete Airtable records.",
		BaseURL:     "https://api.airtable.com/v0",
		CredType:    "airtableTokenApi",
		Auth:        rest.Auth{Kind: "header", Header: "Authorization", Prefix: "Bearer ", ValueField: "accessToken"},
		Ops: []rest.Op{
			{Resource: "record", Name: "list", Label: "List Records", Method: "GET",
				Path: "/{baseId}/{tableId}", ItemsPath: "records",
				Query: map[string]string{"filterByFormula": "filterByFormula", "maxRecords": "maxRecords"},
				Params: []schema.ParamSchema{baseID, tableID, filter, limit}},
			{Resource: "record", Name: "get", Label: "Get Record", Method: "GET",
				Path: "/{baseId}/{tableId}/{recordId}",
				Params: []schema.ParamSchema{baseID, tableID, recID}},
			{Resource: "record", Name: "create", Label: "Create Record", Method: "POST",
				Path: "/{baseId}/{tableId}", BodyParam: "body",
				Params: []schema.ParamSchema{baseID, tableID, body}},
			{Resource: "record", Name: "update", Label: "Update Record (PATCH)", Method: "PATCH",
				Path: "/{baseId}/{tableId}/{recordId}", BodyParam: "body",
				Params: []schema.ParamSchema{baseID, tableID, recID, body}},
			{Resource: "record", Name: "delete", Label: "Delete Record", Method: "DELETE",
				Path: "/{baseId}/{tableId}/{recordId}",
				Params: []schema.ParamSchema{baseID, tableID, recID}},
		},
	}
}

// ── Linear ────────────────────────────────────────────────────────────────────
// Linear's REST API is minimal; most endpoints are GraphQL. We expose the REST
// viewer/issues endpoint; for GraphQL use core.http + the Linear API key.

func Linear() rest.Node {
	issueID := sp("issueId", "Issue ID", true)
	teamID := sp("teamId", "Team ID", true)
	body := jp("body", "Body (JSON)")
	limit := ip("first", "Max results", 50)
	return rest.Node{
		Type: "productivity.linear", Label: "Linear", Group: "integration", Icon: "Layers",
		Description: "Manage Linear issues and teams.",
		BaseURL:     "https://api.linear.app/api",
		CredType:    "linearApi",
		Auth:        rest.Auth{Kind: "header", Header: "Authorization", Prefix: "", ValueField: "accessToken"},
		Ops: []rest.Op{
			{Resource: "issue", Name: "list", Label: "List Issues", Method: "GET",
				Path: "/v1/issues", ItemsPath: "data.nodes",
				Query: map[string]string{"first": "first"},
				Params: []schema.ParamSchema{limit}},
			{Resource: "issue", Name: "get", Label: "Get Issue", Method: "GET",
				Path: "/v1/issues/{issueId}",
				Params: []schema.ParamSchema{issueID}},
			{Resource: "issue", Name: "create", Label: "Create Issue", Method: "POST",
				Path: "/v1/issues", BodyParam: "body",
				Params: []schema.ParamSchema{body}},
			{Resource: "issue", Name: "update", Label: "Update Issue", Method: "PATCH",
				Path: "/v1/issues/{issueId}", BodyParam: "body",
				Params: []schema.ParamSchema{issueID, body}},
			{Resource: "issue", Name: "delete", Label: "Delete Issue", Method: "DELETE",
				Path: "/v1/issues/{issueId}",
				Params: []schema.ParamSchema{issueID}},
			{Resource: "team", Name: "list", Label: "List Teams", Method: "GET",
				Path: "/v1/teams", ItemsPath: "data.nodes"},
			{Resource: "team", Name: "get", Label: "Get Team", Method: "GET",
				Path: "/v1/teams/{teamId}",
				Params: []schema.ParamSchema{teamID}},
		},
	}
}

// ── Todoist ───────────────────────────────────────────────────────────────────

func Todoist() rest.Node {
	taskID := sp("taskId", "Task ID", true)
	projID := sp("projectId", "Project ID", false)
	body := jp("body", "Body (JSON)")
	return rest.Node{
		Type: "productivity.todoist", Label: "Todoist", Group: "integration", Icon: "CheckSquare",
		Description: "Manage Todoist tasks and projects.",
		BaseURL:     "https://api.todoist.com/rest/v2",
		CredType:    "todoistApi",
		Auth:        rest.Auth{Kind: "header", Header: "Authorization", Prefix: "Bearer ", ValueField: "accessToken"},
		Ops: []rest.Op{
			{Resource: "task", Name: "list", Label: "List Tasks", Method: "GET",
				Path: "/tasks",
				Query: map[string]string{"project_id": "projectId"},
				Params: []schema.ParamSchema{projID}},
			{Resource: "task", Name: "get", Label: "Get Task", Method: "GET",
				Path: "/tasks/{taskId}", Params: []schema.ParamSchema{taskID}},
			{Resource: "task", Name: "create", Label: "Create Task", Method: "POST",
				Path: "/tasks", BodyParam: "body", Params: []schema.ParamSchema{body}},
			{Resource: "task", Name: "update", Label: "Update Task", Method: "POST",
				Path: "/tasks/{taskId}", BodyParam: "body", Params: []schema.ParamSchema{taskID, body}},
			{Resource: "task", Name: "close", Label: "Close Task", Method: "POST",
				Path: "/tasks/{taskId}/close", Params: []schema.ParamSchema{taskID}},
			{Resource: "task", Name: "delete", Label: "Delete Task", Method: "DELETE",
				Path: "/tasks/{taskId}", Params: []schema.ParamSchema{taskID}},
			{Resource: "project", Name: "list", Label: "List Projects", Method: "GET",
				Path: "/projects"},
			{Resource: "project", Name: "get", Label: "Get Project", Method: "GET",
				Path: "/projects/{projectId}", Params: []schema.ParamSchema{projID}},
			{Resource: "project", Name: "create", Label: "Create Project", Method: "POST",
				Path: "/projects", BodyParam: "body", Params: []schema.ParamSchema{body}},
		},
	}
}

// ── Asana ─────────────────────────────────────────────────────────────────────

func Asana() rest.Node {
	taskGID := sp("taskGid", "Task GID", true)
	projGID := sp("projectGid", "Project GID", false)
	workspaceGID := sp("workspaceGid", "Workspace GID", false)
	body := jp("body", "Body (JSON: {data:{...}})")
	limit := ip("limit", "Max results", 50)
	return rest.Node{
		Type: "productivity.asana", Label: "Asana", Group: "integration", Icon: "Circle",
		Description: "Manage Asana tasks, projects, and workspaces.",
		BaseURL:     "https://app.asana.com/api/1.0",
		CredType:    "asanaApi",
		Auth:        rest.Auth{Kind: "header", Header: "Authorization", Prefix: "Bearer ", ValueField: "accessToken"},
		Ops: []rest.Op{
			{Resource: "task", Name: "list", Label: "List Tasks", Method: "GET",
				Path: "/tasks", ItemsPath: "data",
				Query: map[string]string{"project": "projectGid", "limit": "limit"},
				Params: []schema.ParamSchema{projGID, limit}},
			{Resource: "task", Name: "get", Label: "Get Task", Method: "GET",
				Path: "/tasks/{taskGid}", ItemsPath: "data",
				Params: []schema.ParamSchema{taskGID}},
			{Resource: "task", Name: "create", Label: "Create Task", Method: "POST",
				Path: "/tasks", BodyParam: "body", ItemsPath: "data",
				Params: []schema.ParamSchema{body}},
			{Resource: "task", Name: "update", Label: "Update Task", Method: "PUT",
				Path: "/tasks/{taskGid}", BodyParam: "body", ItemsPath: "data",
				Params: []schema.ParamSchema{taskGID, body}},
			{Resource: "task", Name: "delete", Label: "Delete Task", Method: "DELETE",
				Path: "/tasks/{taskGid}", Params: []schema.ParamSchema{taskGID}},
			{Resource: "project", Name: "list", Label: "List Projects", Method: "GET",
				Path: "/projects", ItemsPath: "data",
				Query: map[string]string{"workspace": "workspaceGid", "limit": "limit"},
				Params: []schema.ParamSchema{workspaceGID, limit}},
			{Resource: "project", Name: "get", Label: "Get Project", Method: "GET",
				Path: "/projects/{projectGid}", ItemsPath: "data",
				Params: []schema.ParamSchema{projGID}},
			{Resource: "workspace", Name: "list", Label: "List Workspaces", Method: "GET",
				Path: "/workspaces", ItemsPath: "data"},
		},
	}
}

// ── ClickUp ───────────────────────────────────────────────────────────────────

func ClickUp() rest.Node {
	listID := sp("listId", "List ID", true)
	taskID := sp("taskId", "Task ID", true)
	spaceID := sp("spaceId", "Space ID", false)
	body := jp("body", "Body (JSON)")
	return rest.Node{
		Type: "productivity.clickup", Label: "ClickUp", Group: "integration", Icon: "LayoutList",
		Description: "Manage ClickUp tasks, lists, and spaces.",
		BaseURL:     "https://api.clickup.com/api/v2",
		CredType:    "clickUpApi",
		Auth:        rest.Auth{Kind: "header", Header: "Authorization", Prefix: "", ValueField: "accessToken"},
		Ops: []rest.Op{
			{Resource: "task", Name: "list", Label: "List Tasks", Method: "GET",
				Path: "/list/{listId}/task", ItemsPath: "tasks",
				Params: []schema.ParamSchema{listID}},
			{Resource: "task", Name: "get", Label: "Get Task", Method: "GET",
				Path: "/task/{taskId}", Params: []schema.ParamSchema{taskID}},
			{Resource: "task", Name: "create", Label: "Create Task", Method: "POST",
				Path: "/list/{listId}/task", BodyParam: "body",
				Params: []schema.ParamSchema{listID, body}},
			{Resource: "task", Name: "update", Label: "Update Task", Method: "PUT",
				Path: "/task/{taskId}", BodyParam: "body",
				Params: []schema.ParamSchema{taskID, body}},
			{Resource: "task", Name: "delete", Label: "Delete Task", Method: "DELETE",
				Path: "/task/{taskId}", Params: []schema.ParamSchema{taskID}},
			{Resource: "list", Name: "get", Label: "Get List", Method: "GET",
				Path: "/list/{listId}", Params: []schema.ParamSchema{listID}},
			{Resource: "space", Name: "listLists", Label: "Get Lists in Space", Method: "GET",
				Path: "/space/{spaceId}/list", ItemsPath: "lists",
				Params: []schema.ParamSchema{spaceID}},
		},
	}
}

// ── Jira Cloud (native) ───────────────────────────────────────────────────────
// Uses HTTP Basic auth: email:apiToken. The subdomain is part of the URL.

func Jira() schema.NodeDefinition {
	return schema.NodeDefinition{
		Type: "productivity.jira", Label: "Jira", Group: "integration", Icon: "Ticket",
		Description: "Manage Jira Cloud issues and projects (Basic auth: email + API token).",
		Inputs:      []schema.Port{{ID: "main"}},
		Outputs:     []schema.Port{{ID: "main"}},
		Credentials: []string{"jiraCloudApi"},
		Params: []schema.ParamSchema{
			{Name: "credential", Label: "Credential", Type: "credential", Required: true, CredentialType: "jiraCloudApi"},
			{Name: "operation", Label: "Operation", Type: "select", Required: true, Options: []schema.ParamOption{
				{Label: "issue: List (Search)", Value: "issue:list"},
				{Label: "issue: Get", Value: "issue:get"},
				{Label: "issue: Create", Value: "issue:create"},
				{Label: "issue: Update", Value: "issue:update"},
				{Label: "issue: Delete", Value: "issue:delete"},
				{Label: "project: List", Value: "project:list"},
				{Label: "project: Get", Value: "project:get"},
			}},
			{Name: "issueKey", Label: "Issue Key (e.g. PROJ-123)", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{"issue:get", "issue:update", "issue:delete"}}},
			{Name: "jql", Label: "JQL Query", Type: "string", Placeholder: "project = PROJ ORDER BY created DESC",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{"issue:list"}}},
			{Name: "maxResults", Label: "Max results", Type: "number", Default: 50,
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{"issue:list"}}},
			{Name: "body", Label: "Body (JSON)", Type: "json",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{"issue:create", "issue:update"}}},
			{Name: "projectKey", Label: "Project Key", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{"project:get"}}},
		},
		Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
			cred, err := ctx.Credential("credential")
			if err != nil {
				return schema.NodeResult{}, err
			}
			email, _ := cred["email"].(string)
			apiToken, _ := cred["apiToken"].(string)
			subdomain, _ := cred["subdomain"].(string)
			if email == "" || apiToken == "" || subdomain == "" {
				return schema.NodeResult{}, fmt.Errorf("jira: email, apiToken, and subdomain are required")
			}
			base := "https://" + subdomain + ".atlassian.net/rest/api/3"
			op := asString(ctx.Params["operation"], "")

			doJira := func(method, path string, body map[string]any) ([]schema.Item, error) {
				return jiraCall(email, apiToken, method, base+path, body)
			}

			switch op {
			case "issue:list":
				jql := asString(ctx.Params["jql"], "")
				max := asInt(ctx.Params["maxResults"], 50)
				q := url.Values{"jql": {jql}, "maxResults": {fmt.Sprint(max)}, "fields": {"*all"}}
				items, err := doJira("GET", "/search?"+q.Encode(), nil)
				return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, err
			case "issue:get":
				key := asString(ctx.Params["issueKey"], "")
				items, err := doJira("GET", "/issue/"+key, nil)
				return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, err
			case "issue:create":
				body := asObject(ctx.RawParam("body"))
				items, err := doJira("POST", "/issue", body)
				return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, err
			case "issue:update":
				key := asString(ctx.Params["issueKey"], "")
				body := asObject(ctx.RawParam("body"))
				items, err := doJira("PUT", "/issue/"+key, body)
				return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, err
			case "issue:delete":
				key := asString(ctx.Params["issueKey"], "")
				items, err := doJira("DELETE", "/issue/"+key, nil)
				return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, err
			case "project:list":
				items, err := doJira("GET", "/project", nil)
				return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, err
			case "project:get":
				key := asString(ctx.Params["projectKey"], "")
				items, err := doJira("GET", "/project/"+key, nil)
				return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, err
			default:
				return schema.NodeResult{}, fmt.Errorf("jira: unknown operation %q", op)
			}
		},
	}
}

func jiraCall(email, apiToken, method, apiURL string, body map[string]any) ([]schema.Item, error) {
	var reqBody io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, apiURL, reqBody)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(email, apiToken)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jira: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("jira: %d %s", resp.StatusCode, truncate(string(raw), 400))
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return []schema.Item{{JSON: map[string]any{"success": true}}}, nil
	}
	var root any
	if json.Unmarshal(raw, &root) != nil {
		return []schema.Item{{JSON: map[string]any{"raw": string(raw)}}}, nil
	}
	if m, ok := root.(map[string]any); ok {
		// Jira search wraps results in "issues"
		if issues, ok := m["issues"].([]any); ok {
			out := make([]schema.Item, 0, len(issues))
			for _, e := range issues {
				if im, ok := e.(map[string]any); ok {
					out = append(out, schema.Item{JSON: im})
				}
			}
			return out, nil
		}
		return []schema.Item{{JSON: m}}, nil
	}
	if arr, ok := root.([]any); ok {
		out := make([]schema.Item, 0, len(arr))
		for _, e := range arr {
			if m, ok := e.(map[string]any); ok {
				out = append(out, schema.Item{JSON: m})
			}
		}
		return out, nil
	}
	return []schema.Item{{JSON: map[string]any{"value": root}}}, nil
}

// ── Trello (native) ───────────────────────────────────────────────────────────
// Auth is via query params: ?key=K&token=T

func Trello() schema.NodeDefinition {
	return schema.NodeDefinition{
		Type: "productivity.trello", Label: "Trello", Group: "integration", Icon: "LayoutDashboard",
		Description: "Manage Trello boards, lists, and cards.",
		Inputs:      []schema.Port{{ID: "main"}},
		Outputs:     []schema.Port{{ID: "main"}},
		Credentials: []string{"trelloApi"},
		Params: []schema.ParamSchema{
			{Name: "credential", Label: "Credential", Type: "credential", Required: true, CredentialType: "trelloApi"},
			{Name: "operation", Label: "Operation", Type: "select", Required: true, Options: []schema.ParamOption{
				{Label: "card: List (in list)", Value: "card:list"},
				{Label: "card: Get", Value: "card:get"},
				{Label: "card: Create", Value: "card:create"},
				{Label: "card: Update", Value: "card:update"},
				{Label: "card: Delete", Value: "card:delete"},
				{Label: "board: List (my boards)", Value: "board:list"},
				{Label: "board: Get", Value: "board:get"},
				{Label: "list: List (in board)", Value: "list:list"},
			}},
			{Name: "cardId", Label: "Card ID", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{"card:get", "card:update", "card:delete"}}},
			{Name: "listId", Label: "List ID", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{"card:list", "card:create"}}},
			{Name: "boardId", Label: "Board ID", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{"board:get", "list:list"}}},
			{Name: "name", Label: "Card/Board Name", Type: "expression",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{"card:create", "card:update"}}},
			{Name: "desc", Label: "Description", Type: "expression",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{"card:create", "card:update"}}},
		},
		Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
			cred, err := ctx.Credential("credential")
			if err != nil {
				return schema.NodeResult{}, err
			}
			key, _ := cred["apiKey"].(string)
			token, _ := cred["accessToken"].(string)
			if key == "" || token == "" {
				return schema.NodeResult{}, fmt.Errorf("trello: apiKey and token are required")
			}
			auth := url.Values{"key": {key}, "token": {token}}
			op := asString(ctx.Params["operation"], "")

			trelloGET := func(path string, extra url.Values) ([]schema.Item, error) {
				q := url.Values{}
				for k, v := range auth {
					q[k] = v
				}
				for k, v := range extra {
					q[k] = v
				}
				return trelloReq("GET", "https://api.trello.com/1"+path+"?"+q.Encode(), nil)
			}
			trelloPOST := func(path string, form url.Values) ([]schema.Item, error) {
				for k, v := range auth {
					form[k] = v
				}
				req, err := http.NewRequest("POST", "https://api.trello.com/1"+path, strings.NewReader(form.Encode()))
				if err != nil {
					return nil, err
				}
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				return trelloDoReq(req)
			}
			trelloPUT := func(path string, form url.Values) ([]schema.Item, error) {
				for k, v := range auth {
					form[k] = v
				}
				req, err := http.NewRequest("PUT", "https://api.trello.com/1"+path, strings.NewReader(form.Encode()))
				if err != nil {
					return nil, err
				}
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				return trelloDoReq(req)
			}
			trelloDEL := func(path string) ([]schema.Item, error) {
				q := auth.Encode()
				return trelloReq("DELETE", "https://api.trello.com/1"+path+"?"+q, nil)
			}

			switch op {
			case "card:list":
				listID := asString(ctx.Params["listId"], "")
				items, err := trelloGET("/lists/"+listID+"/cards", nil)
				return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, err
			case "card:get":
				cardID := asString(ctx.Params["cardId"], "")
				items, err := trelloGET("/cards/"+cardID, nil)
				return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, err
			case "card:create":
				form := url.Values{
					"idList": {asString(ctx.Params["listId"], "")},
					"name":   {asString(ctx.Params["name"], "")},
					"desc":   {asString(ctx.Params["desc"], "")},
				}
				items, err := trelloPOST("/cards", form)
				return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, err
			case "card:update":
				cardID := asString(ctx.Params["cardId"], "")
				form := url.Values{
					"name": {asString(ctx.Params["name"], "")},
					"desc": {asString(ctx.Params["desc"], "")},
				}
				items, err := trelloPUT("/cards/"+cardID, form)
				return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, err
			case "card:delete":
				cardID := asString(ctx.Params["cardId"], "")
				items, err := trelloDEL("/cards/" + cardID)
				return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, err
			case "board:list":
				items, err := trelloGET("/members/me/boards", nil)
				return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, err
			case "board:get":
				boardID := asString(ctx.Params["boardId"], "")
				items, err := trelloGET("/boards/"+boardID, nil)
				return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, err
			case "list:list":
				boardID := asString(ctx.Params["boardId"], "")
				items, err := trelloGET("/boards/"+boardID+"/lists", nil)
				return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, err
			default:
				return schema.NodeResult{}, fmt.Errorf("trello: unknown operation %q", op)
			}
		},
	}
}

func trelloReq(method, apiURL string, body io.Reader) ([]schema.Item, error) {
	req, err := http.NewRequest(method, apiURL, body)
	if err != nil {
		return nil, err
	}
	return trelloDoReq(req)
}

func trelloDoReq(req *http.Request) ([]schema.Item, error) {
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("trello: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("trello: %d %s", resp.StatusCode, string(raw))
	}
	var root any
	if json.Unmarshal(raw, &root) != nil {
		return []schema.Item{{JSON: map[string]any{"raw": string(raw)}}}, nil
	}
	if arr, ok := root.([]any); ok {
		out := make([]schema.Item, 0, len(arr))
		for _, e := range arr {
			if m, ok := e.(map[string]any); ok {
				out = append(out, schema.Item{JSON: m})
			}
		}
		return out, nil
	}
	if m, ok := root.(map[string]any); ok {
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

func asInt(v any, def int) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	}
	return def
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

// ep is declared above but also needed for the unused variable lint check.
var _ = ep
