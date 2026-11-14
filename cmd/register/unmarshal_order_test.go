package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// unwritable makes path a link to a read-only file in a folder that takes no
// new files, so appending to path and replacing it both fail while a read
// still works.
func unwritable(t *testing.T, path, content string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root writes to a read-only folder")
	}
	dir := filepath.Join(t.TempDir(), "ro")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(dir, filepath.Base(path))
	writeFile(t, real, content)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, path); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(real, 0444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0755) })
}

func envHome(t *testing.T, env []string) string {
	t.Helper()
	home := ""
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "HOME="); ok {
			home = v
		}
	}
	if home == "" {
		t.Fatal("no HOME in env")
	}
	return home
}

// format2Container packs one file record that goes to papers/doc.txt in the
// notes folder.
func format2Container(t *testing.T) string {
	t.Helper()
	const body = "hello world"
	line := fmt.Sprintf(`{"type":"ref","id":"900000002","binder":["Docs"],"filename":"doc.txt","kind":"txt","file":"files/doc.txt","base":"notes","path":"papers/doc.txt","size":%d,"sha256":"%x"}`,
		len(body), sha256.Sum256([]byte(body)))
	return buildContainer(t, "pack.tar.gz", map[string]string{
		"manifest.jsonl": line + "\n",
		"files/doc.txt":  body,
	})
}

// A run that cannot write the index saves no bookmark either. A bookmark
// without a record would park the file as "already present" on the next run.
func TestUnmarshalSavesNoBookmarkWithoutItsRecord(t *testing.T) {
	anchor := engineBin(t)
	container := format2Container(t)
	dest := t.TempDir()
	inbox := filepath.Join(dest, "collections", "inbox.jsonl")
	unwritable(t, inbox, "")
	env := unmarshalEnv(t, dest, anchor)

	_, e, code := runGoUnmarshal(t, env, container)
	if code != 1 {
		t.Fatalf("exit %d, want 1\n%s", code, e)
	}
	db, err := os.ReadFile(filepath.Join(envHome(t, env), ".local", "share", "fileregister", "bookmarks.json"))
	if err == nil && strings.Contains(string(db), "900000002") {
		t.Errorf("bookmark saved although the index was not written:\n%s", db)
	}

	// With the index writable again, the next run takes the placed file over.
	if err := os.Remove(inbox); err != nil {
		t.Fatal(err)
	}
	out, e, code := runGoUnmarshal(t, env, container)
	if code != 0 {
		t.Fatalf("second run: exit %d\n%s%s", code, out, e)
	}
	if idx := mustRead(t, inbox); !strings.Contains(idx, `"id":"900000002"`) {
		t.Errorf("record missing after the second run:\n%s", idx)
	}
}

// A run that cannot save the bookmarks has written the index before. It ends
// with exit 1 and names repair, and the next run makes the bookmark.
func TestUnmarshalWritesTheIndexBeforeTheBookmarks(t *testing.T) {
	anchor := engineBin(t)
	container := format2Container(t)
	dest := t.TempDir()
	os.MkdirAll(filepath.Join(dest, "collections"), 0755)
	inbox := filepath.Join(dest, "collections", "inbox.jsonl")
	env := unmarshalEnv(t, dest, anchor)
	bookmarks := filepath.Join(envHome(t, env), ".local", "share", "fileregister", "bookmarks.json")
	unwritable(t, bookmarks, "{}")

	_, e, code := runGoUnmarshal(t, env, container)
	if code != 1 {
		t.Fatalf("exit %d, want 1\n%s", code, e)
	}
	if !strings.Contains(e, "register repair") {
		t.Errorf("stderr does not name repair:\n%s", e)
	}
	if idx := mustRead(t, inbox); !strings.Contains(idx, `"id":"900000002"`) {
		t.Errorf("record not in the index:\n%s", idx)
	}

	if err := os.Remove(bookmarks); err != nil {
		t.Fatal(err)
	}
	out, e, code := runGoUnmarshal(t, env, container)
	if code != 0 {
		t.Fatalf("second run: exit %d\n%s%s", code, out, e)
	}
	if !strings.Contains(out, "Took over 1 file") {
		t.Errorf("second run did not take the file over:\n%s", out)
	}
	if db := mustRead(t, bookmarks); !strings.Contains(db, "900000002") {
		t.Errorf("bookmark missing after the second run:\n%s", db)
	}
	if n := strings.Count(mustRead(t, inbox), `"id":"900000002"`); n != 1 {
		t.Errorf("%d index lines for the record, want 1", n)
	}
}
