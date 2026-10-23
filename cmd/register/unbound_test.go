package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// deadBlob is a bookmark whose file is gone: it exists in the store and does
// not resolve, which is what a broken bookmark is. The file lived in dir.
func deadBlob(t *testing.T, anchor, dir string) string {
	t.Helper()
	f := filepath.Join(dir, "gone.txt")
	writeFile(t, f, "x")
	real, _ := filepath.EvalSymlinks(f)
	blob := engineSave(t, anchor, real)
	os.Remove(real)
	return blob
}

// A record with no bookmark on this system at all (another machine bound the
// file) is listed on its own, not as a broken bookmark, and changes neither
// refresh's nor audit's exit status.
func TestUnboundRecordIsNotBroken(t *testing.T) {
	anchor := engineBin(t)
	notes, home := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(notes, "collections"), 0755)
	writeFile(t, filepath.Join(notes, "collections", "inbox.jsonl"),
		`{"type":"ref","id":"999999998","binder":[],"filename":"elsewhere.txt"}`+"\n")
	env := refreshEnv(t, notes, home, anchor)

	so, se, code := runGoRefresh(t, env)
	if code != 0 || !strings.Contains(so, "Not bound on this system (1)") ||
		!strings.Contains(so, "[no binder] elsewhere.txt") || strings.Contains(so, "Broken bookmarks") {
		t.Errorf("refresh: exit %d\n%s\n%s", code, so, se)
	}
	so, se, code = runGoAudit(t, env)
	if code != 0 || !strings.Contains(so, "NOT BOUND ON THIS SYSTEM (1)") || strings.Contains(so, "BROKEN BOOKMARK") {
		t.Errorf("audit: exit %d\n%s\n%s", code, so, se)
	}
}
