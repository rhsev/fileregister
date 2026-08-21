package main

import (
	"github.com/rhsev/fileregister/internal/index"

	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func runGoRepair(t *testing.T, env []string, args ...string) (string, string, int) {
	t.Helper()
	restore := setEnv(t, env)
	defer restore()
	index.ResetEngine()

	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	code := cmdRepair(args)

	index.ResetEngine()
	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr

	var ob, eb bytes.Buffer
	io.Copy(&ob, outR)
	io.Copy(&eb, errR)
	return ob.String(), eb.String(), code
}

// nothing-broken: a resolvable bookmark → nothing to repair.
func TestRepair(t *testing.T) {
	anchor := engineBin(t)

	// nothing broken: a resolvable bookmark → "All bookmarks resolve correctly".
	nN, nH, _ := setupRenameCase(t, anchor, "700000800", "Bnd")
	nOut, nErr, nCode := runGoRepair(t, renameEnvHome(t, nN, nH, anchor))
	assertGolden(t, "repair_nothing-broken", cliResult(nOut, nErr, nCode))

	// empty index.
	eN := t.TempDir()
	os.MkdirAll(filepath.Join(eN, "collections"), 0755)
	eOut, eErr, eCode := runGoRepair(t, renameEnvHome(t, eN, t.TempDir(), anchor))
	assertGolden(t, "repair_empty", cliResult(eOut, eErr, eCode))

	// not-found: broken bookmark, file unlocatable → unresolved list (normalize notes).
	fN := t.TempDir()
	os.MkdirAll(filepath.Join(fN, "collections"), 0755)
	writeFile(t, filepath.Join(fN, "collections", "inbox.jsonl"),
		`{"type":"ref","id":"999999999","binder":["RepairBnd"],"filename":"zzz_repair_unlikely_9f3a.txt"}`+"\n")
	fOut, fErr, fCode := runGoRepair(t, renameEnvHome(t, fN, t.TempDir(), anchor))
	repl := map[string]string{fN: "<NOTES>"}
	assertGolden(t, "repair_not-found", cliResult(norm(fOut, repl), norm(fErr, repl), fCode))
}
