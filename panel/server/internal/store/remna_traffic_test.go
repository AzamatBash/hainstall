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
	if err := st.AppendRemnaTrafficSample("p1", now, 1_000_000); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendRemnaTrafficSample("p1", now.Add(5*time.Minute), 1_500_000); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-40 * 24 * time.Hour)
	if err := st.AppendRemnaTrafficSample("p1", old, 1); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendRemnaTrafficSample("p1", now.Add(10*time.Minute), 2_000_000); err != nil {
		t.Fatal(err)
	}

	list, err := st.ListRemnaTrafficSamples("p1", now.Add(-1*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("list len=%d want 3 (old pruned)", len(list))
	}
	if list[0].Bytes != 1_000_000 || list[2].Bytes != 2_000_000 {
		t.Fatalf("unexpected samples: %+v", list)
	}

	latest, err := st.LatestRemnaTrafficSample("p1")
	if err != nil {
		t.Fatal(err)
	}
	if latest == nil || latest.Bytes != 2_000_000 {
		t.Fatalf("latest=%v", latest)
	}
	missing, err := st.LatestRemnaTrafficSample("nope")
	if err != nil || missing != nil {
		t.Fatalf("missing=%v err=%v", missing, err)
	}
}
