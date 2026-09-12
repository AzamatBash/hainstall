package haproxy

import (
	"strings"
	"testing"

	"github.com/azabash/hapanel/agent/internal/store"
)

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"DE Selectel OUT": "DE_Selectel_OUT",
		"ok-name_1":       "ok-name_1",
		"  spaced  ":      "spaced",
		"a!!b":            "a_b",
		"":                "srv",
		"___":             "srv",
	}
	for in, want := range cases {
		if got := SanitizeName(in); got != want {
			t.Fatalf("SanitizeName(%q)=%q want %q", in, got, want)
		}
	}
}

func TestRenderBackendSectionSanitizesName(t *testing.T) {
	body := renderBackendSection("app", []store.Server{{
		Name: "DE Selectel OUT", Address: "1.2.3.4", Port: 8443, Weight: 100,
	}}, "")
	if !strings.Contains(body, "balance leastconn") {
		t.Fatalf("expected default balance leastconn, got:\n%s", body)
	}
	if !strings.Contains(body, "server DE_Selectel_OUT 1.2.3.4:8443") {
		t.Fatalf("expected sanitized server line, got:\n%s", body)
	}
	if strings.Contains(body, "Selectel OUT") {
		t.Fatalf("raw spaced name still present:\n%s", body)
	}
}

func TestRenderBackendSectionBalance(t *testing.T) {
	body := renderBackendSection("app", nil, "roundrobin")
	if !strings.Contains(body, "balance roundrobin") {
		t.Fatalf("expected roundrobin, got:\n%s", body)
	}
	body = renderBackendSection("app", nil, "nope")
	if !strings.Contains(body, "balance leastconn") {
		t.Fatalf("invalid balance should fall back to leastconn, got:\n%s", body)
	}
}

func TestNormalizeBalance(t *testing.T) {
	got, err := NormalizeBalance("")
	if err != nil || got != DefaultBalance {
		t.Fatalf("empty: got %q err %v", got, err)
	}
	got, err = NormalizeBalance(" Source ")
	if err != nil || got != "source" {
		t.Fatalf("source: got %q err %v", got, err)
	}
	if _, err := NormalizeBalance("uri"); err == nil {
		t.Fatal("expected error for uri")
	}
}
