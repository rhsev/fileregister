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

func runGoCleanup(t *testing.T, env []string, args ...string) (string, string, int) {
	t.Helper()
	restore := setEnv(t, env)
	defer restore()
	index.ResetEngine()

	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	code := cmdCleanup(args)

	index.ResetEngine()
	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr

	var ob, eb bytes.Buffer
	io.Copy(&ob, outR)
	io.Copy(&eb, errR)
	return ob.String(), eb.String(), code
}

// seedCleanupFixture: an index with a record in no binder + a Docs member, and an annotation
// file with an ok block, a stale block (binder removed), and an unindexed block.
func seedCleanupFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	col := filepath.Join(dir, "collections")
	if err := os.MkdirAll(col, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(col, "inbox.jsonl"),
		`{"type":"ref","id":"1","binder":["Docs"],"filename":"a.pdf"}`+"\n"+
			`{"type":"ref","id":"3","binder":[],"filename":"c.pdf"}`+"\n")
	writeFile(t, filepath.Join(col, "binder_Docs.md"),
		"### a.pdf\n```yaml\ntype: ref\nid: '1'\nbinder: Docs\n```\n\n"+
			"### a.pdf\n```yaml\ntype: ref\nid: '1'\nbinder: OldRemoved\n```\n\n"+
			"### x\n```yaml\ntype: ref\nid: '2'\nbinder: Docs\n```\n")
	return dir
}

func cleanupEnv(t *testing.T, notes string) []string {
	t.Helper()
	env := filterEnv(os.Environ(), "GRUBBER_SET")
	for _, k := range []string{"GRUBBER_NOTES", "HOME", "REGISTER_BINDER"} {
		env = filterEnv(env, k)
	}
	return append(env, "GRUBBER_NOTES="+notes, "HOME="+t.TempDir())
}

func TestCleanup(t *testing.T) {
	cases := map[string][]string{
		"report":  {},
		"dry-run": {"--interactive", "--dry-run"},
	}
	for label, args := range cases {
		gN := seedCleanupFixture(t)
		gOut, gErr, gCode := runGoCleanup(t, cleanupEnv(t, gN), args...)
		assertGolden(t, "cleanup_"+label, cliResult(gOut, gErr, gCode))
	}

	empty := t.TempDir()
	os.MkdirAll(filepath.Join(empty, "collections"), 0755)
	gOut, gErr, gCode := runGoCleanup(t, cleanupEnv(t, empty))
	assertGolden(t, "cleanup_empty", cliResult(gOut, gErr, gCode))
}

// TestMdDeleteBlock: deleting one block by (id, binder) removes it and its heading,
// leaving the other block intact.
func TestMdDeleteBlock(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "n.md")
	writeFile(t, md,
		"# Notes\n\n### a.pdf\n```yaml\ntype: ref\nid: '1'\nbinder: Docs\n```\n\n"+
			"### b.pdf\n```yaml\ntype: ref\nid: '2'\nbinder: Docs\n```\n")

	n, err := mdDeleteBlock(md, "1", "Docs")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("deleted %d blocks, want 1", n)
	}
	data, _ := os.ReadFile(md)
	body := string(data)
	if strings.Contains(body, "id: '1'") {
		t.Errorf("block 1 not deleted:\n%s", body)
	}
	if !strings.Contains(body, "id: '2'") || !strings.Contains(body, "### b.pdf") {
		t.Errorf("block 2 should remain:\n%s", body)
	}
	// Non-matching delete is a no-op.
	if got, _ := mdDeleteBlock(md, "999", "Docs"); got != 0 {
		t.Errorf("non-matching delete returned %d, want 0", got)
	}
}
