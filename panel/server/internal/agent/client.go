package agent

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
)

// Client talks to hapctl node agents.
// http:// URLs use plain HTTP (no TLS). https:// URLs use uTLS
// (HelloRandomized by default — browser presets often EOF against OpenSSL edges).
type Client struct {
	httpClient *http.Client
	insecure   bool
	helloID    utls.ClientHelloID
	helloName  string
}

func New(insecureSkipVerify bool) *Client {
	return NewWithHello(insecureSkipVerify, "randomized")
}

// NewWithHello builds a client. hello: randomized|chrome|firefox|golang|safari
func NewWithHello(insecureSkipVerify bool, hello string) *Client {
	id, name := parseHello(hello)
	c := &Client{insecure: insecureSkipVerify, helloID: id, helloName: name}
	plain := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	https := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DialTLSContext:        c.dialTLSContext,
	}
	c.httpClient = &http.Client{
		Timeout:   45 * time.Second,
		Transport: &schemeTransport{http: plain, https: https},
	}
	return c
}

// schemeTransport routes http:// via plain Transport and https:// via uTLS.
type schemeTransport struct {
	http  http.RoundTripper
	https http.RoundTripper
}

func (t *schemeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	switch req.URL.Scheme {
	case "http":
		return t.http.RoundTrip(req)
	case "https":
		return t.https.RoundTrip(req)
	default:
		return nil, fmt.Errorf("unsupported scheme %q", req.URL.Scheme)
	}
}

func (c *Client) HelloName() string { return c.helloName }

func parseHello(hello string) (utls.ClientHelloID, string) {
	switch strings.ToLower(strings.TrimSpace(hello)) {
	case "chrome", "chrome_auto":
		return utls.HelloChrome_Auto, "HelloChrome_Auto"
	case "firefox", "firefox_auto":
		return utls.HelloFirefox_Auto, "HelloFirefox_Auto"
	case "safari", "safari_auto":
		return utls.HelloSafari_Auto, "HelloSafari_Auto"
	case "golang", "go":
		return utls.HelloGolang, "HelloGolang"
	case "randomized_alpn":
		return utls.HelloRandomizedALPN, "HelloRandomizedALPN"
	default:
		return utls.HelloRandomized, "HelloRandomized"
	}
}

func (c *Client) dialTLSContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}

	dialer := &net.Dialer{Timeout: 15 * time.Second}
	rawConn, err := dialer.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}

	cfg := &utls.Config{
		ServerName:         host,
		InsecureSkipVerify: c.insecure,
		MinVersion:         tls.VersionTLS12,
		// Force HTTP/1.1 so ALPN does not select h2 against a non-h2 Transport.
		NextProtos: []string{"http/1.1"},
	}

	uConn := utls.UClient(rawConn, cfg, c.helloID)
	if err := uConn.HandshakeContext(ctx); err != nil {
		_ = rawConn.Close()
		return nil, fmt.Errorf("utls handshake (%s): %w", c.helloName, err)
	}
	return uConn, nil
}

type RequestOptions struct {
	Method string
	Path   string
	Token  string
	Body   io.Reader
}

func (c *Client) Do(ctx context.Context, baseURL string, opt RequestOptions) (*http.Response, error) {
	base := strings.TrimRight(baseURL, "/")
	path := opt.Path
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u, err := url.Parse(base + path)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, fmt.Errorf("unsupported scheme %q", u.Scheme)
	}

	req, err := http.NewRequestWithContext(ctx, opt.Method, u.String(), opt.Body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+opt.Token)
	req.Header.Set("Accept", "application/json")
	if opt.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.httpClient.Do(req)
}

func (c *Client) DoJSON(ctx context.Context, baseURL string, opt RequestOptions, dest any) (int, []byte, error) {
	resp, err := c.Do(ctx, baseURL, opt)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if dest != nil && len(body) > 0 && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if err := json.Unmarshal(body, dest); err != nil {
			return resp.StatusCode, body, fmt.Errorf("decode json: %w", err)
		}
	}
	return resp.StatusCode, body, nil
}

func (c *Client) Health(ctx context.Context, baseURL, token string) (int, []byte, error) {
	return c.DoJSON(ctx, baseURL, RequestOptions{
		Method: http.MethodGet,
		Path:   "/_hapctl/v1/health",
		Token:  token,
	}, nil)
}

func (c *Client) Stats(ctx context.Context, baseURL, token string) (int, []byte, error) {
	return c.DoJSON(ctx, baseURL, RequestOptions{
		Method: http.MethodGet,
		Path:   "/_hapctl/v1/stats",
		Token:  token,
	}, nil)
}

