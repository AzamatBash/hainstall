package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRemnaOnlineSamples(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	if err := st.AppendRemnaOnlineSample("p1", now, 42); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendRemnaOnlineSample("p1", now.Add(5*time.Minute), 50); err != nil {
		t.Fatal(err)
	}
	// Old sample outside retention should be pruned on append.
	old := now.Add(-40 * 24 * time.Hour)
	if err := st.AppendRemnaOnlineSample("p1", old, 1); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendRemnaOnlineSample("p1", now.Add(10*time.Minute), 55); err != nil {
		t.Fatal(err)
	}

	list, err := st.ListRemnaOnlineSamples("p1", now.Add(-1*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("list len=%d want 3 (old pruned)", len(list))
	}
	if list[0].Online != 42 || list[2].Online != 55 {
		t.Fatalf("unexpected samples: %+v", list)
	}

	latest, err := st.LatestRemnaOnlineSample("p1")
	if err != nil {
		t.Fatal(err)
	}
	if latest == nil || latest.Online != 55 {
		t.Fatalf("latest=%v", latest)
	}
	missing, err := st.LatestRemnaOnlineSample("nope")
	if err != nil || missing != nil {
		t.Fatalf("missing=%v err=%v", missing, err)
	}
}
