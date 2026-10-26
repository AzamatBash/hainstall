package api

import (
	"testing"
	"time"

	"github.com/azabash/hapanel/panel/internal/store"
)

func TestTrafficAlertSharpDrop(t *testing.T) {
	now := time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)
	var pts []store.RemnaNodeTrafficSample
	for i := 0; i < 72; i++ {
		at := now.Add(-6*time.Hour + time.Duration(i)*5*time.Minute)
		rate := 2_000_000.0
		if !at.Before(now.Add(-30 * time.Minute)) {
			rate = 200_000
		}
		pts = append(pts, store.RemnaNodeTrafficSample{
			PanelID: "p", RemnaUUID: "n", TS: at.UnixMilli(), DownBps: rate,
		})
	}
	alert := evalOneTrafficAlert(pts, now)
	if !alert.drop {
		t.Fatal("expected a sharp drop")
	}
	if alert.decline {
		t.Fatal("one cliff is not a multi-day decline")
	}
}

func TestTrafficAlertQuietNodeStaysQuiet(t *testing.T) {
	now := time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)
	var pts []store.RemnaNodeTrafficSample
	for i := 0; i < 72; i++ {
		at := now.Add(-6*time.Hour + time.Duration(i)*5*time.Minute)
		rate := 1_000.0
		if !at.Before(now.Add(-30 * time.Minute)) {
			rate = 0
		}
		pts = append(pts, store.RemnaNodeTrafficSample{
			PanelID: "p", RemnaUUID: "n", TS: at.UnixMilli(), DownBps: rate,
		})
	}
	alert := evalOneTrafficAlert(pts, now)
	if alert.drop || alert.decline {
		t.Fatalf("idle node should not alert: %+v", alert)
	}
}

func TestTrafficAlertMultiDayDecline(t *testing.T) {
	msk := time.FixedZone("MSK", 3*3600)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, msk)
	var pts []store.RemnaNodeTrafficSample
	for day := 9; day >= 1; day-- {
		dayStart := time.Date(2026, 10, 20-day, 0, 0, 0, 0, msk)
		rate := 2_000_000.0
		if day <= 2 {
			rate = 900_000
		}
		for i := 0; i < 24; i++ {
			at := dayStart.Add(time.Duration(i) * time.Hour)
			pts = append(pts, store.RemnaNodeTrafficSample{
				PanelID: "p", RemnaUUID: "n", TS: at.UnixMilli(), DownBps: rate,
			})
		}
	}
	alert := evalOneTrafficAlert(pts, now)
	if !alert.decline {
		t.Fatal("expected a multi-day decline")
	}
	if alert.drop {
		t.Fatal("steady lower days are not a sharp drop")
	}
}
