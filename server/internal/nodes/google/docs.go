package google

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
	"google.golang.org/api/docs/v1"
	"google.golang.org/api/option"
)

// ---------------------------------------------------------------------------
// Service cache
// ---------------------------------------------------------------------------

var (
	docsSvcMu    sync.Mutex
	docsSvcCache = map[string]*docs.Service{}
)

func getDocsService(ctx *schema.ExecContext, base string) (*docs.Service, error) {
	client, err := ctx.AuthorizedClient("credential")
	if err != nil {
		return nil, fmt.Errorf("docs: authorized client: %w", err)
	}
	credID, _ := ctx.Params["credential"].(string)
	if credID == "" {
		return nil, fmt.Errorf("docs: credential is required")
	}
	cacheKey := credID + "|" + base

	docsSvcMu.Lock()
	if svc, ok := docsSvcCache[cacheKey]; ok {
		docsSvcMu.Unlock()
		return svc, nil
	}
	docsSvcMu.Unlock()

	endpoint := base
	if endpoint == "" {
		endpoint = "https://docs.googleapis.com/"
	}
	retryClient := wrapWithRetry(client)
	svc, err := docs.NewService(context.Background(),
		option.WithHTTPClient(retryClient),
		option.WithEndpoint(strings.TrimRight(endpoint, "/")+"/"),
	)
	if err != nil {
		return nil, fmt.Errorf("docs: new service: %w", err)
	}

	docsSvcMu.Lock()
	if existing, ok := docsSvcCache[cacheKey]; ok {
		docsSvcMu.Unlock()
		return existing, nil
	}
	docsSvcCache[cacheKey] = svc
	docsSvcMu.Unlock()
	return svc, nil
}

// ---------------------------------------------------------------------------
// Node definition
// ---------------------------------------------------------------------------

func DocsNode(base string) schema.NodeDefinition {
	return schema.NodeDefinition{
		Type:        "google.docs",
		Label:       "Google Docs",
		Group:       "integration",
		Icon:        "FileText",
		Description: "Create, read, update, and delete Google Docs documents.",
		Inputs:      []schema.Port{{ID: "main"}},
		Outputs:     []schema.Port{{ID: "main"}},
		Credentials: []string{credType},
		Params: []schema.ParamSchema{
			{Name: "credential", Label: "Credential", Type: "credential", Required: true, CredentialType: credType},
			{Name: "operation", Label: "Operation", Type: "select", Required: true, Options: []schema.ParamOption{
				{Label: "Get Document", Value: "document:get"},
				{Label: "Create Document", Value: "document:create"},
				{Label: "Update Document", Value: "document:update"},
				{Label: "Delete Document", Value: "document:delete"},
			}},
			{
				Name: "documentId", Label: "Document ID", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"document:get", "document:update", "document:delete",
				}},
			},
			{
				Name: "body", Label: "Body (JSON)", Type: "json",
				Description: `For create: {"title":"My Doc"}. For update: {"requests":[{"insertText":{"location":{"index":1},"text":"Hello"}}]}`,
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"document:create", "document:update",
				}},
			},
		},
		Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
			return executeDocsNode(ctx, base)
		},
	}
}

// ---------------------------------------------------------------------------
// Main execution dispatch
// ---------------------------------------------------------------------------

