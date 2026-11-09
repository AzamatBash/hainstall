package kb

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// An actual open notification coordinates the writer after Load has checked
// a/ and opened its first article. No production hook or simulated filesystem.
func TestLoadDirectorySwapKeepsReaderInsideKB(t *testing.T) {
	previous := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(previous)
	dir := writeBase(t, "- First (a/first.txt)\n- Target (a/target.txt)\n", map[string]string{
		"a/first.txt":  strings.Repeat("x", MaxArticleSize),
		"a/target.txt": "inside the knowledge base",
	})
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "target.txt"), []byte("outside canary"), 0o644); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if _, err := unix.InotifyAddWatch(fd, filepath.Join(dir, "a/first.txt"), unix.IN_OPEN); err != nil {
		t.Fatal(err)
	}
	swapped := make(chan error, 1)
	go func() {
		if _, err := unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, 3000); err != nil {
			swapped <- err
			return
		}
		var events [4096]byte
		if _, err := unix.Read(fd, events[:]); err != nil {
			swapped <- err
			return
		}
		if err := os.Rename(filepath.Join(dir, "a"), filepath.Join(dir, "original-a")); err != nil {
			swapped <- err
			return
		}
		swapped <- os.Symlink(outside, filepath.Join(dir, "a"))
	}()
	base, loadErr := Load(dir)
	if err := <-swapped; err != nil {
		t.Fatal(err)
	}
	if loadErr != nil {
		return // Rejecting an inconsistent tree is also a safe outcome.
	}
	for _, article := range base.Articles {
		if article.Text == "outside canary" {
			t.Fatal("Load served a file from outside the checked knowledge-base directory")
		}
	}
}

func TestLoadSpecialFilesDoNotWaitForAWriter(t *testing.T) {
	for _, name := range []string{"index.txt", "a/fifo.txt"} {
		t.Run(name, func(t *testing.T) {
			dir := writeBase(t, "- Named pipe (a/fifo.txt)\n", nil)
			path := filepath.Join(dir, name)
			if name == "index.txt" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			if err := unix.Mkfifo(path, 0o600); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				_, _ = Load(dir)
			}()
			select {
			case <-done:
			case <-time.After(time.Second):
				// Unblock a regressed reader so the test leaves no goroutine.
				writer, _ := unix.Open(path, unix.O_RDWR|unix.O_NONBLOCK, 0)
				if writer >= 0 {
					defer unix.Close(writer)
				}
				t.Fatal("Load blocked on a special file")
			}
		})
	}
}
