package google

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

// ---------------------------------------------------------------------------
// Service cache
// ---------------------------------------------------------------------------

var (
	calSvcMu    sync.Mutex
	calSvcCache = map[string]*calendar.Service{}
)

func getCalendarService(ctx *schema.ExecContext, base string) (*calendar.Service, error) {
	client, err := ctx.AuthorizedClient("credential")
	if err != nil {
		return nil, fmt.Errorf("calendar: authorized client: %w", err)
	}
	credID, _ := ctx.Params["credential"].(string)
	cacheKey := credID + "|" + base

	calSvcMu.Lock()
	if svc, ok := calSvcCache[cacheKey]; ok {
		calSvcMu.Unlock()
		return svc, nil
	}
	calSvcMu.Unlock()

	endpoint := base
	if endpoint == "" {
		endpoint = "https://www.googleapis.com/calendar/v3/"
	}
	retryClient := wrapWithRetry(client)
	svc, err := calendar.NewService(context.Background(),
		option.WithHTTPClient(retryClient),
		option.WithEndpoint(strings.TrimRight(endpoint, "/")+"/"),
	)
	if err != nil {
		return nil, fmt.Errorf("calendar: new service: %w", err)
	}

	calSvcMu.Lock()
	if existing, ok := calSvcCache[cacheKey]; ok {
		calSvcMu.Unlock()
		return existing, nil
	}
	calSvcCache[cacheKey] = svc
	calSvcMu.Unlock()
	return svc, nil
}

// ---------------------------------------------------------------------------
// Node definition
// ---------------------------------------------------------------------------

func CalendarNode(base string) schema.NodeDefinition {
	return schema.NodeDefinition{
		Type:        "google.calendar",
		Label:       "Google Calendar",
		Group:       "integration",
		Icon:        "Calendar",
		Description: "Manage Google Calendar events, query free/busy, and watch for new events.",
		Inputs:      []schema.Port{{ID: "main"}},
		Outputs:     []schema.Port{{ID: "main"}},
		Credentials: []string{credType},
		IsTrigger:   true,
		Params: []schema.ParamSchema{
			{Name: "credential", Label: "Credential", Type: "credential", Required: true, CredentialType: credType},
			{Name: "operation", Label: "Operation", Type: "select", Required: true, Options: []schema.ParamOption{
				{Label: "List Events", Value: "event:list"},
				{Label: "Get Event", Value: "event:get"},
				{Label: "Create Event", Value: "event:create"},
				{Label: "Update Event", Value: "event:update"},
				{Label: "Delete Event", Value: "event:delete"},
				{Label: "List Calendars", Value: "calendar:list"},
				{Label: "Query Free/Busy", Value: "freebusy:query"},
				{Label: "Trigger: New / Updated Event", Value: "trigger:newEvent"},
			}},
			{
				Name: "calendarId", Label: "Calendar ID", Type: "string", Default: "primary",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"event:list", "event:get", "event:create", "event:update", "event:delete",
					"freebusy:query", "trigger:newEvent",
				}},
			},
			{
				Name: "eventId", Label: "Event ID", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"event:get", "event:update", "event:delete",
				}},
			},
			{
				Name: "body", Label: "Event (JSON)", Type: "json",
				Description: `{"summary":"Title","start":{"dateTime":"2025-01-01T10:00:00-08:00"},"end":{"dateTime":"2025-01-01T11:00:00-08:00"}}`,
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"event:create", "event:update",
				}},
			},
			{
				Name: "q", Label: "Search query", Type: "string", Placeholder: "birthday",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"event:list",
				}},
			},
			{
				Name: "maxResults", Label: "Max results", Type: "number", Default: float64(25),
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"event:list",
				}},
			},
			{
				Name: "maxPages", Label: "Max pages", Type: "number", Default: float64(10),
				Description: "Maximum number of pages to fetch. Set to 1 for first page only.",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"event:list", "calendar:list",
				}},
			},
			{
				Name: "timeMin", Label: "Time min (RFC 3339)", Type: "string",
				Description: "Lower bound for event start. Defaults to now if unset for free/busy or trigger.",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"event:list", "freebusy:query", "trigger:newEvent",
				}},
			},
			{
				Name: "timeMax", Label: "Time max (RFC 3339)", Type: "string",
				Description: "Upper bound for event start. Defaults to +7 days if unset for free/busy or trigger.",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"event:list", "freebusy:query", "trigger:newEvent",
				}},
			},
			{
				Name: "calendarIds", Label: "Calendar IDs (comma-separated)", Type: "string",
				Description: "By default the primary calendar is checked. Override to check multiple calendars.",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"freebusy:query",
				}},
			},
			{
				Name: "pollSeconds", Label: "Poll seconds", Type: "number", Default: float64(60),
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"trigger:newEvent",
				}},
			},
		},
		Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
			return executeCalendarNode(ctx, base)
		},
	}
}

