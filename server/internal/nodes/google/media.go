package google

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

const (
	uploadBaseProd = "https://www.googleapis.com/upload/drive/v3"
	driveAPIProd   = "https://www.googleapis.com/drive/v3"
)

// These two nodes are native (not declarative) because Drive media moves binary,
// which the JSON-oriented REST framework doesn't handle. Bytes currently round-trip
// through the in-memory base64 Item.Binary model; a streaming binary store (a
// Phase-0 TODO) would let very large files pipe through without buffering.

func credParam() schema.ParamSchema {
	return schema.ParamSchema{Name: "credential", Label: "Credential", Type: "credential", Required: true, CredentialType: credType}
}

func strOr(v any, def string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}

// DriveUpload uploads an input item's binary to Google Drive via a multipart upload.
func DriveUpload(uploadBase string) schema.NodeDefinition {
	return schema.NodeDefinition{
		Type: "google.driveUpload", Label: "Google Drive: Upload", Group: "integration", Icon: "Upload",
		Description: "Upload a file (from an item's binary) to Google Drive.",
		Inputs:      []schema.Port{{ID: "main"}},
		Outputs:     []schema.Port{{ID: "main"}},
		Credentials: []string{credType},
		Params: []schema.ParamSchema{
			credParam(),
			{Name: "name", Label: "File name (defaults to the binary's)", Type: "string"},
			{Name: "folderId", Label: "Parent folder ID", Type: "string"},
			{Name: "binaryProperty", Label: "Input binary property", Type: "string", Default: "data"},
		},
		Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
			prop := strOr(ctx.Params["binaryProperty"], "data")
			if len(ctx.Input) == 0 || ctx.Input[0].Binary == nil {
				return schema.NodeResult{}, fmt.Errorf("no input binary to upload")
			}
			ref, ok := ctx.Input[0].Binary[prop]
			if !ok {
				return schema.NodeResult{}, fmt.Errorf("input has no binary property %q", prop)
			}
			data, err := base64.StdEncoding.DecodeString(ref.Data)
			if err != nil {
				return schema.NodeResult{}, fmt.Errorf("decode binary: %w", err)
			}
			mimeType := ref.MimeType
			if mimeType == "" {
				mimeType = "application/octet-stream"
			}
			meta := map[string]any{"name": strOr(ctx.Params["name"], orDefault(ref.FileName, "upload"))}
			if f := strOr(ctx.Params["folderId"], ""); f != "" {
				meta["parents"] = []string{f}
			}

			// multipart/related: metadata JSON part + media part (no Content-Disposition).
			var body bytes.Buffer
			mw := multipart.NewWriter(&body)
			mh := textproto.MIMEHeader{}
			mh.Set("Content-Type", "application/json; charset=UTF-8")
			if pw, err := mw.CreatePart(mh); err == nil {
				_ = json.NewEncoder(pw).Encode(meta)
			}
			mh2 := textproto.MIMEHeader{}
			mh2.Set("Content-Type", mimeType)
			if pw, err := mw.CreatePart(mh2); err == nil {
				_, _ = pw.Write(data)
			}
			_ = mw.Close()

			u := strings.TrimRight(uploadBase, "/") + "/files?uploadType=multipart&fields=id,name,mimeType,size,webViewLink"
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, u, &body)
			req.Header.Set("Content-Type", "multipart/related; boundary="+mw.Boundary())

			client, err := ctx.AuthorizedClient("credential")
			if err != nil {
				return schema.NodeResult{}, err
			}
			res, err := client.Do(req)
			if err != nil {
				return schema.NodeResult{}, err
			}
			defer res.Body.Close()
			raw, _ := io.ReadAll(res.Body)
			if res.StatusCode >= 400 {
				return schema.NodeResult{}, fmt.Errorf("drive upload %d: %s", res.StatusCode, string(raw))
			}
			var fileMeta map[string]any
			_ = json.Unmarshal(raw, &fileMeta)
			if fileMeta == nil {
				fileMeta = map[string]any{}
			}
			return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{JSON: fileMeta}}}}, nil
		},
	}
}

// DriveDownload downloads a Drive file's content into an item's binary.
func DriveDownload(apiBase string) schema.NodeDefinition {
	return schema.NodeDefinition{
		Type: "google.driveDownload", Label: "Google Drive: Download", Group: "integration", Icon: "Download",
		Description: "Download a Google Drive file's content into an item's binary.",
		Inputs:      []schema.Port{{ID: "main"}},
		Outputs:     []schema.Port{{ID: "main"}},
		Credentials: []string{credType},
		Params: []schema.ParamSchema{
			credParam(),
			{Name: "fileId", Label: "File ID", Type: "string", Required: true},
			{Name: "binaryProperty", Label: "Output binary property", Type: "string", Default: "data"},
		},
		Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
			fileID := strOr(ctx.Params["fileId"], "")
			if fileID == "" {
				return schema.NodeResult{}, fmt.Errorf("fileId is required")
			}
			prop := strOr(ctx.Params["binaryProperty"], "data")
			client, err := ctx.AuthorizedClient("credential")
			if err != nil {
				return schema.NodeResult{}, err
			}
			base := strings.TrimRight(apiBase, "/") + "/files/" + url.PathEscape(fileID)

			// metadata
			meta := map[string]any{}
			metaReq, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"?fields=id,name,mimeType,size", nil)
			if metaRes, err := client.Do(metaReq); err == nil {
				b, _ := io.ReadAll(metaRes.Body)
				metaRes.Body.Close()
				if metaRes.StatusCode < 400 {
					_ = json.Unmarshal(b, &meta)
				}
			}

			// content
			mediaReq, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"?alt=media", nil)
			mediaRes, err := client.Do(mediaReq)
			if err != nil {
				return schema.NodeResult{}, err
			}
			defer mediaRes.Body.Close()
			content, _ := io.ReadAll(mediaRes.Body)
			if mediaRes.StatusCode >= 400 {
				return schema.NodeResult{}, fmt.Errorf("drive download %d: %s", mediaRes.StatusCode, string(content))
			}

			name, _ := meta["name"].(string)
			mimeType, _ := meta["mimeType"].(string)
			if mimeType == "" {
				mimeType = mediaRes.Header.Get("Content-Type")
			}
			item := schema.Item{
				JSON: meta,
				Binary: map[string]schema.BinaryRef{
					prop: {Data: base64.StdEncoding.EncodeToString(content), MimeType: mimeType, FileName: name},
				},
			}
			return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {item}}}, nil
		},
	}
}

func orDefault(s, def string) string {
	if s != "" {
		return s
	}
	return def
}
