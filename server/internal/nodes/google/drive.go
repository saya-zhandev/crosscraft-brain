package google

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

// ---------------------------------------------------------------------------
// Service cache
// ---------------------------------------------------------------------------

var (
	driveSvcMu    sync.Mutex
	driveSvcCache = map[string]*drive.Service{}
)

func getDriveService(ctx *schema.ExecContext, base string) (*drive.Service, error) {
	client, err := ctx.AuthorizedClient("credential")
	if err != nil {
		return nil, fmt.Errorf("drive: authorized client: %w", err)
	}
	credID, _ := ctx.Params["credential"].(string)
	cacheKey := credID + "|" + base

	// Fast path: read under lock.
	driveSvcMu.Lock()
	if svc, ok := driveSvcCache[cacheKey]; ok {
		driveSvcMu.Unlock()
		return svc, nil
	}
	driveSvcMu.Unlock()

	// Slow path: construct and cache.
	endpoint := base
	if endpoint == "" {
		endpoint = "https://www.googleapis.com/drive/v3/"
	}
	retryClient := wrapWithRetry(client)
	svc, err := drive.NewService(context.Background(),
		option.WithHTTPClient(retryClient),
		option.WithEndpoint(strings.TrimRight(endpoint, "/")+"/"),
	)
	if err != nil {
		return nil, fmt.Errorf("drive: new service: %w", err)
	}

	// Double-checked lock to avoid duplicate construction.
	driveSvcMu.Lock()
	if existing, ok := driveSvcCache[cacheKey]; ok {
		driveSvcMu.Unlock()
		return existing, nil
	}
	driveSvcCache[cacheKey] = svc
	driveSvcMu.Unlock()
	return svc, nil
}

// ---------------------------------------------------------------------------
// Node definition
// ---------------------------------------------------------------------------

func DriveNode(base string) schema.NodeDefinition {
	return schema.NodeDefinition{
		Type:        "google.drive",
		Label:       "Google Drive",
		Group:       "integration",
		Icon:        "HardDrive",
		Description: "Manage Google Drive files: list, get, delete, copy, move, share, create-from-text, upload, download, and trigger.",
		Inputs:      []schema.Port{{ID: "main"}},
		Outputs:     []schema.Port{{ID: "main"}},
		Credentials: []string{credType},
		IsTrigger:   true,
		Params: []schema.ParamSchema{
			{Name: "credential", Label: "Credential", Type: "credential", Required: true, CredentialType: credType},
			{Name: "operation", Label: "Operation", Type: "select", Required: true, Options: []schema.ParamOption{
				{Label: "List Files", Value: "file:list"},
				{Label: "Get File", Value: "file:get"},
				{Label: "Delete File", Value: "file:delete"},
				{Label: "Create Folder", Value: "folder:create"},
				{Label: "Copy File", Value: "file:copy"},
				{Label: "Move File", Value: "file:move"},
				{Label: "Share File", Value: "file:share"},
				{Label: "Create from Text", Value: "file:createFromText"},
				{Label: "Upload File", Value: "file:upload"},
				{Label: "Download File", Value: "file:download"},
				{Label: "Trigger: New File", Value: "trigger:newFile"},
			}},
			{
				Name: "fileId", Label: "File ID", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"file:get", "file:delete", "file:copy", "file:move", "file:share",
					"file:download",
				}},
			},
			{
				Name: "q", Label: "Query (Drive search syntax)", Type: "string", Placeholder: "mimeType='text/plain' and trashed=false",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"file:list", "trigger:newFile",
				}},
			},
			{
				Name: "pageSize", Label: "Page size", Type: "number", Default: float64(50),
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"file:list",
				}},
			},
			{
				Name: "maxPages", Label: "Max pages", Type: "number", Default: float64(10),
				Description: "Maximum number of pages to fetch. Set to 1 for first page only.",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"file:list",
				}},
			},
			{
				Name: "name", Label: "Name", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"folder:create", "file:copy", "file:createFromText", "file:upload",
				}},
			},
			{
				Name: "content", Label: "Content", Type: "code", Description: "Text content for the file.",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"file:createFromText",
				}},
			},
			{
				Name: "mimeType", Label: "MIME Type", Type: "select", Default: "text/plain", Options: []schema.ParamOption{
					{Label: "Plain text", Value: "text/plain"},
					{Label: "Markdown", Value: "text/markdown"},
					{Label: "HTML", Value: "text/html"},
					{Label: "Google Doc", Value: "application/vnd.google-apps.document"},
					{Label: "Google Sheet", Value: "application/vnd.google-apps.spreadsheet"},
				},
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"file:createFromText",
				}},
			},
			{
				Name: "folderId", Label: "Parent folder ID", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"folder:create", "file:copy", "file:move", "file:createFromText", "file:upload",
					"trigger:newFile",
				}},
			},
			{
				Name: "email", Label: "Email address", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"file:share",
				}},
			},
			{
				Name: "role", Label: "Role", Type: "select", Default: "reader", Options: []schema.ParamOption{
					{Label: "Viewer", Value: "reader"},
					{Label: "Commenter", Value: "commenter"},
					{Label: "Editor", Value: "writer"},
				},
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"file:share",
				}},
			},
			{
				Name: "driveId", Label: "Shared Drive ID", Type: "string",
				Description: "Optional: operate within a shared drive.",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"file:list", "file:get", "file:delete", "folder:create",
					"file:copy", "file:move", "file:upload",
				}},
			},
			{
				Name: "binaryProperty", Label: "Binary property", Type: "string", Default: "data",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"file:upload", "file:download",
				}},
			},
			{
				Name: "pollSeconds", Label: "Poll seconds", Type: "number", Default: float64(30),
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"trigger:newFile",
				}},
			},
			{
				Name: "fields", Label: "Fields (partial response)", Type: "string",
				Description: "Comma-separated field names. Defaults to '*' (all fields). Example: files(id,name,mimeType,size,webViewLink)",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"file:list",
				}},
			},
		},
		Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
			return executeDriveNode(ctx, base)
		},
	}
}

