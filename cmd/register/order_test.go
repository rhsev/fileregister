package main

import (
	"github.com/rhsev/fileregister/internal/index"

	"bytes"
	"io"
	"os"
	"path/filepath"
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
	// show: pure rule order.
	assertOrder(t, "show-pure", false, []string{"show", "Ord"})
	assertOrder(t, "show-json", false, []string{"show", "Ord", "--json"})

	// set: create the config block.
	assertOrder(t, "set", true, []string{"set", "Ord"})

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
	assertOrder(t, "set-kind-gone", false, []string{"set", "Ord", "--kind", "absolute"})
	assertOrder(t, "move-both", false, []string{"move", "Ord", "1", "--after", "2", "--to", "1"})
	assertOrder(t, "not-member", false, []string{"move", "Ord", "999", "--to", "1"})
}
