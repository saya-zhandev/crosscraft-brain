package google

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
	chat "google.golang.org/api/chat/v1"
	"google.golang.org/api/option"
)

// ---------------------------------------------------------------------------
// Service cache
// ---------------------------------------------------------------------------

var (
	chatSvcMu    sync.Mutex
	chatSvcCache = map[string]*chat.Service{}
)

func getChatService(ctx *schema.ExecContext, base string) (*chat.Service, error) {
	client, err := ctx.AuthorizedClient("credential")
	if err != nil {
		return nil, fmt.Errorf("chat: authorized client: %w", err)
	}
	credID, _ := ctx.Params["credential"].(string)
	if credID == "" {
		return nil, fmt.Errorf("chat: credential is required")
	}
	cacheKey := credID + "|" + base

	chatSvcMu.Lock()
	if svc, ok := chatSvcCache[cacheKey]; ok {
		chatSvcMu.Unlock()
		return svc, nil
	}
	chatSvcMu.Unlock()

	endpoint := base
	if endpoint == "" {
		endpoint = "https://chat.googleapis.com/"
	}
	retryClient := wrapWithRetry(client)
	svc, err := chat.NewService(context.Background(),
		option.WithHTTPClient(retryClient),
		option.WithEndpoint(strings.TrimRight(endpoint, "/")+"/"),
	)
	if err != nil {
		return nil, fmt.Errorf("chat: new service: %w", err)
	}

	chatSvcMu.Lock()
	if existing, ok := chatSvcCache[cacheKey]; ok {
		chatSvcMu.Unlock()
		return existing, nil
	}
	chatSvcCache[cacheKey] = svc
	chatSvcMu.Unlock()
	return svc, nil
}

// ---------------------------------------------------------------------------
// Node definition
// ---------------------------------------------------------------------------

func ChatNode(base string) schema.NodeDefinition {
	return schema.NodeDefinition{
		Type:        "google.chat",
		Label:       "Google Chat",
		Group:       "integration",
		Icon:        "MessageCircle",
		Description: "Send messages and manage Google Chat spaces and members.",
		Inputs:      []schema.Port{{ID: "main"}},
		Outputs:     []schema.Port{{ID: "main"}},
		Credentials: []string{credType},
		Params: []schema.ParamSchema{
			{Name: "credential", Label: "Credential", Type: "credential", Required: true, CredentialType: credType},
			{Name: "operation", Label: "Operation", Type: "select", Required: true, Options: []schema.ParamOption{
				{Label: "List Spaces", Value: "space:list"},
				{Label: "Get Space", Value: "space:get"},
				{Label: "List Members", Value: "member:list"},
				{Label: "Send Message", Value: "message:send"},
			}},
			{
				Name: "spaceName", Label: "Space name or ID", Type: "string",
				Description: "The space name (e.g. 'spaces/AAA') or just the ID part ('AAA').",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"space:get", "member:list", "message:send",
				}},
			},
			{
				Name: "text", Label: "Message text", Type: "string",
				Description: "Plain text message content.",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"message:send",
				}},
			},
			{
				Name: "pageSize", Label: "Page size", Type: "number", Default: float64(50),
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"space:list", "member:list",
				}},
			},
			{
				Name: "maxPages", Label: "Max pages", Type: "number", Default: float64(10),
				Description: "Maximum number of pages to fetch. Set to 1 for first page only.",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"space:list", "member:list",
				}},
			},
		},
		Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
			return executeChatNode(ctx, base)
		},
	}
}

// ---------------------------------------------------------------------------
// Main execution dispatch
// ---------------------------------------------------------------------------

