package haproxy

import (
	"fmt"
	"strings"

	"github.com/azabash/hapanel/agent/internal/store"
)

const (
	CheckOff = "off"
	CheckTCP = "tcp"
	CheckTLS = "tls"
)

// NormalizeCheck accepts empty as off.
func NormalizeCheck(v string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", CheckOff:
		return CheckOff, nil
	case CheckTCP, CheckTLS:
		return strings.ToLower(strings.TrimSpace(v)), nil
	default:
		return "", fmt.Errorf("check должен быть off, tcp или tls")
	}
}

// CheckSuffix is the health-check tail of a server line.
// A separate check port does not send PROXY: that port is not the client listener.
func CheckSuffix(s store.Server) (string, error) {
	mode, err := NormalizeCheck(s.Check)
	if err != nil {
		return "", err
	}
	if mode == CheckOff {
		return "", nil
	}
	if s.CheckPort < 0 || s.CheckPort > 65535 {
		return "", fmt.Errorf("check_port %d вне диапазона", s.CheckPort)
	}
	var b strings.Builder
	b.WriteString(" check")
	samePort := s.CheckPort == 0 || s.CheckPort == s.Port
	if !samePort {
		fmt.Fprintf(&b, " port %d", s.CheckPort)
	}
	if mode == CheckTLS {
		b.WriteString(" ssl")
		if sni := strings.TrimSpace(s.CheckSNI); sni != "" {
			if strings.ContainsAny(sni, " \t()") {
				return "", fmt.Errorf("check_sni содержит пробел или скобки")
			}
			// HAProxy treats sni as a sample expression. A bare host is parsed
			// as a fetch, so a literal name has to be str(host).
			fmt.Fprintf(&b, " sni str(%s)", sni)
		}
		switch strings.ToLower(strings.TrimSpace(s.CheckVerify)) {
		case "", "none":
			b.WriteString(" verify none")
		case "required":
			ca := strings.TrimSpace(s.CheckCA)
			if ca == "" || strings.ContainsAny(ca, " \t") {
				return "", fmt.Errorf("для verify required нужен check_ca")
			}
			fmt.Fprintf(&b, " ca-file %s verify required", ca)
		default:
			return "", fmt.Errorf("check_verify должен быть none или required")
		}
	}
	writeTime(&b, "inter", s.CheckInter)
	writeTime(&b, "fastinter", s.CheckFast)
	writeTime(&b, "downinter", s.CheckDown)
	if s.CheckFall > 0 {
		fmt.Fprintf(&b, " fall %d", s.CheckFall)
	}
	if s.CheckRise > 0 {
		fmt.Fprintf(&b, " rise %d", s.CheckRise)
	}
	if samePort && SendProxyV2(s.ClientIP) {
		b.WriteString(" check-send-proxy")
	}
	return b.String(), nil
}

func writeTime(b *strings.Builder, key string, ms int) {
	if ms <= 0 {
		return
	}
	fmt.Fprintf(b, " %s %dms", key, ms)
}
