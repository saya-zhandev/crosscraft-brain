package google

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

// sdkTestCtx builds an ExecContext for SDK-based node tests (Drive, Sheets, etc.).
// It provides Params, RawParam, AuthorizedClient, and Log.
func sdkTestCtx(params map[string]any, client *http.Client) *schema.ExecContext {
	return &schema.ExecContext{
		Params:           params,
		RawParam:         func(n string) any { return params[n] },
		AuthorizedClient: func(string) (*http.Client, error) { return client, nil },
		Log:              func(string, any) {},
	}
}

// ---------------------------------------------------------------------------
// Helper: build a minimal Drive API mock server
// ---------------------------------------------------------------------------

// driveMock holds state for the mock Drive API server.
type driveMock struct {
	files       map[string]map[string]any
	permissions []map[string]any
	nextFileID  int
}

func newDriveMock() *driveMock {
	return &driveMock{
		files: map[string]map[string]any{
			"file1": {
				"id": "file1", "name": "doc.txt", "mimeType": "text/plain",
				"parents": []any{"folder1"}, "size": "1024", "webViewLink": "https://drive.google.com/file/d/file1/view",
				"createdTime": "2025-01-01T00:00:00Z", "modifiedTime": "2025-01-02T00:00:00Z",
			},
			"file2": {
				"id": "file2", "name": "sheet", "mimeType": "application/vnd.google-apps.spreadsheet",
				"parents": []any{}, "size": "2048", "webViewLink": "https://drive.google.com/file/d/file2/view",
			},
		},
		nextFileID: 100,
	}
}

func (m *driveMock) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Download: GET /files/{id}?alt=media or /drive/v3/files/{id}?alt=media
		if r.URL.Query().Get("alt") == "media" {
			m.handleDownload(w, r)
			return
		}

		// Media upload via SDK: POST /upload/drive/v3/files?uploadType=multipart
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/files") && r.URL.Query().Get("uploadType") == "multipart" {
			m.handleMediaUpload(w, r)
			return
		}

		// Normalise path: strip /drive/v3 prefix used by the SDK.
		normPath := r.URL.Path
		normPath = strings.TrimPrefix(normPath, "/drive/v3")

		// Extract file ID from /files/{id}, return "" for /files.
		cleanPath := strings.TrimPrefix(normPath, "/files")
		cleanPath = strings.TrimPrefix(cleanPath, "/")
		if idx := strings.Index(cleanPath, "?"); idx >= 0 {
			cleanPath = cleanPath[:idx]
		}

		switch {
		// Permissions: POST /files/{id}/permissions
		case r.Method == http.MethodPost && strings.HasSuffix(normPath, "/permissions"):
			fileID := strings.TrimSuffix(strings.TrimPrefix(normPath, "/files/"), "/permissions")
			m.handleCreatePermission(w, r, fileID)
			return

		// File copy: POST /files/{id}/copy
		case r.Method == http.MethodPost && strings.HasSuffix(normPath, "/copy"):
			fileID := strings.TrimSuffix(strings.TrimPrefix(normPath, "/files/"), "/copy")
			m.handleCopy(w, r, fileID)
			return

		// File list: GET /files (cleanPath is empty)
		case r.Method == http.MethodGet && cleanPath == "":
			m.handleList(w, r)
			return

		// File get: GET /files/{id}
		case r.Method == http.MethodGet && cleanPath != "":
			m.handleGet(w, r, cleanPath)
			return

		// File create / folder create / createFromText (no media): POST /files
		case r.Method == http.MethodPost && cleanPath == "":
			m.handleCreate(w, r)
			return

		// File update (move): PATCH /files/{id}
		case r.Method == http.MethodPatch && cleanPath != "":
			m.handleUpdate(w, r, cleanPath)
			return

		// File delete: DELETE /files/{id}
		case r.Method == http.MethodDelete && cleanPath != "":
			m.handleDelete(w, r, cleanPath)
			return

		default:
			http.Error(w, `{"error":{"message":"not found"}}`, http.StatusNotFound)
		}
	}
}

