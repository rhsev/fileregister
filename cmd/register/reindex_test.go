package main

import (
	"github.com/rhsev/fileregister/v2/internal/index"

	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runGoReindex(t *testing.T, env []string, args ...string) (string, string, int) {
	t.Helper()
	restore := setEnv(t, env)
	defer restore()
	index.ResetEngine()

	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	code := cmdReindex(args)

	index.ResetEngine()
	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr

	var ob, eb bytes.Buffer
	io.Copy(&ob, outR)
	io.Copy(&eb, errR)
	return ob.String(), eb.String(), code
}

// seedReindexFixture: index with id 1; annotations for 1 (indexed), 2 and 3 (missing).
func seedReindexFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	col := filepath.Join(dir, "collections")
	if err := os.MkdirAll(col, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(col, "inbox.jsonl"),
		`{"type":"ref","id":"1","binder":["Docs"],"filename":"a.pdf"}`+"\n")
	writeFile(t, filepath.Join(col, "binder_Docs.md"),
		"### a.pdf\n```yaml\ntype: ref\nid: '1'\nbinder: Docs\n```\n\n"+
			"### b.pdf\n```yaml\ntype: ref\nid: '2'\nbinder: Docs\nfilename: b.pdf\n```\n\n"+
			"### c.jpg\n```yaml\ntype: ref\nid: '3'\nbinder: Photos\nfilename: c.jpg\n```\n")
	return dir
}

func reindexEnv(t *testing.T, notes string) []string {
	t.Helper()
	env := filterEnv(os.Environ(), "GRUBBER_SET")
	for _, k := range []string{"GRUBBER_NOTES", "HOME", "REGISTER_BINDER"} {
		env = filterEnv(env, k)
	}
	return append(env, "GRUBBER_NOTES="+notes, "HOME="+t.TempDir())
}

func TestReindex(t *testing.T) {
	// dry-run: lists the 2 missing records, writes nothing.
	dN := seedReindexFixture(t)
	dOut, dErr, dCode := runGoReindex(t, reindexEnv(t, dN), "--dry-run")
	assertGolden(t, "reindex_dry-run", cliResult(dOut, dErr, dCode))

	// actual reindex: adds id 2 and 3; stdout + resulting index are golden.
	aN := seedReindexFixture(t)
	aOut, aErr, aCode := runGoReindex(t, reindexEnv(t, aN))
	assertGolden(t, "reindex_add", cliResult(aOut, aErr, aCode))
	assertGolden(t, "reindex_index.jsonl", strings.Join(manifestMultiset(t, filepath.Join(aN, "collections", "inbox.jsonl")), "\n")+"\n")

	// complete: a second reindex finds nothing to add.
	cOut, cErr, cCode := runGoReindex(t, reindexEnv(t, aN))
	assertGolden(t, "reindex_complete", cliResult(cOut, cErr, cCode))
}
