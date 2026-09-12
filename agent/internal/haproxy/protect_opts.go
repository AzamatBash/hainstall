package haproxy

import (
	"fmt"
	"strings"
)

// ProtectOpts controls HAProxy frontend anti-abuse rules in base.cfg.
type ProtectOpts struct {
	ClientHello         bool
	ClientHelloDelaySec int
	RateLimit           bool
	MaxConnPerIP        int
	MaxRatePerIP        int
	RateWindowSec       int
}

// DefaultProtectOpts is the recommended frontend protection.
func DefaultProtectOpts() ProtectOpts {
	return ProtectOpts{
		ClientHello:         true,
		ClientHelloDelaySec: 5,
		RateLimit:           true,
		MaxConnPerIP:        100,
		MaxRatePerIP:        60,
		RateWindowSec:       10,
	}
}

func (o ProtectOpts) normalize() ProtectOpts {
	d := DefaultProtectOpts()
	if o.ClientHelloDelaySec <= 0 {
		o.ClientHelloDelaySec = d.ClientHelloDelaySec
	}
	if o.MaxConnPerIP <= 0 {
		o.MaxConnPerIP = d.MaxConnPerIP
	}
	if o.MaxRatePerIP <= 0 {
		o.MaxRatePerIP = d.MaxRatePerIP
	}
	if o.RateWindowSec <= 0 {
		o.RateWindowSec = d.RateWindowSec
	}
	return o
}

func (o ProtectOpts) frontendExtras() string {
	o = o.normalize()
	var b strings.Builder
	if o.RateLimit {
		fmt.Fprintf(&b, "    stick-table type ip size 1m expire 30s store conn_cur,conn_rate(%ds)\n", o.RateWindowSec)
		b.WriteString("    tcp-request connection track-sc0 src\n")
		fmt.Fprintf(&b, "    tcp-request connection reject if { sc_conn_cur(0) gt %d }\n", o.MaxConnPerIP)
		fmt.Fprintf(&b, "    tcp-request connection reject if { sc_conn_rate(0) gt %d }\n", o.MaxRatePerIP)
	}
	if o.ClientHello {
		fmt.Fprintf(&b, "    tcp-request inspect-delay %ds\n", o.ClientHelloDelaySec)
		b.WriteString("    acl is_tls_clienthello req_ssl_hello_type 1\n")
		b.WriteString("    tcp-request content accept if is_tls_clienthello\n")
		b.WriteString("    tcp-request content reject\n")
	}
	return b.String()
}
