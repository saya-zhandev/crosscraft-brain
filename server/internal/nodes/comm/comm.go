// Package comm provides communication-service integration nodes: Slack, Discord,
// Telegram, and Twilio. Slack and Discord use the declarative REST framework;
// Telegram and Twilio require custom Execute functions due to non-standard auth.
package comm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/rest"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

func sp(name, label string, required bool) schema.ParamSchema {
	return schema.ParamSchema{Name: name, Label: label, Type: "string", Required: required}
}
func ip(name, label string, def int) schema.ParamSchema {
	return schema.ParamSchema{Name: name, Label: label, Type: "number", Default: def}
}
func jp(name, label string) schema.ParamSchema {
	return schema.ParamSchema{Name: name, Label: label, Type: "json"}
}
func ep(name, label string) schema.ParamSchema {
	return schema.ParamSchema{Name: name, Label: label, Type: "expression", Required: true}
}

// Nodes returns the full communication node pack.
func Nodes() []schema.NodeDefinition {
	return []schema.NodeDefinition{
		Slack().Build(),
		Discord().Build(),
		Telegram,
		Twilio,
	}
}

// ── Slack ─────────────────────────────────────────────────────────────────────

func Slack() rest.Node {
	ch := sp("channel", "Channel ID or Name", true)
	text := ep("text", "Message Text")
	ts := sp("ts", "Message Timestamp (ts)", true)
	limit := ip("limit", "Max results", 200)
	body := jp("body", "Body (JSON)")
	return rest.Node{
		Type: "comm.slack", Label: "Slack", Group: "integration", Icon: "MessageSquare",
		Description: "Send messages and manage channels in Slack.",
		BaseURL:     "https://slack.com/api",
		CredType:    "slackApi",
		Auth:        rest.Auth{Kind: "header", Header: "Authorization", Prefix: "Bearer ", ValueField: "accessToken"},
		Ops: []rest.Op{
			{Resource: "message", Name: "send", Label: "Send Message", Method: "POST",
				Path: "/chat.postMessage", BodyParam: "body",
				Params: []schema.ParamSchema{ch, text, body}},
			{Resource: "message", Name: "update", Label: "Update Message", Method: "POST",
				Path: "/chat.update", BodyParam: "body",
				Params: []schema.ParamSchema{ch, ts, text, body}},
			{Resource: "message", Name: "delete", Label: "Delete Message", Method: "POST",
				Path: "/chat.delete", BodyParam: "body",
				Params: []schema.ParamSchema{ch, ts, body}},
			{Resource: "message", Name: "list", Label: "List Messages (history)", Method: "GET",
				Path: "/conversations.history", ItemsPath: "messages",
				Query:  map[string]string{"channel": "channel", "limit": "limit"},
				Params: []schema.ParamSchema{ch, limit}},
			{Resource: "channel", Name: "list", Label: "List Channels", Method: "GET",
				Path: "/conversations.list", ItemsPath: "channels",
				Query:  map[string]string{"limit": "limit"},
				Params: []schema.ParamSchema{limit}},
			{Resource: "channel", Name: "get", Label: "Get Channel", Method: "GET",
				Path: "/conversations.info", ItemsPath: "channel",
				Query:  map[string]string{"channel": "channel"},
				Params: []schema.ParamSchema{ch}},
			{Resource: "user", Name: "list", Label: "List Users", Method: "GET",
				Path: "/users.list", ItemsPath: "members",
				Query:  map[string]string{"limit": "limit"},
				Params: []schema.ParamSchema{limit}},
			{Resource: "file", Name: "list", Label: "List Files", Method: "GET",
				Path: "/files.list", ItemsPath: "files",
				Query:  map[string]string{"channel": "channel"},
				Params: []schema.ParamSchema{ch}},
		},
	}
}

// ── Discord ───────────────────────────────────────────────────────────────────

