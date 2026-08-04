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

// Client talks to the HAProxy master/runtime API (Unix socket or TCP).
type Client struct {
	// Addr is either a Unix path ("/var/run/haproxy/admin.sock") or a TCP
	// target ("haproxy:9999" / "tcp://haproxy:9999").
	Addr    string
	Timeout time.Duration
}

// NewClient returns a runtime API client. Default timeout is 5s.
func NewClient(addr string) *Client {
	return &Client{
		Addr:    strings.TrimSpace(addr),
		Timeout: 5 * time.Second,
	}
}

func (c *Client) networkAndAddress() (network, address string, err error) {
	addr := c.Addr
	if addr == "" {
		return "", "", fmt.Errorf("haproxy address is empty")
	}
	switch {
	case strings.HasPrefix(addr, "tcp://"):
		return "tcp", strings.TrimPrefix(addr, "tcp://"), nil
	case strings.HasPrefix(addr, "unix://"):
		return "unix", strings.TrimPrefix(addr, "unix://"), nil
	case strings.HasPrefix(addr, "/"):
		return "unix", addr, nil
	case strings.Contains(addr, ":"):
		// host:port
		return "tcp", addr, nil
	default:
		return "unix", addr, nil
	}
}

// Exec runs a single runtime API command and returns the full response body.
func (c *Client) Exec(cmd string) (string, error) {
	network, address, err := c.networkAndAddress()
	if err != nil {
		return "", err
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	conn, err := net.DialTimeout(network, address, timeout)
	if err != nil {
		return "", fmt.Errorf("dial haproxy %s %s: %w", network, address, err)
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

// DefaultWaitReadyTimeout is how long WaitReady waits for the runtime API.
const DefaultWaitReadyTimeout = 25 * time.Second

// WaitReady polls until the runtime API answers "show info".
func (c *Client) WaitReady(ctx context.Context) error {
	return c.WaitReadyTimeout(ctx, DefaultWaitReadyTimeout)
}

// WaitReadyTimeout is like WaitReady with a custom overall deadline.
func (c *Client) WaitReadyTimeout(ctx context.Context, maxWait time.Duration) error {
	if _, _, err := c.networkAndAddress(); err != nil {
		return err
	}
	if maxWait <= 0 {
		maxWait = DefaultWaitReadyTimeout
	}
	deadline := time.Now().Add(maxWait)
	backoff := 50 * time.Millisecond
	const maxBackoff = 500 * time.Millisecond

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
