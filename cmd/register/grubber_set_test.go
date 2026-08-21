package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func captureStderr(fn func()) string {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	fn()
	w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}

func withEnv(t *testing.T, key, val string, fn func()) {
	t.Helper()
	prev, hadPrev := os.LookupEnv(key)
	if val == "" {
		os.Unsetenv(key)
	} else {
		os.Setenv(key, val)
	}
	fn()
	if hadPrev {
		os.Setenv(key, prev)
	} else {
		os.Unsetenv(key)
	}
}

// makeGrubberConfig writes a minimal grubber config.yaml for testing.
func makeGrubberConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(content), 0644)
	return path
}

func TestGrubberSetMissingConfig(t *testing.T) {
	os.Unsetenv("GRUBBER_NOTES")
	os.Setenv("GRUBBER_SET", "mySet")
	os.Setenv("GRUBBER_CONFIG", "/nonexistent/path/config.yaml")
	defer func() {
		os.Unsetenv("GRUBBER_SET")
		os.Unsetenv("GRUBBER_CONFIG")
	}()

	var gotErr string
	var stderr string
	stderr = captureStderr(func() {
		_, err := notesDir()
		if err != nil {
			gotErr = err.Error()
		}
	})

	if !strings.Contains(stderr, "Error: GRUBBER_SET='mySet' given, but no grubber config at /nonexistent/path/config.yaml") {
		t.Errorf("missing config error message wrong:\n%s", stderr)
	}
	if gotErr == "" {
		t.Error("expected error return")
	}
}

func TestGrubberSetUnknown(t *testing.T) {
	cfgPath := makeGrubberConfig(t, "sets:\n  Alpha:\n    path: /tmp/alpha\n  Beta:\n    path: /tmp/beta\n")
	os.Setenv("GRUBBER_SET", "alpha") // lowercase — won't match "Alpha"
	os.Setenv("GRUBBER_CONFIG", cfgPath)
	os.Unsetenv("GRUBBER_NOTES")
	defer func() {
		os.Unsetenv("GRUBBER_SET")
		os.Unsetenv("GRUBBER_CONFIG")
	}()

	stderr := captureStderr(func() {
		notesDir()
	})

	if !strings.Contains(stderr, "Error: GRUBBER_SET='alpha' is not a set in") {
		t.Errorf("unknown set error missing: %s", stderr)
	}
	if !strings.Contains(stderr, "Hint: did you mean 'Alpha'?") {
		t.Errorf("did-you-mean hint missing: %s", stderr)
	}
	if !strings.Contains(stderr, "Available sets:") {
		t.Errorf("available sets missing: %s", stderr)
	}
}

func TestGrubberSetNoPath(t *testing.T) {
	cfgPath := makeGrubberConfig(t, "sets:\n  mySet:\n    kind: notes\n")
	os.Setenv("GRUBBER_SET", "mySet")
	os.Setenv("GRUBBER_CONFIG", cfgPath)
	os.Unsetenv("GRUBBER_NOTES")
	defer func() {
		os.Unsetenv("GRUBBER_SET")
		os.Unsetenv("GRUBBER_CONFIG")
	}()

	stderr := captureStderr(func() {
		notesDir()
	})

	if !strings.Contains(stderr, "Error: GRUBBER_SET='mySet' has no `path:` entry in") {
		t.Errorf("no path error wrong: %s", stderr)
	}
}

func TestGrubberSetDirMissing(t *testing.T) {
	cfgPath := makeGrubberConfig(t, "sets:\n  mySet:\n    path: /nonexistent/notes/dir\n")
	os.Setenv("GRUBBER_SET", "mySet")
	os.Setenv("GRUBBER_CONFIG", cfgPath)
	os.Unsetenv("GRUBBER_NOTES")
	defer func() {
		os.Unsetenv("GRUBBER_SET")
		os.Unsetenv("GRUBBER_CONFIG")
	}()

	stderr := captureStderr(func() {
		notesDir()
	})

	if !strings.Contains(stderr, "Error: GRUBBER_SET='mySet' points to a path that doesn't exist:") {
		t.Errorf("missing dir error wrong: %s", stderr)
	}
	if !strings.Contains(stderr, "/nonexistent/notes/dir") {
		t.Errorf("missing path in error: %s", stderr)
	}
	if !strings.Contains(stderr, "Hint: check the path entry under sets.mySet") {
		t.Errorf("missing hint: %s", stderr)
	}
}

func TestGrubberNotesNotSet(t *testing.T) {
	os.Unsetenv("GRUBBER_SET")
	os.Unsetenv("GRUBBER_NOTES")

	stderr := captureStderr(func() {
		notesDir()
	})

	if !strings.Contains(stderr, "Error: GRUBBER_NOTES is not set or not a directory (and no GRUBBER_SET given)") {
		t.Errorf("GRUBBER_NOTES error wrong: %s", stderr)
	}
}

func TestGrubberSetSuccess(t *testing.T) {
	dir := t.TempDir()
	cfgPath := makeGrubberConfig(t, "sets:\n  work:\n    path: "+dir+"\n")
	os.Setenv("GRUBBER_SET", "work")
	os.Setenv("GRUBBER_CONFIG", cfgPath)
	os.Unsetenv("GRUBBER_NOTES")
	defer func() {
		os.Unsetenv("GRUBBER_SET")
		os.Unsetenv("GRUBBER_CONFIG")
	}()

	got, err := notesDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != dir {
		t.Errorf("got %q want %q", got, dir)
	}
}
