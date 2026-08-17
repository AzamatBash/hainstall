package snapshot

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/azabash/hapanel/panel/internal/agent"
	"github.com/azabash/hapanel/panel/internal/store"
)

const (
	defaultInterval = 5 * time.Second
	perNodeTimeout  = 8 * time.Second
	maxParallel     = 8
)

// Start runs a background loop that refreshes node snapshots for the list UI.
func Start(ctx context.Context, st *store.Store, ag *agent.Client, logger *slog.Logger) {
	go func() {
		// First tick soon so the UI fills quickly after restart.
		runOnce(ctx, st, ag, logger)
		t := time.NewTicker(defaultInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				runOnce(ctx, st, ag, logger)
			}
		}
	}()
}

func runOnce(parent context.Context, st *store.Store, ag *agent.Client, logger *slog.Logger) {
	nodes, err := st.ListNodes()
	if err != nil {
		logger.Warn("snapshot list nodes", "err", err)
		return
	}
	if len(nodes) == 0 {
		return
	}

	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	for _, n := range nodes {
		n := n
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			ctx, cancel := context.WithTimeout(parent, perNodeTimeout)
			defer cancel()
			if err := pollNode(ctx, st, ag, n); err != nil {
				logger.Debug("snapshot poll", "node", n.ID, "name", n.Name, "err", err)
			}
		}()
	}
	wg.Wait()
}

func pollNode(ctx context.Context, st *store.Store, ag *agent.Client, n store.Node) error {
	prev := n.Snapshot
	sysStatus, sysBody, err := ag.System(ctx, n.URL, n.Token)
	if err != nil || sysStatus < 200 || sysStatus >= 300 {
		now := time.Now().UTC()
		_ = st.UpdateNodeStatus(n.ID, store.StatusOffline, &now)
		return err
	}

	var sys struct {
		CPUPercent float64   `json:"cpu_percent"`
		LoadAvg    []float64 `json:"load_avg"`
		NetRxBytes int64     `json:"net_rx_bytes"`
		NetTxBytes int64     `json:"net_tx_bytes"`
	}
	_ = json.Unmarshal(sysBody, &sys)

	snap := store.NodeSnapshot{
		CPU:      floatPtr(sys.CPUPercent),
		LoadAvg:  sys.LoadAvg,
		NetRx:    sys.NetRxBytes,
		NetTx:    sys.NetTxBytes,
		Backends: nil,
	}
	if prev != nil && prev.UpdatedAt.Unix() > 0 && (sys.NetRxBytes > 0 || sys.NetTxBytes > 0) {
		dt := time.Since(prev.UpdatedAt).Seconds()
		if dt >= 0.5 {
			down := float64(sys.NetTxBytes-prev.NetTx) / dt
			up := float64(sys.NetRxBytes-prev.NetRx) / dt
			if down < 0 {
				down = 0
			}
			if up < 0 {
				up = 0
			}
			snap.DownBps = &down
			snap.UpBps = &up
		} else if prev.DownBps != nil || prev.UpBps != nil {
			snap.DownBps = prev.DownBps
			snap.UpBps = prev.UpBps
		}
	} else if prev != nil {
		snap.DownBps = prev.DownBps
		snap.UpBps = prev.UpBps
	}

	statsStatus, statsBody, statsErr := ag.Stats(ctx, n.URL, n.Token)
	if statsErr == nil && statsStatus >= 200 && statsStatus < 300 {
		if sess := activeSessions(statsBody); sess != nil {
			snap.Sessions = sess
		}
	}

	beStatus, beBody, beErr := ag.Backends(ctx, n.URL, n.Token)
	if beErr == nil && beStatus >= 200 && beStatus < 300 {
		snap.Backends = flattenBackends(beBody)
	} else if prev != nil {
		snap.Backends = prev.Backends
	}

	if n.TrafficLog && prev != nil && (prev.NetRx > 0 || prev.NetTx > 0) {
		rxDelta := sys.NetRxBytes - prev.NetRx
		txDelta := sys.NetTxBytes - prev.NetTx
		if rxDelta >= 0 && txDelta >= 0 && (rxDelta > 0 || txDelta > 0) {
			_ = st.AddTrafficHourlyDelta(n.ID, time.Now().UTC(), rxDelta, txDelta)
		}
	}

	return st.SaveSnapshot(n.ID, store.StatusOnline, snap)
}

func floatPtr(v float64) *float64 { return &v }

func activeSessions(body []byte) *int {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	raw, ok := payload["backends"]
	if !ok {
		return nil
	}
	groups, ok := raw.([]any)
	if !ok {
		return nil
	}
	sessions := 0
	for _, g := range groups {
		m, ok := g.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		if isInternalBackend(name) {
			continue
		}
		switch v := m["sessions"].(type) {
		case float64:
			sessions += int(v)
		case int:
			sessions += v
		}
	}
	return &sessions
}

func flattenBackends(body []byte) []store.SnapshotBackend {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	raw, ok := payload["backends"]
	if !ok {
		return nil
	}
	groups, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]store.SnapshotBackend, 0)
	for _, g := range groups {
		m, ok := g.(map[string]any)
		if !ok {
			continue
		}
		backendName, _ := m["name"].(string)
		if backendName == "" {
			backendName, _ = m["backend"].(string)
		}
		if isInternalBackend(backendName) {
			continue
		}
		servers, _ := m["servers"].([]any)
		if len(servers) == 0 {
			if addr, _ := m["address"].(string); addr != "" {
				out = append(out, store.SnapshotBackend{
					Backend: backendName,
					Name:    strOr(m["name"], backendName),
					Address: addr,
					Port:    intOr(m["port"], 0),
				})
			}
			continue
		}
		for _, s := range servers {
			sm, ok := s.(map[string]any)
			if !ok {
				continue
			}
			be := strOr(sm["backend"], backendName)
			if isInternalBackend(be) {
				continue
			}
			out = append(out, store.SnapshotBackend{
				Backend: be,
				Name:    strOr(sm["name"], ""),
				Address: strOr(sm["address"], ""),
				Port:    intOr(sm["port"], 0),
			})
		}
	}
	return out
}

func isInternalBackend(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	switch n {
	case "hap_agent", "acme", "stats", "prometheus":
		return true
	}
	if strings.HasPrefix(n, "hap_") {
		return true
	}
	if strings.HasSuffix(n, "_mgmt") || strings.HasSuffix(n, "_management") {
		return true
	}
	return false
}

func strOr(v any, fallback string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return fallback
}

func intOr(v any, fallback int) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	default:
		return fallback
	}
}
