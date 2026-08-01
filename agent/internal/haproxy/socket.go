package haproxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// Client talks to the HAProxy master/runtime Unix socket.
type Client struct {
	SocketPath string
	Timeout    time.Duration
}

// NewClient returns a runtime API client. Default timeout is 5s.
func NewClient(socketPath string) *Client {
	return &Client{
		SocketPath: socketPath,
		Timeout:    5 * time.Second,
	}
}

// Exec runs a single runtime API command and returns the full response body.
func (c *Client) Exec(cmd string) (string, error) {
	if c.SocketPath == "" {
		return "", fmt.Errorf("haproxy socket path is empty")
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	conn, err := net.DialTimeout("unix", c.SocketPath, timeout)
	if err != nil {
		return "", fmt.Errorf("dial haproxy socket: %w", err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(timeout))

	if !strings.HasSuffix(cmd, "\n") {
		cmd += "\n"
	}
	if _, err := io.WriteString(conn, cmd); err != nil {
		return "", fmt.Errorf("write command: %w", err)
	}

	var b strings.Builder
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString('\n')
		b.WriteString(line)
		if err == io.EOF {
			break
		}
		if err != nil {
			// Deadline / closed socket after response is common; keep what we have.
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				break
			}
			if b.Len() > 0 {
				break
			}
			return "", fmt.Errorf("read response: %w", err)
		}
	}
	return b.String(), nil
}

// DefaultWaitReadyTimeout is how long WaitReady waits for the admin socket.
const DefaultWaitReadyTimeout = 25 * time.Second

// WaitReady polls the admin socket until it accepts connections and answers
// "show info". Use after container restart/reload while HAProxy recreates the
// unix socket — otherwise immediate stats calls get connection refused.
func (c *Client) WaitReady(ctx context.Context) error {
	return c.WaitReadyTimeout(ctx, DefaultWaitReadyTimeout)
}

// WaitReadyTimeout is like WaitReady with a custom overall deadline.
func (c *Client) WaitReadyTimeout(ctx context.Context, maxWait time.Duration) error {
	if c.SocketPath == "" {
		return fmt.Errorf("haproxy socket path is empty")
	}
	if maxWait <= 0 {
		maxWait = DefaultWaitReadyTimeout
	}
	deadline := time.Now().Add(maxWait)
	backoff := 50 * time.Millisecond
	const maxBackoff = 500 * time.Millisecond

	// Short dial/exec timeout so retries stay responsive while the socket
	// is being recreated after docker restart.
	probe := *c
	probe.Timeout = 500 * time.Millisecond

	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return fmt.Errorf("haproxy not ready: %w (last: %v)", err, lastErr)
			}
			return fmt.Errorf("haproxy not ready: %w", err)
		}
		if time.Now().After(deadline) {
			if lastErr != nil {
				return fmt.Errorf("haproxy not ready after %s: %w", maxWait, lastErr)
			}
			return fmt.Errorf("haproxy not ready after %s", maxWait)
		}

		raw, err := probe.Exec("show info")
		if err == nil && strings.Contains(raw, "Name:") {
			return nil
		}
		if err == nil {
			lastErr = fmt.Errorf("unexpected show info response")
		} else {
			lastErr = err
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("haproxy not ready: %w (last: %v)", ctx.Err(), lastErr)
		case <-time.After(backoff):
		}
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}
