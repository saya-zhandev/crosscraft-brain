package core

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
	"golang.org/x/crypto/ssh"
)

func init() { Nodes = append(Nodes, ftpNode) }

// ftpNode transfers files via FTP and SFTP protocols.
//
// NOTE: For advanced FTP features (passive mode negotiation, directory listing
// parsing, resumable uploads), add github.com/jlaffaye/ftp for FTP and
// github.com/pkg/sftp for SFTP. The SFTP implementation here uses
// golang.org/x/crypto/ssh which is available as a transitive dependency.
var ftpNode = schema.NodeDefinition{
	Type: "core.ftp", Label: "FTP / SFTP", Group: "integration", Icon: "FolderSync",
	Description: "List, upload, download, and delete files via FTP or SFTP.",
	Inputs:  []schema.Port{{ID: "main"}},
	Outputs: []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "action", Label: "Action", Type: "select", Default: "list", Options: []schema.ParamOption{
			{Label: "List Files", Value: "list"},
			{Label: "Upload File", Value: "upload"},
			{Label: "Download File", Value: "download"},
			{Label: "Delete File", Value: "delete"},
		}},
		{Name: "protocol", Label: "Protocol", Type: "select", Default: "sftp", Options: []schema.ParamOption{
			{Label: "SFTP (SSH)", Value: "sftp"},
			{Label: "FTP", Value: "ftp"},
		}},
		{Name: "host", Label: "Host", Type: "string", Required: true, Placeholder: "ftp.example.com"},
		{Name: "port", Label: "Port", Type: "number", Default: float64(22)},
		{Name: "user", Label: "Username", Type: "string", Required: true},
		{Name: "password", Label: "Password", Type: "string"},
		{Name: "remotePath", Label: "Remote path", Type: "string", Required: true, Placeholder: "/remote/file.txt"},
		{Name: "binaryProperty", Label: "Binary property", Type: "string", Default: "data"},
		{Name: "outputProperty", Label: "Output binary property", Type: "string", Default: "data"},
		{Name: "fileName", Label: "File name", Type: "string"},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		action := asString(ctx.Params["action"], "list")
		protocol := asString(ctx.Params["protocol"], "sftp")
		host := asString(ctx.Params["host"], "")
		port := int(asFloat(ctx.Params["port"], 22))
		user := asString(ctx.Params["user"], "")
		password := asString(ctx.Params["password"], "")
		remotePath := asString(ctx.Params["remotePath"], "")
		prop := asString(ctx.Params["binaryProperty"], "data")
		outProp := asString(ctx.Params["outputProperty"], "data")
		fileName := asString(ctx.Params["fileName"], "")

		if host == "" || user == "" {
			return schema.NodeResult{}, fmt.Errorf("ftp: host and user are required")
		}

		if protocol == "sftp" {
			return handleSFTP(ctx, action, host, port, user, password, remotePath, fileName, prop, outProp)
		}
		return handleFTP(ctx, action, host, port, user, password, remotePath, fileName, prop, outProp)
	},
}

// ---------------------------------------------------------------------------
// SFTP via golang.org/x/crypto/ssh
// ---------------------------------------------------------------------------

