package store

import "time"

// TrafficHourlySample is bytes transferred during one UTC hour.
type TrafficHourlySample struct {
	TS      int64 `json:"t"`
	RxBytes int64 `json:"rx_bytes"`
	TxBytes int64 `json:"tx_bytes"`
}

const trafficHourlyRetention = 90 * 24 * time.Hour

// AddTrafficHourlyDelta adds NIC byte deltas to the UTC hour bucket.
func (s *Store) AddTrafficHourlyDelta(nodeID string, at time.Time, rxDelta, txDelta int64) error {
	if nodeID == "" {
		return nil
	}
	if rxDelta < 0 {
		rxDelta = 0
	}
	if txDelta < 0 {
		txDelta = 0
	}
	if rxDelta == 0 && txDelta == 0 {
		return nil
	}
	hourTS := at.UTC().Truncate(time.Hour).UnixMilli()
	cutoff := at.UTC().Add(-trafficHourlyRetention).Truncate(time.Hour).UnixMilli()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`
INSERT INTO traffic_hourly (node_id, hour_ts, rx_bytes, tx_bytes)
VALUES (?, ?, ?, ?)
ON CONFLICT(node_id, hour_ts) DO UPDATE SET
  rx_bytes = rx_bytes + excluded.rx_bytes,
  tx_bytes = tx_bytes + excluded.tx_bytes`,
		nodeID, hourTS, rxDelta, txDelta); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM traffic_hourly WHERE node_id = ? AND hour_ts < ?`, nodeID, cutoff); err != nil {
		return err
	}
	return tx.Commit()
}

// ListTrafficHourlySamples returns hour buckets for a node since the given time.
func (s *Store) ListTrafficHourlySamples(nodeID string, since time.Time) ([]TrafficHourlySample, error) {
	cutoff := since.UTC().Truncate(time.Hour).UnixMilli()
	rows, err := s.db.Query(`
SELECT hour_ts, rx_bytes, tx_bytes FROM traffic_hourly
WHERE node_id = ? AND hour_ts >= ?
ORDER BY hour_ts ASC`, nodeID, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TrafficHourlySample, 0, 96)
	for rows.Next() {
		var p TrafficHourlySample
		if err := rows.Scan(&p.TS, &p.RxBytes, &p.TxBytes); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
