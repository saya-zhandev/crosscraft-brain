package google

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

func calOauthCtx(params map[string]any, client *http.Client, called *bool) *schema.ExecContext {
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
// Calendar list
// ---------------------------------------------------------------------------

func TestCalListCalendars(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/calendar/v3/users/me/calendarList" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []any{
					map[string]any{"id": "primary", "summary": "Primary Calendar", "primary": true},
					map[string]any{"id": "cal2", "summary": "Work", "timeZone": "America/Los_Angeles"},
				},
			})
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/calendar/v3/users/me/calendarList/primary" ||
			r.URL.Path == "/calendar/v3/users/me/calendarList/cal2" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{})
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := CalendarNode(srv.URL + "/calendar/v3")
	called := false
	ctx := calOauthCtx(map[string]any{
		"operation":  "calendar:list",
		"credential": "c1",
	}, srv.Client(), &called)

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := res.Outputs["main"]
	if len(out) != 2 {
		t.Fatalf("expected 2 calendars, got %d", len(out))
	}
	if out[0].JSON["id"] != "primary" {
		t.Fatalf("expected first calendar primary, got %v", out[0].JSON["id"])
	}
}

// ---------------------------------------------------------------------------
// Event list
// ---------------------------------------------------------------------------

func TestCalListEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/calendars/primary/events") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []any{
					map[string]any{
						"id": "e1", "summary": "Event 1",
						"start": map[string]any{"dateTime": "2025-01-01T10:00:00-08:00"},
						"end":   map[string]any{"dateTime": "2025-01-01T11:00:00-08:00"},
					},
					map[string]any{
						"id": "e2", "summary": "Event 2",
						"start": map[string]any{"date": "2025-01-02"},
						"end":   map[string]any{"date": "2025-01-02"},
					},
				},
			})
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := CalendarNode(srv.URL + "/calendar/v3")
	called := false
	ctx := calOauthCtx(map[string]any{
		"operation":  "event:list",
		"calendarId": "primary",
		"credential": "c1",
	}, srv.Client(), &called)

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := res.Outputs["main"]
	if len(out) != 2 {
		t.Fatalf("expected 2 events, got %d", len(out))
	}
	if out[0].JSON["id"] != "e1" {
		t.Fatalf("expected first event e1, got %v", out[0].JSON["id"])
	}
}

// ---------------------------------------------------------------------------
// Event get
// ---------------------------------------------------------------------------

func TestCalGetEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/calendars/primary/events/ev1") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "ev1", "summary": "Important Meeting", "description": "Discuss roadmap",
				"location": "Room 101",
				"start":    map[string]any{"dateTime": "2025-06-01T14:00:00Z", "timeZone": "UTC"},
				"end":      map[string]any{"dateTime": "2025-06-01T15:00:00Z", "timeZone": "UTC"},
				"attendees": []any{
					map[string]any{"email": "bob@example.com", "responseStatus": "accepted"},
				},
			})
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := CalendarNode(srv.URL + "/calendar/v3")
	called := false
	ctx := calOauthCtx(map[string]any{
		"operation":  "event:get",
		"calendarId": "primary",
		"eventId":    "ev1",
		"credential": "c1",
	}, srv.Client(), &called)

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := res.Outputs["main"]
	if len(out) != 1 {
		t.Fatalf("expected 1 event, got %d", len(out))
	}
	ev := out[0].JSON
	if ev["id"] != "ev1" || ev["summary"] != "Important Meeting" {
		t.Fatalf("unexpected event: %+v", ev)
	}
	if atts, ok := ev["attendees"].([]map[string]any); !ok || len(atts) != 1 {
		t.Fatalf("expected 1 attendee, got %+v", ev["attendees"])
	}
}

// ---------------------------------------------------------------------------
// Event create
// ---------------------------------------------------------------------------

func TestCalCreateEvent(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/calendars/primary/events") {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]any{
				"id":      "ev-new",
				"summary": gotBody["summary"],
				"start":   gotBody["start"],
				"end":     gotBody["end"],
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := CalendarNode(srv.URL + "/calendar/v3")
	called := false
	ctx := calOauthCtx(map[string]any{
		"operation":  "event:create",
		"calendarId": "primary",
		"credential": "c1",
		"body": map[string]any{
			"summary": "New Event",
			"start":   map[string]any{"dateTime": "2025-07-01T09:00:00-07:00", "timeZone": "America/Los_Angeles"},
			"end":     map[string]any{"dateTime": "2025-07-01T10:00:00-07:00", "timeZone": "America/Los_Angeles"},
		},
	}, srv.Client(), &called)

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotBody == nil {
		t.Fatal("expected a request body")
	}
	if summary, _ := gotBody["summary"].(string); summary != "New Event" {
		t.Fatalf("expected summary 'New Event', got %v", gotBody)
	}
	out := res.Outputs["main"]
	if len(out) != 1 || out[0].JSON["id"] != "ev-new" {
		t.Fatalf("unexpected create result: %+v", out)
	}
}

// ---------------------------------------------------------------------------
// Event update
// ---------------------------------------------------------------------------