func handleSFTP(ctx *schema.ExecContext, action, host string, port int, user, password, remotePath, fileName, prop, outProp string) (schema.NodeResult, error) {
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
		return schema.NodeResult{}, fmt.Errorf("sftp: connect: %w", err)
	}
	defer client.Close()

	sftpClient, err := newSFTPClient(client)
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("sftp: init: %w", err)
	}
	defer sftpClient.Close()

	switch action {
	case "list":
		files, err := sftpClient.ReadDir(remotePath)
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("sftp list: %w", err)
		}
		items := make([]schema.Item, 0, len(files))
		for _, f := range files {
			items = append(items, schema.Item{JSON: map[string]any{
				"name":    f.Name(),
				"size":    f.Size(),
				"mode":    f.Mode().String(),
				"isDir":   f.IsDir(),
				"modTime": f.ModTime().Format(time.RFC3339),
			}})
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, nil

	case "upload":
		in := itemsOrEmpty(ctx.Input)
		if len(in) == 0 || in[0].Binary == nil {
			return schema.NodeResult{}, fmt.Errorf("sftp upload: no binary data to upload")
		}
		ref, ok := in[0].Binary[prop]
		if !ok {
			return schema.NodeResult{}, fmt.Errorf("sftp upload: binary property %q not found", prop)
		}
		data, err := base64.StdEncoding.DecodeString(ref.Data)
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("sftp upload: decode: %w", err)
		}
		dstName := remotePath
		if fileName != "" {
			dstName = remotePath
			if !strings.HasSuffix(dstName, "/") {
				dstName += "/"
			}
			dstName += fileName
		}
		dst, err := sftpClient.Create(dstName)
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("sftp upload: create: %w", err)
		}
		if _, err := dst.Write(data); err != nil {
			dst.Close()
			return schema.NodeResult{}, fmt.Errorf("sftp upload: write: %w", err)
		}
		dst.Close()
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{JSON: map[string]any{
			"uploaded": true, "path": dstName, "size": len(data),
		}}}}}, nil

	case "download":
		src, err := sftpClient.Open(remotePath)
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("sftp download: %w", err)
		}
		defer src.Close()
		data, err := io.ReadAll(src)
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("sftp download: read: %w", err)
		}
		outName := fileName
		if outName == "" {
			outName = filepath.Base(remotePath)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{
			JSON: map[string]any{"fileName": outName, "size": len(data), "path": remotePath},
			Binary: map[string]schema.BinaryRef{
				outProp: {Data: base64.StdEncoding.EncodeToString(data), MimeType: "application/octet-stream", FileName: outName},
			},
		}}}}, nil

	case "delete":
		if err := sftpClient.Remove(remotePath); err != nil {
			// Try RemoveAll if file doesn't exist as a file (might be a dir)
			if strings.Contains(err.Error(), "file does not exist") || strings.Contains(err.Error(), "not a directory") {
				err = sftpClient.RemoveAll(remotePath)
			}
			if err != nil {
				return schema.NodeResult{}, fmt.Errorf("sftp delete: %w", err)
			}
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{JSON: map[string]any{
			"deleted": true, "path": remotePath,
		}}}}}, nil

	default:
		return schema.NodeResult{}, fmt.Errorf("sftp: unknown action %q", action)
	}
}

// ---------------------------------------------------------------------------
// Basic FTP via stdlib
// ---------------------------------------------------------------------------

