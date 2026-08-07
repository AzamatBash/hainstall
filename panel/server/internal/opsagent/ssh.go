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

// WriteFile writes content to a remote path via base64 (safe for binary/text).
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
