package haproxy

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/azabash/hapanel/agent/internal/store"
)

// EntrancesSatisfied reports whether prev already binds every entrance to its
// backend. Extra frontends in the file do not count as a mismatch: startup
// must not rewrite a tuned config just to match the template.
func EntrancesSatisfied(prev string, ents []store.Entrance) bool {
	if strings.TrimSpace(prev) == "" {
		return false
	}
	binds := frontendBinds(prev)
	if len(binds) == 0 {
		return false
	}
	for _, e := range ents {
		if binds[e.Port] != e.Backend {
			return false
		}
	}
	return true
}

func frontendBinds(body string) map[int]string {
	out := map[int]string{}
	var port int
	var backend string
	flush := func() {
		if port > 0 && backend != "" {
			out[port] = backend
		}
		port = 0
		backend = ""
	}
	for _, line := range strings.Split(body, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "frontend ") {
			flush()
			continue
		}
		if strings.HasPrefix(trim, "backend ") || strings.HasPrefix(trim, "global") || strings.HasPrefix(trim, "defaults") {
			flush()
			continue
		}
		if strings.HasPrefix(trim, "bind ") {
			port = bindPort(trim)
		}
		if strings.HasPrefix(trim, "default_backend ") {
			backend = strings.TrimSpace(strings.TrimPrefix(trim, "default_backend"))
		}
	}
	flush()
	return out
}

func bindPort(line string) int {
	// bind *:8443  or bind :8443
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	addr := fields[1]
	i := strings.LastIndex(addr, ":")
	if i < 0 || i == len(addr)-1 {
		return 0
	}
	n, err := strconv.Atoi(addr[i+1:])
	if err != nil || n < 1 || n > 65535 {
		return 0
	}
	return n
}

type keptBase struct {
	globalMaxconn string
	nbthread      string
	hardStop      string
	timeouts      []string
	retries       string
	options       []string
	frontendMax   string
	frontendRules []string
	sawRules      bool
}

func scrapeBase(prev string) keptBase {
	var k keptBase
	section := ""
	rulesTaken := false
	for _, line := range strings.Split(prev, "\n") {
		trim := strings.TrimSpace(line)
		switch {
		case trim == "global" || strings.HasPrefix(trim, "global "):
			section = "global"
			continue
		case trim == "defaults" || strings.HasPrefix(trim, "defaults "):
			section = "defaults"
			continue
		case strings.HasPrefix(trim, "frontend "):
			section = "frontend"
			continue
		case strings.HasPrefix(trim, "backend "):
			section = "backend"
			continue
		}
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		switch section {
		case "global":
			switch {
			case strings.HasPrefix(trim, "maxconn "):
				k.globalMaxconn = strings.TrimPrefix(trim, "maxconn ")
			case strings.HasPrefix(trim, "nbthread "):
				k.nbthread = strings.TrimPrefix(trim, "nbthread ")
			case strings.HasPrefix(trim, "hard-stop-after "):
				k.hardStop = strings.TrimPrefix(trim, "hard-stop-after ")
			}
		case "defaults":
			switch {
			case strings.HasPrefix(trim, "timeout "):
				k.timeouts = append(k.timeouts, "    "+trim)
			case strings.HasPrefix(trim, "retries "):
				k.retries = "    " + trim
			case strings.HasPrefix(trim, "option ") || trim == "no log":
				k.options = append(k.options, "    "+trim)
			}
		case "frontend":
			if rulesTaken {
				continue
			}
			switch {
			case strings.HasPrefix(trim, "maxconn "):
				k.frontendMax = strings.TrimPrefix(trim, "maxconn ")
			case strings.HasPrefix(trim, "tcp-request ") || strings.HasPrefix(trim, "acl ") || strings.HasPrefix(trim, "stick-table "):
				k.frontendRules = append(k.frontendRules, "    "+trim)
				k.sawRules = true
			case strings.HasPrefix(trim, "default_backend "):
				rulesTaken = true
			}
		}
	}
	return k
}

// BaseConfigBodyKeeping renders frontends for ents. Timeouts, retries, maxconn
// and the TLS/tcp-request block are copied from prev when that file already
// has them. replaceRules uses protect instead of the lines on disk.
func BaseConfigBodyKeeping(prev string, ents []store.Entrance, protect ProtectOpts, replaceRules bool) string {
	if strings.TrimSpace(prev) == "" {
		return BaseConfigBody(ents, protect)
	}
	k := scrapeBase(prev)
	fresh := BaseConfigBody(ents, protect)
	if k.globalMaxconn == "" && k.nbthread == "" && k.hardStop == "" && len(k.timeouts) == 0 && k.retries == "" && k.frontendMax == "" && !k.sawRules {
		return fresh
	}
	nb := k.nbthread
	if nb == "" {
		nb = fmt.Sprint(nbthreadCount())
	}
	hard := k.hardStop
	if hard == "" {
		hard = "5m"
	}
	gmax := k.globalMaxconn
	if gmax == "" {
		gmax = "50000"
	}
	fmax := k.frontendMax
	if fmax == "" {
		fmax = "40000"
	}
	var defaults strings.Builder
	defaults.WriteString("defaults\n    mode    tcp\n")
	if len(k.options) == 0 {
		defaults.WriteString("    no log\n    option  splice-auto\n")
	} else {
		for _, line := range k.options {
			defaults.WriteString(line)
			defaults.WriteByte('\n')
		}
	}
	if len(k.timeouts) == 0 {
		defaults.WriteString("    timeout connect 5s\n    timeout client  30m\n    timeout server  30m\n    timeout tunnel  30m\n    timeout client-fin 30s\n    timeout server-fin 30s\n")
	} else {
		for _, line := range k.timeouts {
			defaults.WriteString(line)
			defaults.WriteByte('\n')
		}
	}
	if k.retries != "" {
		defaults.WriteString(k.retries)
		defaults.WriteByte('\n')
	} else {
		defaults.WriteString("    retries 2\n")
	}

	list, err := store.NormalizeEntrances(ents)
	if err != nil || len(list) == 0 {
		list = []store.Entrance{store.DefaultEntrance}
	}
	rules := ""
	if replaceRules || !k.sawRules {
		rules = protect.frontendExtras()
	} else {
		rules = strings.Join(k.frontendRules, "\n")
		if rules != "" {
			rules += "\n"
		}
	}
	var fronts strings.Builder
	for _, e := range list {
		fmt.Fprintf(&fronts, `
frontend %s
    mode tcp
    bind *:%d
    maxconn %s
%s    default_backend %s
`, FrontendName(e.Port), e.Port, fmax, rules, e.Backend)
	}
	return fmt.Sprintf(`# Managed by hapanel agent — do not edit by hand
# Client frontends live here (not a host bind-mounted haproxy.cfg).
global
    maxconn %s
    nbthread %s
    hard-stop-after %s
    stats socket ipv4@0.0.0.0:9999 level admin
    stats timeout 30s
    master-worker

%s%s`, gmax, nb, hard, defaults.String(), fronts.String())
}