// ---------------------------------------------------------------------------
// Main execution dispatch
// ---------------------------------------------------------------------------

func executeDriveNode(ctx *schema.ExecContext, base string) (schema.NodeResult, error) {
	op, _ := ctx.Params["operation"].(string)
	if op == "" {
		return schema.NodeResult{}, fmt.Errorf("drive: operation is required")
	}

	svc, err := getDriveService(ctx, base)
	if err != nil {
		return schema.NodeResult{}, err
	}

	fileID, _ := ctx.Params["fileId"].(string)
	folderID, _ := ctx.Params["folderId"].(string)
	fileName, _ := ctx.Params["name"].(string)
	driveID, _ := ctx.Params["driveId"].(string)

	switch op {

	case "file:list":
		return driveListFiles(ctx, svc, driveID)

	case "file:get":
		if fileID == "" {
			return schema.NodeResult{}, fmt.Errorf("drive file:get: fileId is required")
		}
		call := svc.Files.Get(fileID).SupportsAllDrives(true).Fields("*")
		f, err := call.Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("drive file:get: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {fileToItem(f)}}}, nil

	case "file:delete":
		if fileID == "" {
			return schema.NodeResult{}, fmt.Errorf("drive file:delete: fileId is required")
		}
		call := svc.Files.Delete(fileID).SupportsAllDrives(true)
		if err := call.Do(); err != nil {
			return schema.NodeResult{}, fmt.Errorf("drive file:delete: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{JSON: map[string]any{"deleted": true, "fileId": fileID}}}}}, nil

	case "folder:create":
		if fileName == "" {
			return schema.NodeResult{}, fmt.Errorf("drive folder:create: name is required")
		}
		f := &drive.File{
			Name:     fileName,
			MimeType: "application/vnd.google-apps.folder",
		}
		if folderID != "" {
			f.Parents = []string{folderID}
		}
		call := svc.Files.Create(f).SupportsAllDrives(true).Fields("*")
		created, err := call.Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("drive folder:create: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {fileToItem(created)}}}, nil

	case "file:copy":
		if fileID == "" {
			return schema.NodeResult{}, fmt.Errorf("drive file:copy: fileId is required")
		}
		copyFile := &drive.File{}
		if fileName != "" {
			copyFile.Name = fileName
		}
		if folderID != "" {
			copyFile.Parents = []string{folderID}
		}
		call := svc.Files.Copy(fileID, copyFile).SupportsAllDrives(true).Fields("*")
		copied, err := call.Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("drive file:copy: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {fileToItem(copied)}}}, nil

	case "file:move":
		return driveMoveFile(ctx, svc, fileID, folderID, driveID)

	case "file:share":
		return driveShareFile(ctx, svc, fileID)

	case "file:createFromText":
		return driveCreateFromText(ctx, svc, folderID)

	case "file:upload":
		return driveUploadStream(ctx, svc, folderID, fileName)

	case "file:download":
		return driveDownloadStream(ctx, svc, fileID)

	case "trigger:newFile":
		return executeDriveTrigger(ctx, svc, base)

	default:
		return schema.NodeResult{}, fmt.Errorf("drive: unknown operation %q", op)
	}
}

// ---------------------------------------------------------------------------
// List files
// ---------------------------------------------------------------------------

func driveListFiles(ctx *schema.ExecContext, svc *drive.Service, driveID string) (schema.NodeResult, error) {
	searchQ, _ := ctx.Params["q"].(string)
	pageSize := parseIntParam(ctx.Params["pageSize"], 50)
	maxPages := parseIntParam(ctx.Params["maxPages"], 10)
	fields, _ := ctx.Params["fields"].(string)
	if fields == "" {
		fields = "*"
	}

	var allItems []schema.Item
	pageToken := ""
	for page := 0; page < maxPages; page++ {
		call := svc.Files.List().
			PageSize(int64(pageSize)).
			SupportsAllDrives(true).
			IncludeItemsFromAllDrives(true).
			Fields(googleapi.Field("nextPageToken, files(" + fields + ")"))

		if searchQ != "" {
			call = call.Q(searchQ)
		}
		if driveID != "" {
			call = call.DriveId(driveID).Corpora("drive")
		}
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		resp, err := call.Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("drive file:list: %w", err)
		}
		for _, f := range resp.Files {
			allItems = append(allItems, fileToItem(f))
		}
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	return schema.NodeResult{Outputs: map[string][]schema.Item{"main": allItems}}, nil
}

// ---------------------------------------------------------------------------
// Move file
// ---------------------------------------------------------------------------

func driveMoveFile(_ *schema.ExecContext, svc *drive.Service, fileID, folderID, _ string) (schema.NodeResult, error) {
	if fileID == "" {
		return schema.NodeResult{}, fmt.Errorf("drive file:move: fileId is required")
	}
	if folderID == "" {
		return schema.NodeResult{}, fmt.Errorf("drive file:move: folderId is required")
	}

	// Get current parents.
	f, err := svc.Files.Get(fileID).SupportsAllDrives(true).Fields("parents").Do()
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("drive file:move: get parents: %w", err)
	}

	call := svc.Files.Update(fileID, &drive.File{}).SupportsAllDrives(true).AddParents(folderID).Fields("*")
	if len(f.Parents) > 0 {
		call = call.RemoveParents(strings.Join(f.Parents, ","))
	}
	moved, err := call.Do()
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("drive file:move: %w", err)
	}
	return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {fileToItem(moved)}}}, nil
}

// ---------------------------------------------------------------------------
// Share file
// ---------------------------------------------------------------------------

func driveShareFile(ctx *schema.ExecContext, svc *drive.Service, fileID string) (schema.NodeResult, error) {
	if fileID == "" {
		return schema.NodeResult{}, fmt.Errorf("drive file:share: fileId is required")
	}
	email, _ := ctx.Params["email"].(string)
	if email == "" {
		return schema.NodeResult{}, fmt.Errorf("drive file:share: email is required")
	}
	role, _ := ctx.Params["role"].(string)
	if role == "" {
		role = "reader"
	}

	perm := &drive.Permission{
		Type:         "user",
		Role:         role,
		EmailAddress: email,
	}
	created, err := svc.Permissions.Create(fileID, perm).
		SupportsAllDrives(true).
		SendNotificationEmail(true).
		Fields("*").
		Do()
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("drive file:share: %w", err)
	}
	return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{JSON: map[string]any{
		"shared":    true,
		"fileId":    fileID,
		"email":     email,
		"role":      role,
		"permission": map[string]any{
			"id":          created.Id,
			"type":        created.Type,
			"role":        created.Role,
			"emailAddress": created.EmailAddress,
			"displayName":  created.DisplayName,
		},
	}}}}}, nil
}

