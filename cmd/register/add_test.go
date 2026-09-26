package main

import (
	"github.com/rhsev/fileregister/internal/index"

	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func addEnv(t *testing.T, notes, anchor string) []string {
	t.Helper()
	env := filterEnv(os.Environ(), "GRUBBER_SET")
	for _, k := range []string{"GRUBBER_NOTES", "HOME", "FILEANCHOR", "REGISTER_BINDER"} {
		env = filterEnv(env, k)
	}
	return append(env,
		"GRUBBER_NOTES="+notes,
		"HOME="+t.TempDir(),
		"FILEANCHOR="+anchor,
	)
}

func runGoAdd(t *testing.T, env []string, args ...string) (string, string, int) {
	t.Helper()
	restore := setEnv(t, env)
	defer restore()
	index.ResetEngine()

	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	code := cmdAdd(args)

	index.ResetEngine()
	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr

	var ob, eb bytes.Buffer
	io.Copy(&ob, outR)
	io.Copy(&eb, errR)
	return ob.String(), eb.String(), code
}

// TestAddValidation: arg-validation error output (golden).
func TestAddValidation(t *testing.T) {
	anchor := engineBin(t)

	cases := []struct {
		label string
		args  []string
	}{
		{"url+files", []string{"--url", "x-devonthink-item://X", "somefile.pdf"}},
		{"xattr+url", []string{"--url", "x-devonthink-item://X", "--xattr", "tags"}},
		{"no-files", []string{"--binder", "B"}},
		{"bad-xattr", []string{"foo.pdf", "--binder", "B", "--xattr", "bogus"}},
		{"aka-multi", []string{"a.pdf", "b.pdf", "--binder", "B", "--aka", "k"}},
		{"bad-url", []string{"--url", "not-a-url", "--binder", "B"}},
	}
	for _, c := range cases {
		gOut, gErr, gCode := runGoAdd(t, addEnv(t, t.TempDir(), anchor), c.args...)
		assertGolden(t, "add_"+c.label, cliResult(gOut, gErr, gCode))
	}
}

// seedInbox writes an inbox.jsonl with the given lines into a fresh notes dir.
func seedInbox(t *testing.T, lines ...string) string {
	t.Helper()
	dir := t.TempDir()
	col := filepath.Join(dir, "collections")
	if err := os.MkdirAll(col, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(col, "inbox.jsonl"), strings.Join(lines, "\n")+"\n")
	return dir
}

// TestAddUrlReuse: adding a URL that already exists set-inserts the new
// binder into its record — deterministic, the id is reused.
func TestAddUrlReuse(t *testing.T) {
	anchor := engineBin(t)
	n := seedInbox(t, `{"type":"ref","id":"5","binder":["A"],"url":"x-devonthink-item://X","filename":"x.md","kind":"devonthink"}`)
	gOut, gErr, gCode := runGoAdd(t, addEnv(t, n, anchor), "--url", "x-devonthink-item://X", "--binder", "B")
	assertGolden(t, "add_url-reuse", cliResult(gOut, gErr, gCode))
	// The record now carries both binders (id reused → deterministic index).
	assertGolden(t, "add_url-reuse_index.jsonl",
		strings.Join(manifestMultiset(t, filepath.Join(n, "collections", "inbox.jsonl")), "\n")+"\n")
}

// TestAddAkaNotApplied: --aka on an existing record lands only as a backfill
// (new membership, no aka yet); otherwise add says so and names the verb.
func TestAddAkaNotApplied(t *testing.T) {
	anchor := engineBin(t)
	n := seedInbox(t, `{"type":"ref","id":"5","binder":["A"],"url":"x-devonthink-item://X","aka":["old"]}`)
	_, gErr, gCode := runGoAdd(t, addEnv(t, n, anchor), "--url", "x-devonthink-item://X", "--binder", "A", "--aka", "new")
	if gCode != 0 || !strings.Contains(gErr, "--aka 'new' not applied; use: register aka 5 --add new") {
		t.Errorf("want not-applied note, exit 0; got %d:\n%s", gCode, gErr)
	}

	n = seedInbox(t, `{"type":"ref","id":"5","binder":["A"],"url":"x-devonthink-item://X"}`)
	_, gErr, gCode = runGoAdd(t, addEnv(t, n, anchor), "--url", "x-devonthink-item://X", "--binder", "B", "--aka", "new")
	if gCode != 0 || strings.Contains(gErr, "not applied") {
		t.Errorf("backfill case must stay silent; got %d:\n%s", gCode, gErr)
	}
}

// TestAddFileBehavior: a real file add writes the index record, stamps the id,
// and marks ★.
func TestAddFileBehavior(t *testing.T) {
	anchor := engineBin(t)
	notes := t.TempDir()
	os.MkdirAll(filepath.Join(notes, "collections"), 0755)

	f := filepath.Join(t.TempDir(), "report.pdf")
	if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(f)

	_, _, code := runGoAdd(t, addEnv(t, notes, anchor), real, "--binder", "Testbinder")
	if code != 0 {
		t.Fatalf("add exit %d", code)
	}

	idx := manifestMultiset(t, filepath.Join(notes, "collections", "inbox.jsonl"))
	joined := strings.Join(idx, "\n")
	for _, want := range []string{`"binder":["Testbinder"]`, `"filename":"report.pdf"`, `"kind":"pdf"`} {
		if !strings.Contains(joined, want) {
			t.Errorf("index missing %s:\n%s", want, joined)
		}
	}
	// id stamped + ★ marked on the file.
	if got := queryStampedID(t, anchor, real); !strings.Contains(got, `"ok":true`) || !strings.Contains(got, `"values"`) {
		t.Errorf("file not id-stamped: %s", got)
	}
	if !strings.Contains(queryTags(t, anchor, real), "★") {
		t.Errorf("file not ★-marked")
	}
}

// TestAddIdempotent: re-adding the same file to the same binder is a noop.
func TestAddIdempotent(t *testing.T) {
	anchor := engineBin(t)
	notes := t.TempDir()
	os.MkdirAll(filepath.Join(notes, "collections"), 0755)
	env := addEnv(t, notes, anchor) // shared HOME across both runs → bookmark id persists

	f := filepath.Join(t.TempDir(), "doc.pdf")
	if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(f)

	if _, _, code := runGoAdd(t, env, real, "--binder", "B"); code != 0 {
		t.Fatalf("first add exit %d", code)
	}
	out, _, code := runGoAdd(t, env, real, "--binder", "B")
	if code != 0 {
		t.Fatalf("second add exit %d", code)
	}
	if !strings.Contains(out, "noop     : 1") || !strings.Contains(out, "appended : 0") {
		t.Errorf("second add not a noop:\n%s", out)
	}
}

// queryTags reads a file's Finder tags via a one-shot engine call.
func queryTags(t *testing.T, anchor, path string) string {
	t.Helper()
	cmd := exec.Command(anchor, "--sync-name", "com.fileregister.id#S")
	cmd.Stdin = strings.NewReader(`{"op":"tags","path":"` + path + `"}` + "\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("queryTags: %v", err)
	}
	return string(out)
}
