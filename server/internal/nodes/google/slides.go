package google

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
	"google.golang.org/api/option"
	slides "google.golang.org/api/slides/v1"
)

// ---------------------------------------------------------------------------
// Service cache
// ---------------------------------------------------------------------------

var (
	slidesSvcMu    sync.Mutex
	slidesSvcCache = map[string]*slides.Service{}
)

func getSlidesService(ctx *schema.ExecContext, base string) (*slides.Service, error) {
	client, err := ctx.AuthorizedClient("credential")
	if err != nil {
		return nil, fmt.Errorf("slides: authorized client: %w", err)
	}
	credID, _ := ctx.Params["credential"].(string)
	if credID == "" {
		return nil, fmt.Errorf("slides: credential is required")
	}
	cacheKey := credID + "|" + base

	slidesSvcMu.Lock()
	if svc, ok := slidesSvcCache[cacheKey]; ok {
		slidesSvcMu.Unlock()
		return svc, nil
	}
	slidesSvcMu.Unlock()

	endpoint := base
	if endpoint == "" {
		endpoint = "https://slides.googleapis.com/"
	}
	retryClient := wrapWithRetry(client)
	svc, err := slides.NewService(context.Background(),
		option.WithHTTPClient(retryClient),
		option.WithEndpoint(strings.TrimRight(endpoint, "/")+"/"),
	)
	if err != nil {
		return nil, fmt.Errorf("slides: new service: %w", err)
	}

	slidesSvcMu.Lock()
	if existing, ok := slidesSvcCache[cacheKey]; ok {
		slidesSvcMu.Unlock()
		return existing, nil
	}
	slidesSvcCache[cacheKey] = svc
	slidesSvcMu.Unlock()
	return svc, nil
}

// ---------------------------------------------------------------------------
// Node definition
// ---------------------------------------------------------------------------

func SlidesNode(base string) schema.NodeDefinition {
	return schema.NodeDefinition{
		Type:        "google.slides",
		Label:       "Google Slides",
		Group:       "integration",
		Icon:        "Presentation",
		Description: "Create, read, update Google Slides presentations and get page thumbnails.",
		Inputs:      []schema.Port{{ID: "main"}},
		Outputs:     []schema.Port{{ID: "main"}},
		Credentials: []string{credType},
		Params: []schema.ParamSchema{
			{Name: "credential", Label: "Credential", Type: "credential", Required: true, CredentialType: credType},
			{Name: "operation", Label: "Operation", Type: "select", Required: true, Options: []schema.ParamOption{
				{Label: "Get Presentation", Value: "presentation:get"},
				{Label: "Create Presentation", Value: "presentation:create"},
				{Label: "Update Presentation", Value: "presentation:update"},
				{Label: "Get Page Thumbnail", Value: "presentation:getThumbnail"},
			}},
			{
				Name: "presentationId", Label: "Presentation ID", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"presentation:get", "presentation:update", "presentation:getThumbnail",
				}},
			},
			{
				Name: "pageObjectId", Label: "Page Object ID", Type: "string",
				Description: "The object ID of the slide/page to get a thumbnail for.",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"presentation:getThumbnail",
				}},
			},
			{
				Name: "body", Label: "Body (JSON)", Type: "json",
				Description: `For create: {"title":"My Presentation"}. For update: {"requests":[{"replaceAllText":{"containsText":{"text":"{{name}}"},"replaceText":"John"}}]}`,
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"presentation:create", "presentation:update",
				}},
			},
		},
		Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
			return executeSlidesNode(ctx, base)
		},
	}
}

// ---------------------------------------------------------------------------
// Main execution dispatch
// ---------------------------------------------------------------------------

