package core

import (
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
	"golang.org/x/crypto/ssh"
)

func init() { Nodes = append(Nodes, sshNode) }

// sshNode executes commands and transfers files over SSH.
var sshNode = schema.NodeDefinition{
	Type: "core.ssh", Label: "SSH", Group: "integration", Icon: "Terminal",
	Description: "Execute commands, list files, upload, and download via SSH.",
	Inputs:  []schema.Port{{ID: "main"}},
	Outputs: []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "action", Label: "Action", Type: "select", Default: "exec", Options: []schema.ParamOption{
			{Label: "Execute Command", Value: "exec"},
			{Label: "List Files", Value: "list"},
			{Label: "Upload File", Value: "upload"},
			{Label: "Download File", Value: "download"},
		}},
		{Name: "host", Label: "Host", Type: "string", Required: true, Placeholder: "server.example.com"},
		{Name: "port", Label: "Port", Type: "number", Default: float64(22)},
		{Name: "user", Label: "Username", Type: "string", Required: true},
		{Name: "password", Label: "Password", Type: "string"},
		{Name: "command", Label: "Command", Type: "code", Placeholder: "ls -la /var/log",
			ShowWhen: &schema.ShowWhen{Param: "action", Equals: []any{"exec"}}},
		{Name: "remotePath", Label: "Remote path", Type: "string", Placeholder: "/var/log/app.log",
			ShowWhen: &schema.ShowWhen{Param: "action", Equals: []any{"list", "download", "delete"}}},
		{Name: "binaryProperty", Label: "Binary property", Type: "string", Default: "data",
			ShowWhen: &schema.ShowWhen{Param: "action", Equals: []any{"upload", "download"}}},
		{Name: "outputProperty", Label: "Output binary property", Type: "string", Default: "data",
			ShowWhen: &schema.ShowWhen{Param: "action", Equals: []any{"download"}}},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		action := asString(ctx.Params["action"], "exec")
		host := asString(ctx.Params["host"], "")
		port := int(asFloat(ctx.Params["port"], 22))
		user := asString(ctx.Params["user"], "")
		password := asString(ctx.Params["password"], "")
		command := asString(ctx.Params["command"], "hostname")
		remotePath := asString(ctx.Params["remotePath"], ".")
		prop := asString(ctx.Params["binaryProperty"], "data")
		outProp := asString(ctx.Params["outputProperty"], "data")

		if host == "" || user == "" {
			return schema.NodeResult{}, fmt.Errorf("ssh: host and user are required")
		}

		addr := net.JoinHostPort(host, strconv.Itoa(port))

		authMethods := []ssh.AuthMethod{}
		if password != "" {
			authMethods = append(authMethods, ssh.Password(password))
		}
		config := &ssh.ClientConfig{
			User:            user,
			Auth:            authMethods,
			HostKeyCallback: ssh.InsecureIgnoreHostKey(), // Note: production should verify host keys
			Timeout:         15 * time.Second,
		}

		client, err := ssh.Dial("tcp", addr, config)
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("ssh: connect: %w", err)
		}
		defer client.Close()

		switch action {
		case "exec":
			return sshExec(client, command)
		case "list":
			return sshListFiles(client, remotePath)
		case "upload":
			return sshUpload(ctx, client, remotePath, prop)
		case "download":
			return sshDownload(client, remotePath, outProp)
		default:
			return schema.NodeResult{}, fmt.Errorf("ssh: unknown action %q", action)
		}
	},
}

func sshExec(client *ssh.Client, command string) (schema.NodeResult, error) {
	session, err := client.NewSession()
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("ssh exec: session: %w", err)
	}
	defer session.Close()

	stdout, err := session.StdoutPipe()
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("ssh exec: stdout: %w", err)
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("ssh exec: stderr: %w", err)
	}

	if err := session.Start(command); err != nil {
		return schema.NodeResult{}, fmt.Errorf("ssh exec: start: %w", err)
	}

	outBytes, _ := io.ReadAll(stdout)
	errBytes, _ := io.ReadAll(stderr)

	err = session.Wait()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*ssh.ExitError); ok {
			exitCode = exitErr.ExitStatus()
		} else {
			exitCode = -1
		}
	}

	return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{
		JSON: map[string]any{
			"stdout":   string(outBytes),
			"stderr":   string(errBytes),
			"exitCode": exitCode,
			"command":  command,
		},
	}}}}, nil
}

