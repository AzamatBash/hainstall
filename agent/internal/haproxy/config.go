package haproxy

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/azabash/hapanel/agent/internal/store"
)

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
// Main haproxy.cfg must `include /etc/haproxy/backends.d/app.cfg` (and optionally others).
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
		if e.Name() == w.FileName {
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
	b.WriteString("    option ssl-hello-chk\n")
	for _, s := range servers {
		weight := s.Weight
		if weight <= 0 {
			weight = 100
		}
		fmt.Fprintf(&b, "    server %s %s:%d check weight %d\n",
			s.Name, s.Address, s.Port, weight)
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