func executeSlidesNode(ctx *schema.ExecContext, base string) (schema.NodeResult, error) {
	op, _ := ctx.Params["operation"].(string)
	if op == "" {
		return schema.NodeResult{}, fmt.Errorf("slides: operation is required")
	}

	svc, err := getSlidesService(ctx, base)
	if err != nil {
		return schema.NodeResult{}, err
	}

	presentationID, _ := ctx.Params["presentationId"].(string)
	pageObjectID, _ := ctx.Params["pageObjectId"].(string)

	switch op {

	case "presentation:get":
		if presentationID == "" {
			return schema.NodeResult{}, fmt.Errorf("slides presentation:get: presentationId is required")
		}
		pres, err := svc.Presentations.Get(presentationID).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("slides presentation:get: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {presToItem(pres)}}}, nil

	case "presentation:create":
		bodyValue := asObject(ctx.RawParam("body"))
		title, _ := bodyValue["title"].(string)
		if title == "" {
			return schema.NodeResult{}, fmt.Errorf("slides presentation:create: body.title is required")
		}
		pres, err := svc.Presentations.Create(&slides.Presentation{Title: title}).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("slides presentation:create: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {presToItem(pres)}}}, nil

	case "presentation:update":
		if presentationID == "" {
			return schema.NodeResult{}, fmt.Errorf("slides presentation:update: presentationId is required")
		}
		bodyValue := asObject(ctx.RawParam("body"))
		bodyBytes, err := json.Marshal(bodyValue)
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("slides presentation:update: marshal body: %w", err)
		}
		var batchReq slides.BatchUpdatePresentationRequest
		if err := json.Unmarshal(bodyBytes, &batchReq); err != nil {
			return schema.NodeResult{}, fmt.Errorf("slides presentation:update: unmarshal requests: %w", err)
		}
		if len(batchReq.Requests) == 0 {
			return schema.NodeResult{}, fmt.Errorf("slides presentation:update: body.requests is required")
		}
		resp, err := svc.Presentations.BatchUpdate(presentationID, &batchReq).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("slides presentation:update: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{JSON: map[string]any{
			"updated":        true,
			"presentationId": resp.PresentationId,
			"replies":        len(resp.Replies),
		}}}}}, nil

	case "presentation:getThumbnail":
		if presentationID == "" {
			return schema.NodeResult{}, fmt.Errorf("slides presentation:getThumbnail: presentationId is required")
		}
		if pageObjectID == "" {
			return schema.NodeResult{}, fmt.Errorf("slides presentation:getThumbnail: pageObjectId is required")
		}
		thumb, err := svc.Presentations.Pages.GetThumbnail(presentationID, pageObjectID).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("slides presentation:getThumbnail: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{JSON: map[string]any{
			"contentUrl": thumb.ContentUrl,
			"width":      thumb.Width,
			"height":     thumb.Height,
		}}}}}, nil

	default:
		return schema.NodeResult{}, fmt.Errorf("slides: unknown operation %q", op)
	}
}

// ---------------------------------------------------------------------------
// SDK struct → schema.Item helper
// ---------------------------------------------------------------------------

func presToItem(p *slides.Presentation) schema.Item {
	if p == nil {
		return schema.Item{JSON: map[string]any{}}
	}
	item := map[string]any{
		"presentationId": p.PresentationId,
		"title":          p.Title,
		"revisionId":     p.RevisionId,
		"locale":         p.Locale,
	}
	if p.PageSize != nil {
		item["pageSize"] = map[string]any{
			"width":  map[string]any{"magnitude": p.PageSize.Width.Magnitude, "unit": p.PageSize.Width.Unit},
			"height": map[string]any{"magnitude": p.PageSize.Height.Magnitude, "unit": p.PageSize.Height.Unit},
		}
	}
	if len(p.Slides) > 0 {
		slidesList := make([]map[string]any, 0, len(p.Slides))
		for _, s := range p.Slides {
			sm := map[string]any{"objectId": s.ObjectId}
			if s.SlideProperties != nil && s.SlideProperties.LayoutObjectId != "" {
				sm["layoutObjectId"] = s.SlideProperties.LayoutObjectId
			}
			slidesList = append(slidesList, sm)
		}
		item["slides"] = slidesList
	}
	if len(p.Masters) > 0 {
		masters := make([]map[string]any, 0, len(p.Masters))
		for _, m := range p.Masters {
			masters = append(masters, map[string]any{"objectId": m.ObjectId})
		}
		item["masters"] = masters
	}
	if len(p.Layouts) > 0 {
		layouts := make([]map[string]any, 0, len(p.Layouts))
		for _, l := range p.Layouts {
			layouts = append(layouts, map[string]any{"objectId": l.ObjectId})
		}
		item["layouts"] = layouts
	}
	return schema.Item{JSON: item}
}
