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
	// Host NIC counters (sum of non-virtual ifaces). For a transit proxy RX≈TX.
	NetRxBytes uint64 `json:"net_rx_bytes,omitempty"`
	NetTxBytes uint64 `json:"net_tx_bytes,omitempty"`
	// MemAvailable is what the kernel can give without swapping. Used is total minus this.
	MemAvailableBytes uint64 `json:"mem_available_bytes,omitempty"`
	// Conntrack is omitted when the host has no nf_conntrack table.
	// A full table is a known limit. TIME-WAIT is not reported as a fault.
	ConntrackCount        uint64  `json:"conntrack_count,omitempty"`
	ConntrackMax          uint64  `json:"conntrack_max,omitempty"`
	ConntrackPercent      float64 `json:"conntrack_percent,omitempty"`
	ConntrackStat         bool    `json:"conntrack_stat,omitempty"`
	ConntrackInsertFailed uint64  `json:"conntrack_insert_failed,omitempty"`
	ConntrackDrop         uint64  `json:"conntrack_drop,omitempty"`
	ConntrackEarlyDrop    uint64  `json:"conntrack_early_drop,omitempty"`
	FdAllocated           uint64  `json:"fd_allocated,omitempty"`
	FdMax                 uint64  `json:"fd_max,omitempty"`
	TCPStat               bool    `json:"tcp_stat,omitempty"`
	TCPInUse              uint64  `json:"tcp_inuse,omitempty"`
	TCPTimeWait           uint64  `json:"tcp_time_wait,omitempty"`
	SwapUsedBytes         uint64  `json:"swap_used_bytes,omitempty"`
	SwapTotalBytes        uint64  `json:"swap_total_bytes,omitempty"`
	SwapPercent           float64 `json:"swap_percent,omitempty"`
	DiskInodesUsed        uint64  `json:"disk_inodes_used,omitempty"`
	DiskInodesTotal       uint64  `json:"disk_inodes_total,omitempty"`
	PSIStat               bool    `json:"psi,omitempty"`
	PSIMemorySomeAvg10    float64 `json:"psi_memory_some_avg10,omitempty"`
	PSIIOSomeAvg10        float64 `json:"psi_io_some_avg10,omitempty"`
	PSICpuSomeAvg10       float64 `json:"psi_cpu_some_avg10,omitempty"`
	NetRxDrops            uint64  `json:"net_rx_drops,omitempty"`
	NetTxDrops            uint64  `json:"net_tx_drops,omitempty"`
	// Scope is "host": readings come from the host proc mount, not the HAProxy netns.
	Scope     string    `json:"scope,omitempty"`
	Timestamp time.Time `json:"timestamp"`
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
	if total > used {
		m.MemAvailableBytes = total - used
	}
	if total > 0 {
		m.MemPercent = float64(used) * 100 / float64(total)
	}
	if count, max, ok := conntrack(proc); ok && max > 0 {
		m.ConntrackCount = count
		m.ConntrackMax = max
		m.ConntrackPercent = float64(count) * 100 / float64(max)
	}
	if failed, drop, early, ok := conntrackStat(proc); ok {
		m.ConntrackStat = true
		m.ConntrackInsertFailed = failed
		m.ConntrackDrop = drop
		m.ConntrackEarlyDrop = early
	}
	if alloc, max, ok := fileNr(proc); ok && max > 0 {
		m.FdAllocated = alloc
		m.FdMax = max
	}
	if inuse, tw, ok := tcpSockstat(proc); ok {
		m.TCPStat = true
		m.TCPInUse = inuse
		m.TCPTimeWait = tw
	}
	if used, total, ok := swapInfo(proc); ok && total > 0 {
		m.SwapUsedBytes = used
		m.SwapTotalBytes = total
		m.SwapPercent = float64(used) * 100 / float64(total)
	}
	if some, ok := pressureSome(proc, "memory"); ok {
		m.PSIStat = true
		m.PSIMemorySomeAvg10 = some
	}
	if some, ok := pressureSome(proc, "io"); ok {
		m.PSIStat = true
		m.PSIIOSomeAvg10 = some
	}
	if some, ok := pressureSome(proc, "cpu"); ok {
		m.PSIStat = true
		m.PSICpuSomeAvg10 = some
	}
	if rxDrop, txDrop, ok := netDrops(proc); ok {
		m.NetRxDrops = rxDrop
		m.NetTxDrops = txDrop
	}
	if used, total, ok := diskInodes(hostRoot()); ok && total > 0 {
		m.DiskInodesUsed = used
		m.DiskInodesTotal = total
	}
	m.Scope = "host"

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
	if rx, tx, err := netDevTotals(proc); err == nil {
		m.NetRxBytes = rx
		m.NetTxBytes = tx
	}
	return m, nil
}

func hostProc() string {
	if v := os.Getenv("HOST_PROC"); v != "" {
		return v
	}
	return "/proc"
}

func conntrack(proc string) (count, max uint64, ok bool) {
	max, ok = readUintFile(filepath.Join(proc, "sys/net/netfilter/nf_conntrack_max"))
	if !ok || max == 0 {
		return 0, 0, false
	}
	count, ok = readUintFile(filepath.Join(proc, "sys/net/netfilter/nf_conntrack_count"))
	if !ok {
		return 0, max, false
	}
	return count, max, true
}

func readUintFile(path string) (uint64, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	n, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
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

// netDevTotals sums RX/TX from the host network namespace.
// Note: /proc/net is a symlink to /proc/self/net — with HOST_PROC mounted that
// still resolves to the container netns. Use <HOST_PROC>/1/net/dev (host init).
func netDevTotals(proc string) (rx, tx uint64, err error) {
	path := filepath.Join(proc, "1/net/dev")
	f, err := os.Open(path)
	if err != nil {
		// Fallback for bare-metal / non-container runs.
		f, err = os.Open(filepath.Join(proc, "net/dev"))
		if err != nil {
			return 0, 0, err
		}
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		if skipNetIface(name) {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 9 {
			continue
		}
		r, _ := strconv.ParseUint(fields[0], 10, 64)
		t, _ := strconv.ParseUint(fields[8], 10, 64)
		rx += r
		tx += t
	}
	return rx, tx, sc.Err()
}

func skipNetIface(name string) bool {
	switch name {
	case "lo", "docker0":
		return true
	}
	for _, p := range []string{"veth", "br-", "virbr", "tun", "tap", "wg", "flannel", "cni", "cali", "nodelocal"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}
