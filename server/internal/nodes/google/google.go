// Package google provides Google Workspace integration nodes (Sheets, Gmail,
// Calendar, Drive) built on the declarative REST framework. They authenticate with
// a googleOAuth2Api credential; the engine's OAuth2 client provider injects and
// auto-refreshes the access token. Each service is one node with its own base URL;
// the builders take the base URL so tests can point them at a mock server.
package google

import (
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/rest"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

const credType = "googleOAuth2Api"

func oauth() rest.Auth { return rest.Auth{Kind: "oauth2"} }

func str(name, label string, required bool) schema.ParamSchema {
	return schema.ParamSchema{Name: name, Label: label, Type: "string", Required: required}
}

// Nodes returns the Google node pack wired to the production endpoints.
func Nodes() []schema.NodeDefinition {
	return []schema.NodeDefinition{
		SheetsNode(""), // SDK default: https://sheets.googleapis.com/
		GmailNode(""), // SDK default: https://gmail.googleapis.com/
		CalendarNode(""), // SDK default: https://www.googleapis.com/calendar/v3/
		DriveNode(""),
	}
}

// Deprecated: Sheets is the original REST-based builder with a limited operation
// set. Use SheetsNode (sheets.go) instead, which uses the official Go SDK and
// supports all operations including triggers and dimension deletes.
//
// Sheets — Google Sheets v4 (REST, limited ops).
func Sheets(base string) rest.Node {
	id := str("spreadsheetId", "Spreadsheet ID", true)
	rng := str("range", "Range (A1 notation, e.g. Sheet1!A1:C10)", true)
	body := schema.ParamSchema{Name: "body", Label: "Body (JSON)", Type: "json"}
	const valueInput = "valueInputOption"
	return rest.Node{
		Type: "google.sheets", Label: "Google Sheets", Group: "integration", Icon: "Sheet",
		Description: "Read and write Google Sheets.", BaseURL: base, CredType: credType, Auth: oauth(),
		Ops: []rest.Op{
			{Resource: "spreadsheet", Name: "get", Label: "Get Spreadsheet", Method: "GET",
				Path: "/spreadsheets/{spreadsheetId}", Params: []schema.ParamSchema{id}},
			{Resource: "spreadsheet", Name: "create", Label: "Create Spreadsheet", Method: "POST",
				Path: "/spreadsheets", BodyParam: "body", Params: []schema.ParamSchema{body}},
			{Resource: "values", Name: "get", Label: "Get Values", Method: "GET",
				Path: "/spreadsheets/{spreadsheetId}/values/{range}", ItemsPath: "values",
				Params: []schema.ParamSchema{id, rng}},
			{Resource: "values", Name: "append", Label: "Append Values", Method: "POST",
				Path: "/spreadsheets/{spreadsheetId}/values/{range}:append", BodyParam: "body",
				Query: map[string]string{valueInput: "=USER_ENTERED"}, Params: []schema.ParamSchema{id, rng, body}},
			{Resource: "values", Name: "update", Label: "Update Values", Method: "PUT",
				Path: "/spreadsheets/{spreadsheetId}/values/{range}", BodyParam: "body",
				Query: map[string]string{valueInput: "=USER_ENTERED"}, Params: []schema.ParamSchema{id, rng, body}},
			{Resource: "values", Name: "clear", Label: "Clear Values", Method: "POST",
				Path: "/spreadsheets/{spreadsheetId}/values/{range}:clear", Params: []schema.ParamSchema{id, rng}},
		},
	}
}

// Deprecated: Gmail is the original REST-based builder with a limited operation
// set (message list/get, label list). Use GmailNode (gmail.go) instead, which
// uses the official Go SDK and supports send, reply, drafts, threads, labels
// CRUD, message/thread modify, and a new-email trigger.
//
// Gmail — Gmail v1 (REST, read-only).
func Gmail(base string) rest.Node {
	q := schema.ParamSchema{Name: "q", Label: "Search query", Type: "string", Placeholder: "from:me is:unread"}
	max := schema.ParamSchema{Name: "maxResults", Label: "Max results", Type: "number", Default: 25}
	msgID := str("messageId", "Message ID", true)
	return rest.Node{
		Type: "google.gmail", Label: "Gmail", Group: "integration", Icon: "Mail",
		Description: "Read Gmail messages and labels.", BaseURL: base, CredType: credType, Auth: oauth(),
		Ops: []rest.Op{
			{Resource: "message", Name: "list", Label: "List Messages", Method: "GET",
				Path: "/users/me/messages", ItemsPath: "messages",
				Query: map[string]string{"q": "q", "maxResults": "maxResults"}, Params: []schema.ParamSchema{q, max}},
			{Resource: "message", Name: "get", Label: "Get Message", Method: "GET",
				Path: "/users/me/messages/{messageId}", Params: []schema.ParamSchema{msgID}},
			{Resource: "label", Name: "list", Label: "List Labels", Method: "GET",
				Path: "/users/me/labels", ItemsPath: "labels"},
		},
	}
}

// Deprecated: Calendar is the original REST-based builder with a limited operation
// set (event list/get/create/delete, calendar list). Use CalendarNode (calendar.go)
// instead, which uses the official Go SDK and supports event update, free/busy
// query, and a new-event trigger.
//
// Calendar — Google Calendar v3 (REST, limited ops).
func Calendar(base string) rest.Node {
	cal := schema.ParamSchema{Name: "calendarId", Label: "Calendar ID", Type: "string", Required: true, Default: "primary"}
	ev := str("eventId", "Event ID", true)
	body := schema.ParamSchema{Name: "body", Label: "Event (JSON)", Type: "json"}
	return rest.Node{
		Type: "google.calendar", Label: "Google Calendar", Group: "integration", Icon: "Calendar",
		Description: "Manage Google Calendar events.", BaseURL: base, CredType: credType, Auth: oauth(),
		Ops: []rest.Op{
			{Resource: "event", Name: "list", Label: "List Events", Method: "GET",
				Path: "/calendars/{calendarId}/events", ItemsPath: "items", Params: []schema.ParamSchema{cal}},
			{Resource: "event", Name: "get", Label: "Get Event", Method: "GET",
				Path: "/calendars/{calendarId}/events/{eventId}", Params: []schema.ParamSchema{cal, ev}},
			{Resource: "event", Name: "create", Label: "Create Event", Method: "POST",
				Path: "/calendars/{calendarId}/events", BodyParam: "body", Params: []schema.ParamSchema{cal, body}},
			{Resource: "event", Name: "delete", Label: "Delete Event", Method: "DELETE",
				Path: "/calendars/{calendarId}/events/{eventId}", Params: []schema.ParamSchema{cal, ev}},
			{Resource: "calendar", Name: "list", Label: "List Calendars", Method: "GET",
				Path: "/users/me/calendarList", ItemsPath: "items"},
		},
	}
}

// Drive — Google Drive v3 (metadata; media upload/download is a follow-up that
// should stream via the official SDK).
func Drive(base string) rest.Node {
	fileID := str("fileId", "File ID", true)
	q := schema.ParamSchema{Name: "q", Label: "Query (Drive search syntax)", Type: "string"}
	pageSize := schema.ParamSchema{Name: "pageSize", Label: "Page size", Type: "number", Default: 50}
	body := schema.ParamSchema{Name: "body", Label: "Body (JSON: {name, mimeType})", Type: "json"}
	return rest.Node{
		Type: "google.drive", Label: "Google Drive", Group: "integration", Icon: "HardDrive",
		Description: "List and manage Google Drive files.", BaseURL: base, CredType: credType, Auth: oauth(),
		Ops: []rest.Op{
			{Resource: "file", Name: "list", Label: "List Files", Method: "GET",
				Path: "/files", ItemsPath: "files",
				Query: map[string]string{"q": "q", "pageSize": "pageSize"}, Params: []schema.ParamSchema{q, pageSize}},
			{Resource: "file", Name: "get", Label: "Get File", Method: "GET",
				Path: "/files/{fileId}", Params: []schema.ParamSchema{fileID}},
			{Resource: "file", Name: "delete", Label: "Delete File", Method: "DELETE",
				Path: "/files/{fileId}", Params: []schema.ParamSchema{fileID}},
			{Resource: "folder", Name: "create", Label: "Create Folder", Method: "POST",
				Path: "/files", BodyParam: "body", Params: []schema.ParamSchema{body}},
		},
	}
}