func Discord() rest.Node {
	chID := sp("channelId", "Channel ID", true)
	msgID := sp("messageId", "Message ID", true)
	guildID := sp("guildId", "Guild (Server) ID", true)
	limit := ip("limit", "Max results", 50)
	body := jp("body", "Body (JSON)")
	return rest.Node{
		Type: "comm.discord", Label: "Discord", Group: "integration", Icon: "Headphones",
		Description: "Send messages and manage Discord servers.",
		BaseURL:     "https://discord.com/api/v10",
		CredType:    "discordApi",
		Auth:        rest.Auth{Kind: "header", Header: "Authorization", Prefix: "Bot ", ValueField: "accessToken"},
		Ops: []rest.Op{
			{Resource: "message", Name: "send", Label: "Send Message", Method: "POST",
				Path: "/channels/{channelId}/messages", BodyParam: "body",
				Params: []schema.ParamSchema{chID, body}},
			{Resource: "message", Name: "get", Label: "Get Message", Method: "GET",
				Path:   "/channels/{channelId}/messages/{messageId}",
				Params: []schema.ParamSchema{chID, msgID}},
			{Resource: "message", Name: "list", Label: "List Messages", Method: "GET",
				Path:   "/channels/{channelId}/messages",
				Query:  map[string]string{"limit": "limit"},
				Params: []schema.ParamSchema{chID, limit}},
			{Resource: "message", Name: "delete", Label: "Delete Message", Method: "DELETE",
				Path:   "/channels/{channelId}/messages/{messageId}",
				Params: []schema.ParamSchema{chID, msgID}},
			{Resource: "channel", Name: "get", Label: "Get Channel", Method: "GET",
				Path:   "/channels/{channelId}",
				Params: []schema.ParamSchema{chID}},
			{Resource: "guild", Name: "get", Label: "Get Guild", Method: "GET",
				Path:   "/guilds/{guildId}",
				Params: []schema.ParamSchema{guildID}},
			{Resource: "guild", Name: "listChannels", Label: "List Guild Channels", Method: "GET",
				Path:   "/guilds/{guildId}/channels",
				Params: []schema.ParamSchema{guildID}},
			{Resource: "guild", Name: "listMembers", Label: "List Guild Members", Method: "GET",
				Path:   "/guilds/{guildId}/members",
				Query:  map[string]string{"limit": "limit"},
				Params: []schema.ParamSchema{guildID, limit}},
		},
	}
}

// ── Telegram (native) ─────────────────────────────────────────────────────────
// Bot token goes in the URL path: /bot{TOKEN}/{method}