func handleFTP(ctx *schema.ExecContext, action, host string, port int, user, password, remotePath, fileName, prop, outProp string) (schema.NodeResult, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 15*time.Second)
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("ftp: connect: %w", err)
	}
	defer conn.Close()

	tp := textproto.NewConn(conn)

	// Read greeting
	_, err = tp.ReadLine()
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("ftp: greeting: %w", err)
	}

	sendCmd := func(cmd string, expectedPrefix string) (string, error) {
		if err := tp.PrintfLine("%s", cmd); err != nil {
			return "", fmt.Errorf("ftp: send %q: %w", cmd, err)
		}
		line, err := tp.ReadLine()
		if err != nil {
			return "", fmt.Errorf("ftp: read: %w", err)
		}
		if expectedPrefix != "" && !strings.HasPrefix(line, expectedPrefix) {
			return line, fmt.Errorf("ftp: unexpected response: %s", line)
		}
		return line, nil
	}

	// Login
	if _, err := sendCmd("USER "+sanitizeFTPArg(user), "331"); err != nil {
		return schema.NodeResult{}, err
	}
	if _, err := sendCmd("PASS "+sanitizeFTPArg(password), "230"); err != nil {
		return schema.NodeResult{}, fmt.Errorf("ftp: login failed: %w", err)
	}

	switch action {
	case "list":
		// Open data connection for LIST
		dataConn, err := ftpDataConn(conn, tp)
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("ftp list: data conn: %w", err)
		}
		defer dataConn.Close()

		if _, err := sendCmd("LIST "+sanitizeFTPArg(remotePath), "150"); err != nil {
			return schema.NodeResult{}, err
		}
		// Read directory listing
		var buf bytes.Buffer
		buf.ReadFrom(dataConn)
		lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
		items := make([]schema.Item, 0, len(lines))
		for _, line := range lines {
			if line != "" {
				items = append(items, schema.Item{JSON: map[string]any{"line": line}})
			}
		}
		// Read transfer complete
		tp.ReadLine()
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": items}}, nil

	case "upload":
		in := itemsOrEmpty(ctx.Input)
		if len(in) == 0 || in[0].Binary == nil {
			return schema.NodeResult{}, fmt.Errorf("ftp upload: no binary data to upload")
		}
		ref, ok := in[0].Binary[prop]
		if !ok {
			return schema.NodeResult{}, fmt.Errorf("ftp upload: binary property %q not found", prop)
		}
		data, err := base64.StdEncoding.DecodeString(ref.Data)
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("ftp upload: decode: %w", err)
		}

		dstName := remotePath
		if fileName != "" {
			dstName = filepath.Join(remotePath, fileName)
		}

		dataConn, err := ftpDataConn(conn, tp)
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("ftp upload: data conn: %w", err)
		}
		defer dataConn.Close()

		if _, err := sendCmd("STOR "+sanitizeFTPArg(dstName), "150"); err != nil {
			return schema.NodeResult{}, err
		}
		dataConn.Write(data)
		dataConn.Close()
		tp.ReadLine() // transfer complete
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{JSON: map[string]any{
			"uploaded": true, "path": dstName, "size": len(data),
		}}}}}, nil

	case "download":
		dataConn, err := ftpDataConn(conn, tp)
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("ftp download: data conn: %w", err)
		}
		defer dataConn.Close()

		if _, err := sendCmd("RETR "+sanitizeFTPArg(remotePath), "150"); err != nil {
			return schema.NodeResult{}, err
		}
		var buf bytes.Buffer
		buf.ReadFrom(dataConn)
		tp.ReadLine() // transfer complete
		data := buf.Bytes()
		outName := fileName
		if outName == "" {
			outName = filepath.Base(remotePath)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{
			JSON: map[string]any{"fileName": outName, "size": len(data), "path": remotePath},
			Binary: map[string]schema.BinaryRef{
				outProp: {Data: base64.StdEncoding.EncodeToString(data), MimeType: "application/octet-stream", FileName: outName},
			},
		}}}}, nil

	case "delete":
		if _, err := sendCmd("DELE "+sanitizeFTPArg(remotePath), "250"); err != nil {
			return schema.NodeResult{}, fmt.Errorf("ftp delete: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{JSON: map[string]any{
			"deleted": true, "path": remotePath,
		}}}}}, nil

	default:
		return schema.NodeResult{}, fmt.Errorf("ftp: unknown action %q", action)
	}
}

func ftpDataConn(ctrlConn net.Conn, tp *textproto.Conn) (net.Conn, error) {
	// Use PASV (passive mode)
	if _, err := tp.Cmd("PASV"); err != nil {
		return nil, fmt.Errorf("ftp PASV: %w", err)
	}
	line, err := tp.ReadLine()
	if err != nil {
		return nil, fmt.Errorf("ftp PASV response: %w", err)
	}
	// Parse PASV response: 227 Entering Passive Mode (h1,h2,h3,h4,p1,p2)
	start := strings.Index(line, "(")
	end := strings.Index(line, ")")
	if start < 0 || end < 0 {
		return nil, fmt.Errorf("ftp: invalid PASV response: %s", line)
	}
	parts := strings.Split(line[start+1:end], ",")
	if len(parts) != 6 {
		return nil, fmt.Errorf("ftp: invalid PASV format: %s", line[start+1:end])
	}
	p1, _ := strconv.Atoi(parts[4])
	p2, _ := strconv.Atoi(parts[5])
	dataPort := p1*256 + p2
	dataHost := strings.Join(parts[:4], ".")

	// Get actual server address from control connection
	ctrlAddr := ctrlConn.RemoteAddr().String()
	host, _, _ := net.SplitHostPort(ctrlAddr)

	dataConn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(dataPort)), 15*time.Second)
	if err != nil {
		// Fall back to the address in PASV response
		return net.DialTimeout("tcp", net.JoinHostPort(dataHost, strconv.Itoa(dataPort)), 15*time.Second)
	}
	return dataConn, nil
}

// ---------------------------------------------------------------------------
// SFTP client wrapper
// ---------------------------------------------------------------------------

// sftpClientInterface abstracts basic SFTP operations.
type sftpClientInterface interface {
	ReadDir(path string) ([]osFileInfo, error)
	Open(path string) (io.ReadCloser, error)
	Create(path string) (io.WriteCloser, error)
	Remove(path string) error
	RemoveAll(path string) error
	Close() error
}

