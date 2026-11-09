package remna

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// PulseBucket is one day or month column for the Pulse table.
type PulseBucket struct {
	Key        string  `json:"key"`
	Label      string  `json:"label"`
	Partial    bool    `json:"partial,omitempty"`
	Base       int     `json:"base"` // always current ACTIVE (same for every column)
	Inflow     int     `json:"inflow"`
	Outflow    int     `json:"outflow"`
	Net        int     `json:"net"`
	InflowPct  float64 `json:"inflow_pct"`
	OutflowPct float64 `json:"outflow_pct"`
	NetPct     float64 `json:"net_pct"`
}

// PulseSnapshot is the payload for GET …/pulse.
type PulseSnapshot struct {
	ActiveNow   int              `json:"active_now"`
	DisabledNow int              `json:"disabled_now"`
	LimitedNow  int              `json:"limited_now"`
	ExpiredNow  int              `json:"expired_now"`
	TotalUsers  int              `json:"total_users"`
	TZ          string           `json:"tz"`
	AsOf        time.Time        `json:"as_of"`
	WeekDays    []PulseBucket    `json:"week_days"`
	WeekTotal   PulseBucket      `json:"week_total"`
	Months      []PulseBucket    `json:"months"`
	Note        string           `json:"note"`
	Statuses    UserStatusCounts `json:"statuses"`
}

type digestJob struct {
	key     string
	label   string
	partial bool
	start   time.Time
	end     time.Time
}

// FetchPulse builds rolling 7-day and calendar-month pulse tables.
// Percentages use ONLY current Remnawave ACTIVE as denominator.
// Historical ACTIVE is not available from Remnawave; we do NOT reconstruct
// base via created/expired (that pulled expired/disabled mass back into «база»).
func (c *Client) FetchPulse(ctx context.Context, baseURL, apiKey, tz string, monthCount int) (PulseSnapshot, error) {
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
	now := time.Now().In(loc)
	asOf := now

	rc := c.withTimeout(20 * time.Second)
	st, err := rc.FetchUserStatusCounts(ctx, baseURL, apiKey, tz)
	if err != nil {
		return PulseSnapshot{}, err
	}
	active := st.Active

	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	weekJobs := make([]digestJob, 7)
	for i := 0; i < 7; i++ {
		d := todayStart.AddDate(0, 0, -6+i)
		weekJobs[i] = digestJob{
			key:   d.Format("2006-01-02"),
			label: formatDayLabel(d),
			start: d,
			end:   d.AddDate(0, 0, 1),
		}
	}

	monthJobs := make([]digestJob, monthCount)
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
		monthJobs[i] = digestJob{
			key:     ms.Format("2006-01"),
			label:   label,
			partial: partial,
			start:   ms,
			end:     end,
		}
	}

	weekDigests, weekErrs := rc.fetchDigestsParallel(ctx, baseURL, apiKey, weekJobs)
	if err := firstErr(weekErrs); err != nil {
		return PulseSnapshot{}, fmt.Errorf("week digest: %w", err)
	}
	monthDigests, monthErrs := rc.fetchDigestsParallel(ctx, baseURL, apiKey, monthJobs)
	if err := firstErr(monthErrs); err != nil {
		return PulseSnapshot{}, fmt.Errorf("month digest: %w", err)
	}

	weekBuckets := makeBuckets(weekJobs, weekDigests, active)
	monthBuckets := makeBuckets(monthJobs, monthDigests, active)
	weekTotal := sumBuckets("7d", "Итого 7д", weekBuckets, active)

	return PulseSnapshot{
		ActiveNow:   st.Active,
		DisabledNow: st.Disabled,
		LimitedNow:  st.Limited,
		ExpiredNow:  st.Expired,
		TotalUsers:  st.Total,
		Statuses:    st,
		TZ:          tz,
		AsOf:        asOf,
		WeekDays:    weekBuckets,
		WeekTotal:   weekTotal,
		Months:      monthBuckets,
		Note: "База для % = только текущий ACTIVE (без DISABLED/LIMITED/EXPIRED). " +
			"Историческую ACTIVE Remnawave не отдаёт — прошлую базу через created/expired больше не восстанавливаем. " +
			"Приток/отток = digest createdCount/expiredCount (триалы в потоке).",
	}, nil
}

func (c *Client) fetchDigestsParallel(ctx context.Context, baseURL, apiKey string, jobs []digestJob) ([]DigestUsers, []error) {
	out := make([]DigestUsers, len(jobs))
	errs := make([]error, len(jobs))
	var wg sync.WaitGroup
	for i := range jobs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			d, err := c.FetchDigest(ctx, baseURL, apiKey, jobs[i].start, jobs[i].end)
			out[i] = d
			errs[i] = err
		}(i)
	}
	wg.Wait()
	return out, errs
}

func firstErr(errs []error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func makeBuckets(jobs []digestJob, digests []DigestUsers, activeBase int) []PulseBucket {
	out := make([]PulseBucket, len(jobs))
	for i, j := range jobs {
		b := PulseBucket{
			Key:     j.key,
			Label:   j.label,
			Partial: j.partial,
			Base:    activeBase,
			Inflow:  digests[i].CreatedCount,
			Outflow: digests[i].ExpiredCount,
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

func sumBuckets(key, label string, days []PulseBucket, activeBase int) PulseBucket {
	out := PulseBucket{Key: key, Label: label, Base: activeBase}
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
