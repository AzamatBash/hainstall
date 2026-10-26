package remna

import (
	"context"
	"net/url"
	"strings"
	"sync"
)

// RuntimeProcess is one Remnawave process from GET /api/system/health.
type RuntimeProcess struct {
	RSS              float64 `json:"rss"`
	HeapUsed         float64 `json:"heapUsed"`
	EventLoopDelayMs float64 `json:"eventLoopDelayMs"`
	InstanceID       string  `json:"instanceId"`
	InstanceType     string  `json:"instanceType"`
}

// ByteWindow is a traffic window from GET /api/system/stats/bandwidth.
type ByteWindow struct {
	Current    string `json:"current"`
	Previous   string `json:"previous"`
	Difference string `json:"difference"`
}

// HomeSnapshot is the Remnawave home dashboard for one panel.
type HomeSnapshot struct {
	Processes []RuntimeProcess
	Memory    struct {
		Total float64
		Free  float64
		Used  float64
	}
	CPUCores        int
	Uptime          float64
	UsersTotal      int
	UsersActive     int
	UsersExpired    int
	UsersLimited    int
	UsersDisabled   int
	OnlineNow       int
	OnlineLastDay   int
	OnlineLastWeek  int
	NeverOnline     int
	NodesOnline     int
	NodesTraffic    string
	BandwidthToday  ByteWindow
	BandwidthWeek   ByteWindow
	Bandwidth30Days ByteWindow
	BandwidthMonth  ByteWindow
	BandwidthYear   ByteWindow
	HealthError     string
	StatsError      string
	BandwidthError  string
}

type healthBody struct {
	Response struct {
		RuntimeMetrics []RuntimeProcess `json:"runtimeMetrics"`
	} `json:"response"`
}

type statsBody struct {
	Response struct {
		CPU struct {
			Cores int `json:"cores"`
		} `json:"cpu"`
		Memory struct {
			Total float64 `json:"total"`
			Free  float64 `json:"free"`
			Used  float64 `json:"used"`
		} `json:"memory"`
		Uptime float64 `json:"uptime"`
		Users  struct {
			StatusCounts map[string]float64 `json:"statusCounts"`
			TotalUsers   float64            `json:"totalUsers"`
		} `json:"users"`
		OnlineStats struct {
			LastDay     float64 `json:"lastDay"`
			LastWeek    float64 `json:"lastWeek"`
			NeverOnline float64 `json:"neverOnline"`
			OnlineNow   float64 `json:"onlineNow"`
		} `json:"onlineStats"`
		Nodes struct {
			TotalOnline        float64 `json:"totalOnline"`
			TotalBytesLifetime string  `json:"totalBytesLifetime"`
		} `json:"nodes"`
	} `json:"response"`
}

type bandwidthBody struct {
	Response struct {
		BandwidthLastTwoDays   ByteWindow `json:"bandwidthLastTwoDays"`
		BandwidthLastSevenDays ByteWindow `json:"bandwidthLastSevenDays"`
		BandwidthLast30Days    ByteWindow `json:"bandwidthLast30Days"`
		BandwidthCalendarMonth ByteWindow `json:"bandwidthCalendarMonth"`
		BandwidthCurrentYear   ByteWindow `json:"bandwidthCurrentYear"`
	} `json:"response"`
}

// FetchHome loads health, system stats and bandwidth for a Remnawave panel.
func (c *Client) FetchHome(ctx context.Context, baseURL, apiKey, tz string) HomeSnapshot {
	tz = strings.TrimSpace(tz)
	q := ""
	if tz != "" {
		q = "?tz=" + url.QueryEscape(tz)
	}
	var (
		health healthBody
		stats  statsBody
		bw     bandwidthBody
		errH   error
		errS   error
		errB   error
		wg     sync.WaitGroup
	)
	wg.Add(3)
	go func() {
		defer wg.Done()
		errH = c.getJSON(ctx, baseURL, apiKey, "/api/system/health", &health)
	}()
	go func() {
		defer wg.Done()
		errS = c.getJSON(ctx, baseURL, apiKey, "/api/system/stats"+q, &stats)
	}()
	go func() {
		defer wg.Done()
		errB = c.getJSON(ctx, baseURL, apiKey, "/api/system/stats/bandwidth"+q, &bw)
	}()
	wg.Wait()

	var out HomeSnapshot
	if errH != nil {
		out.HealthError = errH.Error()
	} else {
		out.Processes = health.Response.RuntimeMetrics
	}
	if errS != nil {
		out.StatsError = errS.Error()
	} else {
		r := stats.Response
		out.Memory.Total = r.Memory.Total
		out.Memory.Free = r.Memory.Free
		out.Memory.Used = r.Memory.Used
		out.CPUCores = r.CPU.Cores
		out.Uptime = r.Uptime
		out.UsersTotal = int(r.Users.TotalUsers)
		out.UsersActive = int(r.Users.StatusCounts["ACTIVE"])
		out.UsersExpired = int(r.Users.StatusCounts["EXPIRED"])
		out.UsersLimited = int(r.Users.StatusCounts["LIMITED"])
		out.UsersDisabled = int(r.Users.StatusCounts["DISABLED"])
		out.OnlineNow = int(r.OnlineStats.OnlineNow)
		out.OnlineLastDay = int(r.OnlineStats.LastDay)
		out.OnlineLastWeek = int(r.OnlineStats.LastWeek)
		out.NeverOnline = int(r.OnlineStats.NeverOnline)
		out.NodesOnline = int(r.Nodes.TotalOnline)
		out.NodesTraffic = r.Nodes.TotalBytesLifetime
	}
	if errB != nil {
		out.BandwidthError = errB.Error()
	} else {
		out.BandwidthToday = bw.Response.BandwidthLastTwoDays
		out.BandwidthWeek = bw.Response.BandwidthLastSevenDays
		out.Bandwidth30Days = bw.Response.BandwidthLast30Days
		out.BandwidthMonth = bw.Response.BandwidthCalendarMonth
		out.BandwidthYear = bw.Response.BandwidthCurrentYear
	}
	return out
}
