package haproxy

import (
	"strings"
	"testing"
)

func TestStripBackendSections(t *testing.T) {
	in := `global
    maxconn 1

defaults
    mode tcp

frontend entrance_8443
    bind *:8443
    default_backend app

backend app
    mode tcp
    server s1 1.2.3.4:443 weight 100

frontend other
    bind *:9443
    default_backend app
`
	out, n := StripBackendSections(in)
	if n != 1 {
		t.Fatalf("stripped=%d", n)
	}
	if err := ValidateBaseProfile(out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "\nbackend ") || strings.HasPrefix(strings.TrimSpace(out), "backend ") {
		t.Fatalf("backend left:\n%s", out)
	}
	if !strings.Contains(out, "frontend other") {
		t.Fatalf("lost frontend other:\n%s", out)
	}
	if strings.Contains(out, "server s1") {
		t.Fatalf("server leaked into base:\n%s", out)
	}
}
