package haproxy

import (
	"fmt"
	"strconv"
	"strings"
)

// FrontendStat summarizes a frontend from "show stat".
type FrontendStat struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Sessions int    `json:"sessions"`
	ReqRate  int    `json:"req_rate"`
	BytesIn  int64  `json:"bytes_in"`
	BytesOut int64  `json:"bytes_out"`
}

// BackendStat summarizes a backend from "show stat".
type BackendStat struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Sessions int    `json:"sessions"`
	Servers  int    `json:"servers_up"`
	BytesIn  int64  `json:"bytes_in"`
	BytesOut int64  `json:"bytes_out"`
}

// Stats is the aggregated view returned by GET /stats.
type Stats struct {
	ActiveConnections int            `json:"active_connections"`
	ActiveSessions    int            `json:"active_sessions"`
	BytesIn           int64          `json:"bytes_in"`
	BytesOut          int64          `json:"bytes_out"`
	Frontends         []FrontendStat `json:"frontends"`
	Backends          []BackendStat  `json:"backends"`
}

// ServerInfo is a server row from stats or servers state.
type ServerInfo struct {
	Backend string `json:"backend"`
	Name    string `json:"name"`
	Address string `json:"address"`
	Port    int    `json:"port"`
	Weight  int    `json:"weight"`
	Status  string `json:"status"`
}

// csvField indexes for "show stat" (HAProxy CSV).
// See https://docs.haproxy.org/3.0/management.html#9.1
const (
	statPxName  = 0
	statSvName  = 1
	statScur    = 4
	statBin     = 8
	statBout    = 9
	statStatus  = 17
	statWeight  = 18
	statReqRate = 46
)

// GetStats parses "show stat" into a summary.
func (c *Client) GetStats() (Stats, error) {
	raw, err := c.Exec("show stat")
	if err != nil {
		return Stats{}, err
	}
	return ParseStats(raw), nil
}

// ParseStats converts HAProxy CSV stats into Stats.
func ParseStats(raw string) Stats {
	var out Stats
	backendServersUp := map[string]int{}
	backendSessions := map[string]int{}
	backendStatus := map[string]string{}
	backendBin := map[string]int64{}
	backendBout := map[string]int64{}

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := splitCSV(line)
		if len(fields) < 19 {
			continue
		}
		px := fields[statPxName]
		sv := fields[statSvName]
		status := fields[statStatus]
		scur, _ := strconv.Atoi(fields[statScur])
		bin := parseInt64Field(fields, statBin)
		bout := parseInt64Field(fields, statBout)

		switch sv {
		case "FRONTEND":
			reqRate := 0
			if len(fields) > statReqRate {
				reqRate, _ = strconv.Atoi(fields[statReqRate])
			}
			out.Frontends = append(out.Frontends, FrontendStat{
				Name:     px,
				Status:   status,
				Sessions: scur,
				ReqRate:  reqRate,
				BytesIn:  bin,
				BytesOut: bout,
			})
			out.ActiveSessions += scur
			out.ActiveConnections += scur
			out.BytesIn += bin
			out.BytesOut += bout
		case "BACKEND":
			backendSessions[px] = scur
			backendStatus[px] = status
			backendBin[px] = bin
			backendBout[px] = bout
			out.ActiveSessions += scur
		default:
			if status == "UP" {
				backendServersUp[px]++
			}
		}
	}

	for name, sessions := range backendSessions {
		out.Backends = append(out.Backends, BackendStat{
			Name:     name,
			Status:   backendStatus[name],
			Sessions: sessions,
			Servers:  backendServersUp[name],
			BytesIn:  backendBin[name],
			BytesOut: backendBout[name],
		})
	}
	return out
}

func parseInt64Field(fields []string, idx int) int64 {
	if len(fields) <= idx {
		return 0
	}
	v, _ := strconv.ParseInt(fields[idx], 10, 64)
	return v
}

// ListServers returns servers preferring "show servers state" (has addr),
// falling back to "show stat".
func (c *Client) ListServers() ([]ServerInfo, error) {
	raw, err := c.Exec("show servers state")
	if err == nil {
		if servers := ParseServersState(raw); len(servers) > 0 {
			return servers, nil
		}
	}
	raw, err = c.Exec("show stat")
	if err != nil {
		return nil, err
	}
	return ParseServersFromStat(raw), nil
}

