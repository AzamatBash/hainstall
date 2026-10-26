package remna

import "testing"

func TestAddressKey(t *testing.T) {
	cases := map[string]string{
		"nl-exit-1.example.net":   "nl-exit-1.example.net",
		" NL-Exit-1.Example.NET ": "nl-exit-1.example.net",
		"\t198.51.100.22\n":       "198.51.100.22",
		"[2001:DB8::10]":          "[2001:db8::10]",
		"node.example.net:443":    "node.example.net:443", // a port stays part of the address
		"Ünïcode.Example":         "ünïcode.example",
		"":                        "",
		"   ":                     "",
	}
	for in, want := range cases {
		if got := AddressKey(in); got != want {
			t.Errorf("AddressKey(%q)=%q want %q", in, got, want)
		}
	}
}

func TestFindNodeByAddress(t *testing.T) {
	nodes := []Node{
		{UUID: "u1", Address: " NL-Exit-1.example.net "},
		{UUID: "u2", Address: "198.51.100.22"},
		{UUID: "u3", Address: "198.51.100.22 "},
		{UUID: "u4", Address: ""},
	}
	cases := []struct {
		address string
		want    string
	}{
		{"nl-exit-1.EXAMPLE.net", "u1"},
		{" 198.51.100.22", "u2"}, // first match wins
		{"198.51.100.23", ""},
		{"nl-exit-1.example.net:443", ""},
		{"", ""}, // an empty address never matches, not even an empty one
		{"  ", ""},
	}
	for _, tc := range cases {
		got := FindNodeByAddress(nodes, tc.address)
		if tc.want == "" {
			if got != nil {
				t.Errorf("FindNodeByAddress(%q)=%s want none", tc.address, got.UUID)
			}
			continue
		}
		if got == nil || got.UUID != tc.want {
			t.Errorf("FindNodeByAddress(%q)=%v want %s", tc.address, got, tc.want)
		}
	}
}
