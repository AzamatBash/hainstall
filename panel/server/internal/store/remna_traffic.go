package store

import (
	"database/sql"
	"errors"
	"time"
)

// RemnaTrafficSample is one total trafficUsedBytes sum for a Remnawave panel.
type RemnaTrafficSample struct {
	TS    int64   `json:"t"`
	Bytes float64 `json:"bytes"`
}

// Keep ~1 month of 5-minute samples (same window as online).
const remnaTrafficRetention = 31 * 24 * time.Hour

// AppendRemnaTrafficSample records a panel traffic total and prunes old rows.
func (s *Store) AppendRemnaTrafficSample(panelID string, at time.Time, bytes float64) error {
	if panelID == "" {
		return nil
	}
	if bytes < 0 || bytes != bytes { // NaN
		bytes = 0
	}
	ts := at.UTC().UnixMilli()
	cutoff := at.UTC().Add(-remnaTrafficRetention).UnixMilli()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(
		`INSERT OR REPLACE INTO remna_traffic_samples (panel_id, ts, bytes) VALUES (?, ?, ?)`,
		panelID, ts, bytes,
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
SELECT ts, bytes FROM remna_traffic_samples
WHERE panel_id = ? AND ts >= ?
ORDER BY ts ASC`, panelID, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RemnaTrafficSample, 0, 128)
	for rows.Next() {
		var p RemnaTrafficSample
		if err := rows.Scan(&p.TS, &p.Bytes); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// LatestRemnaTrafficSample returns the newest sample for a panel, if any.
func (s *Store) LatestRemnaTrafficSample(panelID string) (*RemnaTrafficSample, error) {
	row := s.db.QueryRow(`
SELECT ts, bytes FROM remna_traffic_samples
WHERE panel_id = ?
ORDER BY ts DESC LIMIT 1`, panelID)
	var p RemnaTrafficSample
	if err := row.Scan(&p.TS, &p.Bytes); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}