// ---------------------------------------------------------------------------
// Create from text
// ---------------------------------------------------------------------------

func driveCreateFromText(ctx *schema.ExecContext, svc *drive.Service, folderID string) (schema.NodeResult, error) {
	fileName, _ := ctx.Params["name"].(string)
	if fileName == "" {
		return schema.NodeResult{}, fmt.Errorf("drive file:createFromText: name is required")
	}
	content, _ := ctx.Params["content"].(string)
	mimeType, _ := ctx.Params["mimeType"].(string)
	if mimeType == "" {
		mimeType = "text/plain"
	}

	f := &drive.File{
		Name:     fileName,
		MimeType: mimeType,
	}
	if folderID != "" {
		f.Parents = []string{folderID}
	}

	created, err := svc.Files.Create(f).
		Media(strings.NewReader(content), googleapi.ContentType(mimeType)).
		SupportsAllDrives(true).
		Fields("*").
		Do()
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("drive file:createFromText: %w", err)
	}
	return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {fileToItem(created)}}}, nil
}

// ---------------------------------------------------------------------------
// Upload (streaming via binary input)
// ---------------------------------------------------------------------------

func driveUploadStream(ctx *schema.ExecContext, svc *drive.Service, folderID, fileName string) (schema.NodeResult, error) {
	prop := strOrDefault(ctx.Params["binaryProperty"], "data")
	if len(ctx.Input) == 0 || ctx.Input[0].Binary == nil {
		return schema.NodeResult{}, fmt.Errorf("drive file:upload: no input binary to upload")
	}
	ref, ok := ctx.Input[0].Binary[prop]
	if !ok {
		return schema.NodeResult{}, fmt.Errorf("drive file:upload: input has no binary property %q", prop)
	}

	mimeType := ref.MimeType
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	if fileName == "" {
		fileName = ref.FileName
	}
	if fileName == "" {
		fileName = "upload"
	}

	// Decode binary data for streaming.
	decoded, err := base64.StdEncoding.DecodeString(ref.Data)
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("drive file:upload: decode binary: %w", err)
	}

	f := &drive.File{
		Name:     fileName,
		MimeType: mimeType,
	}
	if folderID != "" {
		f.Parents = []string{folderID}
	}

	// Use SDK media upload — the SDK streams the reader without buffering
	// the entire file content in a second copy.
	created, err := svc.Files.Create(f).
		Media(strings.NewReader(string(decoded)), googleapi.ContentType(mimeType)).
		SupportsAllDrives(true).
		Fields("id,name,mimeType,size,webViewLink").
		Do()
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("drive file:upload: %w", err)
	}
	return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {fileToItem(created)}}}, nil
}