// osFileInfo mirrors os.FileInfo fields.
type osFileInfo struct {
	name    string
	size    int64
	mode    uint32
	isDir   bool
	modTime time.Time
}

func (f osFileInfo) Name() string       { return f.name }
func (f osFileInfo) Size() int64        { return f.size }
func (f osFileInfo) Mode() osFileMode   { return osFileMode(f.mode) }
func (f osFileInfo) ModTime() time.Time { return f.modTime }
func (f osFileInfo) IsDir() bool        { return f.isDir }
func (f osFileInfo) Sys() any           { return nil }

type osFileMode uint32

func (m osFileMode) String() string { return fmt.Sprintf("%o", m) }

// sftpWrapper wraps the SSH/SFTP session.
type sftpWrapper struct {
	client  *ssh.Client
	session *ssh.Session
}

func newSFTPClient(sshClient *ssh.Client) (*sftpWrapper, error) {
	return &sftpWrapper{client: sshClient}, nil
}

func (s *sftpWrapper) ReadDir(path string) ([]osFileInfo, error) {
	if !isValidSFTPPath(path) {
		return nil, fmt.Errorf("sftp: invalid path: %q", path)
	}
	session, err := s.client.NewSession()
	if err != nil {
		return nil, err
	}
	defer session.Close()

	cmd := "ls -la " + path
	out, err := session.Output(cmd)
	if err != nil {
		return nil, fmt.Errorf("sftp ls: %w (output: %s)", err, string(out))
	}

	var files []osFileInfo
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "total ") {
			continue
		}
		files = append(files, osFileInfo{name: line, modTime: time.Now()})
	}
	return files, nil
}

func (s *sftpWrapper) Open(path string) (io.ReadCloser, error) {
	if !isValidSFTPPath(path) {
		return nil, fmt.Errorf("sftp: invalid path: %q", path)
	}
	session, err := s.client.NewSession()
	if err != nil {
		return nil, err
	}
	cmd := "cat " + path
	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, err
	}
	if err := session.Start(cmd); err != nil {
		session.Close()
		return nil, err
	}
	return &sftpReadCloser{session: session, reader: stdout}, nil
}

func (s *sftpWrapper) Create(path string) (io.WriteCloser, error) {
	if !isValidSFTPPath(path) {
		return nil, fmt.Errorf("sftp: invalid path: %q", path)
	}
	session, err := s.client.NewSession()
	if err != nil {
		return nil, err
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		return nil, err
	}
	cmd := "cat > " + path
	if err := session.Start(cmd); err != nil {
		session.Close()
		return nil, err
	}
	return &sftpWriteCloser{session: session, writer: stdin}, nil
}

func (s *sftpWrapper) Remove(path string) error {
	if !isValidSFTPPath(path) {
		return fmt.Errorf("sftp: invalid path: %q", path)
	}
	session, err := s.client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	return session.Run("rm " + path)
}

func (s *sftpWrapper) RemoveAll(path string) error {
	if !isValidSFTPPath(path) {
		return fmt.Errorf("sftp: invalid path: %q", path)
	}
	session, err := s.client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	return session.Run("rm -rf " + path)
}

func (s *sftpWrapper) Close() error {
	return s.client.Close()
}

type sftpReadCloser struct {
	session *ssh.Session
	reader  io.Reader
}

func (r *sftpReadCloser) Read(p []byte) (int, error) { return r.reader.Read(p) }
func (r *sftpReadCloser) Close() error               { return r.session.Close() }

type sftpWriteCloser struct {
	session *ssh.Session
	writer  io.Writer
}

func (w *sftpWriteCloser) Write(p []byte) (int, error) { return w.writer.Write(p) }
func (w *sftpWriteCloser) Close() error {
	w.writer.(io.Closer).Close()
	return w.session.Wait()
}

var safePathRE = regexp.MustCompile(`^[a-zA-Z0-9_./\-]+$`)

func isValidSFTPPath(path string) bool {
	return len(path) > 0 && len(path) < 4096 && safePathRE.MatchString(path) && !strings.Contains(path, "..")
}

// shellEscape quotes a path for use in a shell command via single-quote escaping.
// Prefer isValidSFTPPath validation over shell commands; this is retained for the
// ssh.go node which executes commands over SSH sessions.
func shellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func sanitizeFTPArg(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	return s
}
