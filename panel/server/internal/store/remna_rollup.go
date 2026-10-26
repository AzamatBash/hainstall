package store

import (
	"database/sql"
	"time"
)

const (
	mskOffsetMs  = int64(3 * time.Hour / time.Millisecond)
	mskDayMs     = int64(24 * time.Hour / time.Millisecond)
	onlineHourMs = int64(time.Hour / time.Millisecond)
)

func mskDayNumber(ts int64) int64 {
	return (ts + mskOffsetMs) / mskDayMs
}

func adjustRemnaTrafficDay(tx *sql.Tx, panelID, remnaUUID string, day int64, prevRate float64, replaced bool, rate float64) error {
	if replaced {
		if _, err := tx.Exec(`
UPDATE remna_node_traffic_day
SET sum_bps = sum_bps - ?, n = n - 1
WHERE panel_id = ? AND remna_uuid = ? AND day = ?`, prevRate, panelID, remnaUUID, day); err != nil {
			return err
		}
		if _, err := tx.Exec(`
DELETE FROM remna_node_traffic_day
WHERE panel_id = ? AND remna_uuid = ? AND day = ? AND n <= 0`, panelID, remnaUUID, day); err != nil {
			return err
		}
	}
	_, err := tx.Exec(`
INSERT INTO remna_node_traffic_day (panel_id, remna_uuid, day, sum_bps, n)
VALUES (?, ?, ?, ?, 1)
ON CONFLICT(panel_id, remna_uuid, day) DO UPDATE SET
  sum_bps = sum_bps + excluded.sum_bps,
  n = n + 1`, panelID, remnaUUID, day, rate)
	return err
}

// RemnaNodeTrafficDrop is the last 30 minutes versus the 5.5 hours before that.
type RemnaNodeTrafficDrop struct {
	PanelID   string
	RemnaUUID string
	RecentSum float64
	RecentN   int
	BaseSum   float64
	BaseN     int
}

// RemnaTrafficDayAgg is one Moscow day's mean traffic for a node.
type RemnaTrafficDayAgg struct {
	PanelID   string
	RemnaUUID string
	Day       int64
	Sum       float64
	N         int
}

// ListRemnaNodeTrafficDropWindows reads only the last 6 hours, not the whole history.
func (s *Store) ListRemnaNodeTrafficDropWindows(now time.Time) ([]RemnaNodeTrafficDrop, error) {
	nowMs := now.UTC().UnixMilli()
	recentFrom := now.Add(-30 * time.Minute).UnixMilli()
	baseFrom := now.Add(-6 * time.Hour).UnixMilli()
	rows, err := s.db.Query(`
SELECT panel_id, remna_uuid,
  SUM(CASE WHEN ts >= ? AND ts <= ? THEN down_bps + up_bps ELSE 0 END),
  SUM(CASE WHEN ts >= ? AND ts <= ? THEN 1 ELSE 0 END),
  SUM(CASE WHEN ts >= ? AND ts < ? THEN down_bps + up_bps ELSE 0 END),
  SUM(CASE WHEN ts >= ? AND ts < ? THEN 1 ELSE 0 END)
FROM remna_node_traffic_samples INDEXED BY idx_remna_node_traffic_ts
WHERE ts >= ? AND ts <= ?
GROUP BY panel_id, remna_uuid`,
		recentFrom, nowMs,
		recentFrom, nowMs,
		baseFrom, recentFrom,
		baseFrom, recentFrom,
		baseFrom, nowMs,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RemnaNodeTrafficDrop, 0, 64)
	for rows.Next() {
		var d RemnaNodeTrafficDrop
		if err := rows.Scan(&d.PanelID, &d.RemnaUUID, &d.RecentSum, &d.RecentN, &d.BaseSum, &d.BaseN); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ListRemnaTrafficDayAggs returns per-node daily sums kept for the alert window.
func (s *Store) ListRemnaTrafficDayAggs() ([]RemnaTrafficDayAgg, error) {
	rows, err := s.db.Query(`
SELECT panel_id, remna_uuid, day, sum_bps, n
FROM remna_node_traffic_day
ORDER BY panel_id, remna_uuid, day`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RemnaTrafficDayAgg, 0, 256)
	for rows.Next() {
		var d RemnaTrafficDayAgg
		if err := rows.Scan(&d.PanelID, &d.RemnaUUID, &d.Day, &d.Sum, &d.N); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) backfillRemnaRollups() error {
	if err := s.backfillTrafficRollup(); err != nil {
		return err
	}
	return s.backfillOnlineHour()
}

func (s *Store) backfillTrafficRollup() error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM remna_node_traffic_latest`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`
INSERT INTO remna_node_traffic_day (panel_id, remna_uuid, day, sum_bps, n)
SELECT panel_id, remna_uuid, (ts + ?) / ?, SUM(down_bps + up_bps), COUNT(*)
FROM remna_node_traffic_samples
GROUP BY panel_id, remna_uuid, (ts + ?) / ?`, mskOffsetMs, mskDayMs, mskOffsetMs, mskDayMs); err != nil {
		return err
	}
	if _, err := tx.Exec(`
INSERT INTO remna_node_traffic_latest (panel_id, remna_uuid, ts, down_bps, up_bps)
SELECT t.panel_id, t.remna_uuid, t.ts, t.down_bps, t.up_bps
FROM remna_node_traffic_samples t
INNER JOIN (
  SELECT panel_id, remna_uuid, MAX(ts) AS ts
  FROM remna_node_traffic_samples
  GROUP BY panel_id, remna_uuid
) m ON t.panel_id = m.panel_id AND t.remna_uuid = m.remna_uuid AND t.ts = m.ts`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) backfillOnlineHour() error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM remna_node_online_hour`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err := s.db.Exec(`
INSERT INTO remna_node_online_hour (panel_id, remna_uuid, bucket, ts, online)
SELECT s.panel_id, s.remna_uuid, (s.ts / ?) * ?, s.ts, s.online
FROM remna_node_online_samples s
INNER JOIN (
  SELECT panel_id, remna_uuid, (ts / ?) * ? AS bucket, MAX(ts) AS ts
  FROM remna_node_online_samples
  GROUP BY panel_id, remna_uuid, (ts / ?) * ?
) m ON s.panel_id = m.panel_id AND s.remna_uuid = m.remna_uuid AND s.ts = m.ts`,
		onlineHourMs, onlineHourMs,
		onlineHourMs, onlineHourMs,
		onlineHourMs, onlineHourMs,
	)
	return err
}
