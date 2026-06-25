package google

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func gmailOauthCtx(params map[string]any, client *http.Client, called *bool) *schema.ExecContext {
	return &schema.ExecContext{
		Params:   params,
		RawParam: func(n string) any { return params[n] },
		AuthorizedClient: func(string) (*http.Client, error) {
			*called = true
			return client, nil
		},
		Log: func(string, any) {},
	}
}

// ---------------------------------------------------------------------------
// message:list
// ---------------------------------------------------------------------------

func TestGmailListMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/gmail/v1/users/me/messages" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"messages": []any{
					map[string]any{"id": "m1", "threadId": "t1"},
					map[string]any{"id": "m2", "threadId": "t2"},
				},
			})
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := GmailNode(srv.URL)
	called := false
	ctx := gmailOauthCtx(map[string]any{
		"operation":  "message:list",
		"credential": "c1",
	}, srv.Client(), &called)

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !called {
		t.Fatal("expected OAuth2 client to be used")
	}
	out := res.Outputs["main"]
	if len(out) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(out))
	}
	if out[0].JSON["id"] != "m1" {
		t.Fatalf("expected first message id m1, got %v", out[0].JSON["id"])
	}
}

// ---------------------------------------------------------------------------
// message:get
// ---------------------------------------------------------------------------

func TestGmailGetMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/gmail/v1/users/me/messages/msg123" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":       "msg123",
				"threadId": "t1",
				"snippet":  "Hello world",
				"labelIds": []any{"INBOX", "UNREAD"},
				"payload": map[string]any{
					"mimeType": "text/plain",
					"headers":  []any{map[string]any{"name": "Subject", "value": "Test"}},
					"body":     map[string]any{"size": 11, "data": "SGVsbG8gd29ybGQ="},
				},
			})
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := GmailNode(srv.URL)
	called := false
	ctx := gmailOauthCtx(map[string]any{
		"operation":  "message:get",
		"messageId":  "msg123",
		"credential": "c1",
	}, srv.Client(), &called)

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := res.Outputs["main"]
	if len(out) != 1 {
		t.Fatalf("expected 1 message, got %d", len(out))
	}
	msg := out[0].JSON
	if msg["id"] != "msg123" || msg["snippet"] != "Hello world" {
		t.Fatalf("unexpected message: %+v", msg)
	}
}

// ---------------------------------------------------------------------------
// message:send
// ---------------------------------------------------------------------------

func TestGmailSendMessage(t *testing.T) {
	var gotMethod, gotPath string
	var sentBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if r.Method == http.MethodPost && r.URL.Path == "/gmail/v1/users/me/messages/send" {
			_ = json.NewDecoder(r.Body).Decode(&sentBody)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":       "sent1",
				"threadId": "t-new",
				"labelIds": []any{"SENT"},
			})
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := GmailNode(srv.URL)
	called := false
	ctx := gmailOauthCtx(map[string]any{
		"operation":    "message:send",
		"to":           "bob@example.com",
		"subject":      "Hello",
		"body":         "Test body",
		"contentType":  "text/plain",
		"credential":   "c1",
	}, srv.Client(), &called)

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if gotMethod != http.MethodPost || gotPath != "/gmail/v1/users/me/messages/send" {
		t.Fatalf("unexpected call: %s %s", gotMethod, gotPath)
	}
	if sentBody == nil {
		t.Fatal("expected a request body")
	}
	raw, _ := sentBody["raw"].(string)
	if raw == "" {
		t.Fatal("expected raw MIME message in payload")
	}
	// Decode and verify the raw message contains expected headers.
	decoded, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(raw)
	if err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	rawStr := string(decoded)
	if !strings.Contains(rawStr, "To: bob@example.com") {
		t.Fatalf("expected To header, got:\n%s", rawStr)
	}
	if !strings.Contains(rawStr, "Subject: Hello") {
		t.Fatalf("expected Subject header, got:\n%s", rawStr)
	}
	if !strings.Contains(rawStr, "Test body") {
		t.Fatalf("expected body, got:\n%s", rawStr)
	}

	out := res.Outputs["main"]
	if len(out) != 1 || out[0].JSON["id"] != "sent1" {
		t.Fatalf("unexpected send result: %+v", out)
	}
}

// ---------------------------------------------------------------------------
// message:reply
// ---------------------------------------------------------------------------

