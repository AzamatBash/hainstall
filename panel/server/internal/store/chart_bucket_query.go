package store

import "time"

// Chart samples are stored every 5 minutes. Long windows are averaged in SQL
// so a 30-day chart does not pull thousands of rows into the process.
// until is exclusive of nothing: a zero until means "through now". A set until
// keeps the previous-week window from also returning the current week.

func (s *Store) ListRemnaOnlineSamplesBucketed(panelID string, since, until time.Time, bucketMs int64) ([]RemnaOnlineSample, error) {
	if bucketMs <= 0 {
		return s.listRemnaOnlineBetween(panelID, since, until)
	}
	rows, err := s.db.Query(`
SELECT ((ts + ?) / ?) * ? - ? + ?, CAST(ROUND(AVG(online)) AS INTEGER)
FROM remna_online_samples
WHERE panel_id = ? AND ts >= ? AND ts <= ?
GROUP BY (ts + ?) / ?
ORDER BY 1`, bucketArgs(bucketMs, panelID, since, until)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RemnaOnlineSample, 0, 64)
	for rows.Next() {
		var p RemnaOnlineSample
		if err := rows.Scan(&p.TS, &p.Online); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) ListRemnaTrafficSamplesBucketed(panelID string, since, until time.Time, bucketMs int64) ([]RemnaTrafficSample, error) {
	if bucketMs <= 0 {
		return s.listRemnaTrafficBetween(panelID, since, until)
	}
	rows, err := s.db.Query(`
SELECT ((ts + ?) / ?) * ? - ? + ?, AVG(down_bps), AVG(up_bps)
FROM remna_traffic_samples
WHERE panel_id = ? AND ts >= ? AND ts <= ?
GROUP BY (ts + ?) / ?
ORDER BY 1`, bucketArgs(bucketMs, panelID, since, until)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RemnaTrafficSample, 0, 64)
	for rows.Next() {
		var p RemnaTrafficSample
		if err := rows.Scan(&p.TS, &p.DownBps, &p.UpBps); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) ListRemnaNodeOnlineSamplesBucketed(panelID, remnaUUID string, since, until time.Time, bucketMs int64) ([]RemnaOnlineSample, error) {
	if bucketMs <= 0 {
		return s.listRemnaNodeOnlineBetween(panelID, remnaUUID, since, until)
	}
	rows, err := s.db.Query(`
SELECT ((ts + ?) / ?) * ? - ? + ?, CAST(ROUND(AVG(online)) AS INTEGER)
FROM remna_node_online_samples
WHERE panel_id = ? AND remna_uuid = ? AND ts >= ? AND ts <= ?
GROUP BY (ts + ?) / ?
ORDER BY 1`, bucketArgsUUID(bucketMs, panelID, remnaUUID, since, until)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RemnaOnlineSample, 0, 64)
	for rows.Next() {
		var p RemnaOnlineSample
		if err := rows.Scan(&p.TS, &p.Online); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) ListRemnaNodeTrafficSamplesBucketed(panelID, remnaUUID string, since, until time.Time, bucketMs int64) ([]RemnaTrafficSample, error) {
	if bucketMs <= 0 {
		return s.listRemnaNodeTrafficBetween(panelID, remnaUUID, since, until)
	}
	rows, err := s.db.Query(`
SELECT ((ts + ?) / ?) * ? - ? + ?, AVG(down_bps), AVG(up_bps)
FROM remna_node_traffic_samples
WHERE panel_id = ? AND remna_uuid = ? AND ts >= ? AND ts <= ?
GROUP BY (ts + ?) / ?
ORDER BY 1`, bucketArgsUUID(bucketMs, panelID, remnaUUID, since, until)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RemnaTrafficSample, 0, 64)
	for rows.Next() {
		var p RemnaTrafficSample
		if err := rows.Scan(&p.TS, &p.DownBps, &p.UpBps); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func bucketArgs(bucketMs int64, panelID string, since, until time.Time) []any {
	off := int64(3 * time.Hour / time.Millisecond)
	half := bucketMs / 2
	cutoff := since.UTC().UnixMilli()
	return []any{off, bucketMs, bucketMs, off, half, panelID, cutoff, untilBoundMs(until), off, bucketMs}
}

func bucketArgsUUID(bucketMs int64, panelID, remnaUUID string, since, until time.Time) []any {
	off := int64(3 * time.Hour / time.Millisecond)
	half := bucketMs / 2
	cutoff := since.UTC().UnixMilli()
	return []any{off, bucketMs, bucketMs, off, half, panelID, remnaUUID, cutoff, untilBoundMs(until), off, bucketMs}
}

func untilBoundMs(until time.Time) int64 {
	if until.IsZero() {
		return 1 << 62
	}
	return until.UTC().UnixMilli()
}

func (s *Store) listRemnaOnlineBetween(panelID string, since, until time.Time) ([]RemnaOnlineSample, error) {
	rows, err := s.db.Query(`
SELECT ts, online FROM remna_online_samples
WHERE panel_id = ? AND ts >= ? AND ts <= ?
ORDER BY ts ASC`, panelID, since.UTC().UnixMilli(), untilBoundMs(until))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RemnaOnlineSample, 0, 64)
	for rows.Next() {
		var p RemnaOnlineSample
		if err := rows.Scan(&p.TS, &p.Online); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) listRemnaTrafficBetween(panelID string, since, until time.Time) ([]RemnaTrafficSample, error) {
	rows, err := s.db.Query(`
SELECT ts, down_bps, up_bps FROM remna_traffic_samples
WHERE panel_id = ? AND ts >= ? AND ts <= ?
ORDER BY ts ASC`, panelID, since.UTC().UnixMilli(), untilBoundMs(until))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RemnaTrafficSample, 0, 64)
	for rows.Next() {
		var p RemnaTrafficSample
		if err := rows.Scan(&p.TS, &p.DownBps, &p.UpBps); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) listRemnaNodeOnlineBetween(panelID, remnaUUID string, since, until time.Time) ([]RemnaOnlineSample, error) {
	rows, err := s.db.Query(`
SELECT ts, online FROM remna_node_online_samples
WHERE panel_id = ? AND remna_uuid = ? AND ts >= ? AND ts <= ?
ORDER BY ts ASC`, panelID, remnaUUID, since.UTC().UnixMilli(), untilBoundMs(until))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RemnaOnlineSample, 0, 64)
	for rows.Next() {
		var p RemnaOnlineSample
		if err := rows.Scan(&p.TS, &p.Online); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) listRemnaNodeTrafficBetween(panelID, remnaUUID string, since, until time.Time) ([]RemnaTrafficSample, error) {
	rows, err := s.db.Query(`
SELECT ts, down_bps, up_bps FROM remna_node_traffic_samples
WHERE panel_id = ? AND remna_uuid = ? AND ts >= ? AND ts <= ?
ORDER BY ts ASC`, panelID, remnaUUID, since.UTC().UnixMilli(), untilBoundMs(until))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RemnaTrafficSample, 0, 64)
	for rows.Next() {
		var p RemnaTrafficSample
		if err := rows.Scan(&p.TS, &p.DownBps, &p.UpBps); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