// ---------------------------------------------------------------------------
// Main execution dispatch
// ---------------------------------------------------------------------------

func executeCalendarNode(ctx *schema.ExecContext, base string) (schema.NodeResult, error) {
	op, _ := ctx.Params["operation"].(string)
	if op == "" {
		return schema.NodeResult{}, fmt.Errorf("calendar: operation is required")
	}

	svc, err := getCalendarService(ctx, base)
	if err != nil {
		return schema.NodeResult{}, err
	}

	calID, _ := ctx.Params["calendarId"].(string)
	if calID == "" {
		calID = "primary"
	}
	eventID, _ := ctx.Params["eventId"].(string)

	switch op {

	case "calendar:list":
		maxPages := parseIntParam(ctx.Params["maxPages"], 10)
		var allItems []schema.Item
		pageToken := ""
		for page := 0; page < maxPages; page++ {
			call := svc.CalendarList.List()
			if pageToken != "" {
				call = call.PageToken(pageToken)
			}
			resp, err := call.Do()
			if err != nil {
				return schema.NodeResult{}, fmt.Errorf("calendar calendar:list: %w", err)
			}
			for _, c := range resp.Items {
				allItems = append(allItems, calendarEntryToItem(c))
			}
			if resp.NextPageToken == "" {
				break
			}
			pageToken = resp.NextPageToken
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": allItems}}, nil

	case "event:list":
		return listEvents(ctx, svc, calID)

	case "event:get":
		if eventID == "" {
			return schema.NodeResult{}, fmt.Errorf("calendar event:get: eventId is required")
		}
		ev, err := svc.Events.Get(calID, eventID).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("calendar event:get: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {eventToItem(ev)}}}, nil

	case "event:create":
		bodyVal := asObject(ctx.RawParam("body"))
		ev, err := svc.Events.Insert(calID, mapToEvent(bodyVal)).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("calendar event:create: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {eventToItem(ev)}}}, nil

	case "event:update":
		if eventID == "" {
			return schema.NodeResult{}, fmt.Errorf("calendar event:update: eventId is required")
		}
		bodyVal := asObject(ctx.RawParam("body"))
		ev, err := svc.Events.Update(calID, eventID, mapToEvent(bodyVal)).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("calendar event:update: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {eventToItem(ev)}}}, nil

	case "event:delete":
		if eventID == "" {
			return schema.NodeResult{}, fmt.Errorf("calendar event:delete: eventId is required")
		}
		if err := svc.Events.Delete(calID, eventID).Do(); err != nil {
			return schema.NodeResult{}, fmt.Errorf("calendar event:delete: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{JSON: map[string]any{"deleted": true, "eventId": eventID}}}}}, nil

	case "freebusy:query":
		return queryFreeBusy(ctx, svc, calID)

	case "trigger:newEvent":
		return executeCalendarTrigger(ctx, svc, calID, base)

	default:
		return schema.NodeResult{}, fmt.Errorf("calendar: unknown operation %q", op)
	}
}

// ---------------------------------------------------------------------------
// Event list
// ---------------------------------------------------------------------------

func listEvents(ctx *schema.ExecContext, svc *calendar.Service, calID string) (schema.NodeResult, error) {
	max := parseIntParam(ctx.Params["maxResults"], 25)
	maxPages := parseIntParam(ctx.Params["maxPages"], 10)
	searchQ, _ := ctx.Params["q"].(string)
	tMin, _ := ctx.Params["timeMin"].(string)
	tMax, _ := ctx.Params["timeMax"].(string)

	var allItems []schema.Item
	pageToken := ""
	for page := 0; page < maxPages; page++ {
		call := svc.Events.List(calID).
			MaxResults(int64(max)).
			SingleEvents(true).
			OrderBy("startTime")

		if searchQ != "" {
			call = call.Q(searchQ)
		}
		if tMin != "" {
			call = call.TimeMin(tMin)
		}
		if tMax != "" {
			call = call.TimeMax(tMax)
		}
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		resp, err := call.Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("calendar event:list: %w", err)
		}
		for _, ev := range resp.Items {
			allItems = append(allItems, eventToItem(ev))
		}
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	return schema.NodeResult{Outputs: map[string][]schema.Item{"main": allItems}}, nil
}

// ---------------------------------------------------------------------------
// Free/busy query
// ---------------------------------------------------------------------------

func queryFreeBusy(ctx *schema.ExecContext, svc *calendar.Service, calID string) (schema.NodeResult, error) {
	tMin, _ := ctx.Params["timeMin"].(string)
	tMax, _ := ctx.Params["timeMax"].(string)
	if tMin == "" {
		tMin = time.Now().Format(time.RFC3339)
	}
	if tMax == "" {
		tMax = time.Now().Add(7 * 24 * time.Hour).Format(time.RFC3339)
	}

	ids := []*calendar.FreeBusyRequestItem{{Id: calID}}
	if csv, _ := ctx.Params["calendarIds"].(string); csv != "" {
		ids = nil
		for _, id := range strings.Split(csv, ",") {
			id = strings.TrimSpace(id)
			if id != "" {
				ids = append(ids, &calendar.FreeBusyRequestItem{Id: id})
			}
		}
	}

	req := &calendar.FreeBusyRequest{
		TimeMin: tMin,
		TimeMax: tMax,
		Items:   ids,
	}
	resp, err := svc.Freebusy.Query(req).Do()
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("calendar freebusy:query: %w", err)
	}

	out := make([]schema.Item, 0)
	if resp.Calendars != nil {
		for calKey, calFB := range resp.Calendars {
			busy := make([]map[string]any, 0, len(calFB.Busy))
			for _, b := range calFB.Busy {
				busy = append(busy, map[string]any{
					"start": b.Start,
					"end":   b.End,
				})
			}
			out = append(out, schema.Item{JSON: map[string]any{
				"calendarId": calKey,
				"busy":       busy,
			}})
		}
	}
	if len(out) == 0 {
		out = append(out, schema.Item{JSON: map[string]any{"busy": []any{}}})
	}
	return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
}

// ---------------------------------------------------------------------------
// Trigger (polling for new / updated events)
// ---------------------------------------------------------------------------

func executeCalendarTrigger(ctx *schema.ExecContext, svc *calendar.Service, calID, base string) (schema.NodeResult, error) {
	if ctx.State == nil {
		ctx.State = map[string]any{}
	}

	pollSeconds := parseIntParam(ctx.Params["pollSeconds"], 60)
	if pollSeconds < 10 {
		pollSeconds = 10
	}

	tMin, _ := ctx.Params["timeMin"].(string)
	tMax, _ := ctx.Params["timeMax"].(string)
	if tMin == "" {
		tMin = time.Now().Format(time.RFC3339)
	}
	if tMax == "" {
		tMax = time.Now().Add(7 * 24 * time.Hour).Format(time.RFC3339)
	}

	lastPollKey := fmt.Sprintf("cal:lastPoll:%s:%s:%s", calID, tMin, tMax)
	seenKey := fmt.Sprintf("cal:seenIDs:%s:%s:%s", calID, tMin, tMax)
	lastUpdatedKey := fmt.Sprintf("cal:updatedMin:%s:%s:%s", calID, tMin, tMax)

	if tsAny, ok := ctx.State[lastPollKey]; ok {
		if ts, valid := toInt64(tsAny); valid && time.Since(time.Unix(ts, 0)) < time.Duration(pollSeconds)*time.Second {
			return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {}}}, nil
		}
	}

	updatedMin := tMin
	if um, ok := ctx.State[lastUpdatedKey].(string); ok && um != "" {
		updatedMin = um
	}
	ctx.State[lastUpdatedKey] = time.Now().Format(time.RFC3339)

	// Persist all seen keys across polls — never garbage-collect.
	// This prevents re-emitting events that leave and re-enter the time window.
	seen := map[string]bool{}
	if raw, ok := ctx.State[seenKey].([]any); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok {
				seen[s] = true
			}
		}
	}

	call := svc.Events.List(calID).
		TimeMin(tMin).
		TimeMax(tMax).
		UpdatedMin(updatedMin).
		SingleEvents(true).
		OrderBy("updated").
		MaxResults(250)

	resp, err := call.Do()
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("calendar trigger:newEvent: %w", err)
	}

	var out []schema.Item
	for _, ev := range resp.Items {
		key := ev.Id + "|" + ev.Updated
		if !seen[key] {
			out = append(out, eventToItem(ev))
			seen[key] = true
		}
	}

	// Serialize the full (non-GC'd) seen set back to state.
	seenList := make([]any, 0, len(seen))
	for id := range seen {
		seenList = append(seenList, id)
	}
	ctx.State[seenKey] = seenList
	ctx.State[lastPollKey] = time.Now().Unix()

	return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
}

