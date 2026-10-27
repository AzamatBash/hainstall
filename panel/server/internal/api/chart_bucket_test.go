package api

import (
	"testing"
	"time"

	"github.com/azabash/hapanel/panel/internal/store"
)

func TestAggregateOnlineHour(t *testing.T) {
	start := time.Date(2026, 10, 20, 10, 0, 0, 0, time.FixedZone("MSK", 3*3600))
	points := make([]store.RemnaOnlineSample, 0, 24)
	for i := 0; i < 24; i++ {
		points = append(points, store.RemnaOnlineSample{
			TS:     start.Add(time.Duration(i) * 5 * time.Minute).UnixMilli(),
			Online: 10 + i,
		})
	}
	out := aggregateOnline(points, int64(time.Hour/time.Millisecond))
	if len(out) != 2 {
		t.Fatalf("buckets=%d want 2", len(out))
	}
	if out[0].Online < 10 || out[0].Online > 21 {
		t.Fatalf("first avg=%d", out[0].Online)
	}
	if chartPayloadBucketMs(1, false) != 0 {
		t.Fatal("one hour must stay raw")
	}
	hour := int64(time.Hour / time.Millisecond)
	if chartPayloadBucketMs(6, false) != hour || chartPayloadBucketMs(12, false) != hour || chartPayloadBucketMs(24, false) != hour || chartPayloadBucketMs(168, false) != hour || chartPayloadBucketMs(744, false) != hour {
		t.Fatal("spans longer than one hour must be hourly")
	}
	if chartPayloadBucketMs(744, true) != 0 {
		t.Fatal("raw=1 keeps every sample")
	}
}
