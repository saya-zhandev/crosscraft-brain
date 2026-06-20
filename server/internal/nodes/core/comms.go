package core

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

// Comms-primitive nodes: sendEmail, executeCommand, rssRead. Registered via init.
func init() { Nodes = append(Nodes, commsNodes...) }

var commsNodes = []schema.NodeDefinition{sendEmailNode, executeCommandNode, rssReadNode}

// ── Send Email ───────────────────────────────────────────────────────────────

var sendEmailNode = schema.NodeDefinition{
	Type: "core.sendEmail", Label: "Send Email", Group: "integration", Icon: "Mail",
	Description: "Send an email via SMTP (plain auth + STARTTLS).",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "host", Label: "SMTP Host", Type: "string", Required: true, Placeholder: "smtp.gmail.com"},
		{Name: "port", Label: "SMTP Port", Type: "number", Default: 587},
		{Name: "user", Label: "Username", Type: "string"},
		{Name: "password", Label: "Password", Type: "string"},
		{Name: "from", Label: "From", Type: "expression", Required: true},
		{Name: "to", Label: "To (comma-separated)", Type: "expression", Required: true},
		{Name: "subject", Label: "Subject", Type: "expression"},
		{Name: "body", Label: "Body (HTML or plain text)", Type: "expression"},
		{Name: "isHtml", Label: "Send as HTML", Type: "boolean", Default: true},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		host := asString(ctx.Params["host"], "")
		if host == "" {
			return schema.NodeResult{}, fmt.Errorf("sendEmail: host is required")
		}
		port := asInt(ctx.Params["port"], 587)
		user := asString(ctx.Params["user"], "")
		pass := asString(ctx.Params["password"], "")
		from := asString(ctx.Params["from"], "")
		to := asString(ctx.Params["to"], "")
		subject := asString(ctx.Params["subject"], "")
		body := asString(ctx.Params["body"], "")
		isHtml := true
		if v, ok := ctx.Params["isHtml"]; ok {
			isHtml = isTruthy(v)
		}

		ctype := "text/plain"
		if isHtml {
			ctype = "text/html"
		}
		recipients := []string{}
		for _, r := range strings.Split(to, ",") {
			if t := strings.TrimSpace(r); t != "" {
				recipients = append(recipients, t)
			}
		}
		if len(recipients) == 0 {
			return schema.NodeResult{}, fmt.Errorf("sendEmail: no recipients")
		}

		msg := fmt.Sprintf(
			"From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: %s; charset=UTF-8\r\n\r\n%s",
			from, strings.Join(recipients, ", "), subject, ctype, body,
		)
		addr := fmt.Sprintf("%s:%d", host, port)
		var auth smtp.Auth
		if user != "" {
			auth = smtp.PlainAuth("", user, pass, host)
		}
		if err := smtp.SendMail(addr, auth, from, recipients, []byte(msg)); err != nil {
			return schema.NodeResult{}, fmt.Errorf("sendEmail: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{
			JSON: map[string]any{"sent": true, "to": to, "subject": subject},
		}}}}, nil
	},
}

// ── Execute Command ──────────────────────────────────────────────────────────

var executeCommandNode = schema.NodeDefinition{
	Type: "core.executeCommand", Label: "Execute Command", Group: "integration", Icon: "Terminal",
	Description: "Run a shell command on the server and capture stdout/stderr.",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "command", Label: "Command", Type: "expression", Required: true, Placeholder: "echo hello"},
		{Name: "timeout", Label: "Timeout (seconds)", Type: "number", Default: 30},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		command := asString(ctx.Params["command"], "")
		if command == "" {
			return schema.NodeResult{}, fmt.Errorf("executeCommand: command is required")
		}
		timeoutSec := asInt(ctx.Params["timeout"], 30)

		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/C", command)
		} else {
			cmd = exec.Command("sh", "-c", command)
		}

		done := make(chan struct{})
		var stdout, stderr strings.Builder
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		if err := cmd.Start(); err != nil {
			return schema.NodeResult{}, fmt.Errorf("executeCommand start: %w", err)
		}
		go func() {
			cmd.Wait() //nolint:errcheck
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(time.Duration(timeoutSec) * time.Second):
			cmd.Process.Kill() //nolint:errcheck
			return schema.NodeResult{}, fmt.Errorf("executeCommand: timed out after %ds", timeoutSec)
		}

		exitCode := 0
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{
			JSON: map[string]any{
				"stdout":   strings.TrimRight(stdout.String(), "\r\n"),
				"stderr":   strings.TrimRight(stderr.String(), "\r\n"),
				"exitCode": exitCode,
			},
		}}}}, nil
	},
}