func executeDocsNode(ctx *schema.ExecContext, base string) (schema.NodeResult, error) {
	op, _ := ctx.Params["operation"].(string)
	if op == "" {
		return schema.NodeResult{}, fmt.Errorf("docs: operation is required")
	}

	svc, err := getDocsService(ctx, base)
	if err != nil {
		return schema.NodeResult{}, err
	}

	documentID, _ := ctx.Params["documentId"].(string)

	switch op {

	case "document:get":
		if documentID == "" {
			return schema.NodeResult{}, fmt.Errorf("docs document:get: documentId is required")
		}
		doc, err := svc.Documents.Get(documentID).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("docs document:get: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {docToItem(doc)}}}, nil

	case "document:create":
		bodyValue := asObject(ctx.RawParam("body"))
		title, _ := bodyValue["title"].(string)
		if title == "" {
			return schema.NodeResult{}, fmt.Errorf("docs document:create: body.title is required")
		}
		doc, err := svc.Documents.Create(&docs.Document{Title: title}).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("docs document:create: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {docToItem(doc)}}}, nil

	case "document:update":
		if documentID == "" {
			return schema.NodeResult{}, fmt.Errorf("docs document:update: documentId is required")
		}
		bodyValue := asObject(ctx.RawParam("body"))
		// Marshal the body map back to JSON and unmarshal into BatchUpdateDocumentRequest.
		bodyBytes, err := json.Marshal(bodyValue)
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("docs document:update: marshal body: %w", err)
		}
		var batchReq docs.BatchUpdateDocumentRequest
		if err := json.Unmarshal(bodyBytes, &batchReq); err != nil {
			return schema.NodeResult{}, fmt.Errorf("docs document:update: unmarshal requests: %w", err)
		}
		if len(batchReq.Requests) == 0 {
			return schema.NodeResult{}, fmt.Errorf("docs document:update: body.requests is required")
		}
		resp, err := svc.Documents.BatchUpdate(documentID, &batchReq).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("docs document:update: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{JSON: map[string]any{
			"updated":     true,
			"documentId":  resp.DocumentId,
			"replies":     len(resp.Replies),
			"writeControl": map[string]any{
				"requiredRevisionId": resp.WriteControl.RequiredRevisionId,
			},
		}}}}}, nil

	case "document:delete":
		if documentID == "" {
			return schema.NodeResult{}, fmt.Errorf("docs document:delete: documentId is required")
		}
		// The Docs API does not expose a Delete method; deletion goes through Drive.
		client, err := ctx.AuthorizedClient("credential")
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("docs document:delete: %w", err)
		}
		client = wrapWithRetry(client)
		req, err := http.NewRequestWithContext(
			context.Background(), "DELETE",
			"https://www.googleapis.com/drive/v3/files/"+documentID,
			nil,
		)
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("docs document:delete: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("docs document:delete: %w", err)
		}
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			return schema.NodeResult{}, fmt.Errorf("docs document:delete: Drive API returned %d", resp.StatusCode)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{JSON: map[string]any{"deleted": true, "documentId": documentID}}}}}, nil

	default:
		return schema.NodeResult{}, fmt.Errorf("docs: unknown operation %q", op)
	}
}

// ---------------------------------------------------------------------------
// SDK struct → schema.Item helper
// ---------------------------------------------------------------------------

func docToItem(d *docs.Document) schema.Item {
	if d == nil {
		return schema.Item{JSON: map[string]any{}}
	}
	item := map[string]any{
		"documentId": d.DocumentId,
		"title":      d.Title,
		"revisionId": d.RevisionId,
	}
	if d.Body != nil && len(d.Body.Content) > 0 {
		content := make([]map[string]any, 0, len(d.Body.Content))
		for _, se := range d.Body.Content {
			if se != nil {
				content = append(content, structuralElementToMap(se))
			}
		}
		item["body"] = map[string]any{"content": content}
	}
	return schema.Item{JSON: item}
}

func structuralElementToMap(se *docs.StructuralElement) map[string]any {
	m := map[string]any{}
	if se.StartIndex != 0 {
		m["startIndex"] = se.StartIndex
	}
	if se.EndIndex != 0 {
		m["endIndex"] = se.EndIndex
	}
	if se.Paragraph != nil {
		m["paragraph"] = paragraphToMap(se.Paragraph)
	}
	if se.Table != nil {
		m["table"] = map[string]any{
			"rows":   se.Table.Rows,
			"columns": se.Table.Columns,
		}
	}
	if se.SectionBreak != nil {
		m["sectionBreak"] = map[string]any{}
	}
	return m
}

func paragraphToMap(p *docs.Paragraph) map[string]any {
	m := map[string]any{}
	if p.ParagraphStyle != nil {
		m["namedStyleType"] = p.ParagraphStyle.NamedStyleType
	}
	if len(p.Elements) > 0 {
		elements := make([]map[string]any, 0, len(p.Elements))
		for _, pe := range p.Elements {
			em := map[string]any{}
			if pe.StartIndex != 0 {
				em["startIndex"] = pe.StartIndex
			}
			if pe.EndIndex != 0 {
				em["endIndex"] = pe.EndIndex
			}
			if pe.TextRun != nil {
				tr := map[string]any{
					"content": pe.TextRun.Content,
				}
				if pe.TextRun.TextStyle != nil {
					tr["textStyle"] = map[string]any{
						"bold":   pe.TextRun.TextStyle.Bold,
						"italic": pe.TextRun.TextStyle.Italic,
						"underline": pe.TextRun.TextStyle.Underline,
					}
				}
				em["textRun"] = tr
			}
			elements = append(elements, em)
		}
		m["elements"] = elements
	}
	return m
}
