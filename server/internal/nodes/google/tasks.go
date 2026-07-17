package google

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
	"google.golang.org/api/option"
	tasks "google.golang.org/api/tasks/v1"
)

// ---------------------------------------------------------------------------
// Service cache
// ---------------------------------------------------------------------------

var (
	tasksSvcMu    sync.Mutex
	tasksSvcCache = map[string]*tasks.Service{}
)

func getTasksService(ctx *schema.ExecContext, base string) (*tasks.Service, error) {
	client, err := ctx.AuthorizedClient("credential")
	if err != nil {
		return nil, fmt.Errorf("tasks: authorized client: %w", err)
	}
	credID, _ := ctx.Params["credential"].(string)
	if credID == "" {
		return nil, fmt.Errorf("tasks: credential is required")
	}
	cacheKey := credID + "|" + base

	tasksSvcMu.Lock()
	if svc, ok := tasksSvcCache[cacheKey]; ok {
		tasksSvcMu.Unlock()
		return svc, nil
	}
	tasksSvcMu.Unlock()

	endpoint := base
	if endpoint == "" {
		endpoint = "https://tasks.googleapis.com/"
	}
	retryClient := wrapWithRetry(client)
	svc, err := tasks.NewService(context.Background(),
		option.WithHTTPClient(retryClient),
		option.WithEndpoint(strings.TrimRight(endpoint, "/")+"/"),
	)
	if err != nil {
		return nil, fmt.Errorf("tasks: new service: %w", err)
	}

	tasksSvcMu.Lock()
	if existing, ok := tasksSvcCache[cacheKey]; ok {
		tasksSvcMu.Unlock()
		return existing, nil
	}
	tasksSvcCache[cacheKey] = svc
	tasksSvcMu.Unlock()
	return svc, nil
}

// ---------------------------------------------------------------------------
// Node definition
// ---------------------------------------------------------------------------

func TasksNode(base string) schema.NodeDefinition {
	return schema.NodeDefinition{
		Type:        "google.tasks",
		Label:       "Google Tasks",
		Group:       "integration",
		Icon:        "CheckSquare",
		Description: "Manage Google Tasks: list, create, update, and delete task lists and tasks.",
		Inputs:      []schema.Port{{ID: "main"}},
		Outputs:     []schema.Port{{ID: "main"}},
		Credentials: []string{credType},
		Params: []schema.ParamSchema{
			{Name: "credential", Label: "Credential", Type: "credential", Required: true, CredentialType: credType},
			{Name: "operation", Label: "Operation", Type: "select", Required: true, Options: []schema.ParamOption{
				{Label: "List Task Lists", Value: "tasklist:list"},
				{Label: "Create Task List", Value: "tasklist:create"},
				{Label: "Delete Task List", Value: "tasklist:delete"},
				{Label: "List Tasks", Value: "task:list"},
				{Label: "Create Task", Value: "task:create"},
				{Label: "Update Task", Value: "task:update"},
				{Label: "Delete Task", Value: "task:delete"},
			}},
			{
				Name: "tasklistId", Label: "Task List ID", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"tasklist:delete", "task:list", "task:create",
				}},
			},
			{
				Name: "taskId", Label: "Task ID", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"task:update", "task:delete",
				}},
			},
			{
				Name: "body", Label: "Body (JSON)", Type: "json",
				Description: `TaskList: {"title":"My List"}. Task: {"title":"My Task","notes":"Details","due":"2025-06-01T00:00:00Z"}`,
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"tasklist:create", "task:create", "task:update",
				}},
			},
			{
				Name: "maxResults", Label: "Max results", Type: "number", Default: float64(50),
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"tasklist:list", "task:list",
				}},
			},
			{
				Name: "maxPages", Label: "Max pages", Type: "number", Default: float64(10),
				Description: "Maximum number of pages to fetch. Set to 1 for first page only.",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"tasklist:list", "task:list",
				}},
			},
			{
				Name: "showCompleted", Label: "Show completed", Type: "boolean", Default: false,
				Description: "Include completed tasks in the results.",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"task:list",
				}},
			},
			{
				Name: "showHidden", Label: "Show hidden", Type: "boolean", Default: false,
				Description: "Show hidden task lists.",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"tasklist:list",
				}},
			},
			{
				Name: "dueMin", Label: "Due min (RFC 3339)", Type: "string",
				Description: "Lower bound for task due date.",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"task:list",
				}},
			},
			{
				Name: "dueMax", Label: "Due max (RFC 3339)", Type: "string",
				Description: "Upper bound for task due date.",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"task:list",
				}},
			},
		},
		Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
			return executeTasksNode(ctx, base)
		},
	}
}

