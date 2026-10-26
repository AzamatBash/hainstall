package api

import (
	"fmt"
	"time"

	"github.com/azabash/hapanel/panel/internal/store"
)

const (
	trafficAlertQuietBps   = 50_000 // below this the node is idle, not an incident
	trafficDropRemainRatio = 0.5    // last 30 min at half of the previous hours
	trafficDeclineRatio    = 0.75   // recent days at 3/4 of the earlier average
)

type trafficAlert struct {
	drop        bool
	decline     bool
	dropHint    string
	declineHint string
}

type trafficDayStat struct {
	key string
	sum float64
	n   int
}

func evalOneTrafficAlert(pts []store.RemnaNodeTrafficSample, now time.Time) trafficAlert {
	var alert trafficAlert
	if len(pts) < 8 {
		return alert
	}
	nowMs := now.UTC().UnixMilli()
	recentFrom := now.Add(-30 * time.Minute).UnixMilli()
	baseFrom := now.Add(-6 * time.Hour).UnixMilli()
	var recentSum, baseSum float64
	var recentN, baseN int
	for _, p := range pts {
		rate := p.DownBps + p.UpBps
		if p.TS >= recentFrom && p.TS <= nowMs {
			recentSum += rate
			recentN++
		} else if p.TS >= baseFrom && p.TS < recentFrom {
			baseSum += rate
			baseN++
		}
	}
	applyTrafficDrop(&alert, recentSum, baseSum, recentN, baseN)

	days := map[string]*trafficDayStat{}
	order := make([]string, 0, 16)
	today := mskDayKey(now)
	for _, p := range pts {
		key := mskDayKey(time.UnixMilli(p.TS).UTC())
		if key >= today {
			continue
		}
		st := days[key]
		if st == nil {
			st = &trafficDayStat{key: key}
			days[key] = st
			order = append(order, key)
		}
		st.sum += p.DownBps + p.UpBps
		st.n++
	}
	complete := make([]*trafficDayStat, 0, len(order))
	for _, key := range order {
		st := days[key]
		if st.n >= 12 {
			complete = append(complete, st)
		}
	}
	applyTrafficDecline(&alert, complete)
	return alert
}

func applyTrafficDrop(alert *trafficAlert, recentSum, baseSum float64, recentN, baseN int) {
	if recentN < 2 || baseN < 12 {
		return
	}
	recentAvg := recentSum / float64(recentN)
	baseAvg := baseSum / float64(baseN)
	if baseAvg >= trafficAlertQuietBps && recentAvg <= baseAvg*trafficDropRemainRatio {
		alert.drop = true
		alert.dropHint = fmt.Sprintf(
			"Резкое падение: сейчас %s, предыдущие 6 ч в среднем %s",
			formatAlertRate(recentAvg),
			formatAlertRate(baseAvg),
		)
	}
}

func applyTrafficDecline(alert *trafficAlert, complete []*trafficDayStat) {
	if len(complete) < 5 {
		return
	}
	recentDays := complete[len(complete)-2:]
	olderDays := complete[:len(complete)-2]
	if len(olderDays) < 3 {
		return
	}
	recentAvg := meanDay(recentDays)
	olderAvg := meanDay(olderDays)
	if olderAvg >= trafficAlertQuietBps && recentAvg <= olderAvg*trafficDeclineRatio {
		alert.decline = true
		alert.declineHint = fmt.Sprintf(
			"Ниже обычного: последние дни %s, раньше в среднем %s",
			formatAlertRate(recentAvg),
			formatAlertRate(olderAvg),
		)
	}
}

func (s *Server) loadNodeTrafficAlerts(now time.Time) (map[string]trafficAlert, error) {
	s.trafficAlertMu.Lock()
	if s.trafficAlerts != nil && time.Since(s.trafficAlertAt) < 45*time.Second {
		cached := s.trafficAlerts
		s.trafficAlertMu.Unlock()
		return cached, nil
	}
	s.trafficAlertMu.Unlock()

	drops, err := s.store.ListRemnaNodeTrafficDropWindows(now)
	if err != nil {
		return nil, err
	}
	days, err := s.store.ListRemnaTrafficDayAggs()
	if err != nil {
		return nil, err
	}
	built := evalTrafficAlertsFromRollup(drops, days, now)
	s.trafficAlertMu.Lock()
	s.trafficAlerts = built
	s.trafficAlertAt = time.Now()
	s.trafficAlertMu.Unlock()
	return built, nil
}

func evalTrafficAlertsFromRollup(drops []store.RemnaNodeTrafficDrop, days []store.RemnaTrafficDayAgg, now time.Time) map[string]trafficAlert {
	out := map[string]trafficAlert{}
	for _, d := range drops {
		key := d.PanelID + "\x00" + d.RemnaUUID
		a := out[key]
		applyTrafficDrop(&a, d.RecentSum, d.BaseSum, d.RecentN, d.BaseN)
		out[key] = a
	}
	today := (now.UTC().UnixMilli() + int64(3*time.Hour/time.Millisecond)) / int64(24*time.Hour/time.Millisecond)
	grouped := map[string][]*trafficDayStat{}
	order := make([]string, 0)
	for _, d := range days {
		if d.Day >= today || d.N < 12 {
			continue
		}
		key := d.PanelID + "\x00" + d.RemnaUUID
		if _, ok := grouped[key]; !ok {
			order = append(order, key)
		}
		grouped[key] = append(grouped[key], &trafficDayStat{sum: d.Sum, n: d.N})
	}
	for _, key := range order {
		a := out[key]
		applyTrafficDecline(&a, grouped[key])
		out[key] = a
	}
	return out
}

func meanDay(days []*trafficDayStat) float64 {
	if len(days) == 0 {
		return 0
	}
	var sum float64
	for _, d := range days {
		if d.n > 0 {
			sum += d.sum / float64(d.n)
		}
	}
	return sum / float64(len(days))
}

func mskDayKey(t time.Time) string {
	return t.In(time.FixedZone("MSK", 3*3600)).Format("2006-01-02")
}

func formatAlertRate(bytesPerSec float64) string {
	mbit := bytesPerSec * 8 / 1_000_000
	if mbit >= 100 {
		return fmt.Sprintf("%.0f Мбит/с", mbit)
	}
	if mbit >= 10 {
		return fmt.Sprintf("%.1f Мбит/с", mbit)
	}
	return fmt.Sprintf("%.2f Мбит/с", mbit)
}
