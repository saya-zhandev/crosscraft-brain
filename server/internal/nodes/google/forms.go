package google

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
	forms "google.golang.org/api/forms/v1"
	"google.golang.org/api/option"
)

// ---------------------------------------------------------------------------
// Service cache
// ---------------------------------------------------------------------------

var (
	formsSvcMu    sync.Mutex
	formsSvcCache = map[string]*forms.Service{}
)

func getFormsService(ctx *schema.ExecContext, base string) (*forms.Service, error) {
	client, err := ctx.AuthorizedClient("credential")
	if err != nil {
		return nil, fmt.Errorf("forms: authorized client: %w", err)
	}
	credID, _ := ctx.Params["credential"].(string)
	if credID == "" {
		return nil, fmt.Errorf("forms: credential is required")
	}
	cacheKey := credID + "|" + base

	formsSvcMu.Lock()
	if svc, ok := formsSvcCache[cacheKey]; ok {
		formsSvcMu.Unlock()
		return svc, nil
	}
	formsSvcMu.Unlock()

	endpoint := base
	if endpoint == "" {
		endpoint = "https://forms.googleapis.com/"
	}
	retryClient := wrapWithRetry(client)
	svc, err := forms.NewService(context.Background(),
		option.WithHTTPClient(retryClient),
		option.WithEndpoint(strings.TrimRight(endpoint, "/")+"/"),
	)
	if err != nil {
		return nil, fmt.Errorf("forms: new service: %w", err)
	}

	formsSvcMu.Lock()
	if existing, ok := formsSvcCache[cacheKey]; ok {
		formsSvcMu.Unlock()
		return existing, nil
	}
	formsSvcCache[cacheKey] = svc
	formsSvcMu.Unlock()
	return svc, nil
}

// ---------------------------------------------------------------------------
// Node definition
// ---------------------------------------------------------------------------

func FormsNode(base string) schema.NodeDefinition {
	return schema.NodeDefinition{
		Type:        "google.forms",
		Label:       "Google Forms",
		Group:       "integration",
		Icon:        "FormInput",
		Description: "Read Google Forms metadata and responses, list forms, and poll for new responses.",
		Inputs:      []schema.Port{{ID: "main"}},
		Outputs:     []schema.Port{{ID: "main"}},
		Credentials: []string{credType},
		IsTrigger:   true,
		Params: []schema.ParamSchema{
			{Name: "credential", Label: "Credential", Type: "credential", Required: true, CredentialType: credType},
			{Name: "operation", Label: "Operation", Type: "select", Required: true, Options: []schema.ParamOption{
				{Label: "Get Form", Value: "form:get"},
				{Label: "List Forms", Value: "form:list"},
				{Label: "List Responses", Value: "response:list"},
				{Label: "Get Response", Value: "response:get"},
				{Label: "Trigger: New Response", Value: "trigger:newResponse"},
			}},
			{
				Name: "formId", Label: "Form ID", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"form:get", "response:list", "response:get", "trigger:newResponse",
				}},
			},
			{
				Name: "responseId", Label: "Response ID", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"response:get",
				}},
			},
			{
				Name: "q", Label: "Query (Drive search syntax)", Type: "string",
				Placeholder: "name contains 'Survey'",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"form:list",
				}},
			},
			{
				Name: "pageSize", Label: "Page size", Type: "number", Default: float64(50),
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"form:list", "response:list",
				}},
			},
			{
				Name: "maxPages", Label: "Max pages", Type: "number", Default: float64(10),
				Description: "Maximum number of pages to fetch. Set to 1 for first page only.",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"form:list", "response:list",
				}},
			},
			{
				Name: "pollSeconds", Label: "Poll seconds", Type: "number", Default: float64(30),
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"trigger:newResponse",
				}},
			},
		},
		Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
			return executeFormsNode(ctx, base)
		},
	}
}

// ---------------------------------------------------------------------------
// Main execution dispatch
// ---------------------------------------------------------------------------

