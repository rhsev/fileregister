package main

// These golden tests freeze annotate's verified-correct behaviour, including
// the byte-identity of every line the edit does not own.

import (
	"github.com/rhsev/fileregister/internal/index"

	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func runGoAnnotate(t *testing.T, env []string, stdin string, args ...string) (string, string, int) {
	t.Helper()
	restore := setEnv(t, env)
	defer restore()
	index.ResetEngine()

	oldIn := os.Stdin
	if stdin != "" {
		inR, inW, _ := os.Pipe()
		os.Stdin = inR
		go func() {
			inW.WriteString(stdin)
			inW.Close()
		}()
	}

	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	code := cmdAnnotate(args)

	index.ResetEngine()
	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	os.Stdin = oldIn

	var ob, eb bytes.Buffer
	io.Copy(&ob, outR)
	io.Copy(&eb, errR)
	return ob.String(), eb.String(), code
}

// The fixture holds everything an edit must leave alone: prose before and
// after the target block, a sibling ref block, a hand-written block carrying
// an aka in its id: slot, an ordering block, and a foreign yaml block.
func seedAnnotateFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	col := filepath.Join(dir, "collections")
	if err := os.MkdirAll(col, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(col, "inbox.jsonl"),
		`{"type":"ref","id":"11","binder":["Anno"],"filename":"alpha.pdf","aka":["al"]}`+"\n"+
			`{"type":"ref","id":"22","binder":["Anno"],"filename":"beta.pdf"}`+"\n"+
			`{"type":"ref","id":"33","binder":["Anno"],"filename":"gamma.pdf","aka":["ha"]}`+"\n")
	writeFile(t, filepath.Join(col, "binder_Anno.md"),
		"### alpha.pdf\n"+
			"\n"+
			"Old prose line one.\n"+
			"\n"+
			"```yaml\n"+
			"type: ref\n"+
			"id: '11'\n"+
			"binder: Anno\n"+
			"amount: 5\n"+
			"tags:\n"+
			"- x\n"+
			"- y\n"+
			"```\n"+
			"\n"+
			"Trailing prose after the block.\n"+
			"\n"+
			"### beta.pdf\n"+
			"```yaml\n"+
			"type: ref\n"+
			"id: '22'\n"+
			"binder: Anno\n"+
			"```\n"+
			"\n"+
			"### gamma.pdf (hand-written, aka in the id slot)\n"+
			"```yaml\n"+
			"type: ref\n"+
			"id: ha\n"+
			"binder: Anno\n"+
			"```\n"+
			"\n"+
			"### · Anno (ordering)\n"+
			"```yaml\n"+
			"type: ordering\n"+
			"binder: Anno\n"+
			"rule: name\n"+
			"```\n")
	return dir
}

func annotateEnv(t *testing.T, notes string) []string {
	t.Helper()
	env := filterEnv(os.Environ(), "GRUBBER_SET")
	for _, k := range []string{"GRUBBER_NOTES", "HOME", "REGISTER_BINDER"} {
		env = filterEnv(env, k)
	}
	return append(env, "GRUBBER_NOTES="+notes, "HOME="+t.TempDir())
}

// assertAnnotate runs one annotate on a fresh fixture and goldens stdout/stderr
// plus (optionally) the resulting note.
func assertAnnotate(t *testing.T, label, stdin string, checkMd bool, args ...string) {
	t.Helper()
	n := seedAnnotateFixture(t)
	out, errS, code := runGoAnnotate(t, annotateEnv(t, n), stdin, args...)
	assertGolden(t, "annotate_"+label, cliResult(out, errS, code))
	if checkMd {
		assertGolden(t, "annotate_"+label+".md", mustRead(t, filepath.Join(n, "collections", "binder_Anno.md")))
	}
}

func TestAnnotateFields(t *testing.T) {
	// New key appended; number stays plain, text gets promote's quoting rules.
	assertAnnotate(t, "set-new", "", true, "Anno", "11", "--set", "due=2026-08-01", "--set", "note=needs review")
	// Existing scalar replaced in place; leading-zero numeral stays a string.
	assertAnnotate(t, "set-replace", "", true, "Anno", "11", "--set", "amount=129.50", "--set", "code=007")
	// Array-valued key: the block-sequence items go with it.
	assertAnnotate(t, "set-array", "", true, "Anno", "11", "--set", "tags=replaced")
	assertAnnotate(t, "unset", "", true, "Anno", "11", "--unset", "tags", "--unset", "amount")
	// Unset of an absent key is a no-op, not an error.
	assertAnnotate(t, "unset-absent", "", true, "Anno", "11", "--unset", "nothere")
	// Resolution over aka; block matching over the aka in the id: slot.
	assertAnnotate(t, "via-aka", "", true, "Anno", "al", "--set", "seen=true")
	assertAnnotate(t, "aka-id-slot", "", true, "Anno", "ha", "--set", "seen=true")
}

func TestAnnotateProse(t *testing.T) {
	// Replace: old prose (before AND after the block) goes, blocks stay verbatim.
	assertAnnotate(t, "prose-replace", "", true, "Anno", "11", "--prose", "New prose.\nSecond line.")
	assertAnnotate(t, "prose-clear", "", true, "Anno", "11", "--prose", "")
	assertAnnotate(t, "prose-stdin", "From stdin.\n", true, "Anno", "11", "--prose", "-")
	// Fields + prose in one call, one write.
	assertAnnotate(t, "prose-and-fields", "", true, "Anno", "22", "--set", "k=v", "--prose", "Beta prose.")
}

func TestAnnotateRefusals(t *testing.T) {
	for _, k := range []string{"id", "type", "binder", "aka", "sort"} {
		assertAnnotate(t, "reserved-"+k, "", false, "Anno", "11", "--set", k+"=x")
	}
	assertAnnotate(t, "reserved-unset", "", false, "Anno", "11", "--unset", "sort")
	assertAnnotate(t, "underscore", "", false, "Anno", "11", "--set", "_note_file=x")
	assertAnnotate(t, "bad-key", "", false, "Anno", "11", "--set", "a b=x")
	assertAnnotate(t, "multiline-value", "", false, "Anno", "11", "--set", "k=a\nb")
	assertAnnotate(t, "no-op-args", "", false, "Anno", "11")
	assertAnnotate(t, "unknown-key", "", false, "Anno", "nope", "--set", "k=v")
	// Record exists but has no block in this binder → promote hint.
	assertAnnotate(t, "no-block", "", false, "Other", "11", "--set", "k=v")
}
