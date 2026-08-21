package main

import (
	"github.com/rhsev/fileregister/internal/index"

	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func runGoAlbum(t *testing.T, env []string, args ...string) (string, string, int) {
	t.Helper()
	restore := setEnv(t, env)
	defer restore()
	index.ResetEngine()

	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	code := cmdAlbum(args)

	index.ResetEngine()
	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr

	var ob, eb bytes.Buffer
	io.Copy(&ob, outR)
	io.Copy(&eb, errR)
	return ob.String(), eb.String(), code
}

func seedAlbumFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	col := filepath.Join(dir, "collections")
	if err := os.MkdirAll(col, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(col, "inbox.jsonl"),
		`{"type":"ref","id":"1","binder":["Alb"],"url":"x-devonthink-item://A","filename":"one.pdf","kind":"pdf"}`+"\n"+
			`{"type":"ref","id":"2","binder":["Alb"],"url":"https://example.com/x?a=1&b=2","filename":"two & three.md","kind":"web"}`+"\n")
	writeFile(t, filepath.Join(col, "binder_Alb.md"),
		"### one\n```yaml\ntype: ref\nid: '1'\nbinder: Alb\ntitle: Erstes\ncomment: Ein <Test>\nsort: \"1\"\n```\n")
	return dir
}

func albumEnv(t *testing.T, notes, anchor string) []string {
	t.Helper()
	env := filterEnv(os.Environ(), "GRUBBER_SET")
	for _, k := range []string{"GRUBBER_NOTES", "HOME", "FILEANCHOR", "REGISTER_BINDER"} {
		env = filterEnv(env, k)
	}
	return append(env, "GRUBBER_NOTES="+notes, "HOME="+t.TempDir(), "FILEANCHOR="+anchor)
}

func TestAlbum(t *testing.T) {
	anchor := engineBin(t)
	n := seedAlbumFixture(t)
	out := filepath.Join(t.TempDir(), "alb")

	so, se, code := runGoAlbum(t, albumEnv(t, n, anchor), "Alb", "--out", out)
	repl := map[string]string{out: "<OUT>"}
	assertGolden(t, "album_stdout", cliResult(norm(so, repl), norm(se, repl), code))
	assertGolden(t, "album_index.html", mustRead(t, filepath.Join(out, "index.html")))

	// Error: unknown binder.
	go2, ge2, gc2 := runGoAlbum(t, albumEnv(t, n, anchor), "Ghost")
	assertGolden(t, "album_ghost", cliResult(go2, ge2, gc2))
}