// ParseServersState parses "show servers state" output.
// Format (space-separated, versioned header):
//
//	1
//	# be_id be_name srv_id srv_name srv_addr ... [srv_fqdn srv_port ...]
//
// HAProxy 2.4+/3.x often puts IP in srv_addr and port in a separate srv_port
// column (not "ip:port"). Older dumps may still use addr:port in srv_addr.
func ParseServersState(raw string) []ServerInfo {
	var out []ServerInfo
	// Default column indexes for classic layout (pre-srv_port header).
	idx := map[string]int{
		"be_name":      1,
		"srv_name":     3,
		"srv_addr":     4,
		"srv_op_state": 5,
		"srv_uweight":  7,
		"srv_port":     -1,
	}
	headerDone := false

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Version line is a bare integer (e.g. "1").
		if !headerDone && isDigitsOnly(line) {
			continue
		}
		if strings.HasPrefix(line, "#") {
			header := strings.TrimSpace(strings.TrimPrefix(line, "#"))
			cols := strings.Fields(header)
			if len(cols) >= 5 && cols[0] == "be_id" {
				for i, c := range cols {
					idx[c] = i
				}
				headerDone = true
			}
			continue
		}
		fields := strings.Fields(line)
		// Minimum: be_id be_name srv_id srv_name srv_addr srv_op_state srv_admin_state srv_uweight ...
		if len(fields) < 8 {
			continue
		}
		beName := fieldAt(fields, idx["be_name"])
		srvName := fieldAt(fields, idx["srv_name"])
		rawAddr := fieldAt(fields, idx["srv_addr"])
		if beName == "" || srvName == "" {
			continue
		}
		addr, port := splitAddr(rawAddr)
		if port == 0 {
			if p := fieldAt(fields, idx["srv_port"]); p != "" && p != "-" {
				port, _ = strconv.Atoi(p)
			}
		}
		weight, _ := strconv.Atoi(fieldAt(fields, idx["srv_uweight"]))
		opState := fieldAt(fields, idx["srv_op_state"])
		out = append(out, ServerInfo{
			Backend: beName,
			Name:    srvName,
			Address: addr,
			Port:    port,
			Weight:  weight,
			Status:  mapOpState(opState),
		})
	}
	return out
}

func fieldAt(fields []string, i int) string {
	if i < 0 || i >= len(fields) {
		return ""
	}
	return fields[i]
}

func isDigitsOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ParseServersFromStat extracts server rows from "show stat" CSV (no address guarantee).
func ParseServersFromStat(raw string) []ServerInfo {
	var out []ServerInfo
	// Discover addr column from header if present.
	addrIdx := -1
	for _, line := range strings.Split(raw, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "#") && strings.Contains(trim, "pxname") {
			header := strings.TrimPrefix(trim, "#")
			header = strings.TrimSpace(header)
			cols := splitCSV(header)
			for i, c := range cols {
				if c == "addr" {
					addrIdx = i
					break
				}
			}
			continue
		}
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		fields := splitCSV(trim)
		if len(fields) < 19 {
			continue
		}
		sv := fields[statSvName]
		if sv == "FRONTEND" || sv == "BACKEND" {
			continue
		}
		weight, _ := strconv.Atoi(fields[statWeight])
		addr := ""
		port := 0
		if addrIdx >= 0 && len(fields) > addrIdx && fields[addrIdx] != "" {
			addr, port = splitAddr(fields[addrIdx])
		}
		out = append(out, ServerInfo{
			Backend: fields[statPxName],
			Name:    sv,
			Address: addr,
			Port:    port,
			Weight:  weight,
			Status:  fields[statStatus],
		})
	}
	return out
}

// SetServerState enables or disables a server via runtime API.
// state is typically "ready", "maint", or "drain".
func (c *Client) SetServerState(backend, name, state string) error {
	cmd := fmt.Sprintf("set server %s/%s state %s", backend, name, state)
	resp, err := c.Exec(cmd)
	if err != nil {
		return err
	}
	if isRuntimeError(resp) {
		return fmt.Errorf("set server: %s", strings.TrimSpace(resp))
	}
	return nil
}

// AddServerRuntime tries dynamic "add server" (HAProxy 2.5+ with dynamic servers).
func (c *Client) AddServerRuntime(backend, name, address string, port, weight int) error {
	cmd := fmt.Sprintf("add server %s/%s %s:%d check weight %d", backend, name, address, port, weight)
	resp, err := c.Exec(cmd)
	if err != nil {
		return err
	}
	if isRuntimeError(resp) {
		return fmt.Errorf("add server: %s", strings.TrimSpace(resp))
	}
	return nil
}

// DelServerRuntime tries "del server" (HAProxy 2.4+).
func (c *Client) DelServerRuntime(backend, name string) error {
	cmd := fmt.Sprintf("del server %s/%s", backend, name)
	resp, err := c.Exec(cmd)
	if err != nil {
		return err
	}
	if isRuntimeError(resp) {
		return fmt.Errorf("del server: %s", strings.TrimSpace(resp))
	}
	return nil
}

func mapOpState(op string) string {
	// srv_op_state: 0=DOWN, 1=UP (simplified; HAProxy uses bitflags)
	switch op {
	case "0":
		return "DOWN"
	case "1", "2", "3":
		return "UP"
	default:
		return op
	}
}

func splitCSV(line string) []string {
	return strings.Split(line, ",")
}

func splitAddr(s string) (string, int) {
	if strings.HasPrefix(s, "[") {
		end := strings.LastIndex(s, "]")
		if end > 0 && end+1 < len(s) && s[end+1] == ':' {
			port, _ := strconv.Atoi(s[end+2:])
			return s[1:end], port
		}
		return s, 0
	}
	host, portStr, ok := strings.Cut(s, ":")
	if !ok {
		return s, 0
	}
	port, _ := strconv.Atoi(portStr)
	return host, port
}

func isRuntimeError(resp string) bool {
	lower := strings.ToLower(strings.TrimSpace(resp))
	if lower == "" {
		return false
	}
	return strings.HasPrefix(lower, "unknown") ||
		strings.HasPrefix(lower, "can't") ||
		strings.HasPrefix(lower, "cannot") ||
		strings.Contains(lower, "not found") ||
		strings.Contains(lower, "error") ||
		strings.HasPrefix(lower, "require")
}
