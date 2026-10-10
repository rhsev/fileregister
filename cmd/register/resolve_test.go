package main

import (
	"github.com/rhsev/fileregister/internal/index"

	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// makeResolveFixture builds an index with a URL record (deterministic resolve, no
// bookmark needed) and a file ref (whose bookmark is absent → broken).
func makeResolveFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	col := filepath.Join(dir, "collections")
	if err := os.MkdirAll(col, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(col, "links.jsonl"),
		`{"type":"ref","id":"500000001","binder":["Links"],"url":"x-devonthink-item://ABC","aka":"mylink"}`+"\n")
	writeFile(t, filepath.Join(col, "docs.jsonl"),
		`{"type":"ref","id":"600000001","binder":["Docs"],"filename":"report.pdf","aka":["r","report"]}`+"\n")
	return dir
}

// resolveEnv is the isolated environment shared by both sides: fixture notes,
// no grubber set, and a throwaway HOME so bookmarks.json is empty on both sides.
func resolveEnv(t *testing.T, notes string) []string {
	t.Helper()
	env := filterEnv(os.Environ(), "GRUBBER_SET")
	env = filterEnv(env, "GRUBBER_NOTES")
	env = filterEnv(env, "HOME")
	env = filterEnv(env, "FILEANCHOR")
	return append(env,
		"GRUBBER_NOTES="+notes,
		"HOME="+t.TempDir(),
	)
}

// runGoResolve invokes cmdResolve in-process, capturing stdout, stderr, and code.
func runGoResolve(t *testing.T, env []string, args ...string) (string, string, int) {
	t.Helper()
	// Apply env for the duration of the call (notesDir/index.LoadDB read the process env).
	restore := setEnv(t, env)
	defer restore()
	// Reset the anchor singleton so a stale engine from a prior test isn't reused.
	index.ResetEngine()

	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	code := cmdResolve(args)

	// Shut any spawned engine down before reading: it inherited the swapped
	// os.Stderr pipe, whose read would otherwise block until that fd closes.
	index.ResetEngine()
	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr

	var ob, eb bytes.Buffer
	io.Copy(&ob, outR)
	io.Copy(&eb, errR)
	return ob.String(), eb.String(), code
}

// setEnv sets exactly the given env for the process, returning a restore func.
func setEnv(t *testing.T, env []string) func() {
	t.Helper()
	saved := os.Environ()
	os.Clearenv()
	for _, kv := range env {
		if i := indexByte(kv, '='); i >= 0 {
			os.Setenv(kv[:i], kv[i+1:])
		}
	}
	return func() {
		os.Clearenv()
		for _, kv := range saved {
			if i := indexByte(kv, '='); i >= 0 {
				os.Setenv(kv[:i], kv[i+1:])
			}
		}
	}
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func assertResolve(t *testing.T, label string, notes string, args ...string) {
	t.Helper()
	gOut, gErr, gCode := runGoResolve(t, resolveEnv(t, notes), args...)
	assertGolden(t, "resolve_"+label, cliResult(gOut, gErr, gCode))
}

func TestResolve(t *testing.T) {
	fx := makeResolveFixture(t)

	assertResolve(t, "url-by-aka", fx, "mylink")             // URL record by aka → URL
	assertResolve(t, "url-by-id", fx, "500000001")           // URL record by id
	assertResolve(t, "url-record", fx, "mylink", "--record") // --record block
	assertResolve(t, "broken-bookmark", fx, "600000001")     // file ref, no bookmark → broken
	assertResolve(t, "broken-record", fx, "report", "--record")
	assertResolve(t, "unknown-key", fx, "nope") // No record resolves
	assertResolve(t, "no-key", fx)              // usage
}
