package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRemnaTrafficSamples(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	if err := st.AppendRemnaTrafficSample("p1", now, 1_250_000, 500_000); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendRemnaTrafficSample("p1", now.Add(5*time.Minute), 2_000_000, 800_000); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-40 * 24 * time.Hour)
	if err := st.AppendRemnaTrafficSample("p1", old, 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendRemnaTrafficSample("p1", now.Add(10*time.Minute), 3_000_000, 900_000); err != nil {
		t.Fatal(err)
	}

	list, err := st.ListRemnaTrafficSamples("p1", now.Add(-1*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("list len=%d want 3 (old pruned)", len(list))
	}
	if list[0].DownBps != 1_250_000 || list[0].UpBps != 500_000 || list[2].DownBps != 3_000_000 {
		t.Fatalf("unexpected samples: %+v", list)
	}

	latest, err := st.LatestRemnaTrafficSample("p1")
	if err != nil {
		t.Fatal(err)
	}
	if latest == nil || latest.DownBps != 3_000_000 || latest.UpBps != 900_000 {
		t.Fatalf("latest=%v", latest)
	}
	missing, err := st.LatestRemnaTrafficSample("nope")
	if err != nil || missing != nil {
		t.Fatalf("missing=%v err=%v", missing, err)
	}
}

func TestMigrateRemnaTrafficSamplesFromBytes(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Simulate pre-rate schema that still has a bytes column.
	if _, err := st.db.Exec(`DROP TABLE remna_traffic_samples`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`
CREATE TABLE remna_traffic_samples (
  panel_id TEXT NOT NULL,
  ts INTEGER NOT NULL,
  bytes REAL NOT NULL,
  PRIMARY KEY (panel_id, ts)
)`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`INSERT INTO remna_traffic_samples (panel_id, ts, bytes) VALUES ('p1', 1, 42)`); err != nil {
		t.Fatal(err)
	}
	if err := st.migrateRemnaTrafficSamplesToRates(); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendRemnaTrafficSample("p1", time.Now().UTC(), 10, 20); err != nil {
		t.Fatal(err)
	}
	latest, err := st.LatestRemnaTrafficSample("p1")
	if err != nil || latest == nil || latest.DownBps != 10 || latest.UpBps != 20 {
		t.Fatalf("latest=%v err=%v", latest, err)
	}
}
