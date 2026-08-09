package remna

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to a Remnawave panel HTTP API.
type Client struct {
	http *http.Client
}

// New returns a client with a short timeout and default (secure) TLS.
func New() *Client {
	return &Client{
		http: &http.Client{Timeout: 6 * time.Second},
	}
}

// Node is a Remnawave node from GET /api/nodes.
type Node struct {
	UUID              string      `json:"uuid"`
	Name              string      `json:"name"`
	Address           string      `json:"address"`
	IsConnected       bool        `json:"isConnected"`
	IsNodeOnline      bool        `json:"isNodeOnline"`
	IsXrayRunning     bool        `json:"isXrayRunning"`
	IsDisabled        bool        `json:"isDisabled"`
	UsersOnline       *int        `json:"usersOnline"`
	TrafficUsedBytes  *float64    `json:"trafficUsedBytes"`
	TrafficLimitBytes *float64    `json:"trafficLimitBytes"`
	XrayUptime        json.Number `json:"xrayUptime"`
	System            *NodeSystem `json:"system"`

	// Config profile / inbound attachment (field names vary by Remnawave version).
	ConfigProfile            *NodeConfigProfile `json:"configProfile"`
	ActiveConfigProfileUUID  string             `json:"activeConfigProfileUuid"`
	ConfigProfileInbounds    []string           `json:"configProfileInbounds"`
}

// NodeSystem is live host metrics embedded in newer Remnawave node payloads.
type NodeSystem struct {
	Info  NodeSystemInfo  `json:"info"`
	Stats NodeSystemStats `json:"stats"`
}

type NodeSystemInfo struct {
	CPUs        int     `json:"cpus"`
	MemoryTotal float64 `json:"memoryTotal"`
}

type NodeSystemStats struct {
	MemoryUsed float64          `json:"memoryUsed"`
	LoadAvg    []float64        `json:"loadAvg"`
	Interface  *NetworkIface    `json:"interface"`
}

type NetworkIface struct {
	RxBytesPerSec float64 `json:"rxBytesPerSec"`
	TxBytesPerSec float64 `json:"txBytesPerSec"`
}

// UsageRealtime is a row from legacy realtime endpoints (often 404 on new panels).
type UsageRealtime struct {
	NodeUUID         string  `json:"nodeUuid"`
	NodeName         string  `json:"nodeName"`
	DownloadSpeedBps float64 `json:"downloadSpeedBps"`
	UploadSpeedBps   float64 `json:"uploadSpeedBps"`
}

// LiveStats combines Remnawave node + system metrics for the UI strip.
type LiveStats struct {
	Found           bool
	Online          bool
	UsersOnline     *int
	DownBps         *float64 // RX (↓) bytes/s
	UpBps           *float64 // TX (↑) bytes/s
	RAMPercent      *float64
	LoadAvg         []float64
	CPUCount        *int
	TrafficUsed     *float64
	TrafficLimit    *float64
	Error           string
}

type nodesResponse struct {
	Response []Node `json:"response"`
}

type usageResponse struct {
	Response []UsageRealtime `json:"response"`
}

