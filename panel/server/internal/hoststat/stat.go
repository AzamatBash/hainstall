package hoststat

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var started = time.Now()

// Snapshot is the hapanel process plus the machine it runs on.
type Snapshot struct {
	Panel Panel `json:"panel"`
	Host  Host  `json:"host"`
}

type Panel struct {
	PID        int     `json:"pid"`
	UptimeSec  float64 `json:"uptime_sec"`
	Goroutines int     `json:"goroutines"`
	HeapAlloc  uint64  `json:"heap_alloc"`
	HeapSys    uint64  `json:"heap_sys"`
	Sys        uint64  `json:"sys"`
	NumGC      uint32  `json:"num_gc"`
	GoVersion  string  `json:"go_version"`
}

type Host struct {
	Hostname  string  `json:"hostname"`
	UptimeSec float64 `json:"uptime_sec"`
	CPUCores  int     `json:"cpu_cores"`
	Load1     float64 `json:"load1"`
	Load5     float64 `json:"load5"`
	Load15    float64 `json:"load15"`
	MemTotal  uint64  `json:"mem_total"`
	MemUsed   uint64  `json:"mem_used"`
	DiskTotal uint64  `json:"disk_total"`
	DiskUsed  uint64  `json:"disk_used"`
	DiskPath  string  `json:"disk_path"`
}

func Read() Snapshot {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	memTotal, memAvail := readMem()
	used := uint64(0)
	if memTotal > memAvail {
		used = memTotal - memAvail
	}
	diskPath, diskTotal, diskFree := readDisk()
	diskUsed := uint64(0)
	if diskTotal > diskFree {
		diskUsed = diskTotal - diskFree
	}
	l1, l5, l15 := readLoad()
	return Snapshot{
		Panel: Panel{
			PID:        os.Getpid(),
			UptimeSec:  time.Since(started).Seconds(),
			Goroutines: runtime.NumGoroutine(),
			HeapAlloc:  ms.HeapAlloc,
			HeapSys:    ms.HeapSys,
			Sys:        ms.Sys,
			NumGC:      ms.NumGC,
			GoVersion:  runtime.Version(),
		},
		Host: Host{
			Hostname:  readHostname(),
			UptimeSec: readUptime(),
			CPUCores:  readCPU(),
			Load1:     l1,
			Load5:     l5,
			Load15:    l15,
			MemTotal:  memTotal,
			MemUsed:   used,
			DiskTotal: diskTotal,
			DiskUsed:  diskUsed,
			DiskPath:  diskPath,
		},
	}
}

func procPath(name string) string {
	host := "/host/proc/" + name
	if _, err := os.Stat(host); err == nil {
		return host
	}
	return "/proc/" + name
}

func readMem() (total, avail uint64) {
	f, err := os.Open(procPath("meminfo"))
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, val, ok := memLine(sc.Text())
		if !ok {
			continue
		}
		switch key {
		case "MemTotal":
			total = val
		case "MemAvailable":
			avail = val
		}
	}
	return total, avail
}

func memLine(line string) (string, uint64, bool) {
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return "", 0, false
	}
	key := strings.TrimSuffix(parts[0], ":")
	n, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return "", 0, false
	}
	// meminfo values are KiB.
	return key, n * 1024, true
}

func readLoad() (float64, float64, float64) {
	b, err := os.ReadFile(procPath("loadavg"))
	if err != nil {
		return 0, 0, 0
	}
	parts := strings.Fields(string(b))
	if len(parts) < 3 {
		return 0, 0, 0
	}
	a, _ := strconv.ParseFloat(parts[0], 64)
	c, _ := strconv.ParseFloat(parts[1], 64)
	d, _ := strconv.ParseFloat(parts[2], 64)
	return a, c, d
}

func readUptime() float64 {
	b, err := os.ReadFile(procPath("uptime"))
	if err != nil {
		return 0
	}
	parts := strings.Fields(string(b))
	if len(parts) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(parts[0], 64)
	return v
}

func readCPU() int {
	b, err := os.ReadFile(procPath("cpuinfo"))
	if err != nil {
		return runtime.NumCPU()
	}
	n := 0
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "processor") {
			n++
		}
	}
	if n == 0 {
		return runtime.NumCPU()
	}
	return n
}

func readHostname() string {
	for _, path := range []string{"/host/root/etc/hostname", "/etc/hostname"} {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		name := strings.TrimSpace(string(b))
		if name != "" {
			return name
		}
	}
	name, err := os.Hostname()
	if err != nil {
		return ""
	}
	return name
}

func readDisk() (path string, total, free uint64) {
	for _, candidate := range []string{"/host/root", "/"} {
		st, ok := statfs(candidate)
		if !ok || st.total == 0 {
			continue
		}
		return candidate, st.total, st.free
	}
	return "", 0, 0
}

type diskSpace struct {
	total uint64
	free  uint64
}

func statfs(path string) (diskSpace, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return diskSpace{}, false
	}
	bsize := uint64(st.Bsize)
	if bsize == 0 {
		return diskSpace{}, false
	}
	return diskSpace{
		total: st.Blocks * bsize,
		free:  st.Bavail * bsize,
	}, true
}
