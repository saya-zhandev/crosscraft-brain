package google

import (
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"strings"
	"sync"
	"time"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

// ---------------------------------------------------------------------------
// Service cache
// ---------------------------------------------------------------------------

var (
	gmailSvcMu    sync.Mutex
	gmailSvcCache = map[string]*gmail.Service{}
)

func getGmailService(ctx *schema.ExecContext, base string) (*gmail.Service, error) {
	client, err := ctx.AuthorizedClient("credential")
	if err != nil {
		return nil, fmt.Errorf("gmail: authorized client: %w", err)
	}
	credID, _ := ctx.Params["credential"].(string)
	cacheKey := credID + "|" + base

	gmailSvcMu.Lock()
	if svc, ok := gmailSvcCache[cacheKey]; ok {
		gmailSvcMu.Unlock()
		return svc, nil
	}
	gmailSvcMu.Unlock()

	endpoint := base
	if endpoint == "" {
		endpoint = "https://gmail.googleapis.com/"
	}
	retryClient := wrapWithRetry(client)
	svc, err := gmail.NewService(context.Background(),
		option.WithHTTPClient(retryClient),
		option.WithEndpoint(strings.TrimRight(endpoint, "/")+"/"),
	)
	if err != nil {
		return nil, fmt.Errorf("gmail: new service: %w", err)
	}

	gmailSvcMu.Lock()
	if existing, ok := gmailSvcCache[cacheKey]; ok {
		gmailSvcMu.Unlock()
		return existing, nil
	}
	gmailSvcCache[cacheKey] = svc
	gmailSvcMu.Unlock()
	return svc, nil
}

// ---------------------------------------------------------------------------
// Node definition
// ---------------------------------------------------------------------------

func GmailNode(base string) schema.NodeDefinition {
	return schema.NodeDefinition{
		Type:        "google.gmail",
		Label:       "Gmail",
		Group:       "integration",
		Icon:        "Mail",
		Description: "Read, send, and manage Gmail — messages, drafts, threads, labels, and a new-email trigger.",
		Inputs:      []schema.Port{{ID: "main"}},
		Outputs:     []schema.Port{{ID: "main"}},
		Credentials: []string{credType},
		IsTrigger:   true,
		Params: []schema.ParamSchema{
			{Name: "credential", Label: "Credential", Type: "credential", Required: true, CredentialType: credType},
			{Name: "operation", Label: "Operation", Type: "select", Required: true, Options: []schema.ParamOption{
				{Label: "List Messages", Value: "message:list"},
				{Label: "Get Message", Value: "message:get"},
				{Label: "Send Email", Value: "message:send"},
				{Label: "Reply to Email", Value: "message:reply"},
				{Label: "Modify Message", Value: "message:modify"},
				{Label: "Create Draft", Value: "draft:create"},
				{Label: "Update Draft", Value: "draft:update"},
				{Label: "Delete Draft", Value: "draft:delete"},
				{Label: "Get Draft", Value: "draft:get"},
				{Label: "List Drafts", Value: "draft:list"},
				{Label: "Get Thread", Value: "thread:get"},
				{Label: "List Threads", Value: "thread:list"},
				{Label: "Modify Thread", Value: "thread:modify"},
				{Label: "List Labels", Value: "label:list"},
				{Label: "Create Label", Value: "label:create"},
				{Label: "Update Label", Value: "label:update"},
				{Label: "Delete Label", Value: "label:delete"},
				{Label: "Trigger: New Email", Value: "trigger:newEmail"},
			}},
			{
				Name: "messageId", Label: "Message ID", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"message:get", "message:modify", "message:reply",
				}},
			},
			{
				Name: "threadId", Label: "Thread ID", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"thread:get", "thread:modify",
				}},
			},
			{
				Name: "draftId", Label: "Draft ID", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"draft:get", "draft:update", "draft:delete",
				}},
			},
			{
				Name: "labelId", Label: "Label ID", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"label:update", "label:delete",
				}},
			},
			{
				Name: "q", Label: "Search query", Type: "string", Placeholder: "from:me is:unread",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"message:list", "thread:list", "draft:list", "trigger:newEmail",
				}},
			},
			{
				Name: "maxResults", Label: "Max results", Type: "number", Default: float64(25),
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"message:list", "thread:list", "draft:list",
				}},
			},
			{
				Name: "maxPages", Label: "Max pages", Type: "number", Default: float64(10),
				Description: "Maximum number of pages to fetch (paginated APIs). Set to 1 for first page only.",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"message:list", "thread:list", "draft:list",
				}},
			},
			{
				Name: "to", Label: "To (comma-separated)", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"message:send", "message:reply", "draft:create", "draft:update",
				}},
			},
			{
				Name: "cc", Label: "CC (comma-separated)", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"message:send", "draft:create", "draft:update",
				}},
			},
			{
				Name: "bcc", Label: "BCC (comma-separated)", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"message:send", "draft:create", "draft:update",
				}},
			},
			{
				Name: "subject", Label: "Subject", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"message:send", "message:reply", "draft:create", "draft:update",
				}},
			},
			{
				Name: "body", Label: "Body", Type: "code", Description: "Plain text or HTML body.",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"message:send", "message:reply", "draft:create", "draft:update",
				}},
			},
			{
				Name: "contentType", Label: "Content type", Type: "select", Default: "text/plain", Options: []schema.ParamOption{
					{Label: "Plain text", Value: "text/plain"},
					{Label: "HTML", Value: "text/html"},
				},
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"message:send", "message:reply", "draft:create", "draft:update",
				}},
			},
			{
				Name: "attachments", Label: "Attachments (JSON)", Type: "json",
				Description: `[{"filename":"a.pdf","data":"<base64>","mimeType":"application/pdf"}]`,
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"message:send", "draft:create", "draft:update",
				}},
			},
			{
				Name: "addLabelIds", Label: "Add label IDs (comma-separated)", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"message:modify", "thread:modify",
				}},
			},
			{
				Name: "removeLabelIds", Label: "Remove label IDs (comma-separated)", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"message:modify", "thread:modify",
				}},
			},
			{
				Name: "labelName", Label: "Label name", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"label:create", "label:update",
				}},
			},
			{
				Name: "labelListVisibility", Label: "Label list visibility", Type: "select", Default: "labelShow", Options: []schema.ParamOption{
					{Label: "Show", Value: "labelShow"},
					{Label: "Show if unread", Value: "labelShowIfUnread"},
					{Label: "Hide", Value: "labelHide"},
				},
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"label:create", "label:update",
				}},
			},
			{
				Name: "pollSeconds", Label: "Poll seconds", Type: "number", Default: float64(60),
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"trigger:newEmail",
				}},
			},
		},
		Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
			return executeGmailNode(ctx, base)
		},
	}
}

