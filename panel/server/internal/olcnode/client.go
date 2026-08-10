// Package olcnode is an HTTP client for the olcnode agent (/_olcnode/v1).
package olcnode

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	apiPrefix       = "/_olcnode/v1"
	apiPrefixLegacy = "/_olcrtc/v1"
)

// Client talks to an olcnode control plane.
type Client struct {
	http *http.Client
}

// New builds a client. insecure enables TLS InsecureSkipVerify for https agent URLs.
func New(insecure bool) *Client {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: insecure}, //nolint:gosec // local/MVP agents
	}
	return &Client{
		http: &http.Client{
			Timeout:   20 * time.Second,
			Transport: tr,
		},
	}
}

// Instance matches agent instance JSON.
type Instance struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Transport string `json:"transport"`
	RoomID    string `json:"room_id"`
	KeyHex    string `json:"key_hex"`
	Comment   string `json:"comment"`
	Enabled   bool   `json:"enabled"`
	Status    string `json:"status,omitempty"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// StatusResponse is GET /status.
type StatusResponse struct {
	OK         bool   `json:"ok"`
	Deployed   bool   `json:"deployed"`
	Instances  int    `json:"instances"`
	Status     string `json:"status"`
	UptimeSec  int64  `json:"uptime_sec"`
	Version    string `json:"version"`
	NodeName   string `json:"node_name,omitempty"`
	LastError  string `json:"last_error,omitempty"`
}

// HealthResponse is GET /health (no auth).
type HealthResponse struct {
	OK       bool   `json:"ok"`
	Version  string `json:"version"`
	NodeName string `json:"node_name,omitempty"`
}

// URIResponse is GET /uri/{id}.
type URIResponse struct {
	URI       string `json:"uri"`
	Provider  string `json:"provider,omitempty"`
	Transport string `json:"transport,omitempty"`
	RoomID    string `json:"room_id,omitempty"`
	Comment   string `json:"comment,omitempty"`
}

// CreateInstanceRequest is POST /instances body.
type CreateInstanceRequest struct {
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Transport string `json:"transport"`
	RoomID    string `json:"room_id"`
	KeyHex    string `json:"key_hex"`
	Comment   string `json:"comment,omitempty"`
}

// UpdateInstanceRequest is PUT /instances/{id} body (partial).
type UpdateInstanceRequest struct {
	Name      string `json:"name,omitempty"`
	Provider  string `json:"provider,omitempty"`
	Transport string `json:"transport,omitempty"`
	RoomID    string `json:"room_id,omitempty"`
	KeyHex    string `json:"key_hex,omitempty"`
	Comment   string `json:"comment,omitempty"`
	Enabled   *bool  `json:"enabled,omitempty"`
}

// APIError is a non-2xx agent response.
type APIError struct {
	Status  int
	Message string
	Body    []byte
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("agent HTTP %d", e.Status)
}

func (c *Client) do(ctx context.Context, method, baseURL, token, path string, body any, dest any) error {
	return c.doOnce(ctx, method, baseURL, token, path, body, dest)
}

// doAPI calls path under /_olcnode/v1, falling back to legacy /_olcrtc/v1 on 404
// (old local agents still expose only the legacy prefix).
func (c *Client) doAPI(ctx context.Context, method, baseURL, token, rel string, body any, dest any) error {
	rel = strings.TrimPrefix(rel, "/")
	err := c.doOnce(ctx, method, baseURL, token, apiPrefix+"/"+rel, body, dest)
	if err == nil {
		return nil
	}
	var ae *APIError
	if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
		return c.doOnce(ctx, method, baseURL, token, apiPrefixLegacy+"/"+rel, body, dest)
	}
	return err
}

func (c *Client) doOnce(ctx context.Context, method, baseURL, token, path string, body any, dest any) error {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return fmt.Errorf("пустой olcnode URL")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u, err := url.Parse(base + path)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		var errObj struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &errObj) == nil && errObj.Error != "" {
			msg = errObj.Error
		}
		if msg == "" {
			msg = fmt.Sprintf("olcnode HTTP %d", resp.StatusCode)
		}
		return &APIError{Status: resp.StatusCode, Message: msg, Body: raw}
	}
	if dest != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, dest); err != nil {
			return fmt.Errorf("decode json: %w", err)
		}
	}
	return nil
}

// Health calls GET …/health (no auth).
func (c *Client) Health(ctx context.Context, baseURL string) (*HealthResponse, error) {
	var out HealthResponse
	if err := c.doAPI(ctx, http.MethodGet, baseURL, "", "health", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Status calls GET …/status.
func (c *Client) Status(ctx context.Context, baseURL, token string) (*StatusResponse, error) {
	var out StatusResponse
	if err := c.doAPI(ctx, http.MethodGet, baseURL, token, "status", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListInstances calls GET …/instances.
func (c *Client) ListInstances(ctx context.Context, baseURL, token string) ([]Instance, error) {
	var out struct {
		Instances []Instance `json:"instances"`
	}
	if err := c.doAPI(ctx, http.MethodGet, baseURL, token, "instances", nil, &out); err != nil {
		return nil, err
	}
	if out.Instances == nil {
		out.Instances = []Instance{}
	}
	return out.Instances, nil
}

// CreateInstance calls POST …/instances.
func (c *Client) CreateInstance(ctx context.Context, baseURL, token string, req CreateInstanceRequest) (*Instance, error) {
	var out Instance
	if err := c.doAPI(ctx, http.MethodPost, baseURL, token, "instances", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateInstance calls PUT …/instances/{id}.
func (c *Client) UpdateInstance(ctx context.Context, baseURL, token, id string, req UpdateInstanceRequest) (*Instance, error) {
	var out Instance
	path := "instances/" + url.PathEscape(id)
	if err := c.doAPI(ctx, http.MethodPut, baseURL, token, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteInstance calls DELETE …/instances/{id}.
func (c *Client) DeleteInstance(ctx context.Context, baseURL, token, id string) error {
	path := "instances/" + url.PathEscape(id)
	return c.doAPI(ctx, http.MethodDelete, baseURL, token, path, nil, nil)
}

// RestartInstance calls POST …/instances/{id}/restart.
func (c *Client) RestartInstance(ctx context.Context, baseURL, token, id string) (*Instance, error) {
	var out struct {
		OK       bool      `json:"ok"`
		Action   string    `json:"action"`
		Instance *Instance `json:"instance"`
	}
	path := "instances/" + url.PathEscape(id) + "/restart"
	if err := c.doAPI(ctx, http.MethodPost, baseURL, token, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Instance, nil
}

// Deploy calls POST …/deploy.
func (c *Client) Deploy(ctx context.Context, baseURL, token string) error {
	var out map[string]any
	return c.doAPI(ctx, http.MethodPost, baseURL, token, "deploy", nil, &out)
}

// InstanceURI calls GET …/uri/{id}.
func (c *Client) InstanceURI(ctx context.Context, baseURL, token, id string) (*URIResponse, error) {
	var out URIResponse
	path := "uri/" + url.PathEscape(id)
	if err := c.doAPI(ctx, http.MethodGet, baseURL, token, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// WaitHealthy polls Health until ok or timeout.
func (c *Client) WaitHealthy(ctx context.Context, baseURL string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		h, err := c.Health(ctx, baseURL)
		if err == nil && h != nil && h.OK {
			return nil
		}
		last = err
		if err == nil {
			last = fmt.Errorf("health ok=false")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	if last != nil {
		return fmt.Errorf("агент не поднялся за %s: %w", timeout, last)
	}
	return fmt.Errorf("агент не поднялся за %s", timeout)
}
