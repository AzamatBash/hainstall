package store

import (
	"database/sql"
	"errors"
	"math"
	"time"
)

// RemnaTrafficSample is one summed TX/RX rate snapshot for a Remnawave panel.
// DownBps = host TX (отдача), UpBps = host RX (загрузка) — same convention as TrafficMirrorChart.
type RemnaTrafficSample struct {
	TS      int64   `json:"t"`
	DownBps float64 `json:"down_bps"`
	UpBps   float64 `json:"up_bps"`
}

// Keep ~1 month of 5-minute samples (same window as online).
const remnaTrafficRetention = 31 * 24 * time.Hour

func sanitizeBps(v float64) float64 {
	if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

// AppendRemnaTrafficSample records panel TX/RX totals and prunes old rows.
func (s *Store) AppendRemnaTrafficSample(panelID string, at time.Time, downBps, upBps float64) error {
	if panelID == "" {
		return nil
	}
	downBps = sanitizeBps(downBps)
	upBps = sanitizeBps(upBps)
	ts := at.UTC().UnixMilli()
	cutoff := at.UTC().Add(-remnaTrafficRetention).UnixMilli()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(
		`INSERT OR REPLACE INTO remna_traffic_samples (panel_id, ts, down_bps, up_bps) VALUES (?, ?, ?, ?)`,
		panelID, ts, downBps, upBps,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`DELETE FROM remna_traffic_samples WHERE panel_id = ? AND ts < ?`,
		panelID, cutoff,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// ListRemnaTrafficSamples returns samples for a panel since the given time.
func (s *Store) ListRemnaTrafficSamples(panelID string, since time.Time) ([]RemnaTrafficSample, error) {
	cutoff := since.UTC().UnixMilli()
	rows, err := s.db.Query(`
SELECT ts, down_bps, up_bps FROM remna_traffic_samples
WHERE panel_id = ? AND ts >= ?
ORDER BY ts ASC`, panelID, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RemnaTrafficSample, 0, 128)
	for rows.Next() {
		var p RemnaTrafficSample
		if err := rows.Scan(&p.TS, &p.DownBps, &p.UpBps); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// LatestRemnaTrafficSample returns the newest sample for a panel, if any.
func (s *Store) LatestRemnaTrafficSample(panelID string) (*RemnaTrafficSample, error) {
	row := s.db.QueryRow(`
SELECT ts, down_bps, up_bps FROM remna_traffic_samples
WHERE panel_id = ?
ORDER BY ts DESC LIMIT 1`, panelID)
	var p RemnaTrafficSample
	if err := row.Scan(&p.TS, &p.DownBps, &p.UpBps); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}
