package sysinfo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConntrackReadsCounters(t *testing.T) {
	dir := t.TempDir()
	nf := filepath.Join(dir, "sys/net/netfilter")
	if err := os.MkdirAll(nf, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nf, "nf_conntrack_count"), []byte("1000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nf, "nf_conntrack_max"), []byte("65536\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	count, max, ok := conntrack(dir)
	if !ok || count != 1000 || max != 65536 {
		t.Fatalf("got %d %d %v", count, max, ok)
	}
}

func TestHostExtras(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "1/net/stat"), 0o755); err != nil {
		t.Fatal(err)
	}
	stat := "entries searched found new invalid ignore delete delete_list insert insert_failed drop early_drop\n" +
		"0000000a 0 0 0 0 0 0 0 00000001 00000002 00000003 00000004\n" +
		"00000001 0 0 0 0 0 0 0 00000001 00000001 00000001 00000001\n"
	if err := os.WriteFile(filepath.Join(dir, "1/net/stat/nf_conntrack"), []byte(stat), 0o644); err != nil {
		t.Fatal(err)
	}
	failed, drop, early, ok := conntrackStat(dir)
	if !ok || failed != 3 || drop != 4 || early != 5 {
		t.Fatalf("stat %d %d %d %v", failed, drop, early, ok)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sys/fs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sys/fs/file-nr"), []byte("10 0 100\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	alloc, max, ok := fileNr(dir)
	if !ok || alloc != 10 || max != 100 {
		t.Fatalf("fd %d %d %v", alloc, max, ok)
	}
	if err := os.MkdirAll(filepath.Join(dir, "1/net"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "1/net/sockstat"), []byte("TCP: inuse 7 orphan 1 tw 40 alloc 9 mem 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inuse, tw, ok := tcpSockstat(dir)
	if !ok || inuse != 7 || tw != 40 {
		t.Fatalf("tcp %d %d %v", inuse, tw, ok)
	}
	if err := os.MkdirAll(filepath.Join(dir, "pressure"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pressure/memory"), []byte("some avg10=1.50 avg60=0.00 avg300=0.00 total=1\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	some, ok := pressureSome(dir, "memory")
	if !ok || some != 1.5 {
		t.Fatalf("psi %v %v", some, ok)
	}
}

func TestConntrackMissingIsNotAFault(t *testing.T) {
	if _, _, ok := conntrack(t.TempDir()); ok {
		t.Fatal("missing table should be skipped")
	}
}
