package main

import (
	"github.com/rhsev/fileregister/internal/index"

	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runGoRefresh(t *testing.T, env []string, args ...string) (string, string, int) {
	t.Helper()
	restore := setEnv(t, env)
	defer restore()
	index.ResetEngine()

	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	code := cmdRefresh(args)

	index.ResetEngine()
	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr

	var ob, eb bytes.Buffer
	io.Copy(&ob, outR)
	io.Copy(&eb, errR)
	return ob.String(), eb.String(), code
}

// setupRefreshCase seeds an index with a resolvable file ref, a broken ref, and a
// url ref, plus a bookmarks.json (under home) that resolves only the first.
func setupRefreshCase(t *testing.T, anchor string) (notes, home string) {
	home = t.TempDir()
	notes = t.TempDir()
	if err := os.MkdirAll(filepath.Join(notes, "collections"), 0755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "doc.txt")
	if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(f)

	blob := engineSave(t, anchor, real)
	share := filepath.Join(home, ".local", "share")
	os.MkdirAll(share, 0755)
	dbJSON, _ := json.Marshal(map[string]string{"800000001": blob})
	writeFile(t, filepath.Join(share, "bookmarks.json"), string(dbJSON))

	writeFile(t, filepath.Join(notes, "collections", "inbox.jsonl"),
		`{"type":"ref","id":"800000001","binder":["Docs"],"filename":"doc.txt"}`+"\n"+
			`{"type":"ref","id":"999999999","binder":["Ghost"],"filename":"z.txt"}`+"\n"+
			`{"type":"ref","id":"800000003","binder":["Links"],"url":"x-devonthink-item://U","filename":"u.md"}`+"\n")
	return notes, home
}

func refreshEnv(t *testing.T, notes, home, anchor string) []string {
	t.Helper()
	env := filterEnv(os.Environ(), "GRUBBER_SET")
	for _, k := range []string{"GRUBBER_NOTES", "HOME", "FILEANCHOR", "REGISTER_BINDER"} {
		env = filterEnv(env, k)
	}
	return append(env, "GRUBBER_NOTES="+notes, "HOME="+home, "FILEANCHOR="+anchor)
}

func assertRefresh(t *testing.T, label string, anchor string, args ...string) {
	t.Helper()
	gN, gH := setupRefreshCase(t, anchor)
	gOut, gErr, gCode := runGoRefresh(t, refreshEnv(t, gN, gH, anchor), args...)
	assertGolden(t, "refresh_"+label, cliResult(gOut, gErr, gCode))
}

func TestRefresh(t *testing.T) {
	anchor := engineBin(t)
	assertRefresh(t, "normal", anchor)
	assertRefresh(t, "dry-run", anchor, "--dry-run")

	empty := t.TempDir()
	os.MkdirAll(filepath.Join(empty, "collections"), 0755)
	gOut, gErr, gCode := runGoRefresh(t, refreshEnv(t, empty, t.TempDir(), anchor))
	assertGolden(t, "refresh_empty", cliResult(gOut, gErr, gCode))
}

// iCloud Drive strips kMDItemInformation and keeps the #S copy; refresh puts
// the id back where Spotlight searches it, so repair's id lookup finds the
// file again. Present, it is left alone; other ids on the file stay.
func TestRefreshRestoresSearchableID(t *testing.T) {
	anchor := engineBin(t)
	home, notes := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(notes, "collections"), 0755)
	f := filepath.Join(t.TempDir(), "doc.txt")
	writeFile(t, f, "x")
	real, _ := filepath.EvalSymlinks(f)
	share := filepath.Join(home, ".local", "share")
	os.MkdirAll(share, 0755)
	dbJSON, _ := json.Marshal(map[string]string{"800000011": engineSave(t, anchor, real)})
	writeFile(t, filepath.Join(share, "bookmarks.json"), string(dbJSON))
	writeFile(t, filepath.Join(notes, "collections", "inbox.jsonl"),
		`{"type":"ref","id":"800000011","binder":["Docs"],"filename":"doc.txt"}`+"\n")
	// A copy's id on the file, as cp leaves it: refresh must not take it away.
	engineExec(t, anchor, `{"op":"set_meta","path":"`+real+`","key":"id","value":"700000099","mode":"add"}`)
	env := refreshEnv(t, notes, home, anchor)

	out, _, code := runGoRefresh(t, env)
	if code != 0 || !strings.Contains(out, "IDs restored : 1") {
		t.Fatalf("first refresh: exit %d\n%s", code, out)
	}
	restore := setEnv(t, env)
	has, other := index.HasDescriptionID(real, "800000011"), index.HasDescriptionID(real, "700000099")
	restore()
	if !has || !other {
		t.Errorf("kMDItemInformation after refresh: own id %v, the other id %v", has, other)
	}

	out, _, _ = runGoRefresh(t, env)
	if strings.Contains(out, "IDs restored") {
		t.Errorf("a present id was written again:\n%s", out)
	}
}
