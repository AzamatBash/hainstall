package composeedit

import (
	"fmt"
	"strings"
)

// SetHAProxyPublishPorts replaces the haproxy service `ports:` list with
// host:container mappings for each listen port (same port both sides).
func SetHAProxyPublishPorts(composeYAML string, ports []int) (string, error) {
	if len(ports) == 0 {
		return "", fmt.Errorf("ports list is empty")
	}
	lines := strings.Split(composeYAML, "\n")
	out := make([]string, 0, len(lines)+len(ports))
	replaced := false

	i := 0
	for i < len(lines) {
		line := lines[i]
		if isYAMLKey(line, "haproxy") {
			svcIndent := leadingSpaces(line)
			out = append(out, line)
			i++
			for i < len(lines) {
				l2 := lines[i]
				t2 := strings.TrimSpace(l2)
				ind2 := leadingSpaces(l2)
				if t2 != "" && !strings.HasPrefix(t2, "#") && ind2 <= svcIndent {
					break
				}
				if isYAMLKey(l2, "ports") {
					portsIndent := leadingSpaces(l2)
					out = append(out, strings.Repeat(" ", portsIndent)+"ports:")
					itemIndent := portsIndent + 2
					for _, p := range ports {
						out = append(out, fmt.Sprintf(`%s- "%d:%d"`, strings.Repeat(" ", itemIndent), p, p))
					}
					replaced = true
					i++
					for i < len(lines) {
						l3 := lines[i]
						t3 := strings.TrimSpace(l3)
						ind3 := leadingSpaces(l3)
						if t3 == "" || strings.HasPrefix(t3, "#") {
							i++
							continue
						}
						if ind3 <= portsIndent {
							break
						}
						i++
					}
					continue
				}
				out = append(out, l2)
				i++
			}
			continue
		}
		out = append(out, line)
		i++
	}

	if !replaced {
		return "", fmt.Errorf("haproxy ports: section not found in compose")
	}
	return strings.Join(out, "\n"), nil
}

func isYAMLKey(line, key string) bool {
	trim := strings.TrimSpace(line)
	if strings.HasPrefix(trim, "#") {
		return false
	}
	return trim == key+":" || strings.HasPrefix(trim, key+":")
}

func leadingSpaces(s string) int {
	n := 0
	for _, r := range s {
		if r == ' ' {
			n++
			continue
		}
		if r == '\t' {
			n += 2
			continue
		}
		break
	}
	return n
}