// ---------------------------------------------------------------------------
// Main execution dispatch
// ---------------------------------------------------------------------------

func executeGmailNode(ctx *schema.ExecContext, base string) (schema.NodeResult, error) {
	op, _ := ctx.Params["operation"].(string)
	if op == "" {
		return schema.NodeResult{}, fmt.Errorf("gmail: operation is required")
	}

	svc, err := getGmailService(ctx, base)
	if err != nil {
		return schema.NodeResult{}, err
	}

	userID := "me"
	msgID, _ := ctx.Params["messageId"].(string)
	threadID, _ := ctx.Params["threadId"].(string)
	draftID, _ := ctx.Params["draftId"].(string)
	labelID, _ := ctx.Params["labelId"].(string)
	searchQ, _ := ctx.Params["q"].(string)

	switch op {

	case "message:list":
		max := parseIntParam(ctx.Params["maxResults"], 25)
		maxPages := parseIntParam(ctx.Params["maxPages"], 10)
		var allItems []schema.Item
		pageToken := ""
		for page := 0; page < maxPages; page++ {
			call := svc.Users.Messages.List(userID).MaxResults(int64(max))
			if searchQ != "" {
				call = call.Q(searchQ)
			}
			if pageToken != "" {
				call = call.PageToken(pageToken)
			}
			resp, err := call.Do()
			if err != nil {
				return schema.NodeResult{}, fmt.Errorf("gmail message:list: %w", err)
			}
			for _, m := range resp.Messages {
				allItems = append(allItems, schema.Item{JSON: map[string]any{
					"id":        m.Id,
					"threadId":  m.ThreadId,
					"labelIds":  m.LabelIds,
					"snippet":   m.Snippet,
					"historyId": m.HistoryId,
				}})
			}
			if resp.NextPageToken == "" {
				break
			}
			pageToken = resp.NextPageToken
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": allItems}}, nil

	case "message:get":
		if msgID == "" {
			return schema.NodeResult{}, fmt.Errorf("gmail message:get: messageId is required")
		}
		msg, err := svc.Users.Messages.Get(userID, msgID).Format("full").Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("gmail message:get: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {messageToItem(msg)}}}, nil

	case "message:send":
		return sendOrDraft(ctx, svc, userID, msgID, nil, composeSend)

	case "message:reply":
		if msgID == "" {
			return schema.NodeResult{}, fmt.Errorf("gmail message:reply: messageId is required")
		}
		orig, err := svc.Users.Messages.Get(userID, msgID).Format("metadata").MetadataHeaders("Subject", "Message-ID", "References", "From", "To").Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("gmail message:reply: fetch original: %w", err)
		}
		return sendOrDraft(ctx, svc, userID, msgID, orig, composeReply)

	case "message:modify":
		if msgID == "" {
			return schema.NodeResult{}, fmt.Errorf("gmail message:modify: messageId is required")
		}
		addIDs := splitCSV(ctx.Params, "addLabelIds")
		removeIDs := splitCSV(ctx.Params, "removeLabelIds")
		req := &gmail.ModifyMessageRequest{
			AddLabelIds:    addIDs,
			RemoveLabelIds: removeIDs,
		}
		msg, err := svc.Users.Messages.Modify(userID, msgID, req).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("gmail message:modify: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {messageToItem(msg)}}}, nil

	case "draft:create":
		return sendOrDraft(ctx, svc, userID, "", nil, composeDraft)

	case "draft:update":
		if draftID == "" {
			return schema.NodeResult{}, fmt.Errorf("gmail draft:update: draftId is required")
		}
		return sendOrDraft(ctx, svc, userID, "", nil, composeDraftUpdate)

	case "draft:delete":
		if draftID == "" {
			return schema.NodeResult{}, fmt.Errorf("gmail draft:delete: draftId is required")
		}
		if err := svc.Users.Drafts.Delete(userID, draftID).Do(); err != nil {
			return schema.NodeResult{}, fmt.Errorf("gmail draft:delete: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{JSON: map[string]any{"deleted": true, "draftId": draftID}}}}}, nil

	case "draft:get":
		if draftID == "" {
			return schema.NodeResult{}, fmt.Errorf("gmail draft:get: draftId is required")
		}
		draft, err := svc.Users.Drafts.Get(userID, draftID).Format("full").Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("gmail draft:get: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {draftToItem(draft)}}}, nil

	case "draft:list":
		max := parseIntParam(ctx.Params["maxResults"], 25)
		maxPages := parseIntParam(ctx.Params["maxPages"], 10)
		var allItems []schema.Item
		pageToken := ""
		for page := 0; page < maxPages; page++ {
			call := svc.Users.Drafts.List(userID).MaxResults(int64(max))
			if searchQ != "" {
				call = call.Q(searchQ)
			}
			if pageToken != "" {
				call = call.PageToken(pageToken)
			}
			resp, err := call.Do()
			if err != nil {
				return schema.NodeResult{}, fmt.Errorf("gmail draft:list: %w", err)
			}
			for _, d := range resp.Drafts {
				allItems = append(allItems, schema.Item{JSON: map[string]any{
					"id":      d.Id,
					"message": messageToItem(d.Message),
				}})
			}
			if resp.NextPageToken == "" {
				break
			}
			pageToken = resp.NextPageToken
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": allItems}}, nil

	case "thread:get":
		if threadID == "" {
			return schema.NodeResult{}, fmt.Errorf("gmail thread:get: threadId is required")
		}
		thread, err := svc.Users.Threads.Get(userID, threadID).Format("full").Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("gmail thread:get: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {threadToItem(thread)}}}, nil

	case "thread:list":
		max := parseIntParam(ctx.Params["maxResults"], 25)
		maxPages := parseIntParam(ctx.Params["maxPages"], 10)
		var allItems []schema.Item
		pageToken := ""
		for page := 0; page < maxPages; page++ {
			call := svc.Users.Threads.List(userID).MaxResults(int64(max))
			if searchQ != "" {
				call = call.Q(searchQ)
			}
			if pageToken != "" {
				call = call.PageToken(pageToken)
			}
			resp, err := call.Do()
			if err != nil {
				return schema.NodeResult{}, fmt.Errorf("gmail thread:list: %w", err)
			}
			for _, t := range resp.Threads {
				allItems = append(allItems, schema.Item{JSON: map[string]any{
					"id":           t.Id,
					"snippet":      t.Snippet,
					"historyId":    t.HistoryId,
					"messageCount": len(t.Messages),
				}})
			}
			if resp.NextPageToken == "" {
				break
			}
			pageToken = resp.NextPageToken
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": allItems}}, nil

	case "thread:modify":
		if threadID == "" {
			return schema.NodeResult{}, fmt.Errorf("gmail thread:modify: threadId is required")
		}
		addIDs := splitCSV(ctx.Params, "addLabelIds")
		removeIDs := splitCSV(ctx.Params, "removeLabelIds")
		req := &gmail.ModifyThreadRequest{
			AddLabelIds:    addIDs,
			RemoveLabelIds: removeIDs,
		}
		thread, err := svc.Users.Threads.Modify(userID, threadID, req).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("gmail thread:modify: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {threadToItem(thread)}}}, nil

	case "label:list":
		resp, err := svc.Users.Labels.List(userID).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("gmail label:list: %w", err)
		}
		out := make([]schema.Item, 0, len(resp.Labels))
		for _, l := range resp.Labels {
			out = append(out, labelToItem(l))
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil

	case "label:create":
		name, _ := ctx.Params["labelName"].(string)
		if name == "" {
			return schema.NodeResult{}, fmt.Errorf("gmail label:create: labelName is required")
		}
		vis, _ := ctx.Params["labelListVisibility"].(string)
		label, err := svc.Users.Labels.Create(userID, &gmail.Label{
			Name:                  name,
			LabelListVisibility:   vis,
			MessageListVisibility: "show",
		}).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("gmail label:create: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {labelToItem(label)}}}, nil

	case "label:update":
		if labelID == "" {
			return schema.NodeResult{}, fmt.Errorf("gmail label:update: labelId is required")
		}
		name, _ := ctx.Params["labelName"].(string)
		vis, _ := ctx.Params["labelListVisibility"].(string)
		label, err := svc.Users.Labels.Update(userID, labelID, &gmail.Label{
			Name:                name,
			LabelListVisibility: vis,
		}).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("gmail label:update: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {labelToItem(label)}}}, nil

	case "label:delete":
		if labelID == "" {
			return schema.NodeResult{}, fmt.Errorf("gmail label:delete: labelId is required")
		}
		if err := svc.Users.Labels.Delete(userID, labelID).Do(); err != nil {
			return schema.NodeResult{}, fmt.Errorf("gmail label:delete: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{JSON: map[string]any{"deleted": true, "labelId": labelID}}}}}, nil

	case "trigger:newEmail":
		return executeGmailTrigger(ctx, svc, userID, searchQ, base)

	default:
		return schema.NodeResult{}, fmt.Errorf("gmail: unknown operation %q", op)
	}
}

// ---------------------------------------------------------------------------
// Compose actions (send / reply / draft / draftUpdate)
// ---------------------------------------------------------------------------

// composeAction is the type-safe discriminator for sendOrDraft.
type composeAction string

const (
	composeSend        composeAction = "send"
	composeReply       composeAction = "reply"
	composeDraft       composeAction = "draft"
	composeDraftUpdate composeAction = "draftUpdate"
)

func sendOrDraft(ctx *schema.ExecContext, svc *gmail.Service, userID, msgID string, orig *gmail.Message, action composeAction) (schema.NodeResult, error) {
	to, _ := ctx.Params["to"].(string)
	cc, _ := ctx.Params["cc"].(string)
	bcc, _ := ctx.Params["bcc"].(string)
	subject, _ := ctx.Params["subject"].(string)
	body, _ := ctx.Params["body"].(string)
	contentType, _ := ctx.Params["contentType"].(string)
	if contentType == "" {
		contentType = "text/plain"
	}
	attachments := asObject(ctx.RawParam("attachments"))

	if action == composeReply && orig != nil {
		for _, h := range orig.Payload.Headers {
			if h.Name == "Subject" {
				s := h.Value
				if !strings.HasPrefix(strings.ToLower(s), "re:") {
					s = "Re: " + s
				}
				if subject == "" {
					subject = s
				}
				break
			}
		}
	}

	if action != composeDraftUpdate && to == "" && action != composeReply {
		return schema.NodeResult{}, fmt.Errorf("gmail %s: 'to' is required", action)
	}

	raw, err := buildMIMEMessage(ctx, to, cc, bcc, subject, body, contentType, attachments, orig, string(action))
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("gmail %s: build message: %w", action, err)
	}

	msg := &gmail.Message{Raw: raw}
	if action == composeReply && orig != nil {
		msg.ThreadId = orig.ThreadId
	}

	switch action {
	case composeSend:
		sent, err := svc.Users.Messages.Send(userID, msg).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("gmail message:send: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {messageToItem(sent)}}}, nil

	case composeReply:
		sent, err := svc.Users.Messages.Send(userID, msg).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("gmail message:reply: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {messageToItem(sent)}}}, nil

	case composeDraft:
		draft, err := svc.Users.Drafts.Create(userID, &gmail.Draft{Message: msg}).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("gmail draft:create: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {draftToItem(draft)}}}, nil

	case composeDraftUpdate:
		draftID, _ := ctx.Params["draftId"].(string)
		draft, err := svc.Users.Drafts.Update(userID, draftID, &gmail.Draft{Message: msg}).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("gmail draft:update: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {draftToItem(draft)}}}, nil
	}

	return schema.NodeResult{}, fmt.Errorf("gmail: unknown compose action %q", action)
}

// ---------------------------------------------------------------------------
// MIME / RFC 2822 message builder
// ---------------------------------------------------------------------------

func buildMIMEMessage(ctx *schema.ExecContext, to, cc, bcc, subject, body, contentType string, attachments map[string]any, orig *gmail.Message, action string) (string, error) {
	var buf strings.Builder

	fmt.Fprintf(&buf, "MIME-Version: 1.0\r\n")
	if to != "" {
		fmt.Fprintf(&buf, "To: %s\r\n", to)
	}
	if cc != "" {
		fmt.Fprintf(&buf, "Cc: %s\r\n", cc)
	}
	if bcc != "" {
		fmt.Fprintf(&buf, "Bcc: %s\r\n", bcc)
	}
	if subject != "" {
		fmt.Fprintf(&buf, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	}

	if action == "reply" && orig != nil {
		var origMsgID string
		for _, h := range orig.Payload.Headers {
			if h.Name == "Message-ID" {
				origMsgID = h.Value
				break
			}
		}
		if origMsgID != "" {
			fmt.Fprintf(&buf, "In-Reply-To: %s\r\n", origMsgID)
			var refs []string
			for _, h := range orig.Payload.Headers {
				if h.Name == "References" {
					refs = append(refs, h.Value)
					break
				}
			}
			refs = append(refs, origMsgID)
			fmt.Fprintf(&buf, "References: %s\r\n", strings.Join(refs, " "))
		}
	}

	fmt.Fprintf(&buf, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))

	hasAttachments := false
	if attsRaw, ok := attachments["attachments"].([]any); ok && len(attsRaw) > 0 {
		hasAttachments = true
	}

	if !hasAttachments {
		fmt.Fprintf(&buf, "Content-Type: %s; charset=\"UTF-8\"\r\n", contentType)
		fmt.Fprintf(&buf, "Content-Transfer-Encoding: quoted-printable\r\n")
		fmt.Fprintf(&buf, "\r\n")
		fmt.Fprintf(&buf, "%s", body)
	} else {
		boundary := fmt.Sprintf("__CrossCraft_%x__", time.Now().UnixNano())
		fmt.Fprintf(&buf, "Content-Type: multipart/mixed; boundary=\"%s\"\r\n", boundary)
		fmt.Fprintf(&buf, "\r\n")

		fmt.Fprintf(&buf, "--%s\r\n", boundary)
		fmt.Fprintf(&buf, "Content-Type: %s; charset=\"UTF-8\"\r\n", contentType)
		fmt.Fprintf(&buf, "Content-Transfer-Encoding: quoted-printable\r\n")
		fmt.Fprintf(&buf, "\r\n")
		fmt.Fprintf(&buf, "%s\r\n", body)

		if attsRaw, ok := attachments["attachments"].([]any); ok {
			for _, a := range attsRaw {
				att, _ := a.(map[string]any)
				filename, _ := att["filename"].(string)
				dataB64, _ := att["data"].(string)
				mimeType, _ := att["mimeType"].(string)
				if filename == "" || dataB64 == "" {
					continue
				}
				if mimeType == "" {
					mimeType = "application/octet-stream"
				}
				decoded, err := base64.StdEncoding.DecodeString(dataB64)
				if err != nil {
					return "", fmt.Errorf("attachment %q: decode base64: %w", filename, err)
				}
				fmt.Fprintf(&buf, "--%s\r\n", boundary)
				fmt.Fprintf(&buf, "Content-Type: %s; name=\"%s\"\r\n", mimeType, filename)
				fmt.Fprintf(&buf, "Content-Disposition: attachment; filename=\"%s\"\r\n", filename)
				fmt.Fprintf(&buf, "Content-Transfer-Encoding: base64\r\n")
				fmt.Fprintf(&buf, "\r\n")
				encoded := base64.StdEncoding.EncodeToString(decoded)
				for i := 0; i < len(encoded); i += 76 {
					end := i + 76
					if end > len(encoded) {
						end = len(encoded)
					}
					fmt.Fprintf(&buf, "%s\r\n", encoded[i:end])
				}
			}
		}
		fmt.Fprintf(&buf, "--%s--\r\n", boundary)
	}

	raw := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(buf.String()))
	return raw, nil
}

// ---------------------------------------------------------------------------
// Trigger (polling for new emails)
// ---------------------------------------------------------------------------

func executeGmailTrigger(ctx *schema.ExecContext, svc *gmail.Service, userID, searchQ, base string) (schema.NodeResult, error) {
	if ctx.State == nil {
		ctx.State = map[string]any{}
	}

	pollSeconds := parseIntParam(ctx.Params["pollSeconds"], 60)
	if pollSeconds < 10 {
		pollSeconds = 10
	}

	lastPollKey := fmt.Sprintf("gmail:lastPoll:%s:%s", base, searchQ)
	cursorKey := fmt.Sprintf("gmail:cursor:%s:%s", base, searchQ)
	seenKey := fmt.Sprintf("gmail:seenIDs:%s:%s", base, searchQ)

	if tsAny, ok := ctx.State[lastPollKey]; ok {
		if ts, valid := toInt64(tsAny); valid && time.Since(time.Unix(ts, 0)) < time.Duration(pollSeconds)*time.Second {
			return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {}}}, nil
		}
	}

	q := searchQ
	if q == "" {
		q = "newer_than:1d"
	}

	call := svc.Users.Messages.List(userID).Q(q).MaxResults(50)
	resp, err := call.Do()
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("gmail trigger:newEmail: %w", err)
	}

	seen := map[string]bool{}
	if raw, ok := ctx.State[seenKey].([]any); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok {
				seen[s] = true
			}
		}
	}

	// First pass: identify unseen message IDs from the list response
	// (metadata only — avoiding full message fetch for dedup).
	var unseenIDs []string
	for _, m := range resp.Messages {
		if !seen[m.Id] {
			unseenIDs = append(unseenIDs, m.Id)
		}
	}

	// Second pass: fetch full details only for actually-new messages.
	var out []schema.Item
	var newIDs []string
	for _, id := range unseenIDs {
		msg, err := svc.Users.Messages.Get(userID, id).Format("full").Do()
		if err != nil {
			continue
		}
		out = append(out, messageToItem(msg))
		newIDs = append(newIDs, id)
	}

	for _, id := range newIDs {
		seen[id] = true
	}
	seenList := make([]any, 0, len(seen))
	for id := range seen {
		seenList = append(seenList, id)
	}
	ctx.State[seenKey] = seenList
	if resp.ResultSizeEstimate > 0 {
		ctx.State[cursorKey] = resp.ResultSizeEstimate
	}
	ctx.State[lastPollKey] = time.Now().Unix()

	return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
}

// ---------------------------------------------------------------------------
// SDK struct → schema.Item helpers
// ---------------------------------------------------------------------------

func messageToItem(m *gmail.Message) schema.Item {
	if m == nil {
		return schema.Item{JSON: map[string]any{}}
	}
	item := map[string]any{
		"id":           m.Id,
		"threadId":     m.ThreadId,
		"labelIds":     m.LabelIds,
		"snippet":      m.Snippet,
		"historyId":    m.HistoryId,
		"internalDate": m.InternalDate,
		"sizeEstimate": m.SizeEstimate,
	}
	if m.Payload != nil {
		item["payload"] = partToMap(m.Payload)
	}
	if m.Raw != "" {
		item["raw"] = m.Raw
	}
	return schema.Item{JSON: item}
}

func partToMap(p *gmail.MessagePart) map[string]any {
	if p == nil {
		return nil
	}
	m := map[string]any{
		"partId":   p.PartId,
		"mimeType": p.MimeType,
		"filename": p.Filename,
	}
	if p.Body != nil {
		m["body"] = map[string]any{
			"size":         p.Body.Size,
			"data":         p.Body.Data,
			"attachmentId": p.Body.AttachmentId,
		}
	}
	if len(p.Headers) > 0 {
		headers := map[string]string{}
		for _, h := range p.Headers {
			headers[h.Name] = h.Value
		}
		m["headers"] = headers
	}
	if len(p.Parts) > 0 {
		parts := make([]map[string]any, 0, len(p.Parts))
		for _, cp := range p.Parts {
			parts = append(parts, partToMap(cp))
		}
		m["parts"] = parts
	}
	return m
}

func threadToItem(t *gmail.Thread) schema.Item {
	if t == nil {
		return schema.Item{JSON: map[string]any{}}
	}
	messages := make([]map[string]any, 0, len(t.Messages))
	for _, msg := range t.Messages {
		messages = append(messages, messageToItem(msg).JSON)
	}
	return schema.Item{JSON: map[string]any{
		"id":       t.Id,
		"snippet":  t.Snippet,
		"historyId": t.HistoryId,
		"messages": messages,
	}}
}

func draftToItem(d *gmail.Draft) schema.Item {
	if d == nil {
		return schema.Item{JSON: map[string]any{}}
	}
	return schema.Item{JSON: map[string]any{
		"id":      d.Id,
		"message": messageToItem(d.Message).JSON,
	}}
}

func labelToItem(l *gmail.Label) schema.Item {
	if l == nil {
		return schema.Item{JSON: map[string]any{}}
	}
	return schema.Item{JSON: map[string]any{
		"id":                    l.Id,
		"name":                  l.Name,
		"type":                  l.Type,
		"messagesTotal":         l.MessagesTotal,
		"messagesUnread":        l.MessagesUnread,
		"labelListVisibility":   l.LabelListVisibility,
		"messageListVisibility": l.MessageListVisibility,
	}}
}
