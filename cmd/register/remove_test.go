package main

import (
	"github.com/rhsev/fileregister/internal/index"

	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func removeEnv(t *testing.T, notes, anchor string) []string {
	t.Helper()
	env := filterEnv(os.Environ(), "GRUBBER_SET")
	for _, k := range []string{"GRUBBER_NOTES", "HOME", "FILEANCHOR", "REGISTER_BINDER"} {
		env = filterEnv(env, k)
	}
	return append(env, "GRUBBER_NOTES="+notes, "HOME="+t.TempDir(), "FILEANCHOR="+anchor)
}

func runGoRemove(t *testing.T, env []string, args ...string) (string, string, int) {
	t.Helper()
	restore := setEnv(t, env)
	defer restore()
	index.ResetEngine()

	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	code := cmdRemove(args)

	index.ResetEngine()
	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr

	var ob, eb bytes.Buffer
	io.Copy(&ob, outR)
	io.Copy(&eb, errR)
	return ob.String(), eb.String(), code
}

// engineExec sends several JSONL requests to a one-shot engine invocation.
func engineExec(t *testing.T, anchor string, reqs ...string) {
	t.Helper()
	cmd := exec.Command(anchor, "--sync-name", "com.fileregister.id#S")
	cmd.Stdin = strings.NewReader(strings.Join(reqs, "\n") + "\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("engineExec: %v\n%s", err, out)
	}
}

// setupRemoveCase seeds a notes dir with inboxLines and creates a "doc.txt" file
// stamped with id fileID, the given binder groups, and optionally ★.
func setupRemoveCase(t *testing.T, anchor string, inboxLines []string, fileID string, groups []string, star bool) (string, string) {
	t.Helper()
	notes := t.TempDir()
	col := filepath.Join(notes, "collections")
	if err := os.MkdirAll(col, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(col, "inbox.jsonl"), strings.Join(inboxLines, "\n")+"\n")

	file := filepath.Join(t.TempDir(), "doc.txt")
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(file)

	reqs := []string{`{"op":"set_meta","path":"` + real + `","key":"id","value":"` + fileID + `","mode":"add"}`}
	for _, g := range groups {
		reqs = append(reqs, `{"op":"set_meta","path":"`+real+`","key":"groups","value":"`+g+`","mode":"add"}`)
	}
	if star {
		reqs = append(reqs, `{"op":"tag","path":"`+real+`","value":"★"}`)
	}
	engineExec(t, anchor, reqs...)
	return notes, real
}

// assertRemove runs remove on a stamped file and goldens stdout + the resulting
// index. Output is path-free (basenames) and ids are fixed → deterministic.
func assertRemove(t *testing.T, label, notes, file, anchor, binder string) {
	t.Helper()
	gOut, gErr, gCode := runGoRemove(t, removeEnv(t, notes, anchor), file, "--binder", binder)
	assertGolden(t, "remove_"+label, cliResult(gOut, gErr, gCode))
	assertGolden(t, "remove_"+label+"_index.jsonl",
		strings.Join(manifestMultiset(t, filepath.Join(notes, "collections", "inbox.jsonl")), "\n")+"\n")
}

func TestRemove(t *testing.T) {
	anchor := engineBin(t)

	// one-of-two binders: ★ stays (still a member of Photos).
	n1, f1 := setupRemoveCase(t, anchor,
		[]string{`{"type":"ref","id":"700000700","binder":["Docs","Photos"],"filename":"doc.txt"}`},
		"700000700", []string{"Docs", "Photos"}, true)
	assertRemove(t, "one-of-two", n1, f1, anchor, "Docs")

	// last binder: ★ unmarked.
	n2, f2 := setupRemoveCase(t, anchor,
		[]string{`{"type":"ref","id":"700000701","binder":["Docs"],"filename":"doc.txt"}`},
		"700000701", []string{"Docs"}, true)
	assertRemove(t, "last-binder", n2, f2, anchor, "Docs")

	// not a member of the requested binder (binder has other records).
	n3, f3 := setupRemoveCase(t, anchor, []string{
		`{"type":"ref","id":"700000702","binder":["Docs"],"filename":"doc.txt"}`,
		`{"type":"ref","id":"999","binder":["Other"],"filename":"z.txt"}`,
	}, "700000702", []string{"Docs"}, true)
	assertRemove(t, "not-a-member", n3, f3, anchor, "Other")

	// no refs for the binder at all.
	n4, f4 := setupRemoveCase(t, anchor,
		[]string{`{"type":"ref","id":"700000703","binder":["Docs"],"filename":"doc.txt"}`},
		"700000703", []string{"Docs"}, true)
	assertRemove(t, "no-refs", n4, f4, anchor, "Ghost")
}

func TestRemoveArg(t *testing.T) {
	anchor := engineBin(t)
	n := t.TempDir()
	os.MkdirAll(filepath.Join(n, "collections"), 0755)
	cases := map[string][]string{
		"missing-binder": {"somefile.pdf"},
		"missing-file":   {"--binder", "B"},
		"not-found":      {"/nope/ghost.pdf", "--binder", "B"},
		// several targets: each runs the full flow, worst exit code wins.
		"multi-not-found": {"/nope/a.pdf", "/nope/b.pdf", "--binder", "B"},
	}
	for label, args := range cases {
		gOut, gErr, gCode := runGoRemove(t, removeEnv(t, n, anchor), args...)
		assertGolden(t, "remove_arg_"+label, cliResult(gOut, gErr, gCode))
	}
}
