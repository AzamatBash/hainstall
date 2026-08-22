package remnastats

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/azabash/hapanel/panel/internal/remna"
	"github.com/azabash/hapanel/panel/internal/secretbox"
	"github.com/azabash/hapanel/panel/internal/store"
)

const (
	defaultInterval = 5 * time.Minute
	perPanelTimeout = 20 * time.Second
)

// Last is the most recent poll result for a Remnawave panel.
type Last struct {
	Online  int
	DownBps float64 // TX (отдача), TrafficMirrorChart convention
	UpBps   float64 // RX (загрузка)
	At      time.Time
	Err     string
}

// Poller periodically sums Remnawave usersOnline per panel into SQLite
// and syncs per-node catalog + samples for analytics.
type Poller struct {
	store      *store.Store
	remna      *remna.Client
	secretsKey string
	logger     *slog.Logger
	interval   time.Duration

	mu   sync.RWMutex
	last map[string]Last
}

// New creates a poller (call Start to run).
func New(st *store.Store, secretsKey string, logger *slog.Logger) *Poller {
	return &Poller{
		store:      st,
		remna:      remna.New(),
		secretsKey: secretsKey,
		logger:     logger,
		interval:   defaultInterval,
		last:       make(map[string]Last),
	}
}

// Start runs the background loop until ctx is cancelled.
func (p *Poller) Start(ctx context.Context) {
	go func() {
		p.runOnce(ctx)
		t := time.NewTicker(p.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				p.runOnce(ctx)
			}
		}
	}()
}

// SyncNow runs one poll cycle (all panels). Used by analytics sync API.
func (p *Poller) SyncNow(ctx context.Context) {
	p.runOnce(ctx)
}

// LastFor returns the last in-memory poll for a panel.
func (p *Poller) LastFor(panelID string) (Last, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	v, ok := p.last[panelID]
	return v, ok
}

func (p *Poller) setLast(panelID string, v Last) {
	p.mu.Lock()
	p.last[panelID] = v
	p.mu.Unlock()
}

func (p *Poller) runOnce(parent context.Context) {
	panels, err := p.store.ListRemnaPanels()
	if err != nil {
		p.logger.Warn("remna stats list panels", "err", err)
		return
	}
	for _, panel := range panels {
		if parent.Err() != nil {
			return
		}
		p.pollPanel(parent, panel)
	}
}

