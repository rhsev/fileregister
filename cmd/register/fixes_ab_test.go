package main

import (
	"github.com/rhsev/fileregister/v2/internal/index"

	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const refBlock = "type: ref\nid: '1'\nbinder: Docs\n"

// The writers find every block grubber reads: an empty block closes at its
// own fence, and a fence may be indented or carry a trailing blank.
func TestWritersSeeTheBlocksGrubberReads(t *testing.T) {
	note := filepath.Join(t.TempDir(), "n.md")

	writeFile(t, note, "```yaml\n```\n\ntext\n\n### a.pdf\n```yaml\n"+refBlock+"```\n")
	if refs := mdAllRefs(note); len(refs) != 1 || index.AsString(refs[0]["id"]) != "1" {
		t.Fatalf("block after an empty block: got %v", refs)
	}

	for name, content := range map[string]string{
		"trailing blank": "### a.pdf\n```yaml \n" + refBlock + "```\n",
		"indented":       "- item\n\n  ```yaml\n  type: ref\n  id: '1'\n  binder: Docs\n  ```\n",
	} {
		writeFile(t, note, content)
		rec := map[string]any{"id": "1", "filename": "a.pdf"}
		if promoted, noop, failed := promoteRecords([]map[string]any{rec}, note, "Docs"); promoted != 0 || noop != 1 || failed != 0 {
			t.Errorf("%s: promote must see the block, got promoted=%d noop=%d failed=%d", name, promoted, noop, failed)
		}
		if got := mustRead(t, note); got != content {
			t.Errorf("%s: note changed by a noop promote:\n%s", name, got)
		}
		n, err := mdRenameBinder(note, "Docs", "Papers")
		if err != nil || n != 1 {
			t.Fatalf("%s: rename changed %d block(s), err %v", name, n, err)
		}
		want := strings.Replace(content, "binder: Docs", "binder: Papers", 1)
		if got := mustRead(t, note); got != want {
			t.Errorf("%s: after rename\n got: %q\nwant: %q", name, got, want)
		}
		if n, err := mdDeleteBlock(note, "1", "Papers"); err != nil || n != 1 {
			t.Errorf("%s: delete removed %d block(s), err %v", name, n, err)
		}
	}

	// A fence of another language is no closing fence.
	text := "```yaml\n" + refBlock + "\n```sh\necho\n```\n"
	if blocks := mdBlocks(text); len(blocks) != 1 || !strings.HasSuffix(blocks[0].body, "echo") {
		t.Errorf("```sh must not close a yaml block, got %v", blocks)
	}
}

// A note keeps the line ending most of its lines have, and its byte order
// mark; neither hides the frontmatter from the writers.
func TestNoteKeepsItsLineEndingsAndBOM(t *testing.T) {
	note := filepath.Join(t.TempDir(), "n.md")

	writeFile(t, note, "one\ntwo\r\nthree\n")
	content, style, err := readNote(note)
	if err != nil || style.crlf {
		t.Fatalf("one pasted CRLF line must not make the note CRLF: %+v %v", style, err)
	}
	if err := writeNote(note, content+"four\n", style); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, note); got != "one\ntwo\nthree\nfour\n" {
		t.Errorf("LF note: got %q", got)
	}

	writeFile(t, note, "one\r\ntwo\r\n")
	content, style, _ = readNote(note)
	if !style.crlf || content != "one\ntwo\n" {
		t.Fatalf("CRLF note: %+v %q", style, content)
	}
	writeNote(note, content, style)
	if got := mustRead(t, note); got != "one\r\ntwo\r\n" {
		t.Errorf("CRLF note: got %q", got)
	}

	writeFile(t, note, utf8BOM+"---\ntitle: x\n---\n\nbody\n")
	if what, err := upsertFrontmatterField(note, "album", "Trip"); err != nil || what != "created" {
		t.Fatalf("upsert: %q %v", what, err)
	}
	if got := mustRead(t, note); got != utf8BOM+"---\ntitle: x\nalbum: Trip\n---\n\nbody\n" {
		t.Errorf("BOM note: got %q", got)
	}
}

func TestMoveOrCopyRefusesAnExistingTarget(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "src"), filepath.Join(dir, "dst")
	writeFile(t, src, "new")
	writeFile(t, dst, "old")
	if err := moveOrCopy(src, dst, time.Time{}); err == nil {
		t.Fatal("moveOrCopy replaced an existing file")
	}
	if mustRead(t, dst) != "old" || mustRead(t, src) != "new" {
		t.Error("both files must be as they were")
	}
}

func TestRenameRefusesAnEmptyName(t *testing.T) {
	notes := seedCleanupFixture(t)
	before := mustRead(t, filepath.Join(notes, "collections", "inbox.jsonl"))
	for _, args := range [][]string{{"Docs", ""}, {"", "Docs"}} {
		if _, _, code := runGoRename(t, cleanupEnv(t, notes), args...); code != 1 {
			t.Errorf("rename %q: exit %d, want 1", args, code)
		}
	}
	if got := mustRead(t, filepath.Join(notes, "collections", "inbox.jsonl")); got != before {
		t.Errorf("index changed:\n%s", got)
	}
}

