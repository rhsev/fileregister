package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	uninorm "golang.org/x/text/unicode/norm"
)

// aka: one handle typed composed and decomposed is one handle.
func TestAkaAddTakesTwoFormsAsOneHandle(t *testing.T) {
	needGrubber(t)
	n := seedAkaFixture(t, "")
	nfc, nfd := uninorm.NFC.String("café"), uninorm.NFD.String("café")
	if _, e, code := runGoAka(t, annotateEnv(t, n), "22", "--add", nfc, "--add", nfd); code != 0 {
		t.Fatalf("aka --add: %d %s", code, e)
	}
	rec := inboxByFilename(t, n)["beta.pdf"]
	if got := toStrings(rec["aka"]); len(got) != 1 || got[0] != nfc {
		t.Errorf("aka = %q, want the handle once, composed", got)
	}
}

// aka --remove: a block that holds the handle decomposed in its id: slot is
// the record's block, and gets the real id instead of losing its record.
func TestAkaRemoveFindsADecomposedHandleInABlock(t *testing.T) {
	needGrubber(t)
	nfc, nfd := uninorm.NFC.String("käse"), uninorm.NFD.String("käse")
	dir := t.TempDir()
	col := filepath.Join(dir, "collections")
	os.MkdirAll(col, 0755)
	writeFile(t, filepath.Join(col, "inbox.jsonl"),
		`{"type":"ref","id":"33","binder":["Anno"],"filename":"gamma.pdf","aka":["`+nfc+`"]}`+"\n")
	note := filepath.Join(col, "binder_Anno.md")
	writeFile(t, note, "### gamma.pdf\n```yaml\ntype: ref\nid: "+nfd+"\nbinder: Anno\n```\n")

	if _, e, code := runGoAka(t, annotateEnv(t, dir), "33", "--remove", nfc); code != 0 {
		t.Fatalf("aka --remove: %d %s", code, e)
	}
	if got := mustRead(t, note); !strings.Contains(got, "id: '33'") && !strings.Contains(got, "id: 33") && !strings.Contains(got, `id: "33"`) {
		t.Errorf("the block did not get the record's id:\n%s", got)
	}
}

// A second positional is a mistake the user hears about.
func TestExtraPositionalsAreRefused(t *testing.T) {
	anchor := engineBin(t)
	notes := seedNotes(t)
	env := forgetEnv(t, notes, anchor)
	if _, e, code := runGoResolve(t, env, "a", "b"); code != 1 || !strings.Contains(e, "one key at a time") {
		t.Errorf("resolve a b: exit %d %s", code, e)
	}
	if _, e, code := runGoOf(t, env, "a.pdf", "b.pdf"); code != 1 || !strings.Contains(e, "one file at a time") {
		t.Errorf("of a.pdf b.pdf: exit %d %s", code, e)
	}
	if _, e, code := runGoRename(t, env, "My", "Binder", "New"); code != 1 || !strings.Contains(e, "two names") {
		t.Errorf("rename My Binder New: exit %d %s", code, e)
	}
	if _, e, code := runGoList(t, env, "--inbox", "--curated"); code != 1 || !strings.Contains(e, "not both") {
		t.Errorf("list --inbox --curated: exit %d %s", code, e)
	}
	if _, e, code := runGoList(t, env, "X", "--json", "--paths"); code != 1 || !strings.Contains(e, "not both") {
		t.Errorf("list X --json --paths: exit %d %s", code, e)
	}
}

// Binders that differ only in case come in the same order in every run.
func TestListOrdersCaseTwinsTheSameEveryRun(t *testing.T) {
	anchor := engineBin(t)
	notes := seedNotes(t)
	var lines []string
	for i, b := range []string{"akte", "Akte", "AKTE", "aKte", "akTe", "aktE"} {
		lines = append(lines, `{"type":"ref","id":"`+string(rune('1'+i))+`","binder":["`+b+`"],"url":"x://`+b+`","filename":"f"}`)
	}
	writeFile(t, filepath.Join(notes, "collections", "inbox.jsonl"), strings.Join(lines, "\n")+"\n")
	env := listEnv(t, notes, anchor)
	first, _, code := runGoList(t, env, "--json")
	if code != 0 || strings.Count(first, "\n") != 6 {
		t.Fatalf("list --json: exit %d\n%s", code, first)
	}
	for i := 0; i < 20; i++ {
		if again, _, _ := runGoList(t, env, "--json"); again != first {
			t.Fatalf("run %d differs:\n%s\nfirst:\n%s", i, again, first)
		}
	}
}

