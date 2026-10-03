package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A note saved with \r\n line endings is read like any other and keeps its
// line endings when edited; its blocks used to be invisible.
func TestCRLFNoteIsReadAndKeptCRLF(t *testing.T) {
	note := filepath.Join(t.TempDir(), "binder_proj.md")
	crlf := strings.ReplaceAll("# Proj\n\n### a.pdf\n```yaml\ntype: ref\nid: '1'\nbinder: proj\n```\n", "\n", "\r\n")
	writeFile(t, note, crlf)

	if blocks := mdAllRefs(note); len(blocks) != 1 {
		t.Fatalf("blocks in a CRLF note: %d", len(blocks))
	}
	rec := map[string]any{"id": "1", "filename": "a.pdf"}
	if got := orderWriteOverride(note, "proj", rec, "sort", "m"); got != "updated" {
		t.Fatalf("edit in a CRLF note: %s", got)
	}
	got := mustRead(t, note)
	if strings.Count(got, "```yaml") != 1 || !strings.Contains(got, "sort: m\r\n") ||
		strings.Count(got, "\n") != strings.Count(got, "\r\n") {
		t.Errorf("after the edit:\n%q", got)
	}
}

// Notes parked by unmarshal under collections/import/ wait for the user; they
// are not annotations — annotate, marshal and album must not see them.
func TestParkedNotesAreNotAnnotations(t *testing.T) {
	notes := t.TempDir()
	col := filepath.Join(notes, "collections")
	os.MkdirAll(filepath.Join(col, "import", "ship"), 0755)
	block := "### a.pdf\n```yaml\ntype: ref\nid: '1'\nbinder: trip\n```\n"
	writeFile(t, filepath.Join(col, "binder_trip.md"), block)
	writeFile(t, filepath.Join(col, "import", "ship", "binder_trip.md"), block)

	for _, r := range readAnnotations(notes) {
		if strings.Contains(r["_note_file"].(string), "/import/") {
			t.Errorf("a parked note was read as an annotation: %s", r["_note_file"])
		}
	}
	for _, f := range mdFilesWalk(notes) {
		if strings.Contains(f, "/import/") {
			t.Errorf("the walk found a parked note: %s", f)
		}
	}
}
