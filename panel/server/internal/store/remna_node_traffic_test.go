package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRemnaNodeTrafficSamples(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	if err := st.AppendRemnaNodeTrafficSample("p1", "n1", now, 100, 40); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendRemnaNodeTrafficSample("p1", "n1", now.Add(5*time.Minute), 200, 80); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendRemnaNodeTrafficSample("p1", "n2", now, 10, 5); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-20 * 24 * time.Hour)
	if err := st.AppendRemnaNodeTrafficSample("p1", "n1", old, 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendRemnaNodeTrafficSample("p1", "n1", now.Add(10*time.Minute), 300, 90); err != nil {
		t.Fatal(err)
	}

	list, err := st.ListRemnaNodeTrafficSamples("p1", "n1", now.Add(-1*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("list len=%d want 3 (old pruned)", len(list))
	}
	if list[0].DownBps != 100 || list[2].DownBps != 300 {
		t.Fatalf("unexpected samples: %+v", list)
	}

	latest, err := st.LatestRemnaNodeTrafficSample("p1", "n1")
	if err != nil {
		t.Fatal(err)
	}
	if latest == nil || latest.DownBps != 300 || latest.UpBps != 90 {
		t.Fatalf("latest=%v", latest)
	}

	m, err := st.MapLatestRemnaNodeTraffic()
	if err != nil {
		t.Fatal(err)
	}
	if got := m["p1\x00n1"]; got.DownBps != 300 {
		t.Fatalf("map n1=%v", got)
	}
	if got := m["p1\x00n2"]; got.DownBps != 10 {
		t.Fatalf("map n2=%v", got)
	}
}