func TestGmailReplyMessage(t *testing.T) {
	var gotPath string
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		callCount++

		// First call: get original message metadata.
		if r.Method == http.MethodGet && r.URL.Path == "/gmail/v1/users/me/messages/orig1" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":       "orig1",
				"threadId": "t1",
				"payload": map[string]any{
					"headers": []any{
						map[string]any{"name": "Subject", "value": "Original Subject"},
						map[string]any{"name": "Message-ID", "value": "<orig@mail.com>"},
					},
				},
			})
			return
		}

		// Second call: send the reply.
		if r.Method == http.MethodPost && r.URL.Path == "/gmail/v1/users/me/messages/send" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":       "reply1",
				"threadId": "t1",
				"labelIds": []any{"SENT"},
			})
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := GmailNode(srv.URL)
	called := false
	ctx := gmailOauthCtx(map[string]any{
		"operation":  "message:reply",
		"messageId":  "orig1",
		"body":       "Reply text",
		"credential": "c1",
	}, srv.Client(), &called)

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v (path=%s, calls=%d)", err, gotPath, callCount)
	}
	out := res.Outputs["main"]
	if len(out) != 1 || out[0].JSON["id"] != "reply1" {
		t.Fatalf("unexpected reply result: %+v", out)
	}
}

// ---------------------------------------------------------------------------
// message:modify
// ---------------------------------------------------------------------------

func TestGmailModifyMessage(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/gmail/v1/users/me/messages/msg1/modify" {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":       "msg1",
				"labelIds": []any{"INBOX", "IMPORTANT"},
			})
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := GmailNode(srv.URL)
	called := false
	ctx := gmailOauthCtx(map[string]any{
		"operation":    "message:modify",
		"messageId":    "msg1",
		"addLabelIds":  "IMPORTANT,STARRED",
		"removeLabelIds": "UNREAD",
		"credential":   "c1",
	}, srv.Client(), &called)

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	// Verify the modify request.
	if gotBody == nil {
		t.Fatal("expected a request body")
	}
	addLabels := gotBody["addLabelIds"].([]any)
	removeLabels := gotBody["removeLabelIds"].([]any)
	if len(addLabels) != 2 || addLabels[0] != "IMPORTANT" || addLabels[1] != "STARRED" {
		t.Fatalf("unexpected addLabelIds: %v", addLabels)
	}
	if len(removeLabels) != 1 || removeLabels[0] != "UNREAD" {
		t.Fatalf("unexpected removeLabelIds: %v", removeLabels)
	}

	out := res.Outputs["main"]
	if len(out) != 1 || out[0].JSON["id"] != "msg1" {
		t.Fatalf("unexpected modify result: %+v", out)
	}
}

// ---------------------------------------------------------------------------
// Draft CRUD
// ---------------------------------------------------------------------------

func TestGmailDraftCreate(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method == http.MethodPost && r.URL.Path == "/gmail/v1/users/me/drafts" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "draft1",
				"message": map[string]any{
					"id":       "msgDraft",
					"threadId": "t-draft",
				},
			})
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := GmailNode(srv.URL)
	called := false
	ctx := gmailOauthCtx(map[string]any{
		"operation":  "draft:create",
		"to":         "alice@example.com",
		"subject":    "Draft Subject",
		"body":       "Draft body",
		"credential": "c1",
	}, srv.Client(), &called)

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v (path=%s)", err, gotPath)
	}
	out := res.Outputs["main"]
	if len(out) != 1 || out[0].JSON["id"] != "draft1" {
		t.Fatalf("unexpected draft result: %+v", out)
	}
}

func TestGmailDraftDelete(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if r.Method == http.MethodDelete && r.URL.Path == "/gmail/v1/users/me/drafts/draft1" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := GmailNode(srv.URL)
	called := false
	ctx := gmailOauthCtx(map[string]any{
		"operation":  "draft:delete",
		"draftId":    "draft1",
		"credential": "c1",
	}, srv.Client(), &called)

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/gmail/v1/users/me/drafts/draft1" {
		t.Fatalf("unexpected delete call: %s %s", gotMethod, gotPath)
	}
	if res.Outputs["main"][0].JSON["deleted"] != true {
		t.Fatal("expected deleted=true")
	}
}

// ---------------------------------------------------------------------------
// Threads
// ---------------------------------------------------------------------------

