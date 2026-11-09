package remna

import (
	"time"
)

// PulseBucket is one day or month column for the Pulse table.
type PulseBucket struct {
	Key        string  `json:"key"`
	Label      string  `json:"label"`
	Partial    bool    `json:"partial,omitempty"`
	Base       int     `json:"base"` // ACTIVE at period start (from createdAt/expireAt)
	Inflow     int     `json:"inflow"`
	Outflow    int     `json:"outflow"`
	Net        int     `json:"net"`
	InflowPct  float64 `json:"inflow_pct"`
	OutflowPct float64 `json:"outflow_pct"`
	NetPct     float64 `json:"net_pct"`
}

// PulseSnapshot is the payload for GET …/pulse.
type PulseSnapshot struct {
	ActiveNow     int              `json:"active_now"`
	ActiveByDates int              `json:"active_by_dates"`
	DisabledNow   int              `json:"disabled_now"`
	LimitedNow    int              `json:"limited_now"`
	ExpiredNow    int              `json:"expired_now"`
	TotalUsers    int              `json:"total_users"`
	UsersSampled  int              `json:"users_sampled"`
	TZ            string           `json:"tz"`
	AsOf          time.Time        `json:"as_of"`
	WeekDays      []PulseBucket    `json:"week_days"`
	WeekTotal     PulseBucket      `json:"week_total"`
	Months        []PulseBucket    `json:"months"`
	Note          string           `json:"note"`
	Statuses      UserStatusCounts `json:"statuses"`
}

type pulseRange struct {
	key     string
	label   string
	partial bool
	start   time.Time
	end     time.Time
}

// BuildPulseFromSpans computes week/month tables locally from cached spans.
// No Remnawave HTTP calls.
func BuildPulseFromSpans(spans []UserSpan, st UserStatusCounts, tz string, monthCount int, asOf time.Time) PulseSnapshot {
	if monthCount < 1 {
		monthCount = 5
	}
	if monthCount > 12 {
		monthCount = 12
	}
	loc, err := time.LoadLocation(tz)
	if err != nil || tz == "" {
		loc = time.FixedZone("MSK", 3*3600)
		tz = "Europe/Moscow"
	}
	if asOf.IsZero() {
		asOf = time.Now().In(loc)
	} else {
		asOf = asOf.In(loc)
	}
	now := asOf

	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	week := make([]pulseRange, 7)
	for i := 0; i < 7; i++ {
		d := todayStart.AddDate(0, 0, -6+i)
		week[i] = pulseRange{
			key:   d.Format("2006-01-02"),
			label: formatDayLabel(d),
			start: d,
			end:   d.AddDate(0, 0, 1),
		}
	}

	months := make([]pulseRange, monthCount)
	curMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	for i := 0; i < monthCount; i++ {
		ms := curMonthStart.AddDate(0, -(monthCount-1-i), 0)
		me := ms.AddDate(0, 1, 0)
		partial := false
		label := ms.Format("2006-01")
		end := me
		if !ms.Before(curMonthStart) {
			partial = true
			end = todayStart.AddDate(0, 0, 1)
			label = ms.Format("2006-01") + " (1–" + now.Format("2") + ")"
		}
		months[i] = pulseRange{
			key:     ms.Format("2006-01"),
			label:   label,
			partial: partial,
			start:   ms,
			end:     end,
		}
	}

	weekBuckets := bucketsFromSpans(spans, week)
	monthBuckets := bucketsFromSpans(spans, months)
	weekTotal := sumBucketsDate("7d", "Итого 7д", weekBuckets)
	activeByDates := CountActiveAt(spans, now)

	return PulseSnapshot{
		ActiveNow:     st.Active,
		ActiveByDates: activeByDates,
		DisabledNow:   st.Disabled,
		LimitedNow:    st.Limited,
		ExpiredNow:    st.Expired,
		TotalUsers:    st.Total,
		UsersSampled:  len(spans),
		Statuses:      st,
		TZ:            tz,
		AsOf:          asOf,
		WeekDays:      weekBuckets,
		WeekTotal:     weekTotal,
		Months:        monthBuckets,
		Note: "База/приток/отток из локального кэша юзеров (createdAt/expireAt). " +
			"Remnawave синкается фоном раз в несколько часов, не при каждом открытии Pulse.",
	}
}

func bucketsFromSpans(spans []UserSpan, ranges []pulseRange) []PulseBucket {
	out := make([]PulseBucket, len(ranges))
	for i, r := range ranges {
		b := PulseBucket{
			Key:     r.key,
			Label:   r.label,
			Partial: r.partial,
			Base:    CountActiveAt(spans, r.start),
			Inflow:  CountCreatedIn(spans, r.start, r.end),
			Outflow: CountExpiredIn(spans, r.start, r.end),
		}
		b.Net = b.Inflow - b.Outflow
		setPcts(&b)
		out[i] = b
	}
	return out
}

func setPcts(b *PulseBucket) {
	if b.Base <= 0 {
		return
	}
	base := float64(b.Base)
	b.InflowPct = float64(b.Inflow) / base
	b.OutflowPct = float64(b.Outflow) / base
	b.NetPct = float64(b.Net) / base
}

func sumBucketsDate(key, label string, days []PulseBucket) PulseBucket {
	out := PulseBucket{Key: key, Label: label}
	if len(days) == 0 {
		return out
	}
	out.Base = days[0].Base
	for _, d := range days {
		out.Inflow += d.Inflow
		out.Outflow += d.Outflow
	}
	out.Net = out.Inflow - out.Outflow
	setPcts(&out)
	return out
}

var weekdayRU = [...]string{"вс", "пн", "вт", "ср", "чт", "пт", "сб"}

func formatDayLabel(d time.Time) string {
	return d.Format("02.01") + " " + weekdayRU[d.Weekday()]
}
