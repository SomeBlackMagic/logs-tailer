package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// safeBuffer is a goroutine-safe io.Writer used to capture tailer output.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitFor polls cond until it returns true or timeout expires.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout after %s waiting for: %s", timeout, what)
}

// openFDsTo returns the number of this process' file descriptors that point
// to path, including ones whose target was deleted. Linux only.
func openFDsTo(t *testing.T, path string) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatalf("read /proc/self/fd: %v", err)
	}
	n := 0
	for _, e := range entries {
		target, err := os.Readlink(filepath.Join("/proc/self/fd", e.Name()))
		if err != nil {
			continue // fd was closed while iterating
		}
		if target == path || target == path+" (deleted)" {
			n++
		}
	}
	return n
}

// Regression test: a deleted (rotated) log file must not stay open.
// Before the fix processFile never returned after the file was deleted and
// kept a descriptor to it, so the disk space was never released.
func TestProcessFileReleasesDescriptorOnDelete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deprecation-2026-11-03.log")
	if err := os.WriteFile(path, []byte(`{"level":"info"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := &safeBuffer{}
	done := make(chan struct{})
	go func() {
		processFile(path, out)
		close(done)
	}()

	waitFor(t, 5*time.Second, "first line to be printed", func() bool {
		return strings.Contains(out.String(), `"level":"info"`)
	})

	checkFDs := runtime.GOOS == "linux"
	if checkFDs {
		if n := openFDsTo(t, path); n != 1 {
			t.Fatalf("expected exactly 1 open descriptor to the file while tailing, got %d", n)
		}
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("processFile did not return after the file was deleted")
	}

	if checkFDs {
		if n := openFDsTo(t, path); n != 0 {
			t.Fatalf("expected no open descriptors to the deleted file, got %d", n)
		}
	}
}

// A file discovered but deleted before its tailer started must not leave the
// goroutine waiting forever for the file to appear.
func TestProcessFileReturnsWhenFileIsMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "short-lived.log")

	done := make(chan struct{})
	go func() {
		processFile(path, &safeBuffer{})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("processFile did not return for a missing file")
	}
}

// A file that is deleted and then created again with the same name must be
// tailed again by a new tailer.
func TestWatchFolderPicksUpRecreatedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	watcher := newFolderWatcher(dir)
	defer watcher.Close()

	ctx, cancel := context.WithCancel(context.Background())
	out := &safeBuffer{}
	done := make(chan struct{})
	go func() {
		watchFolder(ctx, watcher, make(map[string]struct{}), out)
		close(done)
	}()
	defer func() {
		cancel()
		<-done
	}()

	if err := os.WriteFile(path, []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "line from the first file", func() bool {
		return strings.Contains(out.String(), "app.log:first")
	})

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "linux" {
		waitFor(t, 5*time.Second, "descriptor of the deleted file to be closed", func() bool {
			return openFDsTo(t, path) == 0
		})
	}

	if err := os.WriteFile(path, []byte("second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "line from the re-created file", func() bool {
		return strings.Contains(out.String(), "app.log:second")
	})

	if got := strings.Count(out.String(), "New file created: "+path); got != 2 {
		t.Fatalf("expected the file to be picked up twice, got %d; output:\n%s", got, out.String())
	}

	// Stop the tailer of the re-created file.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

func TestProcessJSON(t *testing.T) {
	t.Run("plain text gets file prefix", func(t *testing.T) {
		got, err := processJSON("plain line", "app.log")
		if err != nil {
			t.Fatal(err)
		}
		if want := "app.log:plain line"; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("json array is treated as plain text", func(t *testing.T) {
		got, err := processJSON(`[1,2]`, "app.log")
		if err != nil {
			t.Fatal(err)
		}
		if want := "app.log:[1,2]"; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("json object gets file field first", func(t *testing.T) {
		got, err := processJSON(`{"level":"info","ctx":{"a":1}}`, "app.log")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(got, `{"file":"app.log",`) {
			t.Fatalf("output must start with file field, got %q", got)
		}
		var parsed map[string]json.RawMessage
		if err := json.Unmarshal([]byte(got), &parsed); err != nil {
			t.Fatalf("output is not valid JSON: %v (%q)", err, got)
		}
		want := map[string]string{
			"file":  `"app.log"`,
			"level": `"info"`,
			"ctx":   `{"a":1}`,
		}
		if len(parsed) != len(want) {
			t.Fatalf("got keys %v, want %v", parsed, want)
		}
		for k, v := range want {
			if string(parsed[k]) != v {
				t.Fatalf("key %q: got %s, want %s", k, parsed[k], v)
			}
		}
	})
}
