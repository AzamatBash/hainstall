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

	days, err := st.ListRemnaTrafficDayAggs()
	if err != nil {
		t.Fatal(err)
	}
	var n1 int
	var sum float64
	for _, d := range days {
		if d.PanelID == "p1" && d.RemnaUUID == "n1" {
			n1 += d.N
			sum += d.Sum
		}
	}
	if n1 != 3 || sum != 100+40+200+80+300+90 {
		t.Fatalf("day rollup n=%d sum=%v days=%+v", n1, sum, days)
	}

	if err := st.AppendRemnaNodeTrafficSample("p1", "n1", now, 50, 10); err != nil {
		t.Fatal(err)
	}
	days, err = st.ListRemnaTrafficDayAggs()
	if err != nil {
		t.Fatal(err)
	}
	n1, sum = 0, 0
	for _, d := range days {
		if d.PanelID == "p1" && d.RemnaUUID == "n1" {
			n1 += d.N
			sum += d.Sum
		}
	}
	if n1 != 3 || sum != 50+10+200+80+300+90 {
		t.Fatalf("replaced sample double-counted: n=%d sum=%v", n1, sum)
	}
	if _, err := st.ListRemnaNodeTrafficDropWindows(now.Add(10 * time.Minute)); err != nil {
		t.Fatal(err)
	}
}
