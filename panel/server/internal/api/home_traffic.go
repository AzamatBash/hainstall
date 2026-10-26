package api

import (
	"sort"
	"time"
)

const (
	homeTrafficHoldRatio = 0.08
	homeTrafficMinDayN   = 12
	homeTrafficQuietBps  = 50_000
)

type homeTrafficDay struct {
	T  int64   `json:"t"`
	Tx float64 `json:"tx"`
	Rx float64 `json:"rx"`
}

type homeTrafficPanel struct {
	ID    string           `json:"id"`
	Name  string           `json:"name"`
	Trend string           `json:"trend"`
	Delta float64          `json:"delta"`
	TxBps float64          `json:"tx_bps"`
	RxBps float64          `json:"rx_bps"`
	Days  []homeTrafficDay `json:"days"`
}

type homeTrafficDTO struct {
	Panels []homeTrafficPanel `json:"panels"`
}

type trafficWeekPoint struct {
	day  int64
	bps  float64
	down float64
	up   float64
	n    int
}

func (s *Server) loadHomeTraffic() (*homeTrafficDTO, error) {
	s.homeTrafficMu.Lock()
	fresh := s.homeTraffic != nil && time.Since(s.homeTrafficAt) < 60*time.Second
	cached := s.homeTraffic
	s.homeTrafficMu.Unlock()
	if fresh {
		return cached, nil
	}
	built, err := s.computeHomeTraffic()
	if err != nil {
		if cached != nil {
			s.logger.Error("home traffic refresh", "err", err)
			return cached, nil
		}
		return nil, err
	}
	s.homeTrafficMu.Lock()
	s.homeTrafficAt = time.Now()
	s.homeTraffic = built
	s.homeTrafficMu.Unlock()
	return built, nil
}

func (s *Server) computeHomeTraffic() (*homeTrafficDTO, error) {
	now := time.Now().UTC()
	since := now.Add(-8 * 24 * time.Hour)
	rows, err := s.store.ListRemnaTrafficDaily(since)
	if err != nil {
		return nil, err
	}
	panels, err := s.store.ListRemnaPanels()
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(panels))
	for _, p := range panels {
		names[p.ID] = p.Name
	}
	today := mskMidnight(now)
	byPanel := map[string][]trafficWeekPoint{}
	for _, row := range rows {
		byPanel[row.PanelID] = append(byPanel[row.PanelID], trafficWeekPoint{
			day: row.Day, bps: row.Down + row.Up, down: row.Down, up: row.Up, n: row.N,
		})
	}
	outPanels := make([]homeTrafficPanel, 0, len(byPanel))
	for id, pts := range byPanel {
		sort.Slice(pts, func(i, j int) bool { return pts[i].day < pts[j].day })
		tr, d, _, _ := classifyTrafficWeek(pts, today)
		tx, rx := recentDirection(pts, today)
		if tx == 0 && rx == 0 && tr == "unknown" {
			continue
		}
		name := names[id]
		if name == "" {
			name = id
		}
		days := make([]homeTrafficDay, 0, len(pts))
		for _, p := range pts {
			if p.n <= 0 {
				continue
			}
			days = append(days, homeTrafficDay{T: p.day, Tx: p.down, Rx: p.up})
		}
		outPanels = append(outPanels, homeTrafficPanel{
			ID:    id,
			Name:  name,
			Trend: tr,
			Delta: d,
			TxBps: tx,
			RxBps: rx,
			Days:  days,
		})
	}
	sort.Slice(outPanels, func(i, j int) bool {
		ai := outPanels[i].TxBps + outPanels[i].RxBps
		aj := outPanels[j].TxBps + outPanels[j].RxBps
		if ai != aj {
			return ai > aj
		}
		return outPanels[i].Name < outPanels[j].Name
	})
	return &homeTrafficDTO{Panels: outPanels}, nil
}

func recentDirection(points []trafficWeekPoint, todayStart int64) (tx, rx float64) {
	complete := make([]trafficWeekPoint, 0, len(points))
	for _, p := range points {
		if p.n >= homeTrafficMinDayN && p.day < todayStart {
			complete = append(complete, p)
		}
	}
	sort.Slice(complete, func(i, j int) bool { return complete[i].day < complete[j].day })
	if len(complete) > 7 {
		complete = complete[len(complete)-7:]
	}
	if len(complete) == 0 {
		return 0, 0
	}
	window := complete
	if len(complete) >= 4 {
		window = complete[len(complete)-2:]
	}
	var down, up float64
	for _, p := range window {
		down += p.down
		up += p.up
	}
	n := float64(len(window))
	return down / n, up / n
}

func classifyTrafficWeek(points []trafficWeekPoint, todayStart int64) (trend string, delta, recent, earlier float64) {
	complete := make([]trafficWeekPoint, 0, len(points))
	for _, p := range points {
		if p.n >= homeTrafficMinDayN && p.day < todayStart {
			complete = append(complete, p)
		}
	}
	sort.Slice(complete, func(i, j int) bool { return complete[i].day < complete[j].day })
	if len(complete) > 7 {
		complete = complete[len(complete)-7:]
	}
	if len(complete) == 0 {
		return "unknown", 0, 0, 0
	}
	if len(complete) < 4 {
		return "unknown", 0, meanWeek(complete), 0
	}
	recentPts := complete[len(complete)-2:]
	earlierPts := complete[:len(complete)-2]
	recent = meanWeek(recentPts)
	earlier = meanWeek(earlierPts)
	if earlier < homeTrafficQuietBps {
		if recent >= homeTrafficQuietBps && recent >= earlier*1.5 {
			return "up", 1, recent, earlier
		}
		return "flat", 0, recent, earlier
	}
	delta = (recent - earlier) / earlier
	switch {
	case delta >= homeTrafficHoldRatio:
		trend = "up"
	case delta <= -homeTrafficHoldRatio:
		trend = "down"
	default:
		trend = "flat"
	}
	return trend, delta, recent, earlier
}

func meanWeek(points []trafficWeekPoint) float64 {
	if len(points) == 0 {
		return 0
	}
	var sum float64
	for _, p := range points {
		sum += p.bps
	}
	return sum / float64(len(points))
}

func mskMidnight(t time.Time) int64 {
	loc := time.FixedZone("MSK", 3*3600)
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc).UnixMilli()
}
