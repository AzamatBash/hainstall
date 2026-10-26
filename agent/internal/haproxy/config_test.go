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
	body, err := renderBackendSection("app", []store.Server{{
		Name: "DE Selectel OUT", Address: "1.2.3.4", Port: 8443, Weight: 100,
	}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "balance leastconn") {
		t.Fatalf("expected default balance leastconn, got:\n%s", body)
	}
	if !strings.Contains(body, "server DE_Selectel_OUT 1.2.3.4:8443") {
		t.Fatalf("expected sanitized server line, got:\n%s", body)
	}
	if strings.Contains(body, "Selectel OUT") {
		t.Fatalf("raw spaced name still present:\n%s", body)
	}
	if strings.Contains(body, "send-proxy") {
		t.Fatalf("empty client_ip must stay off, got:\n%s", body)
	}
}

func TestRenderBackendSectionClientIP(t *testing.T) {
	body, err := renderBackendSection("app", []store.Server{
		{Name: "nginx", Address: "10.0.0.1", Port: 443, Weight: 100, ClientIP: "proxy-ng"},
		{Name: "vless", Address: "10.0.0.2", Port: 8443, Weight: 50, ClientIP: "proxy-vl"},
		{Name: "plain", Address: "10.0.0.3", Port: 443, Weight: 100, ClientIP: "off"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "server nginx 10.0.0.1:443 weight 100 send-proxy-v2\n") {
		t.Fatalf("proxy-ng line missing:\n%s", body)
	}
	if !strings.Contains(body, "server vless 10.0.0.2:8443 weight 50 send-proxy-v2\n") {
		t.Fatalf("proxy-vl line missing:\n%s", body)
	}
	if !strings.Contains(body, "server plain 10.0.0.3:443 weight 100\n") {
		t.Fatalf("off line should not send proxy:\n%s", body)
	}
	if strings.Contains(body, "plain 10.0.0.3:443 weight 100 send-proxy") {
		t.Fatalf("off server picked up send-proxy:\n%s", body)
	}
}

func TestRenderBackendSectionBalance(t *testing.T) {
	body, err := renderBackendSection("app", nil, "roundrobin")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "balance roundrobin") {
		t.Fatalf("expected roundrobin, got:\n%s", body)
	}
	body, err = renderBackendSection("app", nil, "nope")
	if err != nil {
		t.Fatal(err)
	}
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

func TestRenderTLSCheckUsesLiteralSNI(t *testing.T) {
	body, err := renderBackendSection("app", []store.Server{{
		Name: "t", Address: "10.0.0.4", Port: 443, Weight: 1,
		ClientIP: "proxy-ng", Check: "tls", CheckSNI: "example.com",
	}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "check ssl sni str(example.com) verify none check-send-proxy") {
		t.Fatalf("tls check line:\n%s", body)
	}
}

func TestRenderCheckDoesNotProxyASeparatePort(t *testing.T) {
	same, err := renderBackendSection("app", []store.Server{{
		Name: "n", Address: "10.0.0.1", Port: 443, Weight: 1,
		ClientIP: "proxy-ng", Check: "tcp", CheckInter: 2000, CheckFall: 3, CheckRise: 2,
	}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(same, "send-proxy-v2 check inter 2000ms fall 3 rise 2 check-send-proxy") {
		t.Fatalf("same-port check should send proxy:\n%s", same)
	}
	other, err := renderBackendSection("app", []store.Server{{
		Name: "n", Address: "10.0.0.1", Port: 443, Weight: 1,
		ClientIP: "proxy-vl", Check: "tcp", CheckPort: 9443,
	}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(other, "check-send-proxy") {
		t.Fatalf("separate check port must not send proxy:\n%s", other)
	}
	if !strings.Contains(other, "check port 9443") || !strings.Contains(other, "send-proxy-v2") {
		t.Fatalf("expected check port and client proxy:\n%s", other)
	}
}
