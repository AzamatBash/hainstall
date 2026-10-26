package api

import (
	"testing"
	"time"
)

func TestClassifyTrafficWeekGrowth(t *testing.T) {
	const day = int64(24 * time.Hour / time.Millisecond)
	points := []trafficWeekPoint{
		{day: 1 * day, bps: 100e6, n: 20},
		{day: 2 * day, bps: 100e6, n: 20},
		{day: 3 * day, bps: 110e6, n: 20},
		{day: 4 * day, bps: 100e6, n: 20},
		{day: 5 * day, bps: 90e6, n: 20},
		{day: 6 * day, bps: 160e6, n: 20},
		{day: 7 * day, bps: 170e6, n: 20},
	}
	trend, delta, recent, earlier := classifyTrafficWeek(points, 8*day)
	if trend != "up" {
		t.Fatalf("trend=%s delta=%v recent=%v earlier=%v", trend, delta, recent, earlier)
	}
	if recent < earlier {
		t.Fatalf("recent %v earlier %v", recent, earlier)
	}
}

func TestClassifyTrafficWeekFlatAndShort(t *testing.T) {
	const day = int64(24 * time.Hour / time.Millisecond)
	flat := []trafficWeekPoint{
		{day: 1 * day, bps: 100e6, n: 20},
		{day: 2 * day, bps: 102e6, n: 20},
		{day: 3 * day, bps: 98e6, n: 20},
		{day: 4 * day, bps: 101e6, n: 20},
		{day: 5 * day, bps: 99e6, n: 20},
	}
	trend, _, _, _ := classifyTrafficWeek(flat, 6*day)
	if trend != "flat" {
		t.Fatalf("trend=%s", trend)
	}
	short := flat[:3]
	trend, _, recent, _ := classifyTrafficWeek(short, 6*day)
	if trend != "unknown" || recent == 0 {
		t.Fatalf("short trend=%s recent=%v", trend, recent)
	}
	// Today is excluded, so a cliff today does not count.
	withToday := append(append([]trafficWeekPoint{}, flat...), trafficWeekPoint{day: 6 * day, bps: 10e6, n: 20})
	trend, _, _, _ = classifyTrafficWeek(withToday, 6*day)
	if trend != "flat" {
		t.Fatalf("today leaked into trend: %s", trend)
	}
}