func TestGmailGetThread(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/gmail/v1/users/me/threads/t1" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "t1",
				"snippet": "Thread snippet",
				"messages": []any{
					map[string]any{"id": "m1", "snippet": "First"},
					map[string]any{"id": "m2", "snippet": "Second"},
				},
			})
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := GmailNode(srv.URL)
	called := false
	ctx := gmailOauthCtx(map[string]any{
		"operation":  "thread:get",
		"threadId":   "t1",
		"credential": "c1",
	}, srv.Client(), &called)

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := res.Outputs["main"]
	if len(out) != 1 {
		t.Fatalf("expected 1 thread, got %d", len(out))
	}
	thread := out[0].JSON
	if thread["id"] != "t1" {
		t.Fatalf("unexpected thread: %+v", thread)
	}
		msgs, _ := thread["messages"].([]map[string]any)
		if len(msgs) != 2 {
			t.Fatalf("expected 2 messages in thread, got %d", len(msgs))
		}
}

// ---------------------------------------------------------------------------
// Labels
// ---------------------------------------------------------------------------

func TestGmailListLabels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/gmail/v1/users/me/labels" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"labels": []any{
					map[string]any{"id": "INBOX", "name": "INBOX", "type": "system"},
					map[string]any{"id": "Label_1", "name": "MyLabel", "type": "user"},
				},
			})
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := GmailNode(srv.URL)
	called := false
	ctx := gmailOauthCtx(map[string]any{
		"operation":  "label:list",
		"credential": "c1",
	}, srv.Client(), &called)

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := res.Outputs["main"]
	if len(out) != 2 {
		t.Fatalf("expected 2 labels, got %d", len(out))
	}
}

func TestGmailCreateLabel(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/gmail/v1/users/me/labels" {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":   "Label_1",
				"name": "MyLabel",
				"type": "user",
			})
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := GmailNode(srv.URL)
	called := false
	ctx := gmailOauthCtx(map[string]any{
		"operation":    "label:create",
		"labelName":    "MyLabel",
		"labelListVisibility": "labelShow",
		"credential":   "c1",
	}, srv.Client(), &called)

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if name, _ := gotBody["name"].(string); name != "MyLabel" {
		t.Fatalf("expected label name MyLabel, got %v", gotBody)
	}
	out := res.Outputs["main"]
	if len(out) != 1 || out[0].JSON["id"] != "Label_1" {
		t.Fatalf("unexpected create result: %+v", out)
	}
}

func TestGmailDeleteLabel(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if r.Method == http.MethodDelete && r.URL.Path == "/gmail/v1/users/me/labels/Label_1" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := GmailNode(srv.URL)
	called := false
	ctx := gmailOauthCtx(map[string]any{
		"operation":  "label:delete",
		"labelId":    "Label_1",
		"credential": "c1",
	}, srv.Client(), &called)

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/gmail/v1/users/me/labels/Label_1" {
		t.Fatalf("unexpected delete call: %s %s", gotMethod, gotPath)
	}
	if res.Outputs["main"][0].JSON["deleted"] != true {
		t.Fatal("expected deleted=true")
	}
}

// ---------------------------------------------------------------------------
// Trigger: new email polling
// ---------------------------------------------------------------------------

