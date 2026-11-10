package proxyconfig

import "testing"

func TestDefaultHAProxyValidates(t *testing.T) {
	r := Validate("haproxy", DefaultHAProxyVPN())
	if !r.OK {
		t.Fatalf("default template invalid: %+v", r)
	}
}

func TestHAProxyRejectsEmpty(t *testing.T) {
	r := Validate("haproxy", "global\n")
	if r.OK {
		t.Fatal("expected fail")
	}
}

func TestEnvoyDefaultOK(t *testing.T) {
	r := Validate("envoy", DefaultEnvoyVPN())
	if !r.OK {
		t.Fatalf("%+v", r)
	}
}
