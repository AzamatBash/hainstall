package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestTrafficHourlyOptIn(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	n, err := st.CreateNode("n1", "http://127.0.0.1:9100", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if n.TrafficLog {
		t.Fatal("traffic_log should default off")
	}
	updated, err := st.SetNodeTrafficLog(n.ID, true)
	if err != nil || updated == nil || !updated.TrafficLog {
		t.Fatalf("enable: n=%v err=%v", updated, err)
	}
	got, err := st.GetNode(n.ID)
	if err != nil || got == nil || !got.TrafficLog {
		t.Fatal("GetNode missing traffic_log")
	}

	hour := time.Date(2026, 8, 17, 12, 10, 0, 0, time.UTC)
	if err := st.AddTrafficHourlyDelta(n.ID, hour, 100, 200); err != nil {
		t.Fatal(err)
	}
	if err := st.AddTrafficHourlyDelta(n.ID, hour.Add(20*time.Minute), 50, 25); err != nil {
		t.Fatal(err)
	}
	old := hour.Add(-100 * 24 * time.Hour)
	if err := st.AddTrafficHourlyDelta(n.ID, old, 9, 9); err != nil {
		t.Fatal(err)
	}

	list, err := st.ListTrafficHourlySamples(n.ID, hour.Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("len=%d want 1 (old pruned)", len(list))
	}
	if list[0].RxBytes != 150 || list[0].TxBytes != 225 {
		t.Fatalf("bucket %+v", list[0])
	}
	wantTS := hour.Truncate(time.Hour).UnixMilli()
	if list[0].TS != wantTS {
		t.Fatalf("ts=%d want %d", list[0].TS, wantTS)
	}

	later := hour.Add(3 * time.Hour)
	if err := st.AddTrafficHourlyDelta(n.ID, later, 10, 20); err != nil {
		t.Fatal(err)
	}
	ranged, err := st.ListTrafficHourlyRange(n.ID, hour.Truncate(time.Hour), hour.Truncate(time.Hour).Add(time.Hour-time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if len(ranged) != 1 || ranged[0].RxBytes != 150 {
		t.Fatalf("range %+v", ranged)
	}

	ok, err := st.DeleteNode(n.ID)
	if err != nil || !ok {
		t.Fatalf("delete: ok=%v err=%v", ok, err)
	}
	left, err := st.ListTrafficHourlySamples(n.ID, hour.Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("hourly rows leftover: %d", len(left))
	}
}