var Telegram = schema.NodeDefinition{
	Type: "comm.telegram", Label: "Telegram", Group: "integration", Icon: "Send",
	Description: "Send and receive messages via the Telegram Bot API.",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "main"}},
	Credentials: []string{"telegramApi"},
	Params: []schema.ParamSchema{
		{Name: "credential", Label: "Credential", Type: "credential", Required: true, CredentialType: "telegramApi"},
		{Name: "operation", Label: "Operation", Type: "select", Required: true, Options: []schema.ParamOption{
			{Label: "message: Send Message", Value: "message:send"},
			{Label: "message: Edit Message", Value: "message:edit"},
			{Label: "message: Delete Message", Value: "message:delete"},
			{Label: "message: Pin Message", Value: "message:pin"},
			{Label: "chat: Get Chat", Value: "chat:get"},
			{Label: "chat: Get Updates", Value: "chat:getUpdates"},
		}},
		{Name: "chatId", Label: "Chat ID", Type: "expression", Required: true,
			ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{"message:send", "message:edit", "message:delete", "message:pin", "chat:get"}}},
		{Name: "text", Label: "Message Text", Type: "expression",
			ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{"message:send", "message:edit"}}},
		{Name: "messageId", Label: "Message ID", Type: "string",
			ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{"message:edit", "message:delete", "message:pin"}}},
		{Name: "parseMode", Label: "Parse Mode", Type: "select", Default: "HTML", Options: []schema.ParamOption{
			{Label: "HTML", Value: "HTML"}, {Label: "Markdown", Value: "Markdown"}, {Label: "None", Value: ""},
		}, ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{"message:send", "message:edit"}}},
		{Name: "offset", Label: "Offset (getUpdates)", Type: "number",
			ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{"chat:getUpdates"}}},
		{Name: "limit", Label: "Limit (getUpdates)", Type: "number", Default: 100,
			ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{"chat:getUpdates"}}},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		cred, err := ctx.Credential("credential")
		if err != nil {
			return schema.NodeResult{}, err
		}
		token, _ := cred["accessToken"].(string)
		if token == "" {
			return schema.NodeResult{}, fmt.Errorf("telegram: bot token is required")
		}
		base := "https://api.telegram.org/bot" + token

		op := asString(ctx.Params["operation"], "")
		chatID := fmt.Sprintf("%v", ctx.Params["chatId"])
		text := asString(ctx.Params["text"], "")
		msgID := asString(ctx.Params["messageId"], "")
		parseMode := asString(ctx.Params["parseMode"], "HTML")

		var method string
		var reqBody map[string]any

		switch op {
		case "message:send":
			method = "sendMessage"
			reqBody = map[string]any{"chat_id": chatID, "text": text}
			if parseMode != "" {
				reqBody["parse_mode"] = parseMode
			}
		case "message:edit":
			method = "editMessageText"
			reqBody = map[string]any{"chat_id": chatID, "message_id": msgID, "text": text}
			if parseMode != "" {
				reqBody["parse_mode"] = parseMode
			}
		case "message:delete":
			method = "deleteMessage"
			reqBody = map[string]any{"chat_id": chatID, "message_id": msgID}
		case "message:pin":
			method = "pinChatMessage"
			reqBody = map[string]any{"chat_id": chatID, "message_id": msgID}
		case "chat:get":
			method = "getChat"
			reqBody = map[string]any{"chat_id": chatID}
		case "chat:getUpdates":
			method = "getUpdates"
			reqBody = map[string]any{}
			if v := ctx.Params["offset"]; v != nil {
				reqBody["offset"] = v
			}
			if v := ctx.Params["limit"]; v != nil {
				reqBody["limit"] = v
			}
		default:
			return schema.NodeResult{}, fmt.Errorf("telegram: unknown operation %q", op)
		}

		result, err := telegramCall(base+"/"+method, reqBody)
		if err != nil {
			return schema.NodeResult{}, err
		}
		items := telegramToItems(result)
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, nil
	},
}

