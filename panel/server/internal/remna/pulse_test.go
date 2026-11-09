package remna

import (
	"testing"
	"time"
)

func TestUserSpanActiveAt(t *testing.T) {
	day := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	u := UserSpan{
		Created: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC),
		Expire:  time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC),
	}
	if !u.ActiveAt(day) {
		t.Fatal("should be active on Nov 5")
	}
	if u.ActiveAt(u.Expire) {
		t.Fatal("not active at exact expire")
	}
	if u.ActiveAt(u.Created) {
		t.Fatal("not active at exact create (strict before)")
	}
	u.Disabled = true
	if u.ActiveAt(day) {
		t.Fatal("disabled excluded")
	}
}

func TestBucketsFromSpansDayOverDay(t *testing.T) {
	loc := time.UTC
	d0 := time.Date(2026, 11, 3, 0, 0, 0, 0, loc)
	d1 := d0.AddDate(0, 0, 1)
	d2 := d0.AddDate(0, 0, 2)
	spans := []UserSpan{
		{Created: d0.Add(-48 * time.Hour), Expire: d2.Add(24 * time.Hour)},           // active d0,d1,d2
		{Created: d0.Add(1 * time.Hour), Expire: d2.Add(24 * time.Hour)},              // created on d0 → not in base d0, in d1+
		{Created: d0.Add(-24 * time.Hour), Expire: d1.Add(2 * time.Hour)},             // expires on d1
		{Created: d0.Add(-24 * time.Hour), Expire: d0.Add(2 * time.Hour), Disabled: true}, // ignored
	}
	ranges := []pulseRange{
		{key: "d0", start: d0, end: d1},
		{key: "d1", start: d1, end: d2},
	}
	b := bucketsFromSpans(spans, ranges)
	// base d0: only first + third (second created after d0, fourth disabled) = 2
	if b[0].Base != 2 {
		t.Fatalf("d0 base=%d want 2", b[0].Base)
	}
	// inflow d0: second user
	if b[0].Inflow != 1 {
		t.Fatalf("d0 inflow=%d want 1", b[0].Inflow)
	}
	// outflow d0: none (third expires on d1)
	if b[0].Outflow != 0 {
		t.Fatalf("d0 outflow=%d want 0", b[0].Outflow)
	}
	// base d1: first + second (third expires at d1+2h so still active at d1 start) = 3
	if b[1].Base != 3 {
		t.Fatalf("d1 base=%d want 3", b[1].Base)
	}
	if b[1].Outflow != 1 {
		t.Fatalf("d1 outflow=%d want 1", b[1].Outflow)
	}
}
