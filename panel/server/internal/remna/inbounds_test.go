package remna

import (
	"encoding/json"
	"testing"
)

func TestParseInboundUUIDList(t *testing.T) {
	ids := parseInboundUUIDList(json.RawMessage(`["a","b"]`))
	if len(ids) != 2 || ids[0] != "a" {
		t.Fatalf("strings: %v", ids)
	}
	ids = parseInboundUUIDList(json.RawMessage(`[{"uuid":"x","tag":"t"},{"uuid":"y"}]`))
	if len(ids) != 2 || ids[0] != "x" || ids[1] != "y" {
		t.Fatalf("objects: %v", ids)
	}
}

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

func TestNodeDecodeConfigProfileObjects(t *testing.T) {
	raw := []byte(`{
	  "response": [{
	    "uuid": "n1",
	    "name": "node",
	    "address": "1.2.3.4",
	    "usersOnline": 3,
	    "configProfile": {
	      "activeConfigProfileUuid": "p1",
	      "activeInbounds": [{"uuid":"in1","tag":"vless-in"}]
	    }
	  }]
	}`)
	var out nodesResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Response) != 1 {
		t.Fatalf("nodes=%d", len(out.Response))
	}
	ids := out.Response[0].ActiveInboundUUIDs()
	if len(ids) != 1 || ids[0] != "in1" {
		t.Fatalf("ids=%v", ids)
	}
	if out.Response[0].ConfigProfileUUID() != "p1" {
		t.Fatalf("profile=%q", out.Response[0].ConfigProfileUUID())
	}
}
