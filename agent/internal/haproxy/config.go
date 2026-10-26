package haproxy

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/azabash/hapanel/agent/internal/store"
)

// haproxyNameRe matches characters that are unsafe in HAProxy server/backend names.
// Spaces and most punctuation break `server NAME addr:port` parsing.
var haproxyNameRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// SanitizeName turns a free-form label into a HAProxy-safe identifier.
// Empty input becomes "srv".
func SanitizeName(name string) string {
	name = strings.TrimSpace(name)
	name = haproxyNameRe.ReplaceAllString(name, "_")
	name = strings.Trim(name, "._-")
	for strings.Contains(name, "__") {
		name = strings.ReplaceAll(name, "__", "_")
	}
	if name == "" {
		return "srv"
	}
	return name
}

// ConfigWriter regenerates HAProxy include snippets from persisted servers.
// Each file is a full top-level `backend` section — HAProxy only allows
// `include` at the top level, not inside an existing backend block.
type ConfigWriter struct {
	// Dir is the backends.d directory mounted into the haproxy container
	// (e.g. /etc/haproxy/backends.d).
	Dir string
	// FileName is a reserved index filename (not a backend fragment).
	FileName string
}

// NewConfigWriter creates a writer for Dir.
func NewConfigWriter(dir string) *ConfigWriter {
	return &ConfigWriter{Dir: dir, FileName: "managed.cfg"}
}

// Write regenerates one `{backend}.cfg` per backend from the panel state,
// including weight. balances maps backend name → HAProxy balance algorithm
// (empty → leastconn). ensureBackends forces empty backend sections so
// entrances can reference them.
func (w *ConfigWriter) Write(servers []store.Server, balances map[string]string, ensureBackends ...string) error {
	return w.write(servers, balances, false, ensureBackends...)
}

// WriteStartup syncs backends after an agent restart. A local watcher may keep
// a different weight than state.json; rewriting the file wakes that watcher
// and reloads HAProxy. Name and address still have to match.
func (w *ConfigWriter) WriteStartup(servers []store.Server, balances map[string]string, ensureBackends ...string) error {
	return w.write(servers, balances, true, ensureBackends...)
}

func (w *ConfigWriter) write(servers []store.Server, balances map[string]string, keepFileWeight bool, ensureBackends ...string) error {
	if w.Dir == "" {
		return fmt.Errorf("backends dir is empty")
	}
	if err := os.MkdirAll(w.Dir, 0o755); err != nil {
		return fmt.Errorf("mkdir backends.d: %w", err)
	}

	byBackend := map[string][]store.Server{}
	for _, s := range servers {
		byBackend[s.Backend] = append(byBackend[s.Backend], s)
	}

	// Ensure default backend file exists even if empty.
	if _, ok := byBackend["app"]; !ok {
		byBackend["app"] = nil
	}
	for _, name := range ensureBackends {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := byBackend[name]; !ok {
			byBackend[name] = nil
		}
	}

	written := map[string]struct{}{}
	for backend, list := range byBackend {
		sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
		path := filepath.Join(w.Dir, backend+".cfg")
		bal := ""
		if balances != nil {
			bal = balances[backend]
		}
		prev, _ := os.ReadFile(path)
		body, err := renderBackendSection(backend, list, bal)
		if err != nil {
			return err
		}
		body = mergeBackendDirectives(string(prev), body, keepFileWeight)
		if string(prev) == body {
			written[backend+".cfg"] = struct{}{}
			continue
		}
		if err := atomicWrite(path, body); err != nil {
			return err
		}
		written[backend+".cfg"] = struct{}{}
	}

	entries, err := os.ReadDir(w.Dir)
	if err != nil {
		return fmt.Errorf("readdir backends.d: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".cfg") {
			continue
		}
		if e.Name() == w.FileName || e.Name() == RuntimeTCPFile || e.Name() == BaseConfigFile {
			continue
		}
		if _, ok := written[e.Name()]; ok {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(w.Dir, e.Name()))
		if rerr == nil && !strings.Contains(string(raw), "Managed by hapanel agent") {
			continue
		}
		_ = os.Remove(filepath.Join(w.Dir, e.Name()))
	}

	// managed.cfg is only an index marker. Rewriting identical bytes still
	// updates mtime, and host watchers treat that as a config change and
	// SIGUSR2 HAProxy.
	if _, err := writeIfChanged(filepath.Join(w.Dir, w.FileName), managedIndexBody); err != nil {
		return err
	}
	return nil
}

const managedIndexBody = "# Managed by hapanel agent — do not edit by hand\n"

