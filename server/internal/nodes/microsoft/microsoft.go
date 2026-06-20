// Package microsoft provides Microsoft 365 integration nodes (Outlook, OneDrive,
// Teams, To Do, Excel, Calendar) over the Microsoft Graph API, built on the
// declarative REST framework. They authenticate with a microsoftOAuth2Api
// credential; the engine's OAuth2 client provider injects + refreshes the token.
// One Graph base URL serves every service. Builders take the base so tests can
// point them at a mock server.
package microsoft

import (
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/rest"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

const (
	credType      = "microsoftOAuth2Api"
	graphBaseProd = "https://graph.microsoft.com/v1.0"
)

func node(base, typ, label, icon, desc string, ops []rest.Op) rest.Node {
	return rest.Node{
		Type: typ, Label: label, Group: "integration", Icon: icon, Description: desc,
		BaseURL: base, CredType: credType, Auth: rest.Auth{Kind: "oauth2"}, Ops: ops,
	}
}

func sp(name, label string, required bool) schema.ParamSchema {
	return schema.ParamSchema{Name: name, Label: label, Type: "string", Required: required}
}
func jp(name, label string) schema.ParamSchema {
	return schema.ParamSchema{Name: name, Label: label, Type: "json"}
}

// Nodes returns the Microsoft node pack wired to the production Graph endpoint.
func Nodes() []schema.NodeDefinition {
	return []schema.NodeDefinition{
		Outlook(graphBaseProd).Build(),
		OneDrive(graphBaseProd).Build(),
		Teams(graphBaseProd).Build(),
		ToDo(graphBaseProd).Build(),
		Excel(graphBaseProd).Build(),
		Calendar(graphBaseProd).Build(),
	}
}

// Outlook — mail via Graph.
func Outlook(base string) rest.Node {
	msgID := sp("messageId", "Message ID", true)
	body := jp("body", "Body (JSON)")
	top := schema.ParamSchema{Name: "$top", Label: "Max results", Type: "number", Default: 25}
	filter := schema.ParamSchema{Name: "$filter", Label: "Filter (OData)", Type: "string"}
	return node(base, "microsoft.outlook", "Outlook", "Mail", "Read and send Outlook mail.", []rest.Op{
		{Resource: "message", Name: "list", Label: "List Messages", Method: "GET", Path: "/me/messages", ItemsPath: "value",
			Query: map[string]string{"$top": "$top", "$filter": "$filter"}, Params: []schema.ParamSchema{top, filter}},
		{Resource: "message", Name: "get", Label: "Get Message", Method: "GET", Path: "/me/messages/{messageId}", Params: []schema.ParamSchema{msgID}},
		{Resource: "message", Name: "send", Label: "Send Mail", Method: "POST", Path: "/me/sendMail", BodyParam: "body", Params: []schema.ParamSchema{body}},
		{Resource: "message", Name: "delete", Label: "Delete Message", Method: "DELETE", Path: "/me/messages/{messageId}", Params: []schema.ParamSchema{msgID}},
		{Resource: "folder", Name: "list", Label: "List Mail Folders", Method: "GET", Path: "/me/mailFolders", ItemsPath: "value"},
	})
}

// OneDrive — drive items via Graph.
func OneDrive(base string) rest.Node {
	itemID := sp("itemId", "Item ID", true)
	body := jp("body", "Body (JSON: {name, folder})")
	return node(base, "microsoft.onedrive", "OneDrive", "HardDrive", "List and manage OneDrive items.", []rest.Op{
		{Resource: "item", Name: "listRoot", Label: "List Root Children", Method: "GET", Path: "/me/drive/root/children", ItemsPath: "value"},
		{Resource: "item", Name: "get", Label: "Get Item", Method: "GET", Path: "/me/drive/items/{itemId}", Params: []schema.ParamSchema{itemID}},
		{Resource: "item", Name: "listChildren", Label: "List Children", Method: "GET", Path: "/me/drive/items/{itemId}/children", ItemsPath: "value", Params: []schema.ParamSchema{itemID}},
		{Resource: "item", Name: "delete", Label: "Delete Item", Method: "DELETE", Path: "/me/drive/items/{itemId}", Params: []schema.ParamSchema{itemID}},
		{Resource: "folder", Name: "create", Label: "Create Folder", Method: "POST", Path: "/me/drive/root/children", BodyParam: "body", Params: []schema.ParamSchema{body}},
	})
}

// Teams — channels and messages via Graph.
func Teams(base string) rest.Node {
	teamID := sp("teamId", "Team ID", true)
	channelID := sp("channelId", "Channel ID", true)
	body := jp("body", "Message (JSON: {body:{content}})")
	return node(base, "microsoft.teams", "Microsoft Teams", "Users", "Teams channels and messages.", []rest.Op{
		{Resource: "team", Name: "listJoined", Label: "List Joined Teams", Method: "GET", Path: "/me/joinedTeams", ItemsPath: "value"},
		{Resource: "channel", Name: "list", Label: "List Channels", Method: "GET", Path: "/teams/{teamId}/channels", ItemsPath: "value", Params: []schema.ParamSchema{teamID}},
		{Resource: "channelMessage", Name: "list", Label: "List Channel Messages", Method: "GET", Path: "/teams/{teamId}/channels/{channelId}/messages", ItemsPath: "value", Params: []schema.ParamSchema{teamID, channelID}},
		{Resource: "channelMessage", Name: "send", Label: "Send Channel Message", Method: "POST", Path: "/teams/{teamId}/channels/{channelId}/messages", BodyParam: "body", Params: []schema.ParamSchema{teamID, channelID, body}},
		{Resource: "chat", Name: "list", Label: "List Chats", Method: "GET", Path: "/me/chats", ItemsPath: "value"},
	})
}

// ToDo — Microsoft To Do tasks via Graph.
func ToDo(base string) rest.Node {
	listID := sp("listId", "List ID", true)
	taskID := sp("taskId", "Task ID", true)
	body := jp("body", "Task (JSON: {title})")
	return node(base, "microsoft.todo", "Microsoft To Do", "CheckSquare", "Tasks and task lists.", []rest.Op{
		{Resource: "list", Name: "list", Label: "List Task Lists", Method: "GET", Path: "/me/todo/lists", ItemsPath: "value"},
		{Resource: "task", Name: "list", Label: "List Tasks", Method: "GET", Path: "/me/todo/lists/{listId}/tasks", ItemsPath: "value", Params: []schema.ParamSchema{listID}},
		{Resource: "task", Name: "create", Label: "Create Task", Method: "POST", Path: "/me/todo/lists/{listId}/tasks", BodyParam: "body", Params: []schema.ParamSchema{listID, body}},
		{Resource: "task", Name: "delete", Label: "Delete Task", Method: "DELETE", Path: "/me/todo/lists/{listId}/tasks/{taskId}", Params: []schema.ParamSchema{listID, taskID}},
	})
}

// Excel — workbook worksheets and tables via Graph.
func Excel(base string) rest.Node {
	itemID := sp("itemId", "Workbook (Drive item) ID", true)
	tableID := sp("tableId", "Table ID/Name", true)
	worksheetID := sp("worksheetId", "Worksheet ID/Name", true)
	body := jp("body", "Body (JSON: {values:[[...]]})")
	return node(base, "microsoft.excel", "Microsoft Excel", "Table", "Workbook worksheets and tables.", []rest.Op{
		{Resource: "worksheet", Name: "list", Label: "List Worksheets", Method: "GET", Path: "/me/drive/items/{itemId}/workbook/worksheets", ItemsPath: "value", Params: []schema.ParamSchema{itemID}},
		{Resource: "worksheet", Name: "usedRange", Label: "Get Used Range", Method: "GET", Path: "/me/drive/items/{itemId}/workbook/worksheets/{worksheetId}/usedRange", Params: []schema.ParamSchema{itemID, worksheetID}},
		{Resource: "table", Name: "list", Label: "List Tables", Method: "GET", Path: "/me/drive/items/{itemId}/workbook/tables", ItemsPath: "value", Params: []schema.ParamSchema{itemID}},
		{Resource: "table", Name: "addRow", Label: "Add Table Row", Method: "POST", Path: "/me/drive/items/{itemId}/workbook/tables/{tableId}/rows/add", BodyParam: "body", Params: []schema.ParamSchema{itemID, tableID, body}},
		{Resource: "table", Name: "getRows", Label: "List Table Rows", Method: "GET", Path: "/me/drive/items/{itemId}/workbook/tables/{tableId}/rows", ItemsPath: "value", Params: []schema.ParamSchema{itemID, tableID}},
	})
}

// Calendar — Outlook calendar events via Graph.
func Calendar(base string) rest.Node {
	evID := sp("eventId", "Event ID", true)
	body := jp("body", "Event (JSON)")
	top := schema.ParamSchema{Name: "$top", Label: "Max results", Type: "number", Default: 25}
	return node(base, "microsoft.outlookCalendar", "Outlook Calendar", "Calendar", "Calendar events via Graph.", []rest.Op{
		{Resource: "event", Name: "list", Label: "List Events", Method: "GET", Path: "/me/events", ItemsPath: "value", Query: map[string]string{"$top": "$top"}, Params: []schema.ParamSchema{top}},
		{Resource: "event", Name: "get", Label: "Get Event", Method: "GET", Path: "/me/events/{eventId}", Params: []schema.ParamSchema{evID}},
		{Resource: "event", Name: "create", Label: "Create Event", Method: "POST", Path: "/me/events", BodyParam: "body", Params: []schema.ParamSchema{body}},
		{Resource: "event", Name: "delete", Label: "Delete Event", Method: "DELETE", Path: "/me/events/{eventId}", Params: []schema.ParamSchema{evID}},
	})
}