func TestGmailTriggerNewEmail(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		// messages.list
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/users/me/messages") {
			if callCount == 1 {
				// First poll: 2 new messages.
				_ = json.NewEncoder(w).Encode(map[string]any{
					"messages": []any{
						map[string]any{"id": "new1", "threadId": "t1"},
						map[string]any{"id": "new2", "threadId": "t2"},
					},
					"resultSizeEstimate": 2,
				})
			} else {
				// Second poll: same 2 messages (no new ones).
				_ = json.NewEncoder(w).Encode(map[string]any{
					"messages": []any{
						map[string]any{"id": "new2", "threadId": "t2"},
						map[string]any{"id": "new1", "threadId": "t1"},
					},
					"resultSizeEstimate": 2,
				})
			}
			return
		}

		// messages.get for individual messages.
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/users/me/messages/") {
			id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":       id,
				"threadId": "t-" + id,
				"snippet":  "Snippet for " + id,
			})
			return
		}

		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := GmailNode(srv.URL)
	called := false
	ctx := gmailOauthCtx(map[string]any{
		"operation":  "trigger:newEmail",
		"credential": "c1",
	}, srv.Client(), &called)
	ctx.State = map[string]any{}

	// Poll 1 — should emit 2 new messages.
	res1, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("poll 1: %v", err)
	}
	if n := len(res1.Outputs["main"]); n != 2 {
		t.Fatalf("poll 1: expected 2 new messages, got %d", n)
	}

	// Reset rate-limit for second poll.
	delete(ctx.State, "gmail:lastPoll:/gmail/v1:newer_than:1d")

	// Poll 2 — should emit 0 (no new message IDs).
	res2, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("poll 2: %v", err)
	}
	if n := len(res2.Outputs["main"]); n != 0 {
		t.Fatalf("poll 2: expected 0 new messages, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Operation registration
// ---------------------------------------------------------------------------

func TestGmailNodeIncludesAllOperations(t *testing.T) {
	def := GmailNode("https://example.test/")
	ops := map[string]bool{}
	for _, p := range def.Params {
		if p.Name == "operation" {
			for _, opt := range p.Options {
				ops[opt.Value] = true
			}
		}
	}

	wantOps := []string{
		"message:list", "message:get", "message:send", "message:reply", "message:modify",
		"draft:create", "draft:update", "draft:delete", "draft:get", "draft:list",
		"thread:get", "thread:list", "thread:modify",
		"label:list", "label:create", "label:update", "label:delete",
		"trigger:newEmail",
	}
	for _, want := range wantOps {
		if !ops[want] {
			t.Fatalf("expected operation %q to be registered", want)
		}
	}
	if len(ops) != len(wantOps) {
		t.Fatalf("expected %d operations, got %d (%v)", len(wantOps), len(ops), ops)
	}
}

// ---------------------------------------------------------------------------
// Error paths
// ---------------------------------------------------------------------------

func TestGmailSendRequiresTo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer srv.Close()

	def := GmailNode(srv.URL)
	called := false
	ctx := gmailOauthCtx(map[string]any{
		"operation":  "message:send",
		"subject":    "No recipient",
		"body":       "Body",
		"credential": "c1",
		// "to" intentionally omitted
	}, srv.Client(), &called)

	_, err := def.Execute(ctx)
	if err == nil {
		t.Fatal("expected error for missing 'to', got nil")
	}
}

func TestGmailGetRequiresMessageId(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer srv.Close()

	def := GmailNode(srv.URL)
	called := false
	ctx := gmailOauthCtx(map[string]any{
		"operation":  "message:get",
		"credential": "c1",
		// "messageId" intentionally omitted
	}, srv.Client(), &called)

	_, err := def.Execute(ctx)
	if err == nil {
		t.Fatal("expected error for missing messageId, got nil")
	}
}

// ---------------------------------------------------------------------------
// MIME building: attachment support
// ---------------------------------------------------------------------------

func TestGmailSendWithAttachment(t *testing.T) {
	var sentBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/gmail/v1/users/me/messages/send" {
			_ = json.NewDecoder(r.Body).Decode(&sentBody)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":       "sent-att",
				"threadId": "t-att",
			})
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := GmailNode(srv.URL)
	called := false
	ctx := gmailOauthCtx(map[string]any{
		"operation":   "message:send",
		"to":          "bob@example.com",
		"subject":     "With attachment",
		"body":        "See attached",
		"contentType": "text/plain",
		"credential":  "c1",
		"attachments": map[string]any{
			"attachments": []any{
				map[string]any{
					"filename": "test.txt",
					"data":     "SGVsbG8gV29ybGQ=", // "Hello World" in base64
					"mimeType": "text/plain",
				},
			},
		},
	}, srv.Client(), &called)

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	raw, _ := sentBody["raw"].(string)
	if raw == "" {
		t.Fatal("expected raw MIME message")
	}
	decoded, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(raw)
	if err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	rawStr := string(decoded)
	if !strings.Contains(rawStr, "Content-Type: multipart/mixed") {
		t.Fatalf("expected multipart/mixed for attachment, got:\n%s", rawStr)
	}
	if !strings.Contains(rawStr, "test.txt") {
		t.Fatalf("expected attachment filename, got:\n%s", rawStr)
	}

	out := res.Outputs["main"]
	if len(out) != 1 || out[0].JSON["id"] != "sent-att" {
		t.Fatalf("unexpected result: %+v", out)
	}
}

var _ = schema.ExecContext{}
