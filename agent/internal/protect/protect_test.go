package protect

import "testing"

func TestNormalizeDefaults(t *testing.T) {
	p, err := Normalize(Profile{ClientHello: true, RateLimit: true})
	if err != nil {
		t.Fatal(err)
	}
	if p.ClientHelloDelaySec != 5 || p.MaxConnPerIP != 100 {
		t.Fatalf("%+v", p)
	}
}

func TestNormalizeKeepsFalse(t *testing.T) {
	p, err := Normalize(Profile{ClientHello: false, RateLimit: false})
	if err != nil {
		t.Fatal(err)
	}
	if p.ClientHello || p.RateLimit {
		t.Fatalf("expected false flags: %+v", p)
	}
}
