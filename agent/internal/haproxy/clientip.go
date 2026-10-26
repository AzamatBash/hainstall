package haproxy

import (
	"fmt"
	"strings"
)

// Client IP modes. Both proxy modes emit HAProxy send-proxy-v2.
// proxy-ng is the nginx listener that accepts PROXY protocol.
// proxy-vl is Xray itself accepting PROXY protocol. They are not interchangeable.
const (
	ClientIPOff     = "off"
	ClientIPProxyNG = "proxy-ng"
	ClientIPProxyVL = "proxy-vl"
)

// NormalizeClientIP accepts empty as off. Unknown values are rejected.
func NormalizeClientIP(v string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", ClientIPOff:
		return ClientIPOff, nil
	case ClientIPProxyNG:
		return ClientIPProxyNG, nil
	case ClientIPProxyVL:
		return ClientIPProxyVL, nil
	default:
		return "", fmt.Errorf("client_ip должен быть off, proxy-ng или proxy-vl")
	}
}

// SendProxyV2 reports whether the server line should carry send-proxy-v2.
func SendProxyV2(mode string) bool {
	mode, err := NormalizeClientIP(mode)
	return err == nil && mode != ClientIPOff
}