// ---------------------------------------------------------------------------
// Type mapping: JSON map → Calendar SDK struct
// ---------------------------------------------------------------------------

func mapToEvent(m map[string]any) *calendar.Event {
	ev := &calendar.Event{}

	if v, ok := m["summary"].(string); ok {
		ev.Summary = v
	}
	if v, ok := m["description"].(string); ok {
		ev.Description = v
	}
	if v, ok := m["location"].(string); ok {
		ev.Location = v
	}
	if v, ok := m["colorId"].(string); ok {
		ev.ColorId = v
	}

	if start, ok := m["start"].(map[string]any); ok {
		ev.Start = mapToEventDateTime(start)
	}
	if end, ok := m["end"].(map[string]any); ok {
		ev.End = mapToEventDateTime(end)
	}

	if atts, ok := m["attendees"].([]any); ok {
		for _, a := range atts {
			if am, ok := a.(map[string]any); ok {
				att := &calendar.EventAttendee{}
				if email, ok := am["email"].(string); ok {
					att.Email = email
				}
				if optional, ok := am["optional"].(bool); ok {
					att.Optional = optional
				}
				if resp, ok := am["responseStatus"].(string); ok {
					att.ResponseStatus = resp
				}
				ev.Attendees = append(ev.Attendees, att)
			}
		}
	}

	if rems, ok := m["reminders"].(map[string]any); ok {
		ev.Reminders = &calendar.EventReminders{}
		if useDefault, ok := rems["useDefault"].(bool); ok {
			ev.Reminders.UseDefault = useDefault
		}
		if overrides, ok := rems["overrides"].([]any); ok {
			for _, o := range overrides {
				if om, ok := o.(map[string]any); ok {
					or := &calendar.EventReminder{}
					if method, ok := om["method"].(string); ok {
						or.Method = method
					}
					if minutes, ok := om["minutes"].(float64); ok {
						or.Minutes = int64(minutes)
					}
					ev.Reminders.Overrides = append(ev.Reminders.Overrides, or)
				}
			}
		}
	}

	if rules, ok := m["recurrence"].([]any); ok {
		for _, r := range rules {
			if s, ok := r.(string); ok {
				ev.Recurrence = append(ev.Recurrence, s)
			}
		}
	}

	return ev
}

