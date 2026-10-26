package store

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/azabash/hapanel/panel/internal/protect"
)

func TestHostFromNodeURL(t *testing.T) {
	cases := map[string]string{
		"https://203.0.113.10:47893":         "203.0.113.10",
		"http://node.example.net:9100/agent": "node.example.net",
		"https://user:pw@node.example.net/x": "node.example.net",
		"http://[2001:db8::10]:47893/agent":  "2001:db8::10",
		" https://203.0.113.11 ":             "203.0.113.11",
		"203.0.113.12:47893":                 "",
		"not a url":                          "",
		"":                                   "",
	}
	for in, want := range cases {
		if got := hostFromNodeURL(in); got != want {
			t.Errorf("hostFromNodeURL(%q)=%q want %q", in, got, want)
		}
	}
}

func TestSplitInboundTags(t *testing.T) {
	if got := splitInboundTags(""); !reflect.DeepEqual(got, []string{}) {
		t.Fatalf("empty=%#v", got)
	}
	if got := splitInboundTags("VLESS_REALITY, HY2"); !reflect.DeepEqual(got, []string{"VLESS_REALITY", "HY2"}) {
		t.Fatalf("tags=%#v", got)
	}
}

func TestListNodeConfigs(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	withToken, err := st.CreateNode("a", "https://203.0.113.10:47893", "secret-token")
	if err != nil {
		t.Fatal(err)
	}
	noToken, err := st.CreateNode("b", "https://203.0.113.11:47893", "")
	if err != nil {
		t.Fatal(err)
	}
	custom := protect.Profile{ClientHello: false, ClientHelloDelaySec: 3, RateLimit: true, MaxConnPerIP: 50, MaxRatePerIP: 30, RateWindowSec: 20, RUOnly: true}
	if _, err := st.SetNodeProtect(noToken.ID, custom); err != nil {
		t.Fatal(err)
	}

	list, err := st.ListNodeConfigs()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID > list[1].ID {
		t.Fatalf("list=%+v", list)
	}
	byID := map[string]NodeConfig{}
	for _, c := range list {
		byID[c.ID] = c
	}
	a := byID[withToken.ID]
	if !a.HasToken || a.Host != "203.0.113.10" || !a.ProtectDefault || a.Protect != protect.Default() {
		t.Fatalf("a=%+v", a)
	}
	if !reflect.DeepEqual(a.ListenPorts, []int{8443}) || len(a.Entrances) != 1 || a.Entrances[0].Backend != "app" {
		t.Fatalf("a ports=%v entrances=%v", a.ListenPorts, a.Entrances)
	}
	if a.Backends != nil || a.BackendsObservedAt != nil {
		t.Fatalf("a without snapshot backends=%v at=%v", a.Backends, a.BackendsObservedAt)
	}
	b := byID[noToken.ID]
	if b.HasToken || b.ProtectDefault || b.Protect != custom {
		t.Fatalf("b=%+v", b)
	}
}