func TestCalUpdateEvent(t *testing.T) {
	var gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/calendars/primary/events/ev1") {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]any{
				"id":      "ev1",
				"summary": gotBody["summary"],
				"updated": "2025-06-15T12:00:00Z",
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := CalendarNode(srv.URL + "/calendar/v3")
	called := false
	ctx := calOauthCtx(map[string]any{
		"operation":  "event:update",
		"calendarId": "primary",
		"eventId":    "ev1",
		"credential": "c1",
		"body": map[string]any{
			"summary":     "Updated Event",
			"description": "New description",
			"attendees": []any{
				map[string]any{"email": "alice@example.com"},
			},
		},
	}, srv.Client(), &called)

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Fatalf("expected PUT for update, got %s", gotMethod)
	}
	if summary, _ := gotBody["summary"].(string); summary != "Updated Event" {
		t.Fatalf("expected updated summary, got %v", gotBody)
	}
	// Verify attendees were parsed.
	if atts := gotBody["attendees"].([]any); len(atts) != 1 {
		t.Fatalf("expected 1 attendee, got %v", atts)
	}

	out := res.Outputs["main"]
	if len(out) != 1 || out[0].JSON["id"] != "ev1" {
		t.Fatalf("unexpected update result: %+v", out)
	}
}

// ---------------------------------------------------------------------------
// Event delete
// ---------------------------------------------------------------------------

func TestCalDeleteEvent(t *testing.T) {
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		if r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/calendars/primary/events/ev1") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := CalendarNode(srv.URL + "/calendar/v3")
	called := false
	ctx := calOauthCtx(map[string]any{
		"operation":  "event:delete",
		"calendarId": "primary",
		"eventId":    "ev1",
		"credential": "c1",
	}, srv.Client(), &called)

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("expected DELETE, got %s", gotMethod)
	}
	if res.Outputs["main"][0].JSON["deleted"] != true {
		t.Fatal("expected deleted=true")
	}
}

// ---------------------------------------------------------------------------
// Free/busy query
// ---------------------------------------------------------------------------

func TestCalFreeBusy(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/calendar/v3/freeBusy" {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"calendars": map[string]any{
					"primary": map[string]any{
						"busy": []any{
							map[string]any{"start": "2025-06-01T10:00:00Z", "end": "2025-06-01T11:00:00Z"},
							map[string]any{"start": "2025-06-01T14:00:00Z", "end": "2025-06-01T15:00:00Z"},
						},
					},
				},
			})
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := CalendarNode(srv.URL + "/calendar/v3")
	called := false
	ctx := calOauthCtx(map[string]any{
		"operation":  "freebusy:query",
		"calendarId": "primary",
		"timeMin":    "2025-06-01T00:00:00Z",
		"timeMax":    "2025-06-02T00:00:00Z",
		"credential": "c1",
	}, srv.Client(), &called)

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	// Verify the request had correct timeMin/timeMax.
	if tMin, _ := gotBody["timeMin"].(string); tMin != "2025-06-01T00:00:00Z" {
		t.Fatalf("expected timeMin in request, got %v", gotBody)
	}

	out := res.Outputs["main"]
	if len(out) != 1 {
		t.Fatalf("expected 1 result, got %d", len(out))
	}
	cal := out[0].JSON
	if cal["calendarId"] != "primary" {
		t.Fatalf("expected calendarId primary, got %v", out[0].JSON)
	}
	busy, _ := cal["busy"].([]map[string]any)
	if len(busy) != 2 {
		t.Fatalf("expected 2 busy periods, got %d", len(busy))
	}
}

// ---------------------------------------------------------------------------
// Free/busy with multiple calendars
// ---------------------------------------------------------------------------

func TestCalFreeBusyMultipleCalendars(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/calendar/v3/freeBusy" {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"calendars": map[string]any{
					"cal1": map[string]any{"busy": []any{}},
					"cal2": map[string]any{"busy": []any{
						map[string]any{"start": "2025-06-01T12:00:00Z", "end": "2025-06-01T13:00:00Z"},
					}},
				},
			})
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := CalendarNode(srv.URL + "/calendar/v3")
	called := false
	ctx := calOauthCtx(map[string]any{
		"operation":   "freebusy:query",
		"calendarId":  "primary",
		"calendarIds": "cal1, cal2",
		"timeMin":     "2025-06-01T00:00:00Z",
		"timeMax":     "2025-06-02T00:00:00Z",
		"credential":  "c1",
	}, srv.Client(), &called)

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	// Verify items list has both calendars.
	items, _ := gotBody["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected 2 calendar items, got %v", gotBody)
	}

	out := res.Outputs["main"]
	if len(out) != 2 {
		t.Fatalf("expected 2 calendar results, got %d", len(out))
	}
}

// ---------------------------------------------------------------------------
// Trigger: new / updated events
// ---------------------------------------------------------------------------

