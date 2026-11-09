package remna

import "testing"

func TestFillBasesFromActive(t *testing.T) {
	// Day0: +10 net, day1: +5, day2(today): +2. Active now (end of day2) = 100.
	// start2 = 100 - 12 + 10 = 98
	// start1 = 98 - 20 + 15 = 93
	// start0 = 93 - 30 + 20 = 83
	buckets := []PulseBucket{
		{Inflow: 30, Outflow: 20, Net: 10},
		{Inflow: 20, Outflow: 15, Net: 5},
		{Inflow: 12, Outflow: 10, Net: 2},
	}
	fillBasesFromActive(buckets, 100)
	if buckets[2].Base != 98 {
		t.Fatalf("today base: got %d want 98", buckets[2].Base)
	}
	if buckets[1].Base != 93 {
		t.Fatalf("mid base: got %d want 93", buckets[1].Base)
	}
	if buckets[0].Base != 83 {
		t.Fatalf("old base: got %d want 83", buckets[0].Base)
	}
	// end0 = 83+10 = 93 = start1; end1 = 93+5 = 98 = start2; end2 = 98+2 = 100
	if got := buckets[0].Base + buckets[0].Net; got != buckets[1].Base {
		t.Fatalf("chain break 0→1: %d vs %d", got, buckets[1].Base)
	}
	if got := buckets[1].Base + buckets[1].Net; got != buckets[2].Base {
		t.Fatalf("chain break 1→2: %d vs %d", got, buckets[2].Base)
	}
	if buckets[2].InflowPct <= 0 || buckets[2].OutflowPct <= 0 {
		t.Fatalf("pcts not set: %+v", buckets[2])
	}
}

func TestSumBuckets(t *testing.T) {
	days := []PulseBucket{
		{Base: 100, Inflow: 10, Outflow: 4, Net: 6},
		{Base: 106, Inflow: 8, Outflow: 5, Net: 3},
	}
	setPcts(&days[0])
	setPcts(&days[1])
	tot := sumBuckets("7d", "Итого", days)
	if tot.Base != 100 || tot.Inflow != 18 || tot.Outflow != 9 || tot.Net != 9 {
		t.Fatalf("sum: %+v", tot)
	}
	if tot.NetPct != 0.09 {
		t.Fatalf("net pct: %v", tot.NetPct)
	}
}
