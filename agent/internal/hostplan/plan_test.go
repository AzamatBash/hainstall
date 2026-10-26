package hostplan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPortConflictIgnoresManagedHAProxy(t *testing.T) {
	err := PortConflict(8443, []Occupant{{Port: 8443, Owner: "haproxy"}, {Port: 8443, Owner: "abc_haproxy"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	err = PortConflict(8443, []Occupant{{Port: 8443, Owner: "docker-proxy"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	err = PortConflict(443, []Occupant{{Port: 443, Owner: "docker-proxy"}}, false)
	if err == nil || !strings.Contains(err.Error(), "docker-proxy") {
		t.Fatalf("got %v", err)
	}
	err = PortConflict(8443, []Occupant{{Port: 8443, Owner: "nginx"}}, true)
	if err == nil || !strings.Contains(err.Error(), "nginx") {
		t.Fatalf("got %v", err)
	}
}

func TestParseListeners(t *testing.T) {
	raw := "State Recv-Q Send-Q Local Address:Port Peer Address:PortProcess\n" +
		"LISTEN 0 128 0.0.0.0:8443 0.0.0.0:* users:((\"haproxy\",pid=1,fd=6))\n" +
		"LISTEN 0 128 [::]:443 [::]:* users:((\"nginx\",pid=2,fd=5))\n" +
		"LISTEN 0 128 0.0.0.0:22 0.0.0.0:*\n"
	got := ParseListeners(raw)
	if len(got) != 3 {
		t.Fatalf("%+v", got)
	}
	if got[0].Port != 8443 || got[0].Owner != "haproxy" {
		t.Fatalf("haproxy %+v", got[0])
	}
	if got[1].Port != 443 || got[1].Owner != "nginx" {
		t.Fatalf("nginx %+v", got[1])
	}
	if got[2].Port != 22 || got[2].Owner != "" {
		t.Fatalf("sshd hidden %+v", got[2])
	}
}

func TestApplyEffect(t *testing.T) {
	if ApplyEffect(false) != "reload" || ApplyEffect(true) != "recreate" {
		t.Fatal(ApplyEffect(false), ApplyEffect(true))
	}
}

func TestBackupRestore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bak, err := BackupFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RestoreFile(bak, path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old\n" {
		t.Fatalf("restored %q", got)
	}
}

func TestConntrackDoesNotRaiseOrIgnoreMemory(t *testing.T) {
	if KeepConntrackMax(0, 262144) != 262144 {
		t.Fatal("empty stored value must keep the live limit")
	}
	if KeepConntrackMax(65536, 262144) != 65536 {
		t.Fatal("an explicit stored value is the operator choice")
	}
	if err := ConntrackFitsMemory(262144, 64<<20); err == nil {
		t.Fatal("expected memory refusal")
	}
	if err := ConntrackFitsMemory(65536, 2<<30); err != nil {
		t.Fatal(err)
	}
	count, max, pct, err := ParseConntrack("65535", "65536")
	if err != nil || count != 65535 || max != 65536 || pct < 99 {
		t.Fatalf("parse %d %d %v %v", count, max, pct, err)
	}
}

func TestAgentIngressDoesNotTouchEgress(t *testing.T) {
	body, err := AgentIngress([]string{"185.154.195.230/32"}, 47893)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "ct state new") || !strings.Contains(body, "ct state established,related accept") {
		t.Fatalf("missing connection state:\n%s", body)
	}
	if strings.Contains(body, "hook output") || strings.Contains(body, "flush") {
		t.Fatalf("egress or flush leaked:\n%s", body)
	}
	if _, err := AgentIngress(nil, 47893); err == nil {
		t.Fatal("expected missing panel address")
	}
}

func TestAlertHysteresis(t *testing.T) {
	if AlertLevel(69) != "" || AlertLevel(70) != "warn" || AlertLevel(95) != "crit" {
		t.Fatalf("bands %q %q %q", AlertLevel(69), AlertLevel(70), AlertLevel(95))
	}
	level, latch := AlertHeld(96, "", false)
	if level != "" || latch != "" {
		t.Fatalf("short spike should not fire: %q %q", level, latch)
	}
	level, latch = AlertHeld(96, "", true)
	if level != "crit" || latch != "crit" {
		t.Fatalf("held crit: %q %q", level, latch)
	}
	level, latch = AlertHeld(91, "crit", true)
	if level != "crit" {
		t.Fatalf("hysteresis dropped too early: %q", level)
	}
	level, latch = AlertHeld(80, "crit", true)
	if level != "warn" || latch != "warn" {
		t.Fatalf("should leave crit for the lower band: %q %q", level, latch)
	}
	level, latch = AlertHeld(64, "warn", true)
	if level != "" || latch != "" {
		t.Fatalf("should clear: %q %q", level, latch)
	}
}