func executeChatNode(ctx *schema.ExecContext, base string) (schema.NodeResult, error) {
	op, _ := ctx.Params["operation"].(string)
	if op == "" {
		return schema.NodeResult{}, fmt.Errorf("chat: operation is required")
	}

	svc, err := getChatService(ctx, base)
	if err != nil {
		return schema.NodeResult{}, err
	}

	spaceName, _ := ctx.Params["spaceName"].(string)
	// Normalise: if user provides just the ID, prefix with "spaces/".
	if spaceName != "" && !strings.HasPrefix(spaceName, "spaces/") {
		spaceName = "spaces/" + spaceName
	}

	switch op {

	case "space:list":
		pageSize := parseIntParam(ctx.Params["pageSize"], 50)
		maxPages := parseIntParam(ctx.Params["maxPages"], 10)

		var allItems []schema.Item
		pageToken := ""
		for page := 0; page < maxPages; page++ {
			call := svc.Spaces.List().PageSize(int64(pageSize))
			if pageToken != "" {
				call = call.PageToken(pageToken)
			}
			resp, err := call.Do()
			if err != nil {
				return schema.NodeResult{}, fmt.Errorf("chat space:list: %w", err)
			}
			for _, s := range resp.Spaces {
				allItems = append(allItems, spaceToItem(s))
			}
			if resp.NextPageToken == "" {
				break
			}
			pageToken = resp.NextPageToken
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": allItems}}, nil

	case "space:get":
		if spaceName == "" {
			return schema.NodeResult{}, fmt.Errorf("chat space:get: spaceName is required")
		}
		s, err := svc.Spaces.Get(spaceName).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("chat space:get: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {spaceToItem(s)}}}, nil

	case "member:list":
		if spaceName == "" {
			return schema.NodeResult{}, fmt.Errorf("chat member:list: spaceName is required")
		}
		pageSize := parseIntParam(ctx.Params["pageSize"], 50)
		maxPages := parseIntParam(ctx.Params["maxPages"], 10)

		var allItems []schema.Item
		pageToken := ""
		for page := 0; page < maxPages; page++ {
			call := svc.Spaces.Members.List(spaceName).PageSize(int64(pageSize))
			if pageToken != "" {
				call = call.PageToken(pageToken)
			}
			resp, err := call.Do()
			if err != nil {
				return schema.NodeResult{}, fmt.Errorf("chat member:list: %w", err)
			}
			for _, m := range resp.Memberships {
				allItems = append(allItems, membershipToItem(m))
			}
			if resp.NextPageToken == "" {
				break
			}
			pageToken = resp.NextPageToken
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": allItems}}, nil

	case "message:send":
		if spaceName == "" {
			return schema.NodeResult{}, fmt.Errorf("chat message:send: spaceName is required")
		}
		text, _ := ctx.Params["text"].(string)
		if text == "" {
			return schema.NodeResult{}, fmt.Errorf("chat message:send: text is required")
		}
		msg := &chat.Message{
			Text: text,
		}
		sent, err := svc.Spaces.Messages.Create(spaceName, msg).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("chat message:send: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {chatMessageToItem(sent)}}}, nil

	default:
		return schema.NodeResult{}, fmt.Errorf("chat: unknown operation %q", op)
	}
}

// ---------------------------------------------------------------------------
// SDK struct → schema.Item helpers
// ---------------------------------------------------------------------------

func spaceToItem(s *chat.Space) schema.Item {
	if s == nil {
		return schema.Item{JSON: map[string]any{}}
	}
	item := map[string]any{
		"name":        s.Name,
		"displayName": s.DisplayName,
		"type":        s.Type,
		"singleUserBotDm": s.SingleUserBotDm,
		"threaded":    s.Threaded,
	}
	if s.SpaceThreadingState != "" {
		item["spaceThreadingState"] = s.SpaceThreadingState
	}
	return schema.Item{JSON: item}
}

func membershipToItem(m *chat.Membership) schema.Item {
	if m == nil {
		return schema.Item{JSON: map[string]any{}}
	}
	item := map[string]any{
		"name":  m.Name,
		"state": m.State,
		"role":  m.Role,
	}
	if m.Member != nil {
		item["member"] = map[string]any{
			"name":        m.Member.Name,
			"displayName": m.Member.DisplayName,
			"domainId":    m.Member.DomainId,
			"type":        m.Member.Type,
		}
	}
	if m.GroupMember != nil {
		item["groupMember"] = map[string]any{
			"name": m.GroupMember.Name,
		}
	}
	return schema.Item{JSON: item}
}

func chatMessageToItem(m *chat.Message) schema.Item {
	if m == nil {
		return schema.Item{JSON: map[string]any{}}
	}
	item := map[string]any{
		"name":   m.Name,
		"text":   m.Text,
	}
	if m.Sender != nil {
		item["sender"] = map[string]any{
			"name":        m.Sender.Name,
			"displayName": m.Sender.DisplayName,
			"domainId":    m.Sender.DomainId,
			"type":        m.Sender.Type,
		}
	}
	if m.CreateTime != "" {
		item["createTime"] = m.CreateTime
	}
	if m.Thread != nil {
		item["thread"] = map[string]any{
			"name": m.Thread.Name,
		}
	}
	if m.Space != nil {
		item["space"] = map[string]any{
			"name":        m.Space.Name,
			"displayName": m.Space.DisplayName,
			"type":        m.Space.Type,
		}
	}
	return schema.Item{JSON: item}
}