func (c *Client) getJSON(ctx context.Context, baseURL, apiKey, path string, dest any) error {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return fmt.Errorf("empty base URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	req.Header.Set("Accept", "application/json")

	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		msg := strings.TrimSpace(string(body))
		if len(msg) > 200 {
			msg = msg[:200] + "…"
		}
		if msg == "" {
			msg = res.Status
		}
		return &apiError{path: path, status: res.StatusCode, msg: msg}
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

type apiError struct {
	path   string
	status int
	msg    string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("remna %s: %d %s", e.path, e.status, e.msg)
}

func isNotFound(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && ae.status == http.StatusNotFound
}

// ListNodes fetches all nodes from the Remnawave panel.
func (c *Client) ListNodes(ctx context.Context, baseURL, apiKey string) ([]Node, error) {
	var out nodesResponse
	if err := c.getJSON(ctx, baseURL, apiKey, "/api/nodes", &out); err != nil {
		return nil, err
	}
	if out.Response == nil {
		return []Node{}, nil
	}
	return out.Response, nil
}

var realtimeUsagePaths = []string{
	"/api/bandwidth-stats/nodes/realtime",
	"/api/nodes/usage/realtime",
}

// ListRealtimeUsage fetches realtime traffic speeds (legacy). Empty on 404.
func (c *Client) ListRealtimeUsage(ctx context.Context, baseURL, apiKey string) ([]UsageRealtime, error) {
	for _, path := range realtimeUsagePaths {
		var out usageResponse
		err := c.getJSON(ctx, baseURL, apiKey, path, &out)
		if err == nil {
			if out.Response == nil {
				return []UsageRealtime{}, nil
			}
			return out.Response, nil
		}
		if isNotFound(err) {
			continue
		}
		return nil, err
	}
	return []UsageRealtime{}, nil
}

// FindNodeByAddress returns the first node whose address matches (case-insensitive).
func FindNodeByAddress(nodes []Node, address string) *Node {
	want := strings.ToLower(strings.TrimSpace(address))
	if want == "" {
		return nil
	}
	for i := range nodes {
		if strings.ToLower(strings.TrimSpace(nodes[i].Address)) == want {
			n := nodes[i]
			return &n
		}
	}
	return nil
}

func LiveFromNode(n *Node) LiveStats {
	stats := LiveStats{
		Found:        true,
		Online:       (n.IsConnected || n.IsNodeOnline) && !n.IsDisabled,
		UsersOnline:  n.UsersOnline,
		TrafficUsed:  n.TrafficUsedBytes,
		TrafficLimit: n.TrafficLimitBytes,
	}
	if n.System == nil {
		return stats
	}
	info := n.System.Info
	st := n.System.Stats
	if info.CPUs > 0 {
		cpus := info.CPUs
		stats.CPUCount = &cpus
	}
	if info.MemoryTotal > 0 && st.MemoryUsed >= 0 {
		pct := (st.MemoryUsed / info.MemoryTotal) * 100
		if pct > 100 {
			pct = 100
		}
		stats.RAMPercent = &pct
	}
	if len(st.LoadAvg) > 0 {
		stats.LoadAvg = append([]float64(nil), st.LoadAvg...)
	}
	if st.Interface != nil {
		rx := st.Interface.RxBytesPerSec
		tx := st.Interface.TxBytesPerSec
		stats.DownBps = &rx
		stats.UpBps = &tx
	}
	return stats
}

// GetLiveStats loads nodes (with system metrics) and optional legacy realtime usage.
func (c *Client) GetLiveStats(ctx context.Context, baseURL, apiKey, remnaAddress string) LiveStats {
	addr := strings.TrimSpace(remnaAddress)
	if addr == "" {
		return LiveStats{Error: "empty remna address"}
	}

	nodes, err := c.ListNodes(ctx, baseURL, apiKey)
	if err != nil {
		return LiveStats{Error: err.Error()}
	}
	n := FindNodeByAddress(nodes, addr)
	if n == nil {
		return LiveStats{Found: false}
	}

	stats := LiveFromNode(n)

	// Prefer system.interface speeds; fill from legacy realtime only if missing.
	if stats.DownBps != nil || stats.UpBps != nil {
		return stats
	}
	usage, err := c.ListRealtimeUsage(ctx, baseURL, apiKey)
	if err != nil || len(usage) == 0 {
		return stats
	}
	wantUUID := strings.TrimSpace(n.UUID)
	wantName := strings.ToLower(strings.TrimSpace(n.Name))
	for _, u := range usage {
		match := wantUUID != "" && strings.TrimSpace(u.NodeUUID) == wantUUID
		if !match && wantName != "" && strings.ToLower(strings.TrimSpace(u.NodeName)) == wantName {
			match = true
		}
		if !match {
			continue
		}
		down := u.DownloadSpeedBps
		up := u.UploadSpeedBps
		stats.DownBps = &down
		stats.UpBps = &up
		break
	}
	return stats
}
