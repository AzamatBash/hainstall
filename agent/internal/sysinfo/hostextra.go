package sysinfo

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// conntrackStat sums insert_failed, drop and early_drop across CPUs.
// Counters are cumulative since boot. Missing file is not a fault.
func conntrackStat(proc string) (insertFailed, drop, early uint64, ok bool) {
	path := filepath.Join(proc, "1/net/stat/nf_conntrack")
	f, err := os.Open(path)
	if err != nil {
		f, err = os.Open(filepath.Join(proc, "net/stat/nf_conntrack"))
		if err != nil {
			return 0, 0, 0, false
		}
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return 0, 0, 0, false
	}
	header := strings.Fields(sc.Text())
	col := map[string]int{}
	for i, name := range header {
		col[name] = i
	}
	fi, okF := col["insert_failed"]
	fd, okD := col["drop"]
	fe, okE := col["early_drop"]
	if !okF || !okD || !okE {
		return 0, 0, 0, false
	}
	var failed, drops, earlyDrop uint64
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		failed += hexField(fields, fi)
		drops += hexField(fields, fd)
		earlyDrop += hexField(fields, fe)
	}
	return failed, drops, earlyDrop, true
}

func hexField(fields []string, idx int) uint64 {
	if idx < 0 || idx >= len(fields) {
		return 0
	}
	n, err := strconv.ParseUint(fields[idx], 16, 64)
	if err != nil {
		return 0
	}
	return n
}

func fileNr(proc string) (allocated, max uint64, ok bool) {
	b, err := os.ReadFile(filepath.Join(proc, "sys/fs/file-nr"))
	if err != nil {
		return 0, 0, false
	}
	fields := strings.Fields(string(b))
	if len(fields) < 3 {
		return 0, 0, false
	}
	allocated, err = strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	max, err = strconv.ParseUint(fields[2], 10, 64)
	if err != nil || max == 0 {
		return 0, 0, false
	}
	return allocated, max, true
}

func tcpSockstat(proc string) (inuse, timeWait uint64, ok bool) {
	b, err := os.ReadFile(filepath.Join(proc, "1/net/sockstat"))
	if err != nil {
		b, err = os.ReadFile(filepath.Join(proc, "net/sockstat"))
		if err != nil {
			return 0, 0, false
		}
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "TCP:") {
			continue
		}
		fields := strings.Fields(line)
		return fieldAfter(fields, "inuse"), fieldAfter(fields, "tw"), true
	}
	return 0, 0, false
}

func fieldAfter(fields []string, key string) uint64 {
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == key {
			n, _ := strconv.ParseUint(fields[i+1], 10, 64)
			return n
		}
	}
	return 0
}

func swapInfo(proc string) (used, total uint64, ok bool) {
	f, err := os.Open(filepath.Join(proc, "meminfo"))
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	var swapTotal, swapFree uint64
	seen := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "SwapTotal:"):
			swapTotal = parseMemKB(line) * 1024
			seen++
		case strings.HasPrefix(line, "SwapFree:"):
			swapFree = parseMemKB(line) * 1024
			seen++
		}
	}
	if seen < 2 || swapTotal == 0 {
		return 0, 0, false
	}
	if swapFree > swapTotal {
		swapFree = swapTotal
	}
	return swapTotal - swapFree, swapTotal, true
}

func pressureSome(proc, kind string) (float64, bool) {
	b, err := os.ReadFile(filepath.Join(proc, "pressure", kind))
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "some ") {
			continue
		}
		for _, part := range strings.Fields(line) {
			if strings.HasPrefix(part, "avg10=") {
				n, err := strconv.ParseFloat(strings.TrimPrefix(part, "avg10="), 64)
				if err != nil {
					return 0, false
				}
				return n, true
			}
		}
	}
	return 0, false
}

func netDrops(proc string) (rx, tx uint64, ok bool) {
	path := filepath.Join(proc, "1/net/dev")
	f, err := os.Open(path)
	if err != nil {
		f, err = os.Open(filepath.Join(proc, "net/dev"))
		if err != nil {
			return 0, 0, false
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
		if len(fields) < 12 {
			continue
		}
		r, _ := strconv.ParseUint(fields[3], 10, 64)
		t, _ := strconv.ParseUint(fields[11], 10, 64)
		rx += r
		tx += t
		ok = true
	}
	return rx, tx, ok
}

func diskInodes(root string) (used, total uint64, ok bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(root, &st); err != nil || st.Files == 0 {
		return 0, 0, false
	}
	total = st.Files
	free := st.Ffree
	if free > total {
		free = total
	}
	return total - free, total, true
}