func (c *Client) System(ctx context.Context, baseURL, token string) (int, []byte, error) {
	return c.DoJSON(ctx, baseURL, RequestOptions{
		Method: http.MethodGet,
		Path:   "/_hapctl/v1/system",
		Token:  token,
	}, nil)
}

func (c *Client) Backends(ctx context.Context, baseURL, token string) (int, []byte, error) {
	return c.DoJSON(ctx, baseURL, RequestOptions{
		Method: http.MethodGet,
		Path:   "/_hapctl/v1/backends",
		Token:  token,
	}, nil)
}

func (c *Client) AddBackend(ctx context.Context, baseURL, token string, body io.Reader) (int, []byte, error) {
	return c.DoJSON(ctx, baseURL, RequestOptions{
		Method: http.MethodPost,
		Path:   "/_hapctl/v1/backends",
		Token:  token,
		Body:   body,
	}, nil)
}

func (c *Client) DeleteBackend(ctx context.Context, baseURL, token, backend, name string) (int, []byte, error) {
	path := fmt.Sprintf("/_hapctl/v1/backends/%s/%s", url.PathEscape(backend), url.PathEscape(name))
	return c.DoJSON(ctx, baseURL, RequestOptions{
		Method: http.MethodDelete,
		Path:   path,
		Token:  token,
	}, nil)
}

func (c *Client) Reload(ctx context.Context, baseURL, token string) (int, []byte, error) {
	return c.DoJSON(ctx, baseURL, RequestOptions{
		Method: http.MethodPost,
		Path:   "/_hapctl/v1/haproxy/reload",
		Token:  token,
	}, nil)
}

func (c *Client) Restart(ctx context.Context, baseURL, token string) (int, []byte, error) {
	return c.DoJSON(ctx, baseURL, RequestOptions{
		Method: http.MethodPost,
		Path:   "/_hapctl/v1/haproxy/restart",
		Token:  token,
	}, nil)
}

// IsTransientDisconnect reports errors typical of a killed mid-flight connection
// (e.g. legacy setups that still proxied management through HAProxy).
func IsTransientDisconnect(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	msg := strings.ToLower(err.Error())
	needles := []string{
		"eof",
		"connection reset",
		"broken pipe",
		"connection refused",
		"use of closed network connection",
		"server closed idle connection",
		"connection reset by peer",
	}
	for _, n := range needles {
		if strings.Contains(msg, n) {
			return true
		}
	}
	return false
}

// WaitHealthy polls GET /health until success or timeout (~25s).
func (c *Client) WaitHealthy(ctx context.Context, baseURL, token string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 25 * time.Second
	}
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		status, _, err := c.Health(ctx, baseURL, token)
		if err == nil && status >= 200 && status < 300 {
			return nil
		}
		if err != nil {
			last = err
		} else {
			last = fmt.Errorf("health status %d", status)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1500 * time.Millisecond):
		}
	}
	if last != nil {
		return fmt.Errorf("агент не ответил за %s: %w", timeout, last)
	}
	return fmt.Errorf("агент не ответил за %s", timeout)
}

// RestartResilient calls restart; on transient disconnect, waits for health and
// returns a success payload with note=true if recovery succeeded.
func (c *Client) RestartResilient(ctx context.Context, baseURL, token string) (status int, body []byte, recovered bool, err error) {
	status, body, err = c.Restart(ctx, baseURL, token)
	if err == nil && status >= 200 && status < 300 {
		return status, body, false, nil
	}
	if err != nil && IsTransientDisconnect(err) {
		if werr := c.WaitHealthy(ctx, baseURL, token, 25*time.Second); werr == nil {
			payload, _ := json.Marshal(map[string]any{
				"ok":     true,
				"action": "restart",
				"note":   "Соединение оборвалось во время рестарта; агент снова доступен",
			})
			return http.StatusOK, payload, true, nil
		} else {
			return 0, nil, false, fmt.Errorf("рестарт не подтверждён: %w", werr)
		}
	}
	return status, body, false, err
}

// ReloadResilient is like RestartResilient for soft reload.
func (c *Client) ReloadResilient(ctx context.Context, baseURL, token string) (status int, body []byte, recovered bool, err error) {
	status, body, err = c.Reload(ctx, baseURL, token)
	if err == nil && status >= 200 && status < 300 {
		return status, body, false, nil
	}
	if err != nil && IsTransientDisconnect(err) {
		if werr := c.WaitHealthy(ctx, baseURL, token, 25*time.Second); werr == nil {
			payload, _ := json.Marshal(map[string]any{
				"ok":     true,
				"action": "reload",
				"note":   "Соединение оборвалось во время перезагрузки; агент снова доступен",
			})
			return http.StatusOK, payload, true, nil
		} else {
			return 0, nil, false, fmt.Errorf("перезагрузка не подтверждена: %w", werr)
		}
	}
	return status, body, false, err
}