// ── RSS / Atom Feed Reader ───────────────────────────────────────────────────

var rssReadNode = schema.NodeDefinition{
	Type: "core.rssRead", Label: "RSS Read", Group: "integration", Icon: "Rss",
	Description: "Fetch and parse an RSS 2.0 or Atom 1.0 feed; returns one item per entry.",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "url", Label: "Feed URL", Type: "expression", Required: true, Placeholder: "https://example.com/feed.xml"},
		{Name: "limit", Label: "Max items (0 = all)", Type: "number", Default: 0},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		feedURL := asString(ctx.Params["url"], "")
		if feedURL == "" {
			return schema.NodeResult{}, fmt.Errorf("rssRead: url is required")
		}
		limit := asInt(ctx.Params["limit"], 0)

		resp, err := (&http.Client{Timeout: 30 * time.Second}).Get(feedURL)
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("rssRead fetch: %w", err)
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("rssRead read: %w", err)
		}

		items, err := parseFeed(raw)
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("rssRead parse: %w", err)
		}
		if limit > 0 && len(items) > limit {
			items = items[:limit]
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, nil
	},
}

// parseFeed tries RSS 2.0, then Atom 1.0.
func parseFeed(data []byte) ([]schema.Item, error) {
	// Try RSS
	var rss rssFeed
	if xml.Unmarshal(data, &rss) == nil && len(rss.Channel.Items) > 0 {
		out := make([]schema.Item, 0, len(rss.Channel.Items))
		for _, it := range rss.Channel.Items {
			m := map[string]any{
				"title":       it.Title,
				"link":        it.Link,
				"description": it.Description,
				"pubDate":     it.PubDate,
				"guid":        it.GUID,
				"author":      it.Author,
			}
			// include extra fields as raw JSON if present
			if it.Content != "" {
				m["content"] = it.Content
			}
			out = append(out, schema.Item{JSON: cleanMap(m)})
		}
		return out, nil
	}

	// Try Atom
	var atom atomFeed
	if xml.Unmarshal(data, &atom) == nil && len(atom.Entries) > 0 {
		out := make([]schema.Item, 0, len(atom.Entries))
		for _, e := range atom.Entries {
			link := ""
			for _, l := range e.Links {
				if l.Rel == "" || l.Rel == "alternate" {
					link = l.Href
					break
				}
			}
			out = append(out, schema.Item{JSON: cleanMap(map[string]any{
				"title":     e.Title,
				"link":      link,
				"summary":   e.Summary,
				"content":   e.Content,
				"published": e.Published,
				"updated":   e.Updated,
				"id":        e.ID,
				"author":    e.Author.Name,
			})})
		}
		return out, nil
	}

	// Fallback: return raw JSON if the document was JSON
	var arr []any
	if json.Unmarshal(data, &arr) == nil {
		out := make([]schema.Item, 0, len(arr))
		for _, e := range arr {
			out = append(out, toItem(e))
		}
		return out, nil
	}
	return []schema.Item{{JSON: map[string]any{"raw": string(data)}}}, nil
}

func cleanMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok && s == "" {
			continue
		}
		out[k] = v
	}
	return out
}

// ── XML structs for feed parsing ─────────────────────────────────────────────

type rssFeed struct {
	XMLName xml.Name   `xml:"rss"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
	GUID        string `xml:"guid"`
	Author      string `xml:"author"`
	Content     string `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
}

type atomFeed struct {
	XMLName xml.Name    `xml:"http://www.w3.org/2005/Atom feed"`
	Entries []atomEntry `xml:"http://www.w3.org/2005/Atom entry"`
}

type atomEntry struct {
	ID        string     `xml:"id"`
	Title     string     `xml:"title"`
	Summary   string     `xml:"summary"`
	Content   string     `xml:"content"`
	Published string     `xml:"published"`
	Updated   string     `xml:"updated"`
	Links     []atomLink `xml:"link"`
	Author    atomAuthor `xml:"author"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
}

type atomAuthor struct {
	Name string `xml:"name"`
}