func telegramCall(url string, body map[string]any) (any, error) {
	b, _ := json.Marshal(body)
	resp, err := httpClient.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("telegram: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var res struct {
		OK     bool   `json:"ok"`
		Result any    `json:"result"`
		Desc   string `json:"description"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("telegram: decode response: %w", err)
	}
	if !res.OK {
		return nil, fmt.Errorf("telegram API error: %s", res.Desc)
	}
	return res.Result, nil
}

func telegramToItems(v any) []schema.Item {
	if arr, ok := v.([]any); ok {
		out := make([]schema.Item, 0, len(arr))
		for _, e := range arr {
			if m, ok := e.(map[string]any); ok {
				out = append(out, schema.Item{JSON: m})
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if m, ok := v.(map[string]any); ok {
		return []schema.Item{{JSON: m}}
	}
	return []schema.Item{{JSON: map[string]any{"result": v}}}
}

func asString(v any, def string) string {
	if v == nil {
		return def
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// ── Twilio (native) ───────────────────────────────────────────────────────────
// Uses HTTP Basic auth (AccountSID:AuthToken) with form-encoded bodies.

var Twilio = schema.NodeDefinition{
	Type: "comm.twilio", Label: "Twilio", Group: "integration", Icon: "Phone",
	Description: "Send SMS and WhatsApp messages via Twilio.",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "main"}},
	Credentials: []string{"twilioApi"},
	Params: []schema.ParamSchema{
		{Name: "credential", Label: "Credential", Type: "credential", Required: true, CredentialType: "twilioApi"},
		{Name: "operation", Label: "Operation", Type: "select", Required: true, Options: []schema.ParamOption{
			{Label: "message: Send SMS", Value: "message:sendSms"},
			{Label: "message: Send WhatsApp", Value: "message:sendWhatsapp"},
			{Label: "message: List Messages", Value: "message:list"},
			{Label: "call: List Calls", Value: "call:list"},
		}},
		{Name: "from", Label: "From (Twilio number)", Type: "expression",
			ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{"message:sendSms", "message:sendWhatsapp"}}},
		{Name: "to", Label: "To (phone number)", Type: "expression",
			ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{"message:sendSms", "message:sendWhatsapp"}}},
		{Name: "body", Label: "Message Body", Type: "expression",
			ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{"message:sendSms", "message:sendWhatsapp"}}},
		{Name: "limit", Label: "Max results", Type: "number", Default: 20,
			ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{"message:list", "call:list"}}},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		cred, err := ctx.Credential("credential")
		if err != nil {
			return schema.NodeResult{}, err
		}
		sid, _ := cred["accountSid"].(string)
		authToken, _ := cred["authToken"].(string)
		if sid == "" || authToken == "" {
			return schema.NodeResult{}, fmt.Errorf("twilio: accountSid and authToken are required")
		}

		op := asString(ctx.Params["operation"], "")
		base := "https://api.twilio.com/2010-04-01/Accounts/" + sid

		switch op {
		case "message:sendSms", "message:sendWhatsapp":
			from := asString(ctx.Params["from"], "")
			to := asString(ctx.Params["to"], "")
			msgBody := asString(ctx.Params["body"], "")
			if op == "message:sendWhatsapp" {
				if !strings.HasPrefix(from, "whatsapp:") {
					from = "whatsapp:" + from
				}
				if !strings.HasPrefix(to, "whatsapp:") {
					to = "whatsapp:" + to
				}
			}
			form := url.Values{"From": {from}, "To": {to}, "Body": {msgBody}}
			items, err := twilioPost(sid, authToken, base+"/Messages.json", form)
			return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, err
		case "message:list":
			items, err := twilioGet(sid, authToken, base+"/Messages.json")
			return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, err
		case "call:list":
			items, err := twilioGet(sid, authToken, base+"/Calls.json")
			return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, err
		default:
			return schema.NodeResult{}, fmt.Errorf("twilio: unknown operation %q", op)
		}
	},
}

func twilioPost(sid, authToken, apiURL string, form url.Values) ([]schema.Item, error) {
	req, err := http.NewRequest("POST", apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(sid, authToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("twilio: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("twilio: %d %s", resp.StatusCode, string(raw))
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return []schema.Item{{JSON: map[string]any{"raw": string(raw)}}}, nil
	}
	return []schema.Item{{JSON: m}}, nil
}

func twilioGet(sid, authToken, apiURL string) ([]schema.Item, error) {
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(sid, authToken)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("twilio: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("twilio: %d %s", resp.StatusCode, string(raw))
	}
	var root map[string]any
	if json.Unmarshal(raw, &root) != nil {
		return []schema.Item{{JSON: map[string]any{"raw": string(raw)}}}, nil
	}
	// Twilio wraps lists; find the first array field
	for _, v := range root {
		if arr, ok := v.([]any); ok {
			out := make([]schema.Item, 0, len(arr))
			for _, e := range arr {
				if m, ok := e.(map[string]any); ok {
					out = append(out, schema.Item{JSON: m})
				}
			}
			return out, nil
		}
	}
	return []schema.Item{{JSON: root}}, nil
}
