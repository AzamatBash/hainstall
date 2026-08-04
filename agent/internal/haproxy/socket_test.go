package haproxy

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWaitReadySuccess(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "admin.sock")

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	defer os.Remove(sockPath)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 64)
			_, _ = conn.Read(buf)
			_, _ = conn.Write([]byte("Name: HAProxy\nVersion: 2.8\n"))
			_ = conn.Close()
		}
	}()

	c := NewClient(sockPath)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.WaitReady(ctx); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
}

func TestWaitReadyRetriesUntilSocketAppears(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "admin.sock")

	c := NewClient(sockPath)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		time.Sleep(200 * time.Millisecond)
		ln, err := net.Listen("unix", sockPath)
		if err != nil {
			return
		}
		defer ln.Close()
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		buf := make([]byte, 64)
		_, _ = conn.Read(buf)
		_, _ = conn.Write([]byte("Name: HAProxy\n"))
		_ = conn.Close()
	}()

	if err := c.WaitReadyTimeout(ctx, 3*time.Second); err != nil {
		t.Fatalf("WaitReady after delayed socket: %v", err)
	}
}

func TestWaitReadyTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 64)
			_, _ = conn.Read(buf)
			_, _ = conn.Write([]byte("Name: HAProxy\nVersion: 3.0\n"))
			_ = conn.Close()
		}
	}()

	c := NewClient("tcp://" + ln.Addr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.WaitReady(ctx); err != nil {
		t.Fatalf("WaitReady TCP: %v", err)
	}
}

func TestNetworkAndAddress(t *testing.T) {
	cases := []struct {
		in, network, address string
	}{
		{"/var/run/haproxy/admin.sock", "unix", "/var/run/haproxy/admin.sock"},
		{"unix:///tmp/x.sock", "unix", "/tmp/x.sock"},
		{"tcp://haproxy:9999", "tcp", "haproxy:9999"},
		{"haproxy:9999", "tcp", "haproxy:9999"},
	}
	for _, tc := range cases {
		c := NewClient(tc.in)
		n, a, err := c.networkAndAddress()
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if n != tc.network || a != tc.address {
			t.Fatalf("%q: got %s %s, want %s %s", tc.in, n, a, tc.network, tc.address)
		}
	}
}