func executeFormsNode(ctx *schema.ExecContext, base string) (schema.NodeResult, error) {
	op, _ := ctx.Params["operation"].(string)
	if op == "" {
		return schema.NodeResult{}, fmt.Errorf("forms: operation is required")
	}

	svc, err := getFormsService(ctx, base)
	if err != nil {
		return schema.NodeResult{}, err
	}

	formID, _ := ctx.Params["formId"].(string)
	responseID, _ := ctx.Params["responseId"].(string)

	switch op {

	case "form:get":
		if formID == "" {
			return schema.NodeResult{}, fmt.Errorf("forms form:get: formId is required")
		}
		form, err := svc.Forms.Get(formID).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("forms form:get: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {formToItem(form)}}}, nil

	case "form:list":
		return listFormsViaDrive(ctx)

	case "response:list":
		if formID == "" {
			return schema.NodeResult{}, fmt.Errorf("forms response:list: formId is required")
		}
		pageSize := parseIntParam(ctx.Params["pageSize"], 50)
		maxPages := parseIntParam(ctx.Params["maxPages"], 10)

		var allItems []schema.Item
		pageToken := ""
		for page := 0; page < maxPages; page++ {
			call := svc.Forms.Responses.List(formID).PageSize(int64(pageSize))
			if pageToken != "" {
				call = call.PageToken(pageToken)
			}
			resp, err := call.Do()
			if err != nil {
				return schema.NodeResult{}, fmt.Errorf("forms response:list: %w", err)
			}
			for _, r := range resp.Responses {
				allItems = append(allItems, responseToItem(r))
			}
			if resp.NextPageToken == "" {
				break
			}
			pageToken = resp.NextPageToken
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": allItems}}, nil

	case "response:get":
		if formID == "" {
			return schema.NodeResult{}, fmt.Errorf("forms response:get: formId is required")
		}
		if responseID == "" {
			return schema.NodeResult{}, fmt.Errorf("forms response:get: responseId is required")
		}
		r, err := svc.Forms.Responses.Get(formID, responseID).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("forms response:get: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {responseToItem(r)}}}, nil

	case "trigger:newResponse":
		if formID == "" {
			return schema.NodeResult{}, fmt.Errorf("forms trigger:newResponse: formId is required")
		}
		return executeFormsTrigger(ctx, svc, formID, base)

	default:
		return schema.NodeResult{}, fmt.Errorf("forms: unknown operation %q", op)
	}
}

// ---------------------------------------------------------------------------
// List forms via Drive API (forms do not have a native list endpoint)
// ---------------------------------------------------------------------------

func listFormsViaDrive(ctx *schema.ExecContext) (schema.NodeResult, error) {
	client, err := ctx.AuthorizedClient("credential")
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("forms form:list: %w", err)
	}
	client = wrapWithRetry(client)

	pageSize := parseIntParam(ctx.Params["pageSize"], 50)
	maxPages := parseIntParam(ctx.Params["maxPages"], 10)
	searchQ, _ := ctx.Params["q"].(string)

	q := "mimeType='application/vnd.google-apps.form' and trashed=false"
	if searchQ != "" {
		q = q + " and (" + searchQ + ")"
	}

	var allItems []schema.Item
	pageToken := ""
	for page := 0; page < maxPages; page++ {
		url := fmt.Sprintf(
			"https://www.googleapis.com/drive/v3/files?q=%s&pageSize=%d&fields=nextPageToken,files(id,name,createdTime,modifiedTime,webViewLink)",
			strings.ReplaceAll(q, " ", "%20"), pageSize,
		)
		if pageToken != "" {
			url += "&pageToken=" + pageToken
		}

		req, err := http.NewRequestWithContext(context.Background(), "GET", url, nil)
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("forms form:list: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("forms form:list: %w", err)
		}

		var driveResp struct {
			NextPageToken string `json:"nextPageToken"`
			Files         []struct {
				ID           string `json:"id"`
				Name         string `json:"name"`
				CreatedTime  string `json:"createdTime"`
				ModifiedTime string `json:"modifiedTime"`
				WebViewLink  string `json:"webViewLink"`
			} `json:"files"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&driveResp); err != nil {
			resp.Body.Close()
			return schema.NodeResult{}, fmt.Errorf("forms form:list: decode: %w", err)
		}
		resp.Body.Close()

		for _, f := range driveResp.Files {
			allItems = append(allItems, schema.Item{JSON: map[string]any{
				"id":           f.ID,
				"name":         f.Name,
				"createdTime":  f.CreatedTime,
				"modifiedTime": f.ModifiedTime,
				"webViewLink":  f.WebViewLink,
			}})
		}

		if driveResp.NextPageToken == "" {
			break
		}
		pageToken = driveResp.NextPageToken
	}
	return schema.NodeResult{Outputs: map[string][]schema.Item{"main": allItems}}, nil
}

// ---------------------------------------------------------------------------
// Trigger (polling for new responses)
// ---------------------------------------------------------------------------

func executeFormsTrigger(ctx *schema.ExecContext, svc *forms.Service, formID, base string) (schema.NodeResult, error) {
	if ctx.State == nil {
		ctx.State = map[string]any{}
	}

	pollSeconds := parseIntParam(ctx.Params["pollSeconds"], 30)
	if pollSeconds < 10 {
		pollSeconds = 10
	}

	lastPollKey := fmt.Sprintf("forms:lastPoll:%s:%s", formID, base)
	seenKey := fmt.Sprintf("forms:seenIDs:%s", formID)

	if tsAny, ok := ctx.State[lastPollKey]; ok {
		if ts, valid := toInt64(tsAny); valid && time.Since(time.Unix(ts, 0)) < time.Duration(pollSeconds)*time.Second {
			return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {}}}, nil
		}
	}

	seen := map[string]bool{}
	if raw, ok := ctx.State[seenKey].([]any); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok {
				seen[s] = true
			}
		}
	}

	resp, err := svc.Forms.Responses.List(formID).PageSize(100).Do()
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("forms trigger:newResponse: %w", err)
	}

	var out []schema.Item
	for _, r := range resp.Responses {
		if !seen[r.ResponseId] {
			out = append(out, responseToItem(r))
			seen[r.ResponseId] = true
		}
	}

	seenList := make([]any, 0, len(seen))
	for id := range seen {
		seenList = append(seenList, id)
	}
	ctx.State[seenKey] = seenList
	ctx.State[lastPollKey] = time.Now().Unix()

	return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
}

// ---------------------------------------------------------------------------
// SDK struct → schema.Item helpers
// ---------------------------------------------------------------------------

func formToItem(f *forms.Form) schema.Item {
	if f == nil {
		return schema.Item{JSON: map[string]any{}}
	}
	item := map[string]any{
		"formId":     f.FormId,
		"revisionId": f.RevisionId,
		"responderUri": f.ResponderUri,
	}
	if f.Info != nil {
		info := map[string]any{
			"title":       f.Info.Title,
			"description": f.Info.Description,
		}
		if f.Info.DocumentTitle != "" {
			info["documentTitle"] = f.Info.DocumentTitle
		}
		item["info"] = info
	}
	if len(f.Items) > 0 {
		items := make([]map[string]any, 0, len(f.Items))
		for _, fi := range f.Items {
			im := map[string]any{
				"itemId": fi.ItemId,
				"title":  fi.Title,
			}
			if fi.QuestionItem != nil && fi.QuestionItem.Question != nil {
				im["question"] = map[string]any{
					"questionId": fi.QuestionItem.Question.QuestionId,
					"required":   fi.QuestionItem.Question.Required,
				}
			}
			if fi.PageBreakItem != nil {
				im["pageBreak"] = map[string]any{}
			}
			items = append(items, im)
		}
		item["items"] = items
	}
	return schema.Item{JSON: item}
}

func responseToItem(r *forms.FormResponse) schema.Item {
	if r == nil {
		return schema.Item{JSON: map[string]any{}}
	}
	item := map[string]any{
		"responseId": r.ResponseId,
		"createTime": r.CreateTime,
		"lastSubmittedTime": r.LastSubmittedTime,
	}
	if r.RespondentEmail != "" {
		item["respondentEmail"] = r.RespondentEmail
	}
	if len(r.Answers) > 0 {
		answers := map[string]any{}
		for k, a := range r.Answers {
			am := map[string]any{
				"questionId": a.QuestionId,
			}
			if a.TextAnswers != nil && len(a.TextAnswers.Answers) > 0 {
				texts := make([]map[string]any, 0, len(a.TextAnswers.Answers))
				for _, ta := range a.TextAnswers.Answers {
					texts = append(texts, map[string]any{"value": ta.Value})
				}
				am["textAnswers"] = texts
			}
			if a.FileUploadAnswers != nil && len(a.FileUploadAnswers.Answers) > 0 {
				files := make([]map[string]any, 0, len(a.FileUploadAnswers.Answers))
				for _, fa := range a.FileUploadAnswers.Answers {
					files = append(files, map[string]any{
						"fileId":   fa.FileId,
						"fileName": fa.FileName,
						"mimeType": fa.MimeType,
					})
				}
				am["fileUploadAnswers"] = files
			}
			answers[k] = am
		}
		item["answers"] = answers
	}
	return schema.Item{JSON: item}
}
