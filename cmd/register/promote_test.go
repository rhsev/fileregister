package main

import (
	"github.com/rhsev/fileregister/internal/index"

	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func promoteEnv(t *testing.T, notes string) []string {
	t.Helper()
	env := filterEnv(os.Environ(), "GRUBBER_SET")
	for _, k := range []string{"GRUBBER_NOTES", "HOME", "REGISTER_BINDER"} {
		env = filterEnv(env, k)
	}
	return append(env, "GRUBBER_NOTES="+notes, "HOME="+t.TempDir())
}

func runGoPromote(t *testing.T, env []string, args ...string) (string, string, int) {
	t.Helper()
	restore := setEnv(t, env)
	defer restore()
	index.ResetEngine()

	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	code := cmdPromote(args)

	index.ResetEngine()
	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr

	var ob, eb bytes.Buffer
	io.Copy(&ob, outR)
	io.Copy(&eb, errR)
	return ob.String(), eb.String(), code
}

func seedDocsInbox(t *testing.T) string {
	return seedInbox(t,
		`{"type":"ref","id":"1","binder":["Docs"],"filename":"a.pdf"}`,
		`{"type":"ref","id":"2","binder":["Docs"],"filename":"b.pdf"}`)
}

func TestPromote(t *testing.T) {
	n := seedDocsInbox(t)
	env := promoteEnv(t, n)
	md := filepath.Join(n, "collections", "binder_Docs.md")

	// Fresh promote: stdout + the written annotation file are golden.
	o1, e1, c1 := runGoPromote(t, env, "--binder", "Docs")
	assertGolden(t, "promote_fresh", cliResult(o1, e1, c1))
	assertGolden(t, "promote_binder_Docs.md", mustRead(t, md))

	// Idempotent second run: Annotated 0, Already 2; file unchanged.
	o2, e2, c2 := runGoPromote(t, env, "--binder", "Docs")
	assertGolden(t, "promote_idempotent", cliResult(o2, e2, c2))
	assertGolden(t, "promote_binder_Docs.md", mustRead(t, md)) // unchanged
}

func TestPromoteVariants(t *testing.T) {
	cases := map[string][]string{
		"by-id":      {"--binder", "Docs", "--id", "1"},
		"no-binder":  {},
		"no-records": {"--binder", "Ghost"},
		"bad-id":     {"--binder", "Docs", "--id", "nope"},
	}
	for label, args := range cases {
		n := seedDocsInbox(t)
		o, e, c := runGoPromote(t, promoteEnv(t, n), args...)
		assertGolden(t, "promote_"+label, cliResult(o, e, c))
		if label == "by-id" {
			assertGolden(t, "promote_by-id.md", mustRead(t, filepath.Join(n, "collections", "binder_Docs.md")))
		}
	}
}

// TestAddMdBehavior: `add --md` now runs natively and writes the annotation.
func TestAddMdBehavior(t *testing.T) {
	anchor := engineBin(t)
	notes := t.TempDir()
	os.MkdirAll(filepath.Join(notes, "collections"), 0755)
	f := filepath.Join(t.TempDir(), "report.pdf")
	if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(f)

	out, _, code := runGoAdd(t, addEnv(t, notes, anchor), real, "--binder", "Akten", "--md")
	if code != 0 {
		t.Fatalf("add --md exit %d", code)
	}
	if !strings.Contains(out, "annotated: 1 → binder_Akten.md") {
		t.Errorf("missing annotated line:\n%s", out)
	}
	md := filepath.Join(notes, "collections", "binder_Akten.md")
	data, err := os.ReadFile(md)
	if err != nil {
		t.Fatalf("annotation not written: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, "### report.pdf") || !strings.Contains(body, "binder: Akten") {
		t.Errorf("annotation content unexpected:\n%s", body)
	}
	// The block's id must match the file's stamped bookmark id.
	if got := queryStampedID(t, anchor, real); !strings.Contains(got, `"ok":true`) {
		t.Errorf("file not id-stamped: %s", got)
	}
}
