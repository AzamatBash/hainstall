package remna

import "testing"

func TestDeriveProtocolFromInbounds(t *testing.T) {
	by := map[string]Inbound{
		"a": {UUID: "a", Tag: "vless-in", Type: "vless", Security: "reality"},
		"b": {UUID: "b", Tag: "hy2", Type: "hysteria2"},
		"c": {UUID: "c", Tag: "ss", Type: "shadowsocks"},
	}
	p, tags := DeriveProtocolFromInbounds([]string{"a"}, by)
	if p != "vless_reality" || len(tags) != 1 || tags[0] != "vless-in" {
		t.Fatalf("got %q %v", p, tags)
	}
	p, tags = DeriveProtocolFromInbounds([]string{"a", "b"}, by)
	if p != "mixed" || len(tags) != 2 {
		t.Fatalf("mixed got %q %v", p, tags)
	}
	p, _ = DeriveProtocolFromInbounds(nil, by)
	if p != "unknown" {
		t.Fatalf("empty want unknown got %q", p)
	}
}

func TestEffectiveProtocol(t *testing.T) {
	if EffectiveProtocol("hysteria2", "vless") != "hysteria2" {
		t.Fatal("override")
	}
	if EffectiveProtocol("", "vless") != "vless" {
		t.Fatal("derived")
	}
	if EffectiveProtocol("  ", "") != "unknown" {
		t.Fatal("empty")
	}
}
