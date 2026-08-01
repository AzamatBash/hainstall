// Package sysinfo collects lightweight host metrics for the node agent.
// When running in Docker, mount host /proc (and optionally /) and set
// HOST_PROC / HOST_ROOT so readings reflect the node, not the container.
package sysinfo

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Metrics is the JSON payload for GET /_hapctl/v1/system.
type Metrics struct {
	CPUPercent     float64   `json:"cpu_percent"`
	MemUsedBytes   uint64    `json:"mem_used_bytes"`
	MemTotalBytes  uint64    `json:"mem_total_bytes"`
	MemPercent     float64   `json:"mem_percent"`
	LoadAvg        []float64 `json:"load_avg,omitempty"`
	UptimeSeconds  float64   `json:"uptime_seconds"`
	DiskUsedBytes  uint64    `json:"disk_used_bytes,omitempty"`
	DiskTotalBytes uint64    `json:"disk_total_bytes,omitempty"`
	DiskPercent    float64   `json:"disk_percent,omitempty"`
	Timestamp      time.Time `json:"timestamp"`
}

// Collect gathers CPU, memory, load, uptime and optional root disk usage.
func Collect() (Metrics, error) {
	proc := hostProc()
	m := Metrics{Timestamp: time.Now().UTC()}

	cpu, err := cpuPercent(proc, 150*time.Millisecond)
	if err != nil {
		return m, fmt.Errorf("cpu: %w", err)
	}
	m.CPUPercent = cpu

	used, total, err := memInfo(proc)
	if err != nil {
		return m, fmt.Errorf("mem: %w", err)
	}
	m.MemUsedBytes = used
	m.MemTotalBytes = total
	if total > 0 {
		m.MemPercent = float64(used) * 100 / float64(total)
	}

	if load, err := loadAvg(proc); err == nil {
		m.LoadAvg = load
	}
	if up, err := uptime(proc); err == nil {
		m.UptimeSeconds = up
	}
	if dUsed, dTotal, ok := diskUsage(hostRoot()); ok {
		m.DiskUsedBytes = dUsed
		m.DiskTotalBytes = dTotal
		if dTotal > 0 {
			m.DiskPercent = float64(dUsed) * 100 / float64(dTotal)
		}
	}
	return m, nil
}

func hostProc() string {
	if v := os.Getenv("HOST_PROC"); v != "" {
		return v
	}
	return "/proc"
}

func hostRoot() string {
	if v := os.Getenv("HOST_ROOT"); v != "" {
		return v
	}
	return "/"
}

type cpuSample struct {
	idle  uint64
	total uint64
}

func readCPU(proc string) (cpuSample, error) {
	f, err := os.Open(filepath.Join(proc, "stat"))
	if err != nil {
		return cpuSample{}, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return cpuSample{}, fmt.Errorf("empty %s/stat", proc)
	}
	fields := strings.Fields(sc.Text())
	// cpu user nice system idle iowait irq softirq steal ...
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuSample{}, fmt.Errorf("bad cpu line")
	}
	var vals []uint64
	for _, f := range fields[1:] {
		n, _ := strconv.ParseUint(f, 10, 64)
		vals = append(vals, n)
	}
	var total uint64
	for _, v := range vals {
		total += v
	}
	idle := vals[3]
	if len(vals) > 4 {
		idle += vals[4] // iowait
	}
	return cpuSample{idle: idle, total: total}, nil
}

func cpuPercent(proc string, wait time.Duration) (float64, error) {
	a, err := readCPU(proc)
	if err != nil {
		return 0, err
	}
	time.Sleep(wait)
	b, err := readCPU(proc)
	if err != nil {
		return 0, err
	}
	dt := float64(b.total - a.total)
	if dt <= 0 {
		return 0, nil
	}
	di := float64(b.idle - a.idle)
	pct := (1 - di/dt) * 100
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct, nil
}

func memInfo(proc string) (used, total uint64, err error) {
	f, err := os.Open(filepath.Join(proc, "meminfo"))
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	var memTotal, memAvailable uint64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			memTotal = parseMemKB(line) * 1024
		case strings.HasPrefix(line, "MemAvailable:"):
			memAvailable = parseMemKB(line) * 1024
		}
	}
	if memTotal == 0 {
		return 0, 0, fmt.Errorf("MemTotal missing")
	}
	if memAvailable > memTotal {
		memAvailable = memTotal
	}
	return memTotal - memAvailable, memTotal, nil
}

func parseMemKB(line string) uint64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	n, _ := strconv.ParseUint(fields[1], 10, 64)
	return n
}

func loadAvg(proc string) ([]float64, error) {
	b, err := os.ReadFile(filepath.Join(proc, "loadavg"))
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(string(b))
	if len(fields) < 3 {
		return nil, fmt.Errorf("bad loadavg")
	}
	out := make([]float64, 3)
	for i := 0; i < 3; i++ {
		out[i], _ = strconv.ParseFloat(fields[i], 64)
	}
	return out, nil
}

func uptime(proc string) (float64, error) {
	b, err := os.ReadFile(filepath.Join(proc, "uptime"))
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(b))
	if len(fields) < 1 {
		return 0, fmt.Errorf("bad uptime")
	}
	return strconv.ParseFloat(fields[0], 64)
}

func diskUsage(root string) (used, total uint64, ok bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(root, &st); err != nil {
		return 0, 0, false
	}
	total = st.Blocks * uint64(st.Bsize)
	free := st.Bavail * uint64(st.Bsize)
	if total == 0 {
		return 0, 0, false
	}
	used = total - free
	return used, total, true
}
