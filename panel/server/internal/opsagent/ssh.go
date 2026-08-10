package opsagent

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type SSHClient struct {
	client *ssh.Client
}

func DialSSH(host, user, password string, port int, timeout time.Duration) (*SSHClient, error) {
	host = strings.TrimSpace(host)
	user = strings.TrimSpace(user)
	if host == "" || user == "" {
		return nil, fmt.Errorf("host and user required")
	}
	if port <= 0 {
		port = 22
	}
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // MVP: first-connect trust
		Timeout:         timeout,
	}
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("ssh dial: %w", err)
	}
	return &SSHClient{client: client}, nil
}

func (c *SSHClient) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Close()
}

// Run executes a remote shell command; returns combined stdout+stderr and exit error.
func (c *SSHClient) Run(cmd string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = 3 * time.Minute
	}
	session, err := c.client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	var buf bytes.Buffer
	session.Stdout = &buf
	session.Stderr = &buf

	done := make(chan error, 1)
	go func() { done <- session.Run(cmd) }()

	select {
	case err := <-done:
		out := buf.String()
		if err != nil {
			return out, fmt.Errorf("%w\n%s", err, truncate(out, 4000))
		}
		return out, nil
	case <-time.After(timeout):
		_ = session.Signal(ssh.SIGKILL)
		return buf.String(), fmt.Errorf("command timeout after %s", timeout)
	}
}

// WriteFile writes content to a remote path via base64 (safe for small text).
func (c *SSHClient) WriteFile(path string, content []byte) error {
	dir := path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		dir = path[:i]
	}
	b64 := base64.StdEncoding.EncodeToString(content)
	cmd := fmt.Sprintf(`mkdir -p %q && echo %s | base64 -d > %q && chmod 644 %q`, dir, shellQuote(b64), path, path)
	_, err := c.Run(cmd, 2*time.Minute)
	return err
}

// Upload streams content to a remote path (for binaries). Sets mode 0755.
func (c *SSHClient) Upload(path string, content []byte) error {
	dir := path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		dir = path[:i]
	}
	session, err := c.client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	session.Stdout = &buf
	session.Stderr = &buf

	cmd := fmt.Sprintf(`mkdir -p %q && cat > %q && chmod 755 %q`, dir, path, path)
	done := make(chan error, 1)
	go func() { done <- session.Run(cmd) }()

	if _, err := stdin.Write(content); err != nil {
		_ = stdin.Close()
		<-done
		return fmt.Errorf("upload write: %w", err)
	}
	_ = stdin.Close()

	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("%w\n%s", err, truncate(buf.String(), 2000))
		}
		return nil
	case <-time.After(10 * time.Minute):
		_ = session.Signal(ssh.SIGKILL)
		return fmt.Errorf("upload timeout")
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
