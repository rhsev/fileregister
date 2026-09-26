package main

// Golden tests for `register aka`: index edits, the Markdown guard, and the
// byte-identity of every block line the edit does not own.

import (
	"github.com/rhsev/fileregister/internal/index"

	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func runGoAka(t *testing.T, env []string, args ...string) (string, string, int) {
	t.Helper()
	restore := setEnv(t, env)
	defer restore()
	index.ResetEngine()

	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	code := cmdAka(args)

	index.ResetEngine()
	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr

	var ob, eb bytes.Buffer
	io.Copy(&ob, outR)
	io.Copy(&eb, errR)
	return ob.String(), eb.String(), code
}

// The fixture: record 11 carries two handles, written into its blocks in
// both YAML list styles and in two binders; record 33's hand-written block
// reaches it only through the aka in its id: slot, record 44's through an
// aka-only block; "loose" is a hand-written block no index record claims.
// extra is appended to binder_Anno.md.
func seedAkaFixture(t *testing.T, extra string) string {
	t.Helper()
	dir := t.TempDir()
	col := filepath.Join(dir, "collections")
	if err := os.MkdirAll(col, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(col, "inbox.jsonl"),
		`{"type":"ref","id":"11","binder":["Anno","Other"],"filename":"alpha.pdf","aka":["al","alpha"]}`+"\n"+
			`{"type":"ref","id":"22","binder":["Anno"],"filename":"beta.pdf"}`+"\n"+
			`{"type":"ref","id":"33","binder":["Anno"],"filename":"gamma.pdf","aka":["ha"]}`+"\n"+
			`{"type":"ref","id":"44","binder":["Anno"],"filename":"delta.pdf","aka":["de","delta"]}`+"\n")
	writeFile(t, filepath.Join(col, "binder_Anno.md"),
		"### alpha.pdf\n"+
			"```yaml\n"+
			"type: ref\n"+
			"id: '11'\n"+
			"binder: Anno\n"+
			"aka:\n"+
			"- al\n"+
			"- alpha\n"+
			"amount: 5\n"+
			"```\n"+
			"\n"+
			"### gamma.pdf (hand-written, aka in the id slot)\n"+
			"```yaml\n"+
			"type: ref\n"+
			"id: ha\n"+
			"binder: Anno\n"+
			"```\n"+
			"\n"+
			"### delta.pdf (hand-written, aka only)\n"+
			"```yaml\n"+
			"type: ref\n"+
			"binder: Anno\n"+
			"aka: [de]\n"+
			"note: keep me\n"+
			"```\n"+
			"\n"+
			"### loose.pdf (hand-written, unindexed)\n"+
			"```yaml\n"+
			"type: ref\n"+
			"id: loose\n"+
			"binder: Anno\n"+
			"```\n"+extra)
	writeFile(t, filepath.Join(col, "binder_Other.md"),
		"### alpha.pdf\n"+
			"Prose.\n"+
			"\n"+
			"```yaml\n"+
			"type: ref\n"+
			"id: '11'\n"+
			"aka: [alpha, al]\n"+
			"binder: Other\n"+
			"```\n")
	return dir
}

// assertAka runs one aka on a fresh fixture and goldens stdout/stderr plus
// the index and both notes.
func assertAka(t *testing.T, label string, args ...string) {
	t.Helper()
	assertAkaWith(t, label, "", args...)
}

func assertAkaWith(t *testing.T, label, extra string, args ...string) {
	t.Helper()
	n := seedAkaFixture(t, extra)
	out, errS, code := runGoAka(t, annotateEnv(t, n), args...)
	col := filepath.Join(n, "collections")
	assertGolden(t, "aka_"+label, cliResult(out, errS, code)+
		"=== inbox.jsonl ===\n"+mustRead(t, filepath.Join(col, "inbox.jsonl"))+
		"=== binder_Anno.md ===\n"+mustRead(t, filepath.Join(col, "binder_Anno.md"))+
		"=== binder_Other.md ===\n"+mustRead(t, filepath.Join(col, "binder_Other.md")))
}

func TestAkaAdd(t *testing.T) {
	assertAka(t, "add", "22", "--add", "be", "--add", "beta")
	// Resolution over an existing handle.
	assertAka(t, "add-via-aka", "al", "--add", "a1")
	// Already on the record: noop, exit 0, nothing written.
	assertAka(t, "add-present", "11", "--add", "al")
}

func TestAkaRemove(t *testing.T) {
	// Stripped from the index and from both block styles; other lines stay.
	assertAka(t, "remove", "11", "--remove", "alpha")
	// Last handles gone: the key disappears everywhere.
	assertAka(t, "remove-all", "alpha", "--remove", "al", "--remove", "alpha")
	// Not on the record: noop, exit 0.
	assertAka(t, "remove-absent", "22", "--remove", "nope")
	// Add and remove in one call.
	assertAka(t, "swap", "11", "--remove", "al", "--add", "a1")
}

// Blocks that reach the record only through the removed handle are cleaned
// up: they get the real id instead of losing their record.
func TestAkaRemoveCleansBlocks(t *testing.T) {
	// aka in the id: slot → id: replaced.
	assertAka(t, "remove-id-slot", "33", "--remove", "ha")
	// aka-only block → id: inserted after type:, handle leaves the list.
	assertAka(t, "remove-aka-only", "44", "--remove", "de")
	// id: slot holds a handle that stays → only the list shrinks.
	assertAkaWith(t, "remove-kept-id-slot",
		"\n### alpha.pdf (hand-written, other handle in the id slot)\n```yaml\ntype: ref\nid: al\nbinder: Anno\naka: [alpha]\n```\n",
		"11", "--remove", "alpha")
}

func TestAkaRefusals(t *testing.T) {
	assertAka(t, "add-clash", "22", "--add", "al")
	assertAka(t, "add-own-id", "22", "--add", "22")
	// A hand-written block would be silently adopted.
	assertAka(t, "add-adopts-block", "22", "--add", "loose")
	// A block of another record carries the handle: ambiguous, refused.
	assertAkaWith(t, "remove-foreign",
		"\n### beta.pdf (hand-written, stale handle)\n```yaml\ntype: ref\nid: '22'\nbinder: Anno\naka: [al]\n```\n",
		"11", "--remove", "al")
	assertAka(t, "add-and-remove", "11", "--add", "x", "--remove", "x")
	assertAka(t, "unknown-key", "nope", "--add", "x")
	assertAka(t, "no-op-args", "11")
	assertAka(t, "blank-handle", "11", "--add", " ")
}
