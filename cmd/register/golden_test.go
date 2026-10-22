package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// -update regenerates the golden files from the current (correct) output.
//
//	go test -run TestX -update
var updateGolden = flag.Bool("update", false, "update golden files in testdata/")

// assertGolden compares got against testdata/<name>.golden. With -update it
// writes the file instead. The golden file freezes the verified-correct output,
// so any regression surfaces as a diff.
//
// Off macOS a command may word a line differently (it names the engine's
// search, not Spotlight, and attributes by their role). Such output lives in
// testdata/<name>.linux.golden; where Linux prints the same, the one golden
// serves both, and -update on Linux writes a .linux.golden only for a
// difference.
func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	linux := filepath.Join("testdata", name+".linux.golden")
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0755); err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "darwin" {
			if shared, err := os.ReadFile(path); err == nil && string(shared) == got {
				os.Remove(linux)
				return
			}
			path = linux
		}
		if err := os.WriteFile(path, []byte(got), 0644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if runtime.GOOS != "darwin" {
		if _, err := os.Stat(linux); err == nil {
			path = linux
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %q missing — regenerate with `go test -update`: %v", name, err)
	}
	if got != string(want) {
		t.Errorf("%s: output changed from golden\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// cliResult renders a command's stdout/stderr/exit into one golden-able string.
func cliResult(stdout, stderr string, code int) string {
	return fmt.Sprintf("exit: %d\n=== stdout ===\n%s=== stderr ===\n%s", code, stdout, stderr)
}

// mustRead returns a produced file's contents (for goldening).
func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// norm replaces each temp path with a stable placeholder so golden output is
// deterministic across runs.
func norm(s string, repl map[string]string) string {
	for path, placeholder := range repl {
		if path != "" {
			s = strings.ReplaceAll(s, path, placeholder)
		}
	}
	return s
}
