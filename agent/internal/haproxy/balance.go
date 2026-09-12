package haproxy

import (
	"fmt"
	"strings"
)

// DefaultBalance is used when a backend has no explicit algorithm.
const DefaultBalance = "leastconn"

var allowedBalances = map[string]struct{}{
	"roundrobin": {},
	"leastconn":  {},
	"source":     {},
	"first":      {},
	"random":     {},
}

// NormalizeBalance validates a HAProxy TCP balance algorithm.
// Empty input becomes DefaultBalance.
func NormalizeBalance(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return DefaultBalance, nil
	}
	if _, ok := allowedBalances[s]; !ok {
		return "", fmt.Errorf("unsupported balance %q (want roundrobin|leastconn|source|first|random)", s)
	}
	return s, nil
}