func renderBackendSection(backend string, servers []store.Server, balance string) (string, error) {
	bal, err := NormalizeBalance(balance)
	if err != nil {
		bal = DefaultBalance
	}
	var b strings.Builder
	b.WriteString("# Managed by hapanel agent — do not edit by hand\n")
	fmt.Fprintf(&b, "backend %s\n", backend)
	b.WriteString("    mode tcp\n")
	fmt.Fprintf(&b, "    balance %s\n", bal)
	// No health-check by default: ssl-hello-chk / aggressive checks break Reality
	// (server marked DOWN → clients cannot connect).
	for _, s := range servers {
		weight := s.Weight
		if weight <= 0 {
			weight = 100
		}
		line := fmt.Sprintf("    server %s %s:%d weight %d",
			SanitizeName(s.Name), s.Address, s.Port, weight)
		if SendProxyV2(s.ClientIP) {
			line += " send-proxy-v2"
		}
		suffix, err := CheckSuffix(s)
		if err != nil {
			return "", fmt.Errorf("backend %s server %s: %w", backend, s.Name, err)
		}
		line += suffix
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// mergeBackendDirectives keeps timeout, retries and other non-server lines
// from the previous file, and keeps a server's extra tokens (maxconn, …)
// when the panel did not change that server's address, port or weight.
func mergeBackendDirectives(prev, generated string, keepFileWeight bool) string {
	if strings.TrimSpace(prev) == "" {
		return generated
	}
	// Watcher-owned health checks sit before `weight`. On startup the file
	// weight can also differ from state.json (watcher keeps 25, state says
	// 100). Rewriting either one wakes systemd.path.
	if sameBackendServers(prev, generated, keepFileWeight) {
		return prev
	}
	var extra []string
	oldServers := map[string]string{}
	for _, line := range strings.Split(prev, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") || trim == "backend" || strings.HasPrefix(trim, "backend ") {
			continue
		}
		if strings.HasPrefix(trim, "server ") {
			name := serverName(trim)
			if name != "" {
				oldServers[name] = "    " + trim
			}
			continue
		}
		if strings.HasPrefix(trim, "mode ") || strings.HasPrefix(trim, "balance ") {
			continue
		}
		extra = append(extra, "    "+trim)
	}
	if len(extra) == 0 && len(oldServers) == 0 {
		return generated
	}
	var out []string
	for _, line := range strings.Split(generated, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "server ") {
			name := serverName(trim)
			if old, ok := oldServers[name]; ok && keepOldServer(old, "    "+trim, keepFileWeight) {
				out = append(out, old)
				continue
			}
		}
		if strings.HasPrefix(trim, "balance ") && len(extra) > 0 {
			out = append(out, line)
			out = append(out, extra...)
			extra = nil
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func sameBackendServers(prev, generated string, keepFileWeight bool) bool {
	if balanceOf(prev) != balanceOf(generated) {
		return false
	}
	oldLines := serverLines(prev)
	newLines := serverLines(generated)
	if len(oldLines) != len(newLines) {
		return false
	}
	oldByName := make(map[string]string, len(oldLines))
	for _, line := range oldLines {
		name := serverName(line)
		if name == "" {
			return false
		}
		if _, dup := oldByName[name]; dup {
			return false
		}
		oldByName[name] = line
	}
	for _, line := range newLines {
		old, ok := oldByName[serverName(line)]
		if !ok || !keepOldServer(old, line, keepFileWeight) {
			return false
		}
	}
	return true
}

func serverLines(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "server ") {
			out = append(out, trim)
		}
	}
	return out
}

func balanceOf(body string) string {
	for _, line := range strings.Split(body, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "balance" {
			return fields[1]
		}
	}
	return ""
}

func serverName(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "server" {
		return ""
	}
	return fields[1]
}

func keepOldServer(oldLine, newLine string, keepFileWeight bool) bool {
	old, okOld := parseServerLine(oldLine)
	neu, okNew := parseServerLine(newLine)
	if !okOld || !okNew {
		return false
	}
	if old.name != neu.name || old.addr != neu.addr {
		return false
	}
	if !keepFileWeight && neu.weight != "" && old.weight != neu.weight {
		return false
	}
	if neu.sendProxy && !old.sendProxy {
		return false
	}
	if neu.check && !old.check {
		return false
	}
	return true
}

type parsedServer struct {
	name      string
	addr      string
	weight    string
	check     bool
	sendProxy bool
}

func parseServerLine(line string) (parsedServer, bool) {
	fields := strings.Fields(line)
	if len(fields) < 3 || fields[0] != "server" {
		return parsedServer{}, false
	}
	s := parsedServer{name: fields[1], addr: fields[2]}
	for i := 3; i < len(fields); i++ {
		switch fields[i] {
		case "weight":
			if i+1 < len(fields) {
				s.weight = fields[i+1]
				i++
			}
		case "check":
			s.check = true
		case "send-proxy", "send-proxy-v2":
			s.sendProxy = true
		}
	}
	return s, true
}

func writeIfChanged(path, body string) (bool, error) {
	prev, err := os.ReadFile(path)
	if err == nil && string(prev) == body {
		return false, nil
	}
	if err := atomicWrite(path, body); err != nil {
		return false, err
	}
	return true, nil
}

func atomicWrite(path, body string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename %s: %w", path, err)
	}
	return nil
}
