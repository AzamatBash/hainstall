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

// Write regenerates one `{backend}.cfg` per backend with a full backend section.
// Frontends live in 00-hapanel-base.cfg (written by the agent).
func (w *ConfigWriter) Write(servers []store.Server) error {
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

	written := map[string]struct{}{}
	for backend, list := range byBackend {
		sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
		path := filepath.Join(w.Dir, backend+".cfg")
		body := renderBackendSection(backend, list)
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
		_ = os.Remove(filepath.Join(w.Dir, e.Name()))
	}

	index := "# Managed by hapanel agent — do not edit by hand\n"
	if err := atomicWrite(filepath.Join(w.Dir, w.FileName), index); err != nil {
		return err
	}
	return nil
}

func renderBackendSection(backend string, servers []store.Server) string {
	var b strings.Builder
	b.WriteString("# Managed by hapanel agent — do not edit by hand\n")
	fmt.Fprintf(&b, "backend %s\n", backend)
	b.WriteString("    mode tcp\n")
	b.WriteString("    balance leastconn\n")
	// No health-check by default: ssl-hello-chk / aggressive checks break Reality
	// (server marked DOWN → clients cannot connect).
	for _, s := range servers {
		weight := s.Weight
		if weight <= 0 {
			weight = 100
		}
		fmt.Fprintf(&b, "    server %s %s:%d weight %d\n",
			SanitizeName(s.Name), s.Address, s.Port, weight)
	}
	return b.String()
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