func sshListFiles(client *ssh.Client, path string) (schema.NodeResult, error) {
	if !isValidSFTPPath(path) {
		return schema.NodeResult{}, fmt.Errorf("ssh: invalid path: %q", path)
	}
	session, err := client.NewSession()
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("ssh list: session: %w", err)
	}
	defer session.Close()

	out, err := session.Output("ls -la " + path)
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("ssh list: %w (output: %s)", err, string(out))
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	items := make([]schema.Item, 0, len(lines)-1) // skip "total" line
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "total ") {
			continue
		}
		// Parse ls -la output: permissions links owner group size month day time/year name
		parts := strings.Fields(line)
		if len(parts) < 9 {
			items = append(items, schema.Item{JSON: map[string]any{"line": line}})
			continue
		}
		name := strings.Join(parts[8:], " ")
		items = append(items, schema.Item{JSON: map[string]any{
			"name":        name,
			"permissions": parts[0],
			"owner":       parts[2],
			"group":       parts[3],
			"size":        parts[4],
			"date":        parts[5] + " " + parts[6] + " " + parts[7],
		}})
	}
	return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, nil
}

func sshUpload(ctx *schema.ExecContext, client *ssh.Client, remotePath, prop string) (schema.NodeResult, error) {
	in := itemsOrEmpty(ctx.Input)
	if len(in) == 0 || in[0].Binary == nil {
		return schema.NodeResult{}, fmt.Errorf("ssh upload: no binary data to upload")
	}
	ref, ok := in[0].Binary[prop]
	if !ok {
		return schema.NodeResult{}, fmt.Errorf("ssh upload: binary property %q not found", prop)
	}
	data, err := base64.StdEncoding.DecodeString(ref.Data)
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("ssh upload: decode: %w", err)
	}

	session, err := client.NewSession()
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("ssh upload: session: %w", err)
	}
	defer session.Close()

	if !isValidSFTPPath(remotePath) {
		return schema.NodeResult{}, fmt.Errorf("ssh: invalid path: %q", remotePath)
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("ssh upload: stdin: %w", err)
	}

	cmd := "cat > " + remotePath
	if err := session.Start(cmd); err != nil {
		return schema.NodeResult{}, fmt.Errorf("ssh upload: start: %w", err)
	}

	stdin.Write(data)
	stdin.Close()

	if err := session.Wait(); err != nil {
		return schema.NodeResult{}, fmt.Errorf("ssh upload: %w", err)
	}

	return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{JSON: map[string]any{
		"uploaded": true, "path": remotePath, "size": len(data),
	}}}}}, nil
}

func sshDownload(client *ssh.Client, remotePath, outProp string) (schema.NodeResult, error) {
	session, err := client.NewSession()
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("ssh download: session: %w", err)
	}
	defer session.Close()

	if !isValidSFTPPath(remotePath) {
		return schema.NodeResult{}, fmt.Errorf("ssh: invalid path: %q", remotePath)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("ssh download: stdout: %w", err)
	}

	cmd := "cat " + remotePath
	if err := session.Start(cmd); err != nil {
		return schema.NodeResult{}, fmt.Errorf("ssh download: start: %w", err)
	}

	data, err := io.ReadAll(stdout)
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("ssh download: read: %w", err)
	}

	if err := session.Wait(); err != nil {
		return schema.NodeResult{}, fmt.Errorf("ssh download: %w", err)
	}

	// Derive file name from remote path
	fileName := remotePath
	if idx := strings.LastIndex(remotePath, "/"); idx >= 0 {
		fileName = remotePath[idx+1:]
	}

	return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{
		JSON: map[string]any{"fileName": fileName, "size": len(data), "path": remotePath},
		Binary: map[string]schema.BinaryRef{
			outProp: {Data: base64.StdEncoding.EncodeToString(data), MimeType: "application/octet-stream", FileName: fileName},
		},
	}}}}, nil
}
