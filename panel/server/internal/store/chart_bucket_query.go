package store

import "time"

// Chart samples are stored every 5 minutes. Long windows are averaged in SQL
// so a 30-day chart does not pull thousands of rows into the process.

func (s *Store) ListRemnaOnlineSamplesBucketed(panelID string, since time.Time, bucketMs int64) ([]RemnaOnlineSample, error) {
	if bucketMs <= 0 {
		return s.ListRemnaOnlineSamples(panelID, since)
	}
	rows, err := s.db.Query(`
SELECT ((ts + ?) / ?) * ? - ? + ?, CAST(ROUND(AVG(online)) AS INTEGER)
FROM remna_online_samples
WHERE panel_id = ? AND ts >= ?
GROUP BY (ts + ?) / ?
ORDER BY 1`, bucketArgs(bucketMs, panelID, since)...)
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

func (s *Store) ListRemnaTrafficSamplesBucketed(panelID string, since time.Time, bucketMs int64) ([]RemnaTrafficSample, error) {
	if bucketMs <= 0 {
		return s.ListRemnaTrafficSamples(panelID, since)
	}
	rows, err := s.db.Query(`
SELECT ((ts + ?) / ?) * ? - ? + ?, AVG(down_bps), AVG(up_bps)
FROM remna_traffic_samples
WHERE panel_id = ? AND ts >= ?
GROUP BY (ts + ?) / ?
ORDER BY 1`, bucketArgs(bucketMs, panelID, since)...)
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

func (s *Store) ListRemnaNodeOnlineSamplesBucketed(panelID, remnaUUID string, since time.Time, bucketMs int64) ([]RemnaOnlineSample, error) {
	if bucketMs <= 0 {
		return s.ListRemnaNodeOnlineSamples(panelID, remnaUUID, since)
	}
	rows, err := s.db.Query(`
SELECT ((ts + ?) / ?) * ? - ? + ?, CAST(ROUND(AVG(online)) AS INTEGER)
FROM remna_node_online_samples
WHERE panel_id = ? AND remna_uuid = ? AND ts >= ?
GROUP BY (ts + ?) / ?
ORDER BY 1`, bucketArgsUUID(bucketMs, panelID, remnaUUID, since)...)
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

func (s *Store) ListRemnaNodeTrafficSamplesBucketed(panelID, remnaUUID string, since time.Time, bucketMs int64) ([]RemnaTrafficSample, error) {
	if bucketMs <= 0 {
		return s.ListRemnaNodeTrafficSamples(panelID, remnaUUID, since)
	}
	rows, err := s.db.Query(`
SELECT ((ts + ?) / ?) * ? - ? + ?, AVG(down_bps), AVG(up_bps)
FROM remna_node_traffic_samples
WHERE panel_id = ? AND remna_uuid = ? AND ts >= ?
GROUP BY (ts + ?) / ?
ORDER BY 1`, bucketArgsUUID(bucketMs, panelID, remnaUUID, since)...)
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

func bucketArgs(bucketMs int64, panelID string, since time.Time) []any {
	off := int64(3 * time.Hour / time.Millisecond)
	half := bucketMs / 2
	cutoff := since.UTC().UnixMilli()
	return []any{off, bucketMs, bucketMs, off, half, panelID, cutoff, off, bucketMs}
}

func bucketArgsUUID(bucketMs int64, panelID, remnaUUID string, since time.Time) []any {
	off := int64(3 * time.Hour / time.Millisecond)
	half := bucketMs / 2
	cutoff := since.UTC().UnixMilli()
	return []any{off, bucketMs, bucketMs, off, half, panelID, remnaUUID, cutoff, off, bucketMs}
}