func (p *Poller) pollPanel(parent context.Context, panel store.RemnaPanel) {
	now := time.Now().UTC()
	full, err := p.store.GetRemnaPanel(panel.ID)
	if err != nil || full == nil {
		p.setLast(panel.ID, Last{At: now, Err: "панель не найдена"})
		return
	}
	if len(full.ApiKeyEnc) == 0 {
		p.setLast(panel.ID, Last{At: now, Err: "api key не задан"})
		return
	}
	plain, err := secretbox.Open(p.secretsKey, full.ApiKeyEnc)
	if err != nil {
		p.setLast(panel.ID, Last{At: now, Err: "не удалось расшифровать api key"})
		p.logger.Warn("remna stats decrypt", "panel", panel.ID, "err", err)
		return
	}

	ctx, cancel := context.WithTimeout(parent, perPanelTimeout)
	defer cancel()
	nodes, err := p.remna.ListNodes(ctx, full.BaseURL, string(plain))
	if err != nil {
		p.setLast(panel.ID, Last{At: now, Err: err.Error()})
		p.logger.Warn("remna stats list nodes", "panel", panel.ID, "name", panel.Name, "err", err)
		return
	}

	inboundByUUID := map[string]remna.Inbound{}
	inbounds, err := p.remna.ListAllInbounds(ctx, full.BaseURL, string(plain))
	if err != nil {
		// Non-fatal: keep catalog sync with unknown protocol.
		p.logger.Warn("remna stats list inbounds", "panel", panel.ID, "err", err)
	} else {
		inboundByUUID = remna.InboundByUUID(inbounds)
	}

	keepUUIDs := make([]string, 0, len(nodes))
	for _, n := range nodes {
		uuid := strings.TrimSpace(n.UUID)
		if uuid == "" {
			continue
		}
		keepUUIDs = append(keepUUIDs, uuid)
		online := 0
		if n.UsersOnline != nil {
			online = *n.UsersOnline
		}
		nodeOK := (n.IsConnected || n.IsNodeOnline) && !n.IsDisabled
		inboundUUIDs := n.ActiveInboundUUIDs()
		proto, tags := remna.DeriveProtocolFromInbounds(inboundUUIDs, inboundByUUID)
		if err := p.store.UpsertRemnaNodeCatalogSync(store.RemnaNodeSyncInput{
			PanelID:           panel.ID,
			RemnaUUID:         uuid,
			Name:              n.Name,
			Address:           n.Address,
			ConfigProfileUUID: n.ConfigProfileUUID(),
			InboundUUIDs:      inboundUUIDs,
			InboundTags:       tags,
			ProtocolDerived:   proto,
			UsersOnline:       online,
			NodeOK:            nodeOK,
			At:                now,
		}); err != nil {
			p.logger.Warn("remna node catalog upsert", "panel", panel.ID, "node", uuid, "err", err)
		}
		if err := p.store.AppendRemnaNodeOnlineSample(panel.ID, uuid, now, online, nodeOK); err != nil {
			p.logger.Warn("remna node sample append", "panel", panel.ID, "node", uuid, "err", err)
		}
		nodeDown, nodeUp := NodeTrafficRates(&n)
		if err := p.store.AppendRemnaNodeTrafficSample(panel.ID, uuid, now, nodeDown, nodeUp); err != nil {
			p.logger.Warn("remna node traffic append", "panel", panel.ID, "node", uuid, "err", err)
		}
	}
	// Drop catalog/stats for Remna nodes that no longer exist on the panel.
	if pruned, err := p.store.PruneRemnaNodeCatalog(panel.ID, keepUUIDs); err != nil {
		p.logger.Warn("remna node catalog prune", "panel", panel.ID, "err", err)
	} else if pruned > 0 {
		p.logger.Info("remna node catalog pruned", "panel", panel.ID, "removed", pruned)
	}

	online := SumUsersOnline(nodes)
	downBps, upBps := SumTrafficRates(nodes)
	if err := p.store.AppendRemnaOnlineSample(panel.ID, now, online); err != nil {
		p.logger.Warn("remna stats append online", "panel", panel.ID, "err", err)
		return
	}
	if err := p.store.AppendRemnaTrafficSample(panel.ID, now, downBps, upBps); err != nil {
		p.logger.Warn("remna stats append traffic", "panel", panel.ID, "err", err)
		return
	}
	p.setLast(panel.ID, Last{Online: online, DownBps: downBps, UpBps: upBps, At: now})
}

// SumUsersOnline totals Remnawave usersOnline across nodes (nil counts as 0).
func SumUsersOnline(nodes []remna.Node) int {
	total := 0
	for _, n := range nodes {
		if n.UsersOnline != nil {
			total += *n.UsersOnline
		}
	}
	return total
}

// SumTrafficRates totals host NIC speeds across nodes.
// Returns chart convention: downBps = TX (отдача), upBps = RX (загрузка).
func SumTrafficRates(nodes []remna.Node) (downBps, upBps float64) {
	for i := range nodes {
		d, u := NodeTrafficRates(&nodes[i])
		downBps += d
		upBps += u
	}
	return downBps, upBps
}

// NodeTrafficRates returns chart convention rates for one node:
// downBps = TX (отдача), upBps = RX (загрузка).
func NodeTrafficRates(n *remna.Node) (downBps, upBps float64) {
	if n == nil || n.System == nil || n.System.Stats.Interface == nil {
		return 0, 0
	}
	iface := n.System.Stats.Interface
	if iface.TxBytesPerSec > 0 {
		downBps = iface.TxBytesPerSec
	}
	if iface.RxBytesPerSec > 0 {
		upBps = iface.RxBytesPerSec
	}
	return downBps, upBps
}
