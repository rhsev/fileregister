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

func runGoForget(t *testing.T, env []string, args ...string) (string, string, int) {
	t.Helper()
	restore := setEnv(t, env)
	defer restore()
	index.ResetEngine()

	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	code := cmdForget(args)

	index.ResetEngine()
	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr

	var ob, eb bytes.Buffer
	io.Copy(&ob, outR)
	io.Copy(&eb, errR)
	return ob.String(), eb.String(), code
}

// forgetEnv keeps one HOME across the steps of a test: the bookmark store
// lives there, and every step has to see the same one.
func forgetEnv(t *testing.T, notes, anchor string) []string {
	t.Helper()
	env := filterEnv(os.Environ(), "GRUBBER_SET")
	for _, k := range []string{"GRUBBER_NOTES", "HOME", "FILEANCHOR", "REGISTER_BINDER"} {
		env = filterEnv(env, k)
	}
	return append(env, "GRUBBER_NOTES="+notes, "HOME="+t.TempDir(), "FILEANCHOR="+anchor)
}

// The life of a record that leaves: in a binder, annotated, removed from the
// binder (a bookmark now, block kept), forgotten (index line, bookmark entry,
// id in the block and on the file gone; the block and its fields stay), and
// back again: added anew, the old block is reattached by its heading.
func TestForgetAndReattach(t *testing.T) {
	anchor := engineBin(t)
	notes := t.TempDir()
	os.MkdirAll(filepath.Join(notes, "collections"), 0755)
	env := forgetEnv(t, notes, anchor)
	f := filepath.Join(t.TempDir(), "f.txt")
	writeFile(t, f, "f")
	note := filepath.Join(notes, "collections", "binder_B.md")

	if _, e, code := runGoAdd(t, env, f, "--binder", "B", "--aka", "alpha"); code != 0 {
		t.Fatalf("add: %d %s", code, e)
	}
	id := inboxByFilename(t, notes)["f.txt"]["id"].(string)
	if _, e, code := runGoPromote(t, env, "--binder", "B"); code != 0 {
		t.Fatalf("promote: %d %s", code, e)
	}
	if _, e, code := runGoAnnotate(t, env, "", "B", "alpha", "--set", "note=keep me"); code != 0 {
		t.Fatalf("annotate: %d %s", code, e)
	}

	// A member is refused: remove first.
	if _, e, code := runGoForget(t, env, "alpha"); code != 1 || !strings.Contains(e, "remove it from them first") {
		t.Fatalf("forgetting a member: exit %d, %q", code, e)
	}
	if _, e, code := runGoRemove(t, env, f, "--binder", "B"); code != 0 {
		t.Fatalf("remove: %d %s", code, e)
	}

	// Dry run changes nothing.
	before := mustRead(t, note)
	if out, _, code := runGoForget(t, env, "alpha", "--dry-run"); code != 0 || !strings.Contains(out, "[dry-run] forget f.txt") {
		t.Fatalf("dry run: exit %d\n%s", code, out)
	}
	if mustRead(t, note) != before {
		t.Fatalf("the dry run changed the note")
	}

	out, e, code := runGoForget(t, env, "alpha")
	if code != 0 {
		t.Fatalf("forget: exit %d %s", code, e)
	}
	if !strings.Contains(out, "Forgot f.txt") || !strings.Contains(out, "bookmark entry") || !strings.Contains(out, "id on the file") {
		t.Errorf("forget did not say what it removed:\n%s", out)
	}
	if _, still := inboxByFilename(t, notes)["f.txt"]; still {
		t.Errorf("the index line is still there")
	}
	got := mustRead(t, note)
	if strings.Contains(got, id) || !strings.Contains(got, "### f.txt") || !strings.Contains(got, "note: keep me") {
		t.Errorf("the block should keep heading and fields and lose only the id:\n%s", got)
	}
	if ids := fileMeta(t, anchor, f, "id"); len(ids) != 0 {
		t.Errorf("kMDItemInformation still carries %v", ids)
	}
	if s := fileMeta(t, anchor, f, "sync"); len(s) != 0 && s[0] != "" {
		t.Errorf("the #S copy still carries %v", s)
	}

	// reindex must not bring the record back from the block.
	if out, _, _ := runGoReindex(t, env, "--dry-run"); strings.Contains(out, id) {
		t.Errorf("reindex would rebuild the forgotten record:\n%s", out)
	}

	// The file comes back: a new id, and promote reattaches the old block
	// instead of writing a second one.
	if _, e, code := runGoAdd(t, env, f, "--binder", "B"); code != 0 {
		t.Fatalf("re-add: %d %s", code, e)
	}
	newID := inboxByFilename(t, notes)["f.txt"]["id"].(string)
	if newID == id {
		t.Errorf("the file kept its forgotten id %s", id)
	}
	out, e, code = runGoPromote(t, env, "--binder", "B")
	if code != 0 || !strings.Contains(out, "Reattached the block for f.txt") {
		t.Fatalf("promote: exit %d %s\n%s", code, e, out)
	}
	got = mustRead(t, note)
	if strings.Count(got, "### f.txt") != 1 || !strings.Contains(got, "id: '"+newID+"'") || !strings.Contains(got, "note: keep me") {
		t.Errorf("the old block was not reattached:\n%s", got)
	}
}

// Two blocks without an id under the same heading: which one is the file's
// cannot be told, so promote writes a new block and leaves both alone.
func TestPromoteDoesNotGuessBetweenBlocks(t *testing.T) {
	anchor := engineBin(t)
	notes := t.TempDir()
	os.MkdirAll(filepath.Join(notes, "collections"), 0755)
	env := forgetEnv(t, notes, anchor)
	f := filepath.Join(t.TempDir(), "f.txt")
	writeFile(t, f, "f")
	note := filepath.Join(notes, "collections", "binder_B.md")
	orphan := "### f.txt\n```yaml\ntype: ref\nbinder: B\nnote: one\n```\n\n"
	writeFile(t, note, orphan+strings.Replace(orphan, "one", "two", 1))

	if _, e, code := runGoAdd(t, env, f, "--binder", "B"); code != 0 {
		t.Fatalf("add: %d %s", code, e)
	}
	if out, e, code := runGoPromote(t, env, "--binder", "B"); code != 0 || strings.Contains(out, "Reattached") {
		t.Fatalf("promote: exit %d %s\n%s", code, e, out)
	}
	if got := mustRead(t, note); strings.Count(got, "### f.txt") != 3 {
		t.Errorf("want the two orphans untouched and one new block:\n%s", got)
	}
}
