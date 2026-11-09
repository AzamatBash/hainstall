package remnapulse

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/azabash/hapanel/panel/internal/remna"
	"github.com/azabash/hapanel/panel/internal/secretbox"
	"github.com/azabash/hapanel/panel/internal/store"
)

const (
	defaultInterval = 6 * time.Hour
	syncTimeout     = 4 * time.Minute
)

// Syncer pulls Remnawave users into SQLite on a schedule so Pulse never streams on page load.
type Syncer struct {
	store      *store.Store
	remna      *remna.Client
	secretsKey string
	logger     *slog.Logger
	interval   time.Duration

	mu       sync.Mutex
	inflight map[string]bool
}

func New(st *store.Store, secretsKey string, logger *slog.Logger) *Syncer {
	return &Syncer{
		store:      st,
		remna:      remna.New(),
		secretsKey: secretsKey,
		logger:     logger,
		interval:   defaultInterval,
		inflight:   make(map[string]bool),
	}
}

// Start runs an immediate sync then repeats on interval.
func (s *Syncer) Start(ctx context.Context) {
	go func() {
		s.SyncAll(ctx)
		t := time.NewTicker(s.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.SyncAll(ctx)
			}
		}
	}()
}

// SyncAll syncs every Remnawave panel sequentially.
func (s *Syncer) SyncAll(ctx context.Context) {
	panels, err := s.store.ListRemnaPanels()
	if err != nil {
		s.logger.Warn("pulse sync list panels", "err", err)
		return
	}
	for _, p := range panels {
		if ctx.Err() != nil {
			return
		}
		s.SyncPanel(ctx, p.ID)
	}
}

// SyncPanelForce starts a sync if not already running (used by refresh=1).
func (s *Syncer) SyncPanelForce(panelID string) {
	go s.SyncPanel(context.Background(), panelID)
}

// InFlight reports whether a panel sync is currently running.
func (s *Syncer) InFlight(panelID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inflight[panelID]
}

// SyncPanel streams users for one panel into SQLite.
func (s *Syncer) SyncPanel(parent context.Context, panelID string) {
	s.mu.Lock()
	if s.inflight[panelID] {
		s.mu.Unlock()
		return
	}
	s.inflight[panelID] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.inflight, panelID)
		s.mu.Unlock()
	}()

	now := time.Now().UTC()
	full, err := s.store.GetRemnaPanel(panelID)
	if err != nil || full == nil {
		_ = s.store.UpsertPulseSync(store.PulseSyncMeta{PanelID: panelID, SyncedAt: now, Err: "панель не найдена"})
		return
	}
	if len(full.ApiKeyEnc) == 0 {
		_ = s.store.UpsertPulseSync(store.PulseSyncMeta{PanelID: panelID, SyncedAt: now, Err: "api key не задан"})
		return
	}
	plain, err := secretbox.Open(s.secretsKey, full.ApiKeyEnc)
	if err != nil {
		_ = s.store.UpsertPulseSync(store.PulseSyncMeta{PanelID: panelID, SyncedAt: now, Err: "не удалось расшифровать api key"})
		s.logger.Warn("pulse sync decrypt", "panel", panelID, "err", err)
		return
	}

	ctx, cancel := context.WithTimeout(parent, syncTimeout)
	defer cancel()

	st, err := s.remna.FetchUserStatusCounts(ctx, full.BaseURL, string(plain), "Europe/Moscow")
	if err != nil {
		s.logger.Warn("pulse sync status", "panel", panelID, "name", full.Name, "err", err)
		// continue — spans still useful; status can stay from previous sync
	}

	spans, err := s.remna.FetchUserSpans(ctx, full.BaseURL, string(plain))
	if err != nil {
		s.logger.Warn("pulse sync users", "panel", panelID, "name", full.Name, "err", err)
		_ = s.store.UpsertPulseSync(store.PulseSyncMeta{
			PanelID: panelID, SyncedAt: now, Err: err.Error(),
			ActiveNow: st.Active, DisabledNow: st.Disabled, LimitedNow: st.Limited, ExpiredNow: st.Expired, TotalUsers: st.Total,
		})
		return
	}

	rows := make([]store.PulseSpanRow, 0, len(spans))
	for _, sp := range spans {
		rows = append(rows, store.PulseSpanRow{
			Created:  sp.Created,
			Expire:   sp.Expire,
			Revoked:  sp.Revoked,
			Disabled: sp.Disabled,
		})
	}
	if err := s.store.ReplacePulseSpans(panelID, rows); err != nil {
		s.logger.Warn("pulse sync store spans", "panel", panelID, "err", err)
		_ = s.store.UpsertPulseSync(store.PulseSyncMeta{PanelID: panelID, SyncedAt: now, Err: err.Error()})
		return
	}

	meta := store.PulseSyncMeta{
		PanelID:      panelID,
		SyncedAt:     time.Now().UTC(),
		UsersSampled: len(rows),
		ActiveNow:    st.Active,
		DisabledNow:  st.Disabled,
		LimitedNow:   st.Limited,
		ExpiredNow:   st.Expired,
		TotalUsers:   st.Total,
	}
	if err := s.store.UpsertPulseSync(meta); err != nil {
		s.logger.Warn("pulse sync meta", "panel", panelID, "err", err)
		return
	}
	s.logger.Info("pulse sync ok", "panel", panelID, "name", full.Name, "users", len(rows), "active", st.Active)
}
