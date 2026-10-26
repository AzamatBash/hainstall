package hostplan

import (
	"fmt"
	"os"
	"strings"
)

// Occupant is one process already bound to a TCP port.
type Occupant struct {
	Port  int
	Owner string
}

// PortConflict reports a listener that is not this node's HAProxy.
// alreadyOurs is true when the port is already published for this node:
// docker-proxy on that port is the managed publish, not a foreign service.
// A new port held by anything else is a conflict.
func PortConflict(port int, occupants []Occupant, alreadyOurs bool) error {
	for _, o := range occupants {
		if o.Port != port {
			continue
		}
		owner := strings.TrimPrefix(strings.TrimSpace(o.Owner), "/")
		if owner == "haproxy" || strings.Contains(owner, "haproxy") {
			continue
		}
		if alreadyOurs && (owner == "" || owner == "docker-proxy") {
			continue
		}
		if owner == "" {
			owner = "unknown"
		}
		return fmt.Errorf("порт %d занят %s", port, owner)
	}
	return nil
}

// ParseListeners reads `ss -ltnp` text. The process name is empty when ss
// could not see it.
func ParseListeners(ss string) []Occupant {
	var out []Occupant
	for _, line := range strings.Split(ss, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "State") || strings.HasPrefix(line, "Netid") {
			continue
		}
		fields := strings.Fields(line)
		local := ""
		for _, f := range fields {
			if strings.HasPrefix(f, "users:") {
				break
			}
			if portOf(f) > 0 {
				local = f
				break
			}
		}
		port := portOf(local)
		if port == 0 {
			continue
		}
		out = append(out, Occupant{Port: port, Owner: processName(line)})
	}
	return out
}

func portOf(local string) int {
	if local == "" {
		return 0
	}
	i := strings.LastIndex(local, ":")
	if i < 0 || i == len(local)-1 {
		return 0
	}
	n := 0
	for _, c := range local[i+1:] {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	if n < 1 || n > 65535 {
		return 0
	}
	return n
}

func processName(line string) string {
	const mark = `users:(("`
	i := strings.Index(line, mark)
	if i < 0 {
		return ""
	}
	rest := line[i+len(mark):]
	j := strings.Index(rest, `"`)
	if j <= 0 {
		return ""
	}
	return rest[:j]
}

// ApplyEffect is what the operator should expect before confirming.
func ApplyEffect(portsChanged bool) string {
	if portsChanged {
		return "recreate"
	}
	return "reload"
}

// BackupFile copies path to path+".hapanel.bak".
func BackupFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	bak := path + ".hapanel.bak"
	if err := os.WriteFile(bak, data, 0o644); err != nil {
		return "", err
	}
	return bak, nil
}

// RestoreFile copies the backup back over path.
func RestoreFile(backup, path string) error {
	data, err := os.ReadFile(backup)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// KeepConntrackMax returns the operator's stored limit, or the live kernel
// value when the panel has never set one. It does not invent a higher max.
func KeepConntrackMax(stored, live int64) int64 {
	if stored <= 0 {
		return live
	}
	return stored
}

// ConntrackFitsMemory refuses a table that would take more than a quarter of
// MemAvailable. An entry is counted as 320 bytes. It never raises the limit.
func ConntrackFitsMemory(requested, memAvailable uint64) error {
	if requested == 0 || memAvailable == 0 {
		return fmt.Errorf("нет данных, чтобы оценить память под conntrack")
	}
	need := requested * 320
	if need > memAvailable/4 {
		return fmt.Errorf("conntrack %d требует %d байт, доступно %d", requested, need, memAvailable)
	}
	return nil
}

// ParseConntrack reads count and max. Percent is 0 when max is 0.
func ParseConntrack(countRaw, maxRaw string) (count, max int64, pct float64, err error) {
	count, err = parseInt(countRaw)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("count: %w", err)
	}
	max, err = parseInt(maxRaw)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("max: %w", err)
	}
	if max > 0 {
		pct = float64(count) * 100 / float64(max)
	}
	return count, max, pct, nil
}

func parseInt(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("empty")
	}
	var n int64
	for _, c := range raw {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("bad integer %q", raw)
		}
		n = n*10 + int64(c-'0')
	}
	return n, nil
}

// AgentIngress is an nftables snippet for NEW connections to the agent port.
// It does not flush tables and does not touch the output path.
func AgentIngress(panelCIDRs []string, port int) (string, error) {
	if port < 1 || port > 65535 {
		return "", fmt.Errorf("agent port %d", port)
	}
	if len(panelCIDRs) == 0 {
		return "", fmt.Errorf("нет адресов панели")
	}
	var b strings.Builder
	b.WriteString("table inet hapanel {\n")
	b.WriteString("  chain input {\n")
	b.WriteString("    type filter hook input priority 0; policy accept;\n")
	b.WriteString("    ct state established,related accept\n")
	for _, cidr := range panelCIDRs {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" || strings.ContainsAny(cidr, " ;{}\n") {
			return "", fmt.Errorf("плохой адрес панели %q", cidr)
		}
		fmt.Fprintf(&b, "    ct state new ip saddr %s tcp dport %d accept\n", cidr, port)
	}
	fmt.Fprintf(&b, "    ct state new tcp dport %d drop\n", port)
	b.WriteString("  }\n}\n")
	return b.String(), nil
}

// AlertLevel maps a percent of a known limit to a band.
// Metrics without a max must not call this.
func AlertLevel(pct float64) string {
	switch {
	case pct >= 95:
		return "crit"
	case pct >= 85:
		return "high"
	case pct >= 70:
		return "warn"
	default:
		return ""
	}
}

// AlertHeld fires only after the caller has seen the band long enough (hold).
// A latched band stays until pct falls 5 points under its threshold, so a
// value flickering around 70, 85, or 95 does not flap. Nothing here restarts
// a service or raises a limit.
func AlertHeld(pct float64, latched string, hold bool) (level string, latch string) {
	raw := AlertLevel(pct)
	if latched != "" && pct >= clearBelow(latched) {
		if hold && severity(raw) > severity(latched) {
			return raw, raw
		}
		return latched, latched
	}
	if raw != "" && hold {
		return raw, raw
	}
	if raw != "" {
		return "", latched
	}
	return "", ""
}

func severity(level string) int {
	switch level {
	case "warn":
		return 1
	case "high":
		return 2
	case "crit":
		return 3
	default:
		return 0
	}
}

func clearBelow(latched string) float64 {
	switch latched {
	case "crit":
		return 90
	case "high":
		return 80
	default:
		return 65
	}
}