func TestCalTriggerNewEvent(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/calendars/primary/events") {
			if callCount == 1 {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"items": []any{
						map[string]any{
							"id": "ev1", "summary": "First Event",
							"updated": "2025-06-01T10:00:00Z",
							"start":   map[string]any{"dateTime": "2025-06-01T10:00:00Z"},
							"end":     map[string]any{"dateTime": "2025-06-01T11:00:00Z"},
						},
					},
				})
			} else {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"items": []any{
						map[string]any{
							"id": "ev1", "summary": "First Event (updated)",
							"updated": "2025-06-01T11:00:00Z",
							"start":   map[string]any{"dateTime": "2025-06-01T10:00:00Z"},
							"end":     map[string]any{"dateTime": "2025-06-01T11:30:00Z"},
						},
						map[string]any{
							"id": "ev2", "summary": "Second Event (new)",
							"updated": "2025-06-01T11:00:00Z",
							"start":   map[string]any{"dateTime": "2025-06-01T14:00:00Z"},
							"end":     map[string]any{"dateTime": "2025-06-01T15:00:00Z"},
						},
					},
				})
			}
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := CalendarNode(srv.URL + "/calendar/v3")
	called := false
	ctx := calOauthCtx(map[string]any{
		"operation":  "trigger:newEvent",
		"calendarId": "primary",
		"timeMin":    "2025-06-01T00:00:00Z",
		"timeMax":    "2025-06-08T00:00:00Z",
		"credential": "c1",
	}, srv.Client(), &called)
	ctx.State = map[string]any{}

	// Poll 1 — should emit the first (new) event.
	res1, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("poll 1: %v", err)
	}
	if n := len(res1.Outputs["main"]); n != 1 {
		t.Fatalf("poll 1: expected 1 new event, got %d", n)
	}

	// Reset rate-limit for second poll.
	delete(ctx.State, "cal:lastPoll:primary:2025-06-01T00:00:00Z:2025-06-08T00:00:00Z")

	// Poll 2 — ev1 was updated (new Updated timestamp) + ev2 is new.
	res2, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("poll 2: %v", err)
	}
	if n := len(res2.Outputs["main"]); n != 2 {
		t.Fatalf("poll 2: expected 2 events (1 updated + 1 new), got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Operation registration
// ---------------------------------------------------------------------------

func TestCalNodeIncludesAllOps(t *testing.T) {
	def := CalendarNode("https://example.test/calendar/v3")
	ops := map[string]bool{}
	for _, p := range def.Params {
		if p.Name == "operation" {
			for _, opt := range p.Options {
				ops[opt.Value] = true
			}
		}
	}

	wantOps := []string{
		"event:list", "event:get", "event:create", "event:update", "event:delete",
		"calendar:list", "freebusy:query", "trigger:newEvent",
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

func TestCalGetRequiresEventId(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer srv.Close()

	def := CalendarNode(srv.URL + "/calendar/v3")
	called := false
	ctx := calOauthCtx(map[string]any{
		"operation":  "event:get",
		"calendarId": "primary",
		"credential": "c1",
		// eventId intentionally omitted
	}, srv.Client(), &called)

	_, err := def.Execute(ctx)
	if err == nil {
		t.Fatal("expected error for missing eventId, got nil")
	}
}

func TestCalUpdateRequiresEventId(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer srv.Close()

	def := CalendarNode(srv.URL + "/calendar/v3")
	called := false
	ctx := calOauthCtx(map[string]any{
		"operation":  "event:update",
		"calendarId": "primary",
		"credential": "c1",
		"body":       map[string]any{"summary": "No ID"},
	}, srv.Client(), &called)

	_, err := def.Execute(ctx)
	if err == nil {
		t.Fatal("expected error for missing eventId, got nil")
	}
}

// ---------------------------------------------------------------------------
// Event create with reminders
// ---------------------------------------------------------------------------

func TestCalCreateEventWithReminders(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/calendars/primary/events") {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]any{"id": "ev-rem", "summary": gotBody["summary"]}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	def := CalendarNode(srv.URL + "/calendar/v3")
	called := false
	ctx := calOauthCtx(map[string]any{
		"operation":  "event:create",
		"calendarId": "primary",
		"credential": "c1",
		"body": map[string]any{
			"summary": "Event with reminders",
			"start":   map[string]any{"dateTime": "2025-07-01T09:00:00Z"},
			"end":     map[string]any{"dateTime": "2025-07-01T10:00:00Z"},
			"reminders": map[string]any{
				"useDefault": false,
				"overrides": []any{
					map[string]any{"method": "email", "minutes": float64(30)},
					map[string]any{"method": "popup", "minutes": float64(10)},
				},
			},
		},
	}, srv.Client(), &called)

	res, err := def.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	// Verify reminders were parsed.
	reminders, _ := gotBody["reminders"].(map[string]any)
	if reminders == nil {
		t.Fatal("expected reminders in request body")
	}
	overrides := reminders["overrides"].([]any)
	if len(overrides) != 2 {
		t.Fatalf("expected 2 reminder overrides, got %d", len(overrides))
	}

	out := res.Outputs["main"]
	if len(out) != 1 || out[0].JSON["id"] != "ev-rem" {
		t.Fatalf("unexpected result: %+v", out)
	}
}

var _ = schema.ExecContext{}