func mapToEventDateTime(m map[string]any) *calendar.EventDateTime {
	edt := &calendar.EventDateTime{}
	if dt, ok := m["dateTime"].(string); ok {
		edt.DateTime = dt
	}
	if d, ok := m["date"].(string); ok {
		edt.Date = d
	}
	if tz, ok := m["timeZone"].(string); ok {
		edt.TimeZone = tz
	}
	return edt
}

// ---------------------------------------------------------------------------
// Type mapping: Calendar SDK struct → schema.Item
// ---------------------------------------------------------------------------

func eventToItem(ev *calendar.Event) schema.Item {
	if ev == nil {
		return schema.Item{JSON: map[string]any{}}
	}
	item := map[string]any{
		"id":          ev.Id,
		"summary":     ev.Summary,
		"description": ev.Description,
		"location":    ev.Location,
		"htmlLink":    ev.HtmlLink,
		"created":     ev.Created,
		"updated":     ev.Updated,
		"status":      ev.Status,
		"colorId":     ev.ColorId,
		"eventType":   ev.EventType,
		"iCalUID":     ev.ICalUID,
	}
	if ev.Start != nil {
		item["start"] = map[string]any{
			"dateTime": ev.Start.DateTime,
			"date":     ev.Start.Date,
			"timeZone": ev.Start.TimeZone,
		}
	}
	if ev.End != nil {
		item["end"] = map[string]any{
			"dateTime": ev.End.DateTime,
			"date":     ev.End.Date,
			"timeZone": ev.End.TimeZone,
		}
	}
	if ev.Creator != nil {
		item["creator"] = map[string]any{
			"email":       ev.Creator.Email,
			"displayName": ev.Creator.DisplayName,
		}
	}
	if ev.Organizer != nil {
		item["organizer"] = map[string]any{
			"email":       ev.Organizer.Email,
			"displayName": ev.Organizer.DisplayName,
		}
	}
	if len(ev.Attendees) > 0 {
		atts := make([]map[string]any, 0, len(ev.Attendees))
		for _, a := range ev.Attendees {
			atts = append(atts, map[string]any{
				"email":          a.Email,
				"displayName":    a.DisplayName,
				"responseStatus": a.ResponseStatus,
				"optional":       a.Optional,
				"organizer":      a.Organizer,
			})
		}
		item["attendees"] = atts
	}
	if len(ev.Recurrence) > 0 {
		item["recurrence"] = ev.Recurrence
	}
	if ev.Reminders != nil {
		rem := map[string]any{"useDefault": ev.Reminders.UseDefault}
		if len(ev.Reminders.Overrides) > 0 {
			ovr := make([]map[string]any, 0, len(ev.Reminders.Overrides))
			for _, o := range ev.Reminders.Overrides {
				ovr = append(ovr, map[string]any{
					"method":  o.Method,
					"minutes": o.Minutes,
				})
			}
			rem["overrides"] = ovr
		}
		item["reminders"] = rem
	}
	return schema.Item{JSON: item}
}

func calendarEntryToItem(c *calendar.CalendarListEntry) schema.Item {
	if c == nil {
		return schema.Item{JSON: map[string]any{}}
	}
	return schema.Item{JSON: map[string]any{
		"id":          c.Id,
		"summary":     c.Summary,
		"description": c.Description,
		"location":    c.Location,
		"timeZone":    c.TimeZone,
		"accessRole":  c.AccessRole,
		"primary":     c.Primary,
		"selected":    c.Selected,
	}}
}