// annotate: a value with an underscore is text. Written plain, YAML would
// read 2024_05 as 202405.
func TestAnnotateQuotesUnderscoreNumerals(t *testing.T) {
	for _, v := range []string{"1_000", "2024_05"} {
		if got := annotateValue(v); got == v {
			t.Errorf("annotateValue(%q) is written plain", v)
		}
	}
	for _, v := range []string{"1000", "3.5", "-2"} {
		if got := annotateValue(v); got != v {
			t.Errorf("annotateValue(%q) = %q, want it plain", v, got)
		}
	}
}

// A record without a name is not reattached to a block that stands before
// the first heading.
func TestReattachNeedsAName(t *testing.T) {
	note := filepath.Join(t.TempDir(), "binder_B.md")
	before := "```yaml\ntype: ref\nbinder: B\nurl: https://example.com/other\n```\n"
	writeFile(t, note, before)
	done, err := mdReattachBlock(note, "B", "", "77")
	if err != nil || done {
		t.Errorf("reattach without a name: %v %v", done, err)
	}
	if got := mustRead(t, note); got != before {
		t.Errorf("the note changed:\n%s", got)
	}
}

// rename to the same note file is no clash: the NFD to NFC rename audit
// recommends, and a change of case on a volume that ignores case.
func TestRenameToTheSameNoteFileIsNoClash(t *testing.T) {
	anchor := engineBin(t)
	nfc, nfd := uninorm.NFC.String("Bücher"), uninorm.NFD.String("Bücher")
	notes := seedNotes(t)
	col := filepath.Join(notes, "collections")
	writeFile(t, filepath.Join(col, "inbox.jsonl"),
		`{"type":"ref","id":"1","binder":["`+nfd+`"],"url":"x://a","filename":"a"}`+"\n")
	writeFile(t, defaultPromoteTarget(notes, nfc), "### a\n```yaml\ntype: ref\nid: '1'\nbinder: "+nfd+"\n```\n")
	_, e, code := runGoRename(t, forgetEnv(t, notes, anchor), nfd, nfc)
	if code != 0 || strings.Contains(e, "already exists") {
		t.Errorf("rename NFD to NFC: exit %d\n%s", code, e)
	}

	notes = seedNotes(t)
	col = filepath.Join(notes, "collections")
	writeFile(t, filepath.Join(col, "inbox.jsonl"),
		`{"type":"ref","id":"1","binder":["foo"],"url":"x://a","filename":"a"}`+"\n")
	writeFile(t, defaultPromoteTarget(notes, "foo"), "### a\n```yaml\ntype: ref\nid: '1'\nbinder: foo\n```\n")
	_, e, code = runGoRename(t, forgetEnv(t, notes, anchor), "foo", "Foo")
	if code != 0 || strings.Contains(e, "already exists") {
		t.Errorf("rename foo Foo: exit %d\n%s", code, e)
	}
	names, _ := filepath.Glob(filepath.Join(col, "binder_*.md"))
	if len(names) != 1 || filepath.Base(names[0]) != filepath.Base(defaultPromoteTarget(notes, "Foo")) {
		t.Errorf("note file after rename foo Foo: %v", names)
	}
}

// The search for moved files goes into a root that is a symlink, and names
// what it finds below that root.
func TestContentFinderWalksASymlinkedRoot(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	os.MkdirAll(filepath.Join(real, "sub"), 0755)
	writeFile(t, filepath.Join(real, "sub", "f.txt"), "12345")
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	f := newContentFinder([]string{link}, map[int64]bool{5: true}, nil)
	want := filepath.Join(link, "sub", "f.txt")
	if got := f.bySize[5]; len(got) != 1 || got[0] != want {
		t.Errorf("found %v, want %s", got, want)
	}
}

// repair --interactive: a typed path may start with ~, and a path with no
// file is named instead of dropped without a word.
func TestRepairPromptExpandsHome(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "doc.txt"), "x")
	t.Setenv("HOME", home)
	rec := map[string]any{"id": "1", "binder": []any{"B"}}
	if got := promptForPath(rec, bufio.NewReader(strings.NewReader("~/doc.txt\n"))); got != filepath.Join(home, "doc.txt") {
		t.Errorf("~/doc.txt = %q", got)
	}
	if got := promptForPath(rec, bufio.NewReader(strings.NewReader("~/none.txt\n"))); got != "" {
		t.Errorf("a path without a file = %q", got)
	}
}
