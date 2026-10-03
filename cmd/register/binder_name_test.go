package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A binder ends up on files as a Finder tag or a kMDItemProjects entry, so a
// name fileanchor would refuse is refused where it is introduced — before
// anything is written.

func TestAddRefusesBinderWithComma(t *testing.T) {
	anchor := engineBin(t)
	notes := t.TempDir()
	f := filepath.Join(t.TempDir(), "a.txt")
	writeFile(t, f, "a")

	_, errOut, code := runGoAdd(t, addEnv(t, notes, anchor), f, "--binder", "Müller, Hans")
	if code != 1 || !strings.Contains(errOut, "binder name 'Müller, Hans' contains a comma") {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(notes, "collections", "inbox.jsonl")); err == nil {
		t.Fatal("inbox.jsonl was written for a refused binder")
	}
}

func TestRenameChecksOnlyTheNewName(t *testing.T) {
	anchor := engineBin(t)
	notes := seedInbox(t, `{"type":"ref","id":"1","binder":["Alt, Neu"],"url":"https://example.com/"}`)
	env := renameEnvHome(t, notes, t.TempDir(), anchor)

	_, errOut, code := runGoRename(t, env, "Alt, Neu", "Neu, Alt")
	if code != 1 || !strings.Contains(errOut, "contains a comma") {
		t.Fatalf("new name with a comma: code=%d stderr=%q", code, errOut)
	}
	// The old name breaks the rule too, and must stay renamable.
	if _, errOut, code := runGoRename(t, env, "Alt, Neu", "Alt-Neu"); code != 0 {
		t.Fatalf("renaming a comma binder away: code=%d stderr=%q", code, errOut)
	}
	inbox := mustRead(t, filepath.Join(notes, "collections", "inbox.jsonl"))
	if !strings.Contains(inbox, `"Alt-Neu"`) || strings.Contains(inbox, "Alt, Neu") {
		t.Fatalf("inbox after rename: %s", inbox)
	}
}

func TestWriteRefusesBinderWithComma(t *testing.T) {
	n := t.TempDir()
	os.MkdirAll(filepath.Join(n, "collections"), 0755)
	md := filepath.Join(n, "collections", "binder_X.md")
	input := `{"_note_file":"` + md + `","type":"ref","id":"5","binder":"a,b","filename":"d.pdf"}` + "\n"

	out, _, code := runGoWrite(t, writeEnv(t, n), input)
	if code != 1 || !strings.Contains(out, `"ok":false`) || !strings.Contains(out, "contains a comma") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if _, err := os.Stat(md); err == nil {
		t.Fatal("annotation written for a refused binder")
	}
}

func TestUnmarshalRefusesBinderWithComma(t *testing.T) {
	anchor := engineBin(t)
	staging := t.TempDir()
	inner := filepath.Join(staging, "ship")
	os.MkdirAll(inner, 0755)
	writeFile(t, filepath.Join(inner, "manifest.jsonl"),
		`{"type":"ref","id":"1","binder":["a,b"],"url":"https://example.com/"}`+"\n")
	container := filepath.Join(t.TempDir(), "ship.tar.gz")
	if out, err := exec.Command("tar", "-czf", container, "-C", staging, "ship").CombinedOutput(); err != nil {
		t.Fatalf("tar: %v %s", err, out)
	}

	notes := t.TempDir()
	_, errOut, code := runGoUnmarshal(t, unmarshalEnv(t, notes, anchor), container)
	if code != 1 || !strings.Contains(errOut, "binder name 'a,b' contains a comma") {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(notes, "collections", "inbox.jsonl")); err == nil {
		t.Fatal("records imported from a refused container")
	}
}

// Foo and foo share binder_foo.md on a case-insensitive volume; renaming one
// must not carry the other's blocks away.
func TestRenameLeavesASharedNote(t *testing.T) {
	anchor := engineBin(t)
	notes := seedInbox(t,
		`{"type":"ref","id":"1","binder":["Foo"],"url":"https://example.com/1"}`,
		`{"type":"ref","id":"2","binder":["foo"],"url":"https://example.com/2"}`)
	col := filepath.Join(notes, "collections")
	shared := "### one\n```yaml\ntype: ref\nid: '1'\nbinder: Foo\n```\n\n### two\n```yaml\ntype: ref\nid: '2'\nbinder: foo\n```\n"
	writeFile(t, filepath.Join(col, "binder_foo.md"), shared)

	env := renameEnvHome(t, notes, t.TempDir(), anchor)
	if _, e, code := runGoRename(t, env, "foo", "bar"); code != 0 {
		t.Fatalf("rename: %d %s", code, e)
	}
	note, err := os.ReadFile(filepath.Join(col, "binder_foo.md"))
	if err != nil || !strings.Contains(string(note), "binder: Foo") {
		t.Errorf("Foo's block left with the renamed binder (err %v):\n%s", err, note)
	}
}
