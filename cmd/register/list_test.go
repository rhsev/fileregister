package main

import (
	"github.com/rhsev/fileregister/internal/index"

	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// listEnv: fixture notes, throwaway HOME (empty bookmarks.json), no grubber set,
// and the engine (list <binder> resolves file refs through it).
func listEnv(t *testing.T, notes, anchor string) []string {
	t.Helper()
	env := filterEnv(os.Environ(), "GRUBBER_SET")
	for _, k := range []string{"GRUBBER_NOTES", "HOME", "FILEANCHOR"} {
		env = filterEnv(env, k)
	}
	return append(env,
		"GRUBBER_NOTES="+notes,
		"HOME="+t.TempDir(),
		"FILEANCHOR="+anchor,
	)
}

func runGoList(t *testing.T, env []string, args ...string) (string, string, int) {
	t.Helper()
	restore := setEnv(t, env)
	defer restore()
	index.ResetEngine()

	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	code := cmdList(args)

	index.ResetEngine()
	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr

	var ob, eb bytes.Buffer
	io.Copy(&ob, outR)
	io.Copy(&eb, errR)
	return ob.String(), eb.String(), code
}

func assertList(t *testing.T, label string, env []string, args ...string) {
	t.Helper()
	gOut, gErr, gCode := runGoList(t, env, args...)
	assertGolden(t, "list_"+label, cliResult(gOut, gErr, gCode))
}

// TestListAll covers the all-binders view and the Markdown-layer filters.
func TestListAll(t *testing.T) {
	anchor := engineBin(t)
	fx := makeFixture(t) // 4 binders; notes/ has an annotation for id 200000001
	env := listEnv(t, fx, anchor)

	assertList(t, "all", env)
	assertList(t, "inbox", env, "--inbox")
	assertList(t, "curated", env, "--curated")

	// empty index → "No active binders found." with matching label per filter.
	empty := t.TempDir()
	os.MkdirAll(filepath.Join(empty, "collections"), 0755)
	eenv := listEnv(t, empty, anchor)
	assertList(t, "empty-all", eenv)
	assertList(t, "empty-inbox", eenv, "--inbox")
	assertList(t, "empty-curated", eenv, "--curated")
}

// makeMixFixture builds one binder "Mix" with two url refs and one file ref whose
// bookmark is absent (→ broken). Deterministic without real bookmarks.
func makeMixFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	col := filepath.Join(dir, "collections")
	if err := os.MkdirAll(col, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(col, "mix.jsonl"),
		`{"type":"ref","id":"500000001","binder":["Mix"],"url":"x-devonthink-item://AAA","filename":"note-a.md","kind":"doc","aka":"a"}`+"\n"+
			`{"type":"ref","id":"500000002","binder":["Mix"],"url":"x-devonthink-item://BBB","filename":"note-b.md"}`+"\n"+
			`{"type":"ref","id":"600000001","binder":["Mix"],"filename":"gone.pdf"}`+"\n")
	return dir
}

// TestListBinder covers the single-binder table, --paths, and --json.
func TestListBinder(t *testing.T) {
	anchor := engineBin(t)
	fx := makeMixFixture(t)
	env := listEnv(t, fx, anchor)

	assertList(t, "binder-table", env, "Mix")
	assertList(t, "binder-paths", env, "Mix", "--paths")
	assertList(t, "binder-json", env, "Mix", "--json")
	assertList(t, "binder-missing", env, "Nonexistent")
}
