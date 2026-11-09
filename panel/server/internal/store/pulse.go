package store

import (
	"database/sql"
	"fmt"
	"time"
)

// PulseSpanRow is one Remnawave user subscription window cached locally.
type PulseSpanRow struct {
	Created  time.Time
	Expire   time.Time
	Revoked  *time.Time
	Disabled bool
}

// PulseSyncMeta is the last successful (or failed) Pulse user sync for a panel.
type PulseSyncMeta struct {
	PanelID      string
	SyncedAt     time.Time
	UsersSampled int
	ActiveNow    int
	DisabledNow  int
	LimitedNow   int
	ExpiredNow   int
	TotalUsers   int
	Err          string
}

func (s *Store) migratePulseTables() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS remna_pulse_spans (
  panel_id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  expire_at TEXT NOT NULL,
  revoked_at TEXT,
  disabled INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_remna_pulse_spans_panel ON remna_pulse_spans(panel_id);
CREATE TABLE IF NOT EXISTS remna_pulse_sync (
  panel_id TEXT PRIMARY KEY,
  synced_at TEXT NOT NULL,
  users_sampled INTEGER NOT NULL DEFAULT 0,
  active_now INTEGER NOT NULL DEFAULT 0,
  disabled_now INTEGER NOT NULL DEFAULT 0,
  limited_now INTEGER NOT NULL DEFAULT 0,
  expired_now INTEGER NOT NULL DEFAULT 0,
  total_users INTEGER NOT NULL DEFAULT 0,
  err TEXT NOT NULL DEFAULT ''
);
`)
	return err
}

// ReplacePulseSpans atomically replaces cached spans for a panel.
func (s *Store) ReplacePulseSpans(panelID string, spans []PulseSpanRow) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM remna_pulse_spans WHERE panel_id = ?`, panelID); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`
INSERT INTO remna_pulse_spans(panel_id, created_at, expire_at, revoked_at, disabled)
VALUES(?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, sp := range spans {
		var rev any
		if sp.Revoked != nil {
			rev = sp.Revoked.UTC().Format(time.RFC3339Nano)
		}
		dis := 0
		if sp.Disabled {
			dis = 1
		}
		if _, err := stmt.Exec(
			panelID,
			sp.Created.UTC().Format(time.RFC3339Nano),
			sp.Expire.UTC().Format(time.RFC3339Nano),
			rev,
			dis,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListPulseSpans returns cached spans for a panel.
func (s *Store) ListPulseSpans(panelID string) ([]PulseSpanRow, error) {
	rows, err := s.db.Query(`
SELECT created_at, expire_at, revoked_at, disabled
FROM remna_pulse_spans WHERE panel_id = ?`, panelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PulseSpanRow
	for rows.Next() {
		var createdS, expireS string
		var revokedS sql.NullString
		var disabled int
		if err := rows.Scan(&createdS, &expireS, &revokedS, &disabled); err != nil {
			return nil, err
		}
		created, err1 := parseStoreTime(createdS)
		expire, err2 := parseStoreTime(expireS)
		if err1 != nil || err2 != nil {
			continue
		}
		row := PulseSpanRow{Created: created, Expire: expire, Disabled: disabled != 0}
		if revokedS.Valid && revokedS.String != "" {
			if rev, err := parseStoreTime(revokedS.String); err == nil {
				row.Revoked = &rev
			}
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// UpsertPulseSync stores sync metadata.
func (s *Store) UpsertPulseSync(m PulseSyncMeta) error {
	_, err := s.db.Exec(`
INSERT INTO remna_pulse_sync(
  panel_id, synced_at, users_sampled, active_now, disabled_now, limited_now, expired_now, total_users, err
) VALUES(?,?,?,?,?,?,?,?,?)
ON CONFLICT(panel_id) DO UPDATE SET
  synced_at=excluded.synced_at,
  users_sampled=excluded.users_sampled,
  active_now=excluded.active_now,
  disabled_now=excluded.disabled_now,
  limited_now=excluded.limited_now,
  expired_now=excluded.expired_now,
  total_users=excluded.total_users,
  err=excluded.err
`, m.PanelID, m.SyncedAt.UTC().Format(time.RFC3339Nano), m.UsersSampled,
		m.ActiveNow, m.DisabledNow, m.LimitedNow, m.ExpiredNow, m.TotalUsers, m.Err)
	return err
}

// GetPulseSync returns last sync meta or nil.
func (s *Store) GetPulseSync(panelID string) (*PulseSyncMeta, error) {
	var m PulseSyncMeta
	var synced string
	err := s.db.QueryRow(`
SELECT panel_id, synced_at, users_sampled, active_now, disabled_now, limited_now, expired_now, total_users, err
FROM remna_pulse_sync WHERE panel_id = ?`, panelID).Scan(
		&m.PanelID, &synced, &m.UsersSampled, &m.ActiveNow, &m.DisabledNow, &m.LimitedNow, &m.ExpiredNow, &m.TotalUsers, &m.Err,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t, err := parseStoreTime(synced)
	if err != nil {
		return nil, fmt.Errorf("pulse sync time: %w", err)
	}
	m.SyncedAt = t
	return &m, nil
}
