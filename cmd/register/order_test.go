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

func runGoOrder(t *testing.T, env []string, args ...string) (string, string, int) {
	t.Helper()
	restore := setEnv(t, env)
	defer restore()
	index.ResetEngine()

	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	code := cmdOrder(args)

	index.ResetEngine()
	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr

	var ob, eb bytes.Buffer
	io.Copy(&ob, outR)
	io.Copy(&eb, errR)
	return ob.String(), eb.String(), code
}

func seedOrderFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	col := filepath.Join(dir, "collections")
	if err := os.MkdirAll(col, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(col, "inbox.jsonl"),
		`{"type":"ref","id":"1","binder":["Ord"],"filename":"c.pdf"}`+"\n"+
			`{"type":"ref","id":"2","binder":["Ord"],"filename":"a.pdf"}`+"\n"+
			`{"type":"ref","id":"3","binder":["Ord"],"filename":"b.pdf"}`+"\n")
	return dir
}

func orderEnv(t *testing.T, notes string) []string {
	t.Helper()
	env := filterEnv(os.Environ(), "GRUBBER_SET")
	for _, k := range []string{"GRUBBER_NOTES", "HOME", "REGISTER_BINDER"} {
		env = filterEnv(env, k)
	}
	return append(env, "GRUBBER_NOTES="+notes, "HOME="+t.TempDir())
}

// assertOrder runs the op(s) on a fresh fixture and goldens stdout (+ ORDERING.md).
func assertOrder(t *testing.T, label string, checkMd bool, argSets ...[]string) {
	t.Helper()
	n := seedOrderFixture(t)
	var gOut, gErr string
	var gCode int
	for _, args := range argSets {
		gOut, gErr, gCode = runGoOrder(t, orderEnv(t, n), args...)
	}
	assertGolden(t, "order_"+label, cliResult(gOut, gErr, gCode))
	if checkMd {
		assertGolden(t, "order_"+label+".md", mustRead(t, filepath.Join(n, "collections", "binder_Ord.md")))
	}
}

func TestOrder(t *testing.T) {
	// show: no note yet, so by file name.
	assertOrder(t, "show-pure", false, []string{"show", "Ord"})
	assertOrder(t, "show-json", false, []string{"show", "Ord", "--json"})

	// set is gone: there is no rule to set, the order is the document's.
	assertOrder(t, "set-gone", false, []string{"set", "Ord"})

	// move: --after and --to. On a keyless binder the first move materializes
	// sort keys for every member; a second move places a single key between them.
	assertOrder(t, "move-after", true, []string{"move", "Ord", "1", "--after", "2"})
	assertOrder(t, "move-after-show", false,
		[]string{"move", "Ord", "1", "--after", "2"}, []string{"show", "Ord"})
	assertOrder(t, "move-to", true, []string{"move", "Ord", "3", "--to", "1"})
	assertOrder(t, "move-second", true,
		[]string{"move", "Ord", "1", "--after", "2"}, []string{"move", "Ord", "3", "--to", "2"})
	assertOrder(t, "move-second-show", false,
		[]string{"move", "Ord", "1", "--after", "2"}, []string{"move", "Ord", "3", "--to", "2"},
		[]string{"show", "Ord"})

	// errors. convert and --kind are gone (absolute-only).
	assertOrder(t, "unknown-sub", false, []string{"bogus", "Ord"})
	assertOrder(t, "convert-gone", false, []string{"convert", "Ord", "--kind", "absolute"})
	assertOrder(t, "move-both", false, []string{"move", "Ord", "1", "--after", "2", "--to", "1"})
	assertOrder(t, "not-member", false, []string{"move", "Ord", "999", "--to", "1"})
}

// The order is the document's: members follow their blocks in the note, a
// sort: key places a member before all of them, and members without a block
// come last by file name — here c.pdf before a.pdf because its block stands
// first, b.pdf last although it sorts between them.
func TestOrderFollowsTheDocument(t *testing.T) {
	n := seedOrderFixture(t)
	note := filepath.Join(n, "collections", "binder_Ord.md")
	writeFile(t, note,
		"### c.pdf\n```yaml\ntype: ref\nid: '1'\nbinder: Ord\n```\n\n"+
			"### a.pdf\n```yaml\ntype: ref\nid: '2'\nbinder: Ord\n```\n")
	show := func() string {
		out, se, code := runGoOrder(t, orderEnv(t, n), "show", "Ord", "--json")
		if code != 0 {
			t.Fatalf("show: exit %d: %s", code, se)
		}
		var ids []string
		for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
			ids = append(ids, l[strings.Index(l, `"id":"`)+6:strings.Index(l, `","filename"`)])
		}
		return strings.Join(ids, " ")
	}
	if got := show(); got != "1 2 3" {
		t.Errorf("document order: %s, want 1 2 3 (c.pdf, a.pdf, then b.pdf without a block)", got)
	}

	// Swap the blocks by hand: the order follows.
	writeFile(t, note,
		"### a.pdf\n```yaml\ntype: ref\nid: '2'\nbinder: Ord\n```\n\n"+
			"### c.pdf\n```yaml\ntype: ref\nid: '1'\nbinder: Ord\n```\n")
	if got := show(); got != "2 1 3" {
		t.Errorf("after swapping the blocks: %s, want 2 1 3", got)
	}

	// A sort: key is set order and wins over the document.
	writeFile(t, note,
		"### a.pdf\n```yaml\ntype: ref\nid: '2'\nbinder: Ord\n```\n\n"+
			"### c.pdf\n```yaml\ntype: ref\nid: '1'\nbinder: Ord\nsort: m\n```\n")
	if got := show(); got != "1 2 3" {
		t.Errorf("with a key on c.pdf: %s, want 1 2 3", got)
	}

	// A rule: line from before is inert.
	writeFile(t, note,
		"### · Ord (ordering)\n```yaml\ntype: ordering\nbinder: Ord\nrule: name\n```\n\n"+
			"### c.pdf\n```yaml\ntype: ref\nid: '1'\nbinder: Ord\n```\n\n"+
			"### a.pdf\n```yaml\ntype: ref\nid: '2'\nbinder: Ord\n```\n")
	if got := show(); got != "1 2 3" {
		t.Errorf("an old rule: name took effect: %s, want 1 2 3", got)
	}
}