// ---------------------------------------------------------------------------
// Download (streaming via binary output)
// ---------------------------------------------------------------------------

func driveDownloadStream(ctx *schema.ExecContext, svc *drive.Service, fileID string) (schema.NodeResult, error) {
	if fileID == "" {
		return schema.NodeResult{}, fmt.Errorf("drive file:download: fileId is required")
	}
	prop := strOrDefault(ctx.Params["binaryProperty"], "data")

	// 1. Get metadata.
	meta, err := svc.Files.Get(fileID).SupportsAllDrives(true).Fields("id,name,mimeType,size").Do()
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("drive file:download: metadata: %w", err)
	}

	// 2. Download content via SDK (returns *http.Response).
	resp, err := svc.Files.Get(fileID).SupportsAllDrives(true).Download()
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("drive file:download: %w", err)
	}
	defer resp.Body.Close()

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("drive file:download: read: %w", err)
	}

	name := meta.Name
	mimeType := meta.MimeType
	if mimeType == "" {
		mimeType = resp.Header.Get("Content-Type")
	}

	item := schema.Item{
		JSON: fileToItem(meta).JSON,
		Binary: map[string]schema.BinaryRef{
			prop: {
				Data:     base64.StdEncoding.EncodeToString(content),
				MimeType: mimeType,
				FileName: name,
			},
		},
	}
	return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {item}}}, nil
}

// ---------------------------------------------------------------------------
// Trigger (polling for new files)
// ---------------------------------------------------------------------------

