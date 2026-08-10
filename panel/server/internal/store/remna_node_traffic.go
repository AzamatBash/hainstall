package store

import (
	"database/sql"
	"errors"
	"time"
)

// RemnaNodeTrafficSample is one TX/RX rate snapshot for a Remnawave node.
// DownBps = host TX (отдача), UpBps = host RX (загрузка).
type RemnaNodeTrafficSample struct {
	PanelID   string  `json:"panel_id"`
	RemnaUUID string  `json:"remna_uuid"`
	TS        int64   `json:"t"`
	DownBps   float64 `json:"down_bps"`
	UpBps     float64 `json:"up_bps"`
}

// Same retention as per-node online history.
const remnaNodeTrafficRetention = 14 * 24 * time.Hour

// AppendRemnaNodeTrafficSample stores one per-node rate sample and prunes old rows.
func (s *Store) AppendRemnaNodeTrafficSample(panelID, remnaUUID string, at time.Time, downBps, upBps float64) error {
	if panelID == "" || remnaUUID == "" {
		return nil
	}
	downBps = sanitizeBps(downBps)
	upBps = sanitizeBps(upBps)
	ts := at.UTC().UnixMilli()
	cutoff := at.UTC().Add(-remnaNodeTrafficRetention).UnixMilli()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`
INSERT OR REPLACE INTO remna_node_traffic_samples (panel_id, remna_uuid, ts, down_bps, up_bps)
VALUES (?, ?, ?, ?, ?)`, panelID, remnaUUID, ts, downBps, upBps); err != nil {
		return err
	}
	if _, err := tx.Exec(`
DELETE FROM remna_node_traffic_samples
WHERE panel_id = ? AND remna_uuid = ? AND ts < ?`, panelID, remnaUUID, cutoff); err != nil {
		return err
	}
	return tx.Commit()
}

// ListRemnaNodeTrafficSamples returns samples for one node since the given time.
func (s *Store) ListRemnaNodeTrafficSamples(panelID, remnaUUID string, since time.Time) ([]RemnaTrafficSample, error) {
	cutoff := since.UTC().UnixMilli()
	rows, err := s.db.Query(`
SELECT ts, down_bps, up_bps FROM remna_node_traffic_samples
WHERE panel_id = ? AND remna_uuid = ? AND ts >= ?
ORDER BY ts ASC`, panelID, remnaUUID, cutoff)
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

// LatestRemnaNodeTrafficSample returns the newest sample for a node, if any.
func (s *Store) LatestRemnaNodeTrafficSample(panelID, remnaUUID string) (*RemnaTrafficSample, error) {
	row := s.db.QueryRow(`
SELECT ts, down_bps, up_bps FROM remna_node_traffic_samples
WHERE panel_id = ? AND remna_uuid = ?
ORDER BY ts DESC LIMIT 1`, panelID, remnaUUID)
	var p RemnaTrafficSample
	if err := row.Scan(&p.TS, &p.DownBps, &p.UpBps); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// MapLatestRemnaNodeTraffic returns the newest sample per node across all panels.
func (s *Store) MapLatestRemnaNodeTraffic() (map[string]RemnaTrafficSample, error) {
	rows, err := s.db.Query(`
SELECT t.panel_id, t.remna_uuid, t.ts, t.down_bps, t.up_bps
FROM remna_node_traffic_samples t
INNER JOIN (
  SELECT panel_id, remna_uuid, MAX(ts) AS ts
  FROM remna_node_traffic_samples
  GROUP BY panel_id, remna_uuid
) m ON t.panel_id = m.panel_id AND t.remna_uuid = m.remna_uuid AND t.ts = m.ts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]RemnaTrafficSample)
	for rows.Next() {
		var panelID, remnaUUID string
		var p RemnaTrafficSample
		if err := rows.Scan(&panelID, &remnaUUID, &p.TS, &p.DownBps, &p.UpBps); err != nil {
			return nil, err
		}
		out[panelID+"\x00"+remnaUUID] = p
	}
	return out, rows.Err()
}
