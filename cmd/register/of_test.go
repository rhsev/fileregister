package main

import (
	"github.com/rhsev/fileregister/internal/index"

	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// engineBin returns the fileanchor engine to test against: $FILEANCHOR, else
// one on PATH, else the build `make fileanchor` produces. About half the suite
// needs a live engine, so a missing one is a skip, not a failure — but it is a
// loud one: CI builds the engine first (see .github/workflows/test.yml), and a
// skipped run there means the engine step broke, not that the tests passed.
func engineBin(t *testing.T) string {
	t.Helper()
	if env := os.Getenv("FILEANCHOR"); env != "" {
		return env
	}
	if p, err := exec.LookPath("fileanchor"); err == nil {
		return p
	}
	bin, err := filepath.Abs(filepath.Join("..", "..", ".build", "fileanchor"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("no fileanchor engine: FILEANCHOR unset, none on PATH, none at %s — run `make fileanchor`", bin)
	}
	return bin
}

// grubberTestBin finds grubber the way engineBin finds the engine: GRUBBER_BIN,
// then PATH, then the copy `make grubber` builds. The album reads the Markdown
// layer through grubber, so without one its checks cannot run — and the CI job
// builds one and refuses skips, the same contract as the engine.
func grubberTestBin(t *testing.T) string {
	t.Helper()
	if env := os.Getenv("GRUBBER_BIN"); env != "" {
		return env
	}
	if p, err := exec.LookPath("grubber"); err == nil {
		return p
	}
	bin, err := filepath.Abs(filepath.Join("..", "..", ".build", "grubber"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("no grubber: GRUBBER_BIN unset, none on PATH, none at %s — run `make grubber`", bin)
	}
	return bin
}

// stampID writes an id into the file's kMDItemInformation xattr via a one-shot
// engine invocation, so both sides then read the same stamped identity.
func stampID(t *testing.T, bin, path, id string) {
	t.Helper()
	cmd := exec.Command(bin, "--sync-name", "com.fileregister.id#S")
	cmd.Stdin = strings.NewReader(
		fmt.Sprintf(`{"op":"set_meta","path":%q,"key":"id","value":%q,"mode":"add"}`+"\n", path, id))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("stampID: %v", err)
	}
	if !strings.Contains(string(out), `"ok":true`) {
		t.Fatalf("stampID not ok: %s", out)
	}
}

// ofEnv builds the isolated env for both sides: fixture notes, throwaway HOME,
// no grubber set, and the engine (of always hits it).
func ofEnv(t *testing.T, notes, anchor string) []string {
	t.Helper()
	env := filterEnv(os.Environ(), "GRUBBER_SET")
	for _, k := range []string{"GRUBBER_NOTES", "HOME", "FILEANCHOR"} {
		env = filterEnv(env, k)
	}
	return append(env,
		"GRUBBER_NOTES="+notes,
		"HOME="+t.TempDir(),
		"FILEANCHOR="+anchor,
	)
}

// runGoOf invokes cmdOf in-process under env, capturing stdout, stderr, and code.
func runGoOf(t *testing.T, env []string, args ...string) (string, string, int) {
	t.Helper()
	restore := setEnv(t, env)
	defer restore()
	index.ResetEngine()

	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	code := cmdOf(args)

	// Shut the engine down first: it inherited the swapped os.Stderr pipe, so the
	// stderr read below would block until that fd closes.
	index.ResetEngine()
	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr

	var ob, eb bytes.Buffer
	io.Copy(&ob, outR)
	io.Copy(&eb, errR)
	return ob.String(), eb.String(), code
}

func assertOf(t *testing.T, label string, env []string, args ...string) {
	t.Helper()
	gOut, gErr, gCode := runGoOf(t, env, args...)
	repl := map[string]string{}
	for _, a := range args {
		if strings.HasPrefix(a, "/") {
			repl[a] = "<FILE>"
		}
	}
	assertGolden(t, "of_"+label, cliResult(norm(gOut, repl), norm(gErr, repl), gCode))
}

func TestOf(t *testing.T) {
	anchor := engineBin(t)

	// Index: one record stamped by id, one matched only by filename.
	notes := t.TempDir()
	col := filepath.Join(notes, "collections")
	if err := os.MkdirAll(col, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(col, "docs.jsonl"),
		`{"type":"ref","id":"700000001","binder":["Docs"],"filename":"report.pdf","aka":["r","report"]}`+"\n"+
			`{"type":"ref","id":"800000001","binder":["Photos"],"filename":"beach.jpg"}`+"\n")

	env := ofEnv(t, notes, anchor)

	// Case A — id match: a file stamped with id 700000001.
	fileA := filepath.Join(t.TempDir(), "renamed.pdf") // name differs → must match by id, not filename
	if err := os.WriteFile(fileA, []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	realA, _ := filepath.EvalSymlinks(fileA)
	stampID(t, anchor, realA, "700000001")
	assertOf(t, "id-match", env, realA)

	// Case B — filename fallback: a file named like a record, no id xattr.
	dirB := t.TempDir()
	fileB := filepath.Join(dirB, "beach.jpg")
	if err := os.WriteFile(fileB, []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
	realB, _ := filepath.EvalSymlinks(fileB)
	assertOf(t, "filename-fallback", env, realB)

	// Case C — unmanaged: file with no id and no name match.
	fileC := filepath.Join(t.TempDir(), "stranger.txt")
	if err := os.WriteFile(fileC, []byte("c"), 0644); err != nil {
		t.Fatal(err)
	}
	realC, _ := filepath.EvalSymlinks(fileC)
	assertOf(t, "unmanaged", env, realC)

	// Case D — no such file.
	assertOf(t, "no-file", env, filepath.Join(t.TempDir(), "ghost.txt"))

	// Case E — missing arg.
	assertOf(t, "no-arg", env)
}