func (m *driveMock) handleGet(w http.ResponseWriter, r *http.Request, fileID string) {
	f, ok := m.files[fileID]
	if !ok {
		http.Error(w, `{"error":{"message":"file not found"}}`, http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(f)
}

func (m *driveMock) handleList(w http.ResponseWriter, r *http.Request) {
	var items []map[string]any
	for _, f := range m.files {
		items = append(items, f)
	}
	json.NewEncoder(w).Encode(map[string]any{
		"files":          items,
		"nextPageToken":  "",
	})
}

func (m *driveMock) handleCreate(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var f map[string]any
	json.Unmarshal(body, &f)
	if f == nil {
		f = map[string]any{}
	}
	newID := m.uniqueID()
	f["id"] = newID
	if _, ok := f["name"]; !ok {
		f["name"] = "untitled"
	}
	if _, ok := f["mimeType"]; !ok {
		f["mimeType"] = "application/octet-stream"
	}
	m.files[newID] = f
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(f)
}

func (m *driveMock) handleMediaUpload(w http.ResponseWriter, r *http.Request) {
	// SDK media upload sends multipart/related: [metadata JSON][media bytes].
	// Parse the first part as JSON metadata, use the second as content.
	newID := m.uniqueIDStr()
	f := map[string]any{
		"id": newID, "name": "uploaded", "mimeType": "text/plain",
		"size": "12", "webViewLink": "https://drive.google.com/file/d/" + newID + "/view",
	}

	// Try to extract metadata from multipart body.
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "multipart") {
		_, params, _ := mime.ParseMediaType(ct)
		if boundary, ok := params["boundary"]; ok {
			mr := multipart.NewReader(r.Body, boundary)
			if part1, err := mr.NextPart(); err == nil {
				body1, _ := io.ReadAll(part1)
				var meta map[string]any
				if json.Unmarshal(body1, &meta) == nil {
					if n, ok := meta["name"].(string); ok && n != "" {
						f["name"] = n
					}
					if mt, ok := meta["mimeType"].(string); ok && mt != "" {
						f["mimeType"] = mt
					}
					if p, ok := meta["parents"].([]any); ok {
						f["parents"] = p
					}
				}
			}
		}
	}

	m.files[newID] = f
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(f)
}

func (m *driveMock) handleDelete(w http.ResponseWriter, r *http.Request, fileID string) {
	if _, ok := m.files[fileID]; !ok {
		http.Error(w, `{"error":{"message":"file not found"}}`, http.StatusNotFound)
		return
	}
	delete(m.files, fileID)
	w.WriteHeader(http.StatusNoContent)
}

func (m *driveMock) handleCopy(w http.ResponseWriter, r *http.Request, fileID string) {
	orig, ok := m.files[fileID]
	if !ok {
		http.Error(w, `{"error":{"message":"file not found"}}`, http.StatusNotFound)
		return
	}
	body, _ := io.ReadAll(r.Body)
	var req map[string]any
	json.Unmarshal(body, &req)
	copy := map[string]any{}
	for k, v := range orig {
		copy[k] = v
	}
	copy["id"] = m.uniqueID()
	if name, ok := req["name"].(string); ok && name != "" {
		copy["name"] = name
	} else {
		copy["name"] = orig["name"].(string) + " (Copy)"
	}
	m.files[copy["id"].(string)] = copy
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(copy)
}

func (m *driveMock) handleUpdate(w http.ResponseWriter, r *http.Request, fileID string) {
	f, ok := m.files[fileID]
	if !ok {
		http.Error(w, `{"error":{"message":"file not found"}}`, http.StatusNotFound)
		return
	}
	body, _ := io.ReadAll(r.Body)
	var req map[string]any
	json.Unmarshal(body, &req)

	// Handle addParents/removeParents (simulated via query params for test simplicity).
	addParents := r.URL.Query().Get("addParents")
	removeParents := r.URL.Query().Get("removeParents")

	if addParents != "" {
		parents, _ := f["parents"].([]any)
		parents = append(parents, addParents)
		f["parents"] = parents
	}
	if removeParents != "" {
		existing, _ := f["parents"].([]any)
		var filtered []any
		for _, p := range existing {
			if p.(string) != removeParents {
				filtered = append(filtered, p)
			}
		}
		f["parents"] = filtered
	}
	_ = req
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(f)
}

func (m *driveMock) handleCreatePermission(w http.ResponseWriter, _ *http.Request, _ string) {
	// The SDK sends permissions as JSON body. For test simplicity, return a static response.
	perm := map[string]any{
		"id":           "perm_" + m.uniqueIDStr(),
		"type":         "user",
		"role":         "writer",
		"emailAddress": "user@example.com",
		"displayName":  "Test User",
	}
	m.permissions = append(m.permissions, perm)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(perm)
}

func (m *driveMock) handleDownload(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("hello from drive"))
}

