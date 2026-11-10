package haproxy

import (
	"fmt"
	"strings"
)

// StripBackendSections removes backend blocks from a HAProxy config so panel
// profiles only own global/defaults/frontends. Backend servers stay in
// per-backend files regenerated from agent state.
func StripBackendSections(body string) (string, int) {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	inBackend := false
	removed := 0
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		low := strings.ToLower(trim)

		if !inBackend && strings.HasPrefix(low, "backend ") {
			inBackend = true
			removed++
			continue
		}
		if inBackend {
			if isTopLevelSection(line, low) && !strings.HasPrefix(low, "backend ") {
				inBackend = false
				out = append(out, line)
			}
			continue
		}
		out = append(out, line)
	}
	cleaned := strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n"
	return cleaned, removed
}

func isTopLevelSection(raw, low string) bool {
	if strings.TrimSpace(raw) == "" || strings.HasPrefix(strings.TrimSpace(raw), "#") {
		return false
	}
	// Indented lines are never section headers.
	if strings.HasPrefix(raw, " ") || strings.HasPrefix(raw, "\t") {
		return false
	}
	switch {
	case low == "global", low == "defaults", low == "peers", low == "mailers", low == "userlist":
		return true
	case strings.HasPrefix(low, "frontend "),
		strings.HasPrefix(low, "backend "),
		strings.HasPrefix(low, "listen "),
		strings.HasPrefix(low, "resolvers "),
		strings.HasPrefix(low, "cache "),
		strings.HasPrefix(low, "http-errors "):
		return true
	default:
		return false
	}
}

// ValidateBaseProfile does a light structural check on a panel base profile
// (backends must already be stripped).
func ValidateBaseProfile(body string) error {
	body = strings.TrimSpace(body)
	if body == "" {
		return fmt.Errorf("пустой конфиг")
	}
	var hasGlobal, hasDefaults, hasFrontend bool
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		low := strings.ToLower(t)
		switch {
		case low == "global":
			hasGlobal = true
		case low == "defaults":
			hasDefaults = true
		case strings.HasPrefix(low, "frontend "):
			hasFrontend = true
		case strings.HasPrefix(low, "backend "):
			return fmt.Errorf("в профиле не должно быть backend — серверы живут в state и *.cfg")
		}
	}
	if !hasGlobal {
		return fmt.Errorf("нет секции global")
	}
	if !hasDefaults {
		return fmt.Errorf("нет секции defaults")
	}
	if !hasFrontend {
		return fmt.Errorf("нет frontend")
	}
	return nil
}
