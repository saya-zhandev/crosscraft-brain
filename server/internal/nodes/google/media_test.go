package google

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

func TestDriveUpload(t *testing.T) {
	var mediaType, gotName, gotMedia string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/files" {
			http.Error(w, "nope", http.StatusNotFound)
			return
		}
		mt, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		mediaType = mt
		mr := multipart.NewReader(r.Body, params["boundary"])
		p1, _ := mr.NextPart()
		b1, _ := io.ReadAll(p1)
		var meta map[string]any
		_ = json.Unmarshal(b1, &meta)
		gotName, _ = meta["name"].(string)
		p2, _ := mr.NextPart()
		b2, _ := io.ReadAll(p2)
		gotMedia = string(b2)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "f1", "name": gotName})
	}))
	defer srv.Close()

	def := DriveUpload(srv.URL)
	called := false
	ctx := &schema.ExecContext{
		Input: []schema.Item{{Binary: map[string]schema.BinaryRef{
			"data": {Data: base64.StdEncoding.EncodeToString([]byte("hello")), MimeType: "text/plain", FileName: "hello.txt"},
		}}},
		Params:           map[string]any{"credential": "c1", "name": "hello.txt", "binaryProperty": "data"},
		AuthorizedClient: func(string) (*http.Client, error) { called = true; return srv.Client(), nil },
		Log:              func(string, any) {},
	}
	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected the OAuth2 client to be used")
	}
	if mediaType != "multipart/related" {
		t.Fatalf("upload content-type: %s", mediaType)
	}
	if gotName != "hello.txt" || gotMedia != "hello" {
		t.Fatalf("uploaded name=%q media=%q", gotName, gotMedia)
	}
	if res.Outputs["main"][0].JSON["id"] != "f1" {
		t.Fatalf("output: %+v", res.Outputs["main"])
	}
}

func TestDriveDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/files/F1" {
			http.Error(w, "nope", http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("alt") == "media" {
			_, _ = w.Write([]byte("hello world"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "F1", "name": "doc.txt", "mimeType": "text/plain"})
	}))
	defer srv.Close()

	def := DriveDownload(srv.URL)
	ctx := &schema.ExecContext{
		Params:           map[string]any{"credential": "c1", "fileId": "F1", "binaryProperty": "data"},
		AuthorizedClient: func(string) (*http.Client, error) { return srv.Client(), nil },
		Log:              func(string, any) {},
	}
	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	out := res.Outputs["main"][0]
	bin, ok := out.Binary["data"]
	if !ok {
		t.Fatalf("no binary in output: %+v", out)
	}
	if bin.FileName != "doc.txt" {
		t.Fatalf("filename: %s", bin.FileName)
	}
	if got, _ := base64.StdEncoding.DecodeString(bin.Data); string(got) != "hello world" {
		t.Fatalf("content: %q", string(got))
	}
}
