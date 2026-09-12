package protect

import "fmt"

// Profile mirrors agent protect settings (HAProxy + future host flags).
type Profile struct {
	ClientHello         bool `json:"client_hello"`
	ClientHelloDelaySec int  `json:"client_hello_delay_sec"`
	RateLimit           bool `json:"rate_limit"`
	MaxConnPerIP        int  `json:"max_conn_per_ip"`
	MaxRatePerIP        int  `json:"max_rate_per_ip"`
	RateWindowSec       int  `json:"rate_window_sec"`
	RUOnly              bool `json:"ru_only"`
	SynProxy            bool `json:"synproxy"`
}

// Default is applied to new nodes and generated base.cfg.
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

// Normalize fills zero numerics and validates.
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

// FrontendExtras returns HAProxy frontend rules for base.cfg generation.
func FrontendExtras(p Profile) string {
	p, err := Normalize(p)
	if err != nil {
		p = Default()
	}
	var b string
	if p.RateLimit {
		b += fmt.Sprintf("    stick-table type ip size 1m expire 30s store conn_cur,conn_rate(%ds)\n", p.RateWindowSec)
		b += "    tcp-request connection track-sc0 src\n"
		b += fmt.Sprintf("    tcp-request connection reject if { sc_conn_cur(0) gt %d }\n", p.MaxConnPerIP)
		b += fmt.Sprintf("    tcp-request connection reject if { sc_conn_rate(0) gt %d }\n", p.MaxRatePerIP)
	}
	if p.ClientHello {
		b += fmt.Sprintf("    tcp-request inspect-delay %ds\n", p.ClientHelloDelaySec)
		b += "    acl is_tls_clienthello req_ssl_hello_type 1\n"
		b += "    tcp-request content accept if is_tls_clienthello\n"
		b += "    tcp-request content reject\n"
	}
	return b
}
