package protect

import (
	"fmt"

	"github.com/azabash/hapanel/agent/internal/haproxy"
	"github.com/azabash/hapanel/agent/internal/store"
)

// Profile is an alias for the persisted protect document.
type Profile = store.ProtectProfile

// Default returns the recommended profile for new nodes.
func Default() Profile {
	return Profile{
		ClientHello:         true,
		ClientHelloDelaySec: 5,
		RateLimit:           true,
		MaxConnPerIP:        100,
		MaxRatePerIP:        60,
		RateWindowSec:       10,
		RUOnly:              false,
		SynProxy:            false,
	}
}

// Normalize fills zero numeric fields and validates ranges. Bools are kept as-is.
func Normalize(in Profile) (Profile, error) {
	d := Default()
	out := in
	if out.ClientHelloDelaySec == 0 {
		out.ClientHelloDelaySec = d.ClientHelloDelaySec
	}
	if out.MaxConnPerIP == 0 {
		out.MaxConnPerIP = d.MaxConnPerIP
	}
	if out.MaxRatePerIP == 0 {
		out.MaxRatePerIP = d.MaxRatePerIP
	}
	if out.RateWindowSec == 0 {
		out.RateWindowSec = d.RateWindowSec
	}
	if out.ClientHelloDelaySec < 1 || out.ClientHelloDelaySec > 60 {
		return Profile{}, fmt.Errorf("client_hello_delay_sec must be 1–60")
	}
	if out.MaxConnPerIP < 1 || out.MaxConnPerIP > 100000 {
		return Profile{}, fmt.Errorf("max_conn_per_ip must be 1–100000")
	}
	if out.MaxRatePerIP < 1 || out.MaxRatePerIP > 100000 {
		return Profile{}, fmt.Errorf("max_rate_per_ip must be 1–100000")
	}
	if out.RateWindowSec < 1 || out.RateWindowSec > 3600 {
		return Profile{}, fmt.Errorf("rate_window_sec must be 1–3600")
	}
	return out, nil
}

// ToHAProxy maps profile to frontend ProtectOpts (host flags ignored).
func ToHAProxy(p Profile) haproxy.ProtectOpts {
	return haproxy.ProtectOpts{
		ClientHello:         p.ClientHello,
		ClientHelloDelaySec: p.ClientHelloDelaySec,
		RateLimit:           p.RateLimit,
		MaxConnPerIP:        p.MaxConnPerIP,
		MaxRatePerIP:        p.MaxRatePerIP,
		RateWindowSec:       p.RateWindowSec,
	}
}
