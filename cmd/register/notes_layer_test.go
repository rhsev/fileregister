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

// For register's own commands a block is a record and is read as it stands:
// nothing comes down from the note's frontmatter, an album: there included.
// A note without blocks is no block, whatever its header says.
func TestAnnotationsCarryOnlyTheBlock(t *testing.T) {
	notes := t.TempDir()
	col := filepath.Join(notes, "collections")
	os.MkdirAll(col, 0755)
	writeFile(t, filepath.Join(col, "binder_trip.md"),
		"---\nalbum: Safari\nurl: https://example.com/note\n---\n\n"+
			"### a.pdf\n```yaml\ntype: ref\nid: '1'\nbinder: trip\ntitle: Own\n```\n\n"+
			"### b.pdf\n```yaml\ntype: ref\nid: 270450536\nbinder: trip\nalbum: Kalahari\n```\n")
	writeFile(t, filepath.Join(col, "header_only.md"), "---\ntype: ref\nid: '9'\nbinder: trip\n---\n\ntext\n")

	annos, err := readAnnotations(notes)
	if err != nil {
		t.Fatal(err)
	}
	if len(annos) != 2 {
		t.Fatalf("blocks: %d (a note without blocks counted?) %v", len(annos), annos)
	}
	a, b := annos[0], annos[1]
	if _, inherited := a["album"]; inherited {
		t.Errorf("the frontmatter's album came along: %v", a)
	}
	if _, inherited := a["url"]; inherited {
		t.Errorf("the frontmatter's url came along: %v", a)
	}
	if a["binder"] != "trip" || a["title"] != "Own" {
		t.Errorf("the block's own fields went missing: %v", a)
	}
	if b["album"] != "Kalahari" {
		t.Errorf("a block's own value went missing: %v", b)
	}
	if index.AsString(b["id"]) != "270450536" {
		t.Errorf("an unquoted id lost its digits: %v", b["id"])
	}
}

// reindex rebuilds index lines from blocks and takes url, filename and kind
// from them. A note's own url: in the frontmatter must not turn a file record
// into a URL record, and nothing else from the header reaches the index.
func TestReindexDoesNotIndexTheFrontmatter(t *testing.T) {
	notes := t.TempDir()
	col := filepath.Join(notes, "collections")
	os.MkdirAll(col, 0755)
	writeFile(t, filepath.Join(col, "inbox.jsonl"), "")
	writeFile(t, filepath.Join(col, "binder_trip.md"),
		"---\nurl: https://example.com/this-note\nalbum: Safari\n---\n\n### a.pdf\n```yaml\ntype: ref\nid: '1'\nbinder: trip\nfilename: a.pdf\ntags: [draft]\n```\n")

	if _, se, code := runGoReindex(t, reindexEnv(t, notes)); code != 0 {
		t.Fatalf("reindex: exit %d: %s", code, se)
	}
	got := mustRead(t, filepath.Join(col, "inbox.jsonl"))
	if !strings.Contains(got, `"id":"1"`) || !strings.Contains(got, `"filename":"a.pdf"`) {
		t.Fatalf("record not rebuilt:\n%s", got)
	}
	if strings.Contains(got, "example.com") || strings.Contains(got, "album") {
		t.Errorf("the note's frontmatter reached the index:\n%s", got)
	}
	// A label in the block is annotation and stays there.
	if strings.Contains(got, "draft") {
		t.Errorf("the block's tags reached the index:\n%s", got)
	}
}

func TestHeadingTextStaysOneLine(t *testing.T) {
	if got := headingText("two\nlines .pdf"); got != "two lines .pdf" {
		t.Errorf("headingText = %q", got)
	}
}
