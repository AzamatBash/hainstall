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

func TestWaitReadyTimeout(t *testing.T) {
	c := NewClient(filepath.Join(t.TempDir(), "missing.sock"))
	ctx := context.Background()
	err := c.WaitReadyTimeout(ctx, 150*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