// cleanup compares binder names as labels and takes a handle in the id slot
// for the record it names; reindex makes no second record from such a block.
func TestCleanupAndReindexOnDecomposedBinderAndHandle(t *testing.T) {
	needGrubber(t)
	notes := t.TempDir()
	col := filepath.Join(notes, "collections")
	os.MkdirAll(col, 0755)
	indexFile := filepath.Join(col, "inbox.jsonl")
	writeFile(t, indexFile,
		`{"type":"ref","id":"1","binder":["Bücher"],"filename":"a.pdf"}`+"\n"+
			`{"type":"ref","id":"33","binder":["Anno"],"aka":["ha"],"filename":"b.pdf"}`+"\n")
	writeFile(t, filepath.Join(col, "notes.md"),
		"### a.pdf\n```yaml\ntype: ref\nid: '1'\nbinder: Bücher\n```\n\n"+
			"### b.pdf\n```yaml\ntype: ref\nid: ha\nbinder: Anno\n```\n")

	out, _, _ := runGoCleanup(t, cleanupEnv(t, notes))
	for _, want := range []string{"=== Stale context blocks (0) ===", "=== Unindexed annotations (0) ==="} {
		if !strings.Contains(out, want) {
			t.Errorf("cleanup: want %q in\n%s", want, out)
		}
	}

	before := mustRead(t, indexFile)
	runGoReindex(t, reindexEnv(t, notes))
	if got := mustRead(t, indexFile); got != before {
		t.Errorf("reindex made a record from a handle block:\n%s", got)
	}
}

func TestListBinderInboxAndCurated(t *testing.T) {
	needGrubber(t)
	notes := seedCleanupFixture(t) // a.pdf (id 1) has a block in Docs
	f, err := os.OpenFile(filepath.Join(notes, "collections", "inbox.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"type":"ref","id":"9","binder":["Docs"],"filename":"plain.pdf"}` + "\n")
	f.Close()
	env := listEnv(t, notes, engineBin(t))

	inbox, _, _ := runGoList(t, env, "Docs", "--inbox", "--json")
	if !strings.Contains(inbox, "plain.pdf") || strings.Contains(inbox, "a.pdf") {
		t.Errorf("--inbox: got\n%s", inbox)
	}
	curated, _, _ := runGoList(t, env, "Docs", "--curated", "--json")
	if strings.Contains(curated, "plain.pdf") || !strings.Contains(curated, "a.pdf") {
		t.Errorf("--curated: got\n%s", curated)
	}
}

func TestAlbumTitleNeedsAnExistingBinder(t *testing.T) {
	needGrubber(t)
	notes := seedCleanupFixture(t)
	_, _, code := runGoAlbum(t, albumEnv(t, notes, engineBin(t)), "Typo", "--title", "X")
	if code == 0 {
		t.Error("album for an unknown binder must fail")
	}
	if _, err := os.Stat(defaultPromoteTarget(notes, "Typo")); err == nil {
		t.Error("a note was written for a binder that does not exist")
	}
}

// The dead bookmark of a record that is still in a binder is what lets forget
// tell that its file is gone, so --prune keeps it.
func TestPruneKeepsTheDeadEntryOfAMember(t *testing.T) {
	needGrubber(t)
	notes := t.TempDir()
	env := identityEnv(t, notes, stubSearchable(t, engineBin(t), `{"ok":true,"searchable":true}`))
	restore := setEnv(t, env)
	index.ResetEngine()
	_, _, id := goneBookmark(t)
	index.ResetEngine()
	restore()

	col := filepath.Join(notes, "collections")
	os.MkdirAll(col, 0755)
	writeFile(t, filepath.Join(col, "inbox.jsonl"),
		`{"type":"ref","id":"`+id+`","binder":["Docs"],"filename":"gone.pdf"}`+"\n")

	out, errOut, _ := runIn(t, env, cmdCleanup, "--prune")
	if !strings.Contains(out, "register forget "+id) {
		t.Errorf("cleanup must point to forget:\n%s\n%s", out, errOut)
	}
	restore = setEnv(t, env)
	defer restore()
	db, err := index.LoadDB()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := db[id]; !ok {
		t.Error("--prune dropped the bookmark entry of a record that is still in a binder")
	}
}

// The readers see what the writers see. An empty yaml block used to hide the
// block after it from grubber, up to v0.20.0: promote and rename then worked
// on a block that list --curated, annotate and cleanup did not find.
func TestReadersSeeTheBlockAfterAnEmptyOne(t *testing.T) {
	needGrubber(t)
	notes := t.TempDir()
	col := filepath.Join(notes, "collections")
	os.MkdirAll(col, 0755)
	writeFile(t, filepath.Join(col, "binder_Docs.md"),
		"### empty\n```yaml\n```\n\n### doc\n```yaml\n"+refBlock+"```\n")
	restore := setEnv(t, annotateEnv(t, notes))
	defer restore()
	blocks, err := readAnnotations(notes)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 1 || index.AsString(blocks[0]["id"]) != "1" {
		t.Errorf("grubber read %d ref block(s), want the one after the empty block: %v", len(blocks), blocks)
	}
}
