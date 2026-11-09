package remna

import "testing"

func TestMakeBucketsUsesActiveOnly(t *testing.T) {
	jobs := []digestJob{{key: "d1", label: "d1"}, {key: "d2", label: "d2"}}
	digests := []DigestUsers{{CreatedCount: 30, ExpiredCount: 10}, {CreatedCount: 20, ExpiredCount: 25}}
	buckets := makeBuckets(jobs, digests, 10000)
	if buckets[0].Base != 10000 || buckets[1].Base != 10000 {
		t.Fatalf("base must be current ACTIVE for every column: %+v", buckets)
	}
	if buckets[0].Net != 20 || buckets[1].Net != -5 {
		t.Fatalf("net: %+v", buckets)
	}
	if buckets[0].InflowPct != 0.003 {
		t.Fatalf("inflow pct: %v", buckets[0].InflowPct)
	}
	tot := sumBuckets("7d", "Итого", buckets, 10000)
	if tot.Base != 10000 || tot.Inflow != 50 || tot.Outflow != 35 || tot.Net != 15 {
		t.Fatalf("sum: %+v", tot)
	}
}