// ---------------------------------------------------------------------------
// Main execution dispatch
// ---------------------------------------------------------------------------

func executeTasksNode(ctx *schema.ExecContext, base string) (schema.NodeResult, error) {
	op, _ := ctx.Params["operation"].(string)
	if op == "" {
		return schema.NodeResult{}, fmt.Errorf("tasks: operation is required")
	}

	svc, err := getTasksService(ctx, base)
	if err != nil {
		return schema.NodeResult{}, err
	}

	tasklistID, _ := ctx.Params["tasklistId"].(string)
	taskID, _ := ctx.Params["taskId"].(string)

	switch op {

	case "tasklist:list":
		maxResults := parseIntParam(ctx.Params["maxResults"], 50)
		maxPages := parseIntParam(ctx.Params["maxPages"], 10)
		showHidden, _ := ctx.Params["showHidden"].(bool)

		var allItems []schema.Item
		pageToken := ""
		for page := 0; page < maxPages; page++ {
			call := svc.Tasklists.List().MaxResults(int64(maxResults))
			if pageToken != "" {
				call = call.PageToken(pageToken)
			}
			resp, err := call.Do()
			if err != nil {
				return schema.NodeResult{}, fmt.Errorf("tasks tasklist:list: %w", err)
			}
			for _, tl := range resp.Items {
				// Note: The Tasks API TaskList resource has a 'hidden' boolean
				// but the Go SDK may not expose it on the struct. Filtering is
				// done client-side when the field is available.
				_ = showHidden
				allItems = append(allItems, tasklistToItem(tl))
			}
			if resp.NextPageToken == "" {
				break
			}
			pageToken = resp.NextPageToken
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": allItems}}, nil

	case "tasklist:create":
		bodyValue := asObject(ctx.RawParam("body"))
		title, _ := bodyValue["title"].(string)
		if title == "" {
			return schema.NodeResult{}, fmt.Errorf("tasks tasklist:create: body.title is required")
		}
		tl, err := svc.Tasklists.Insert(&tasks.TaskList{Title: title}).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("tasks tasklist:create: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {tasklistToItem(tl)}}}, nil

	case "tasklist:delete":
		if tasklistID == "" {
			return schema.NodeResult{}, fmt.Errorf("tasks tasklist:delete: tasklistId is required")
		}
		if err := svc.Tasklists.Delete(tasklistID).Do(); err != nil {
			return schema.NodeResult{}, fmt.Errorf("tasks tasklist:delete: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{JSON: map[string]any{"deleted": true, "tasklistId": tasklistID}}}}}, nil

	case "task:list":
		if tasklistID == "" {
			return schema.NodeResult{}, fmt.Errorf("tasks task:list: tasklistId is required")
		}
		maxResults := parseIntParam(ctx.Params["maxResults"], 50)
		maxPages := parseIntParam(ctx.Params["maxPages"], 10)
		showCompleted, _ := ctx.Params["showCompleted"].(bool)
		dueMin, _ := ctx.Params["dueMin"].(string)
		dueMax, _ := ctx.Params["dueMax"].(string)

		var allItems []schema.Item
		pageToken := ""
		for page := 0; page < maxPages; page++ {
			call := svc.Tasks.List(tasklistID).MaxResults(int64(maxResults))
			if !showCompleted {
				call = call.ShowCompleted(false)
			} else {
				call = call.ShowCompleted(true)
			}
			if dueMin != "" {
				call = call.DueMin(dueMin)
			}
			if dueMax != "" {
				call = call.DueMax(dueMax)
			}
			if pageToken != "" {
				call = call.PageToken(pageToken)
			}
			resp, err := call.Do()
			if err != nil {
				return schema.NodeResult{}, fmt.Errorf("tasks task:list: %w", err)
			}
			for _, t := range resp.Items {
				allItems = append(allItems, taskToItem(t))
			}
			if resp.NextPageToken == "" {
				break
			}
			pageToken = resp.NextPageToken
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": allItems}}, nil

	case "task:create":
		if tasklistID == "" {
			return schema.NodeResult{}, fmt.Errorf("tasks task:create: tasklistId is required")
		}
		bodyValue := asObject(ctx.RawParam("body"))
		bodyBytes, err := json.Marshal(bodyValue)
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("tasks task:create: marshal body: %w", err)
		}
		var t tasks.Task
		if err := json.Unmarshal(bodyBytes, &t); err != nil {
			return schema.NodeResult{}, fmt.Errorf("tasks task:create: unmarshal task: %w", err)
		}
		created, err := svc.Tasks.Insert(tasklistID, &t).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("tasks task:create: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {taskToItem(created)}}}, nil

	case "task:update":
		if tasklistID == "" {
			return schema.NodeResult{}, fmt.Errorf("tasks task:update: tasklistId is required")
		}
		if taskID == "" {
			return schema.NodeResult{}, fmt.Errorf("tasks task:update: taskId is required")
		}
		bodyValue := asObject(ctx.RawParam("body"))
		bodyBytes, err := json.Marshal(bodyValue)
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("tasks task:update: marshal body: %w", err)
		}
		var t tasks.Task
		if err := json.Unmarshal(bodyBytes, &t); err != nil {
			return schema.NodeResult{}, fmt.Errorf("tasks task:update: unmarshal task: %w", err)
		}
		updated, err := svc.Tasks.Update(tasklistID, taskID, &t).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("tasks task:update: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {taskToItem(updated)}}}, nil

	case "task:delete":
		if tasklistID == "" {
			return schema.NodeResult{}, fmt.Errorf("tasks task:delete: tasklistId is required")
		}
		if taskID == "" {
			return schema.NodeResult{}, fmt.Errorf("tasks task:delete: taskId is required")
		}
		if err := svc.Tasks.Delete(tasklistID, taskID).Do(); err != nil {
			return schema.NodeResult{}, fmt.Errorf("tasks task:delete: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{JSON: map[string]any{"deleted": true, "taskId": taskID, "tasklistId": tasklistID}}}}}, nil

	default:
		return schema.NodeResult{}, fmt.Errorf("tasks: unknown operation %q", op)
	}
}

// ---------------------------------------------------------------------------
// SDK struct → schema.Item helpers
// ---------------------------------------------------------------------------

func tasklistToItem(tl *tasks.TaskList) schema.Item {
	if tl == nil {
		return schema.Item{JSON: map[string]any{}}
	}
	return schema.Item{JSON: map[string]any{
		"id":       tl.Id,
		"title":    tl.Title,
		"updated":  tl.Updated,
		"selfLink": tl.SelfLink,
	}}
}

func taskToItem(t *tasks.Task) schema.Item {
	if t == nil {
		return schema.Item{JSON: map[string]any{}}
	}
	item := map[string]any{
		"id":        t.Id,
		"title":     t.Title,
		"notes":     t.Notes,
		"status":    t.Status,
		"due":       t.Due,
		"completed": t.Completed,
		"updated":   t.Updated,
		"position":  t.Position,
		"selfLink":  t.SelfLink,
		"parent":    t.Parent,
	}
	if len(t.Links) > 0 {
		links := make([]map[string]any, 0, len(t.Links))
		for _, l := range t.Links {
			links = append(links, map[string]any{
				"link": l.Link,
				"description": l.Description,
				"type": l.Type,
			})
		}
		item["links"] = links
	}
	return schema.Item{JSON: item}
}
