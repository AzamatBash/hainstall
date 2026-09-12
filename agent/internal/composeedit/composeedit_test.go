package composeedit

import (
	"strings"
	"testing"
)

func TestSetHAProxyPublishPorts(t *testing.T) {
	in := `services:
  haproxy:
    image: haproxy:3.0-alpine
    ports:
      - "8443:8443"
    volumes:
      - ./x:/x
  agent:
    ports:
      - "47893:9100"
`
	out, err := SetHAProxyPublishPorts(in, []int{443, 8443})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `- "443:443"`) || !strings.Contains(out, `- "8443:8443"`) {
		t.Fatalf("missing published ports:\n%s", out)
	}
	if !strings.Contains(out, `- "47893:9100"`) {
		t.Fatalf("agent ports should stay:\n%s", out)
	}
	// only one haproxy ports block
	if strings.Count(out, `ports:`) != 2 {
		t.Fatalf("expected 2 ports sections:\n%s", out)
	}
}
