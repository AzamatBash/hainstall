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
	Traffic float64 // sum of trafficUsedBytes across nodes
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

	for _, n := range nodes {
		uuid := strings.TrimSpace(n.UUID)
		if uuid == "" {
			continue
		}
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
	}

	online := SumUsersOnline(nodes)
	traffic := SumTrafficUsedBytes(nodes)
	if err := p.store.AppendRemnaOnlineSample(panel.ID, now, online); err != nil {
		p.logger.Warn("remna stats append online", "panel", panel.ID, "err", err)
		return
	}
	if err := p.store.AppendRemnaTrafficSample(panel.ID, now, traffic); err != nil {
		p.logger.Warn("remna stats append traffic", "panel", panel.ID, "err", err)
		return
	}
	p.setLast(panel.ID, Last{Online: online, Traffic: traffic, At: now})
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

// SumTrafficUsedBytes totals Remnawave trafficUsedBytes across nodes (nil counts as 0).
func SumTrafficUsedBytes(nodes []remna.Node) float64 {
	var total float64
	for _, n := range nodes {
		if n.TrafficUsedBytes != nil {
			v := *n.TrafficUsedBytes
			if v > 0 {
				total += v
			}
		}
	}
	return total
}