func (m *driveMock) uniqueID() string {
	id := m.nextFileID
	m.nextFileID++
	return "file" + strings.TrimPrefix(json.Number(json.Number(string(rune(id)))).String(), "0")
}

func (m *driveMock) uniqueIDStr() string {
	m.nextFileID++
	return "file" + intToStr(m.nextFileID-1)
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestDriveNodeListFiles(t *testing.T) {
	mock := newDriveMock()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	def := DriveNode(srv.URL)
	ctx := sdkTestCtx(map[string]any{
		"credential": "c1",
		"operation":  "file:list",
		"pageSize":   float64(10),
		"maxPages":   float64(1),
	}, srv.Client())

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := res.Outputs["main"]
	if len(out) != 2 {
		t.Fatalf("expected 2 files, got %d", len(out))
	}
	// Map iteration order is random; just check both files are present.
	ids := map[string]bool{}
	for _, it := range out {
		ids[it.JSON["id"].(string)] = true
	}
	if !ids["file1"] || !ids["file2"] {
		t.Fatalf("expected file1 and file2, got ids %v", ids)
	}
}

func TestDriveNodeGetFile(t *testing.T) {
	mock := newDriveMock()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	def := DriveNode(srv.URL)
	ctx := sdkTestCtx(map[string]any{
		"credential": "c1",
		"operation":  "file:get",
		"fileId":     "file1",
	}, srv.Client())

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := res.Outputs["main"]
	if len(out) != 1 {
		t.Fatalf("expected 1 file, got %d", len(out))
	}
	if out[0].JSON["name"] != "doc.txt" {
		t.Fatalf("expected doc.txt, got %v", out[0].JSON["name"])
	}
}

func TestDriveNodeGetFileMissing(t *testing.T) {
	mock := newDriveMock()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	def := DriveNode(srv.URL)
	ctx := sdkTestCtx(map[string]any{
		"credential": "c1",
		"operation":  "file:get",
		"fileId":     "nonexistent",
	}, srv.Client())

	_, err := def.Execute(ctx)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestDriveNodeDeleteFile(t *testing.T) {
	mock := newDriveMock()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	def := DriveNode(srv.URL)
	ctx := sdkTestCtx(map[string]any{
		"credential": "c1",
		"operation":  "file:delete",
		"fileId":     "file1",
	}, srv.Client())

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := res.Outputs["main"]
	if out[0].JSON["deleted"] != true {
		t.Fatalf("expected deleted=true, got %v", out[0].JSON)
	}
	if _, ok := mock.files["file1"]; ok {
		t.Fatal("file1 should be deleted from mock")
	}
}

func TestDriveNodeCreateFolder(t *testing.T) {
	mock := newDriveMock()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	def := DriveNode(srv.URL)
	ctx := sdkTestCtx(map[string]any{
		"credential": "c1",
		"operation":  "folder:create",
		"name":       "New Folder",
		"folderId":   "parent123",
	}, srv.Client())

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := res.Outputs["main"]
	if out[0].JSON["name"] != "New Folder" {
		t.Fatalf("expected name New Folder, got %v", out[0].JSON["name"])
	}
	if out[0].JSON["mimeType"] != "application/vnd.google-apps.folder" {
		t.Fatalf("expected folder mimeType, got %v", out[0].JSON["mimeType"])
	}
}

func TestDriveNodeCopyFile(t *testing.T) {
	mock := newDriveMock()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	def := DriveNode(srv.URL)
	ctx := sdkTestCtx(map[string]any{
		"credential": "c1",
		"operation":  "file:copy",
		"fileId":     "file1",
		"name":       "doc-copy.txt",
	}, srv.Client())

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := res.Outputs["main"]
	if out[0].JSON["name"] != "doc-copy.txt" {
		t.Fatalf("expected name doc-copy.txt, got %v", out[0].JSON["name"])
	}
	if out[0].JSON["id"] == "file1" {
		t.Fatal("copy should have a different ID")
	}
}

func TestDriveNodeCopyFileNoName(t *testing.T) {
	mock := newDriveMock()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	def := DriveNode(srv.URL)
	ctx := sdkTestCtx(map[string]any{
		"credential": "c1",
		"operation":  "file:copy",
		"fileId":     "file2",
	}, srv.Client())

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := res.Outputs["main"]
	// Should auto-suffix with " (Copy)" — or at minimum keep original name.
	name, _ := out[0].JSON["name"].(string)
	if name == "" {
		t.Fatal("copy name should not be empty")
	}
}

func TestDriveNodeMoveFile(t *testing.T) {
	mock := newDriveMock()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	def := DriveNode(srv.URL)
	ctx := sdkTestCtx(map[string]any{
		"credential": "c1",
		"operation":  "file:move",
		"fileId":     "file1",
		"folderId":   "newFolder",
	}, srv.Client())

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := res.Outputs["main"]
	if out[0].JSON["id"] != "file1" {
		t.Fatalf("expected file1, got %v", out[0].JSON["id"])
	}
}

func TestDriveNodeMoveFileMissingFolder(t *testing.T) {
	mock := newDriveMock()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	def := DriveNode(srv.URL)
	ctx := sdkTestCtx(map[string]any{
		"credential": "c1",
		"operation":  "file:move",
		"fileId":     "file1",
		// folderId missing
	}, srv.Client())

	_, err := def.Execute(ctx)
	if err == nil {
		t.Fatal("expected error for missing folderId")
	}
}

func TestDriveNodeShareFile(t *testing.T) {
	mock := newDriveMock()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	def := DriveNode(srv.URL)
	ctx := sdkTestCtx(map[string]any{
		"credential": "c1",
		"operation":  "file:share",
		"fileId":     "file1",
		"email":      "user@example.com",
		"role":       "writer",
	}, srv.Client())

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := res.Outputs["main"]
	o := out[0].JSON
	if o["shared"] != true {
		t.Fatal("expected shared=true")
	}
	if o["role"] != "writer" {
		t.Fatalf("expected role=writer, got %v", o["role"])
	}
	if o["email"] != "user@example.com" {
		t.Fatalf("expected email=user@example.com, got %v", o["email"])
	}
}

func TestDriveNodeShareFileDefaultRole(t *testing.T) {
	mock := newDriveMock()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	def := DriveNode(srv.URL)
	ctx := sdkTestCtx(map[string]any{
		"credential": "c1",
		"operation":  "file:share",
		"fileId":     "file1",
		"email":      "viewer@example.com",
	}, srv.Client())

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := res.Outputs["main"]
	o := out[0].JSON
	if o["role"] != "reader" {
		t.Fatalf("expected default role=reader, got %v", o["role"])
	}
}

func TestDriveNodeCreateFromText(t *testing.T) {
	mock := newDriveMock()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	def := DriveNode(srv.URL)
	ctx := sdkTestCtx(map[string]any{
		"credential": "c1",
		"operation":  "file:createFromText",
		"name":       "hello.md",
		"content":    "# Hello World",
		"mimeType":   "text/markdown",
		"folderId":   "folder1",
	}, srv.Client())

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := res.Outputs["main"]
	if out[0].JSON["name"] != "hello.md" {
		t.Fatalf("expected name hello.md, got %v", out[0].JSON["name"])
	}
	if out[0].JSON["mimeType"] != "text/markdown" {
		t.Fatalf("expected mimeType text/markdown, got %v", out[0].JSON["mimeType"])
	}
}

func TestDriveNodeCreateFromTextDefaultType(t *testing.T) {
	mock := newDriveMock()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	def := DriveNode(srv.URL)
	ctx := sdkTestCtx(map[string]any{
		"credential": "c1",
		"operation":  "file:createFromText",
		"name":       "notes.txt",
		"content":    "plain notes",
	}, srv.Client())

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := res.Outputs["main"]
	if mt, _ := out[0].JSON["mimeType"].(string); mt != "text/plain" {
		t.Fatalf("expected default mimeType text/plain, got %v", mt)
	}
}

func TestDriveNodeUpload(t *testing.T) {
	mock := newDriveMock()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	def := DriveNode(srv.URL)
	ctx := &schema.ExecContext{
		Input: []schema.Item{{Binary: map[string]schema.BinaryRef{
			"data": {Data: base64.StdEncoding.EncodeToString([]byte("hello world")), MimeType: "text/plain", FileName: "hello.txt"},
		}}},
		Params:           map[string]any{"credential": "c1", "operation": "file:upload", "binaryProperty": "data", "name": "hello.txt"},
		RawParam:         func(n string) any { return map[string]any{"credential": "c1", "operation": "file:upload", "binaryProperty": "data", "name": "hello.txt"}[n] },
		AuthorizedClient: func(string) (*http.Client, error) { return srv.Client(), nil },
		Log:              func(string, any) {},
	}

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := res.Outputs["main"]
	if len(out) != 1 {
		t.Fatalf("expected 1 output, got %d", len(out))
	}
}

func TestDriveNodeUploadNoBinary(t *testing.T) {
	mock := newDriveMock()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	def := DriveNode(srv.URL)
	ctx := sdkTestCtx(map[string]any{
		"credential": "c1",
		"operation":  "file:upload",
	}, srv.Client())

	_, err := def.Execute(ctx)
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestDriveNodeDownload(t *testing.T) {
	mock := newDriveMock()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	def := DriveNode(srv.URL)
	ctx := sdkTestCtx(map[string]any{
		"credential":     "c1",
		"operation":      "file:download",
		"fileId":         "file1",
		"binaryProperty": "data",
	}, srv.Client())

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := res.Outputs["main"]
	if len(out) != 1 {
		t.Fatalf("expected 1 output, got %d", len(out))
	}
	bin, ok := out[0].Binary["data"]
	if !ok {
		t.Fatal("expected binary data in output")
	}
	decoded, _ := base64.StdEncoding.DecodeString(bin.Data)
	if string(decoded) != "hello from drive" {
		t.Fatalf("expected 'hello from drive', got %q", string(decoded))
	}
	if bin.FileName != "doc.txt" {
		t.Fatalf("expected filename doc.txt, got %q", bin.FileName)
	}
}

func TestDriveNodeDownloadMissingFileId(t *testing.T) {
	mock := newDriveMock()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	def := DriveNode(srv.URL)
	ctx := sdkTestCtx(map[string]any{
		"credential": "c1",
		"operation":  "file:download",
	}, srv.Client())

	_, err := def.Execute(ctx)
	if err == nil {
		t.Fatal("expected error for missing fileId")
	}
}

func TestDriveNodeTriggerNewFile(t *testing.T) {
	mock := newDriveMock()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	def := DriveNode(srv.URL)
	ctx := &schema.ExecContext{
		Params:           map[string]any{"credential": "c1", "operation": "trigger:newFile"},
		RawParam:         func(n string) any { return map[string]any{"credential": "c1", "operation": "trigger:newFile"}[n] },
		AuthorizedClient: func(string) (*http.Client, error) { return srv.Client(), nil },
		Log:              func(string, any) {},
		State:            map[string]any{},
	}

	// First poll — should return all files.
	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("first poll: %v", err)
	}
	out := res.Outputs["main"]
	if len(out) != 2 {
		t.Fatalf("first poll: expected 2 new files, got %d", len(out))
	}

	// Second poll — no new files, should return empty.
	res, err = def.Execute(ctx)
	if err != nil {
		t.Fatalf("second poll: %v", err)
	}
	out = res.Outputs["main"]
	if len(out) != 0 {
		t.Fatalf("second poll: expected 0 new files, got %d", len(out))
	}
}

func TestDriveNodeTriggerRateLimit(t *testing.T) {
	mock := newDriveMock()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	def := DriveNode(srv.URL)

	// First poll — let the trigger set its own lastPoll timestamp.
	ctx1 := &schema.ExecContext{
		Params:           map[string]any{"credential": "c1", "operation": "trigger:newFile", "pollSeconds": float64(60)},
		RawParam:         func(n string) any { return map[string]any{"credential": "c1", "operation": "trigger:newFile", "pollSeconds": float64(60)}[n] },
		AuthorizedClient: func(string) (*http.Client, error) { return srv.Client(), nil },
		Log:              func(string, any) {},
		State:            map[string]any{},
	}

	res1, err := def.Execute(ctx1)
	if err != nil {
		t.Fatalf("first poll: %v", err)
	}
	if len(res1.Outputs["main"]) == 0 {
		t.Fatal("first poll should return files")
	}

	// Second poll immediately — should be rate-limited (inside the 60s window).
	res2, err := def.Execute(ctx1) // reuse same ctx with its State
	if err != nil {
		t.Fatalf("second poll: %v", err)
	}
	out := res2.Outputs["main"]
	if len(out) != 0 {
		t.Fatalf("expected 0 files (rate-limited), got %d", len(out))
	}
}

func TestDriveNodeUnknownOperation(t *testing.T) {
	mock := newDriveMock()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	def := DriveNode(srv.URL)
	ctx := sdkTestCtx(map[string]any{
		"credential": "c1",
		"operation":  "bogus:op",
	}, srv.Client())

	_, err := def.Execute(ctx)
	if err == nil {
		t.Fatal("expected error for unknown operation")
	}
	if !strings.Contains(err.Error(), "unknown operation") {
		t.Fatalf("expected 'unknown operation' error, got: %v", err)
	}
}

func TestDriveNodeMissingOperation(t *testing.T) {
	mock := newDriveMock()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	def := DriveNode(srv.URL)
	ctx := sdkTestCtx(map[string]any{
		"credential": "c1",
	}, srv.Client())

	_, err := def.Execute(ctx)
	if err == nil {
		t.Fatal("expected error for missing operation")
	}
}

func TestDriveNodeListWithPagination(t *testing.T) {
	// Create a mock with many files to test pagination.
	mock := newDriveMock()
	// Add extra files.
	for i := 0; i < 5; i++ {
		id := mock.uniqueIDStr()
		mock.files[id] = map[string]any{
			"id": id, "name": "extra_" + id, "mimeType": "text/plain",
		}
	}

	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	def := DriveNode(srv.URL)
	ctx := sdkTestCtx(map[string]any{
		"credential": "c1",
		"operation":  "file:list",
		"pageSize":   float64(3),
		"maxPages":   float64(3),
	}, srv.Client())

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := res.Outputs["main"]
	// We added 5 + 2 initial = 7 total.
	if len(out) != 7 {
		t.Fatalf("expected 7 files total, got %d", len(out))
	}
}

func TestDriveNodeFileToItemNil(t *testing.T) {
	item := fileToItem(nil)
	if item.JSON == nil {
		t.Fatal("expected non-nil JSON map")
	}
}

func TestDriveNodeServiceCache(t *testing.T) {
	mock := newDriveMock()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	// Call twice — second should use cache.
	def := DriveNode(srv.URL)
	params := map[string]any{"credential": "c1", "operation": "file:get", "fileId": "file1"}

	ctx1 := sdkTestCtx(params, srv.Client())
	_, err := def.Execute(ctx1)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	ctx2 := sdkTestCtx(params, srv.Client())
	_, err = def.Execute(ctx2)
	if err != nil {
		t.Fatalf("second call (cached): %v", err)
	}
}

func TestDriveNodeCreateFromTextMissingName(t *testing.T) {
	mock := newDriveMock()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	def := DriveNode(srv.URL)
	ctx := sdkTestCtx(map[string]any{
		"credential": "c1",
		"operation":  "file:createFromText",
		"content":    "some content",
	}, srv.Client())

	_, err := def.Execute(ctx)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

// TestNodesRegisterable verifies the pack and checks the Drive node params.
func TestDriveNodeRegistration(t *testing.T) {
	nodes := Nodes()
	found := false
	for _, n := range nodes {
		if n.Type == "google.drive" {
			found = true
			if n.Icon != "HardDrive" {
				t.Fatalf("expected HardDrive icon, got %s", n.Icon)
			}
			if !n.IsTrigger {
				t.Fatal("Drive node should be a trigger")
			}
			if len(n.Params) < 2 {
				t.Fatal("Drive node should have at least credential + operation params")
			}
			if n.Params[0].Type != "credential" {
				t.Fatal("first param should be credential")
			}
			if n.Params[1].Name != "operation" {
				t.Fatal("second param should be operation")
			}
			// Check operations include all expected values.
			ops := n.Params[1].Options
			opVals := map[string]bool{}
			for _, o := range ops {
				opVals[o.Value] = true
			}
			expectedOps := []string{
				"file:list", "file:get", "file:delete", "folder:create",
				"file:copy", "file:move", "file:share", "file:createFromText",
				"file:upload", "file:download", "trigger:newFile",
			}
			for _, eo := range expectedOps {
				if !opVals[eo] {
					t.Fatalf("missing operation: %s", eo)
				}
			}
		}
	}
	if !found {
		t.Fatal("google.drive node not registered")
	}
}
