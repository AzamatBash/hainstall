// Package olcrtcuri builds olcrtc:// connection URIs for client apps.
// Format: olcrtc://{provider}?{transport}@{room}#{key}${comment}
// Optional transport payload <k=v&...> is omitted for MVP (datachannel has none).
package olcrtcuri

import (
	"fmt"
	"strings"
)

// Build returns an olcrtc:// URI without transport payload parameters.
// Room may be an https URL (e.g. Jitsi); delimiter chars in room/comment are percent-encoded.
func Build(provider, transport, room, key, comment string) (string, error) {
	provider = strings.TrimSpace(provider)
	transport = strings.TrimSpace(transport)
	room = strings.TrimSpace(room)
	key = strings.TrimSpace(key)
	comment = strings.TrimSpace(comment)
	if provider == "" || transport == "" || room == "" || key == "" {
		return "", fmt.Errorf("provider, transport, room and key are required")
	}
	var b strings.Builder
	b.Grow(len(provider) + len(transport) + len(room) + len(key) + len(comment) + 32)
	b.WriteString("olcrtc://")
	b.WriteString(provider)
	b.WriteByte('?')
	b.WriteString(transport)
	b.WriteByte('@')
	b.WriteString(escapeField(room))
	b.WriteByte('#')
	b.WriteString(key)
	if comment != "" {
		b.WriteByte('$')
		b.WriteString(escapeField(comment))
	}
	return b.String(), nil
}

// escapeField percent-encodes URI delimiter characters so https room URLs stay intact
// while # $ @ ? < > % inside fields do not break parsing.
func escapeField(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '#', '$', '@', '?', '<', '>', '%':
			fmt.Fprintf(&b, "%%%02X", c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
