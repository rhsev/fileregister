package main

import (
	"github.com/rhsev/fileregister/internal/index"

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
// are not annotations — annotate, marshal and album must not see them. Nor are
// hidden files, or notes that are not Markdown.
func TestParkedNotesAreNotAnnotations(t *testing.T) {
	notes := t.TempDir()
	col := filepath.Join(notes, "collections")
	os.MkdirAll(filepath.Join(col, "import", "ship"), 0755)
	block := "### a.pdf\n```yaml\ntype: ref\nid: '1'\nbinder: trip\n```\n"
	writeFile(t, filepath.Join(col, "binder_trip.md"), block)
	writeFile(t, filepath.Join(col, "import", "ship", "binder_trip.md"), block)
	writeFile(t, filepath.Join(col, ".hidden.md"), block)

	annos, err := readAnnotations(notes)
	if err != nil {
		t.Fatal(err)
	}
	if len(annos) != 1 || !strings.HasSuffix(annos[0]["_note_file"].(string), "collections/binder_trip.md") {
		t.Errorf("want the one block in binder_trip.md, got %v", annos)
	}
}

// A block is what grubber makes of it: its own fields, and what the note's
// frontmatter hands down unless the block says otherwise.
func TestAnnotationsAreWhatGrubberReads(t *testing.T) {
	notes := t.TempDir()
	col := filepath.Join(notes, "collections")
	os.MkdirAll(col, 0755)
	writeFile(t, filepath.Join(col, "binder_trip.md"),
		"---\nalbum: Safari\n---\n\n"+
			"### a.pdf\n```yaml\ntype: ref\nid: '1'\nbinder: trip\ntitle: Own\n```\n\n"+
			"### b.pdf\n```yaml\ntype: ref\nid: 270450536\nbinder: trip\nalbum: Kalahari\n```\n")

	annos, err := readAnnotations(notes)
	if err != nil {
		t.Fatal(err)
	}
	if len(annos) != 2 {
		t.Fatalf("blocks: %d", len(annos))
	}
	a, b := annos[0], annos[1]
	if a["album"] != "Safari" || a["title"] != "Own" || a["binder"] != "trip" {
		t.Errorf("block a: %v", a)
	}
	if b["album"] != "Kalahari" {
		t.Errorf("a block that overrides the frontmatter lost its value: %v", b)
	}
	if index.AsString(b["id"]) != "270450536" {
		t.Errorf("an unquoted id lost its digits: %v", b["id"])
	}
	if _, ok := a["_mtime"]; ok {
		t.Errorf("grubber's _mtime came along: %v", a)
	}
}

// reindex rebuilds an index line from the record fields of a block only; the
// rest of what a block carries (its curation, an inherited album) stays in the
// Markdown layer.
func TestReindexTakesOnlyIndexFields(t *testing.T) {
	notes := t.TempDir()
	col := filepath.Join(notes, "collections")
	os.MkdirAll(col, 0755)
	writeFile(t, filepath.Join(col, "inbox.jsonl"), "")
	writeFile(t, filepath.Join(col, "binder_trip.md"),
		"---\nalbum: Safari\n---\n\n### a.pdf\n```yaml\ntype: ref\nid: '1'\nbinder: trip\nfilename: a.pdf\ntitle: Own\n```\n")

	if _, se, code := runGoReindex(t, reindexEnv(t, notes)); code != 0 {
		t.Fatalf("reindex: exit %d: %s", code, se)
	}
	got := mustRead(t, filepath.Join(col, "inbox.jsonl"))
	if !strings.Contains(got, `"id":"1"`) || !strings.Contains(got, `"filename":"a.pdf"`) {
		t.Fatalf("record not rebuilt:\n%s", got)
	}
	if strings.Contains(got, "album") || strings.Contains(got, "title") || strings.Contains(got, "_mtime") {
		t.Errorf("more than the record reached the index:\n%s", got)
	}
}

func TestHeadingTextStaysOneLine(t *testing.T) {
	if got := headingText("two\nlines .pdf"); got != "two lines .pdf" {
		t.Errorf("headingText = %q", got)
	}
}
