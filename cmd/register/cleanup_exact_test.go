package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Deleting one stale block must leave everything else in a hand-edited note:
// other blocks of the same id, and section headings that are not the
// block's own.
func TestDeleteBlockIsExact(t *testing.T) {
	p := filepath.Join(t.TempDir(), "binder_proj.md")
	doc := "# Project\n\nMy working notes.\n\n" +
		"## Links worth keeping\n\n```yaml\ntype: ref\nid: '1'\nbinder: old\n```\n\n" +
		"### a.pdf\n```yaml\ntype: ref\nid: '1'\n```\n\n" +
		"### a.pdf\n```yaml\ntype: ref\nid: '1'\nbinder: proj\ncomment: important curated caption\n```\n"
	writeFile(t, p, doc)

	if n, err := mdDeleteBlock(p, "1", "old"); err != nil || n != 1 {
		t.Fatalf("delete (1, old): %d, %v", n, err)
	}
	got := mustRead(t, p)
	if !strings.Contains(got, "## Links worth keeping") {
		t.Errorf("a section heading went with the block:\n%s", got)
	}

	if n, err := mdDeleteBlock(p, "1", ""); err != nil || n != 1 {
		t.Fatalf("delete (1, no binder): %d, %v", n, err)
	}
	got = mustRead(t, p)
	if !strings.Contains(got, "comment: important curated caption") || !strings.Contains(got, "My working notes.") {
		t.Errorf("deleting the block without binder took other blocks of the id:\n%s", got)
	}
}

// A block that names no binder claims no membership, so it is never stale.
func TestCleanupDoesNotCallABinderlessBlockStale(t *testing.T) {
	dir := t.TempDir()
	col := filepath.Join(dir, "collections")
	os.MkdirAll(col, 0755)
	writeFile(t, filepath.Join(col, "inbox.jsonl"), `{"type":"ref","id":"1","binder":["proj"],"filename":"a.pdf"}`+"\n")
	writeFile(t, filepath.Join(col, "binder_proj.md"),
		"### a.pdf\n```yaml\ntype: ref\nid: '1'\n```\n\n### a.pdf\n```yaml\ntype: ref\nid: '1'\nbinder: proj\n```\n")

	out, _, _ := runGoCleanup(t, cleanupEnv(t, dir))
	if !strings.Contains(out, "=== Stale context blocks (0) ===") {
		t.Errorf("a binderless block was reported as stale:\n%s", out)
	}
}