func executeDriveTrigger(ctx *schema.ExecContext, svc *drive.Service, base string) (schema.NodeResult, error) {
	if ctx.State == nil {
		ctx.State = map[string]any{}
	}

	pollSeconds := parseIntParam(ctx.Params["pollSeconds"], 30)
	if pollSeconds < 10 {
		pollSeconds = 10
	}

	searchQ, _ := ctx.Params["q"].(string)
	folderID, _ := ctx.Params["folderId"].(string)

	// Build state keys scoped to the query + folder + base.
	stateScope := base + "|" + searchQ + "|" + folderID
	lastPollKey := fmt.Sprintf("drive:lastPoll:%s", stateScope)
	seenKey := fmt.Sprintf("drive:seenIDs:%s", stateScope)

	// Rate-limit polling.
	if tsAny, ok := ctx.State[lastPollKey]; ok {
		if ts, valid := toInt64(tsAny); valid && time.Since(time.Unix(ts, 0)) < time.Duration(pollSeconds)*time.Second {
			return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {}}}, nil
		}
	}

	// Build query. Default: all non-trashed files modified in the last day.
	q := searchQ
	if q == "" {
		q = "trashed=false and modifiedTime > '" + time.Now().Add(-24*time.Hour).Format(time.RFC3339) + "'"
	}
	if folderID != "" {
		if q != "" {
			q = "'" + folderID + "' in parents and " + q
		} else {
			q = "'" + folderID + "' in parents"
		}
	}

	// Restore seen set from state.
	seen := map[string]bool{}
	if raw, ok := ctx.State[seenKey].([]any); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok {
				seen[s] = true
			}
		}
	}

	// List files matching the query.
	call := svc.Files.List().
		Q(q).
		SupportsAllDrives(true).
		IncludeItemsFromAllDrives(true).
		OrderBy("modifiedTime desc").
		PageSize(100).
		Fields("files(id,name,mimeType,modifiedTime,size,webViewLink,parents)")

	resp, err := call.Do()
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("drive trigger:newFile: %w", err)
	}

	// Emit unseen files.
	var out []schema.Item
	for _, f := range resp.Files {
		if !seen[f.Id] {
			out = append(out, fileToItem(f))
			seen[f.Id] = true
		}
	}

	// Persist seen set back to state.
	seenList := make([]any, 0, len(seen))
	for id := range seen {
		seenList = append(seenList, id)
	}
	ctx.State[seenKey] = seenList
	ctx.State[lastPollKey] = time.Now().Unix()

	return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
}

// ---------------------------------------------------------------------------
// SDK struct → schema.Item helper
// ---------------------------------------------------------------------------

func fileToItem(f *drive.File) schema.Item {
	if f == nil {
		return schema.Item{JSON: map[string]any{}}
	}
	item := map[string]any{
		"id":           f.Id,
		"name":         f.Name,
		"mimeType":     f.MimeType,
		"kind":         f.Kind,
		"description":  f.Description,
		"starred":      f.Starred,
		"trashed":      f.Trashed,
		"explicitlyTrashed": f.ExplicitlyTrashed,
		"version":      f.Version,
		"webViewLink":  f.WebViewLink,
		"webContentLink": f.WebContentLink,
		"size":         f.Size,
		"createdTime":  f.CreatedTime,
		"modifiedTime": f.ModifiedTime,
		"shared":       f.Shared,
		"ownedByMe":    f.OwnedByMe,
		"driveId":      f.DriveId,
	}
	if len(f.Parents) > 0 {
		item["parents"] = f.Parents
	}
	if len(f.Owners) > 0 {
		owners := make([]map[string]any, 0, len(f.Owners))
		for _, o := range f.Owners {
			owners = append(owners, map[string]any{
				"kind":        o.Kind,
				"displayName": o.DisplayName,
				"emailAddress": o.EmailAddress,
				"me":          o.Me,
			})
		}
		item["owners"] = owners
	}
	if f.LastModifyingUser != nil {
		item["lastModifyingUser"] = map[string]any{
			"kind":        f.LastModifyingUser.Kind,
			"displayName": f.LastModifyingUser.DisplayName,
			"emailAddress": f.LastModifyingUser.EmailAddress,
		}
	}
	if f.Capabilities != nil {
		item["capabilities"] = map[string]any{
			"canEdit":          f.Capabilities.CanEdit,
			"canComment":       f.Capabilities.CanComment,
			"canShare":         f.Capabilities.CanShare,
			"canCopy":          f.Capabilities.CanCopy,
			"canDownload":      f.Capabilities.CanDownload,
			"canMoveItemIntoTeamDrive": f.Capabilities.CanMoveItemIntoTeamDrive,
		}
	}
	return schema.Item{JSON: item}
}

// strOrDefault returns the string value or a default.
func strOrDefault(v any, def string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}
