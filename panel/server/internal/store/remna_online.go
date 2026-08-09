package store

import (
	"database/sql"
	"errors"
	"time"
)

// RemnaOnlineSample is one users-online total for a Remnawave panel.
type RemnaOnlineSample struct {
	TS     int64 `json:"t"`
	Online int   `json:"online"`
}

// Keep ~1 month of 5-minute samples.
const remnaOnlineRetention = 31 * 24 * time.Hour

// AppendRemnaOnlineSample records a panel online total and prunes old rows.
func (s *Store) AppendRemnaOnlineSample(panelID string, at time.Time, online int) error {
	if panelID == "" {
		return nil
	}
	if online < 0 {
		online = 0
	}
	ts := at.UTC().UnixMilli()
	cutoff := at.UTC().Add(-remnaOnlineRetention).UnixMilli()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(
		`INSERT OR REPLACE INTO remna_online_samples (panel_id, ts, online) VALUES (?, ?, ?)`,
		panelID, ts, online,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`DELETE FROM remna_online_samples WHERE panel_id = ? AND ts < ?`,
		panelID, cutoff,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// ListRemnaOnlineSamples returns samples for a panel since the given time.
func (s *Store) ListRemnaOnlineSamples(panelID string, since time.Time) ([]RemnaOnlineSample, error) {
	cutoff := since.UTC().UnixMilli()
	rows, err := s.db.Query(`
SELECT ts, online FROM remna_online_samples
WHERE panel_id = ? AND ts >= ?
ORDER BY ts ASC`, panelID, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RemnaOnlineSample, 0, 128)
	for rows.Next() {
		var p RemnaOnlineSample
		if err := rows.Scan(&p.TS, &p.Online); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// LatestRemnaOnlineSample returns the newest sample for a panel, if any.
func (s *Store) LatestRemnaOnlineSample(panelID string) (*RemnaOnlineSample, error) {
	row := s.db.QueryRow(`
SELECT ts, online FROM remna_online_samples
WHERE panel_id = ?
ORDER BY ts DESC LIMIT 1`, panelID)
	var p RemnaOnlineSample
	if err := row.Scan(&p.TS, &p.Online); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}
