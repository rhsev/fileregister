package index

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func inodeOf(t *testing.T, path string) uint64 {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return st.Sys().(*syscall.Stat_t).Ino
}

// The note stays the same file, so a bookmark to it survives a later rename,
// and a hard link sees the new content.
func TestWriteInPlaceKeepsInodeAndLinks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("### a.pdf\n\nA long old paragraph.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.md")
	if err := os.Link(path, link); err != nil {
		t.Fatal(err)
	}
	before := inodeOf(t, path)
	if err := WriteInPlace(path, []byte("### a.pdf\n")); err != nil {
		t.Fatal(err)
	}
	if got := inodeOf(t, path); got != before {
		t.Errorf("inode changed: %d → %d", before, got)
	}
	if data, _ := os.ReadFile(link); string(data) != "### a.pdf\n" {
		t.Errorf("hard link reads %q; a shorter text must leave no tail", data)
	}
	if _, err := os.Lstat(JournalPath(path)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("journal left behind: %v", err)
	}
}

// A journal from an unfinished write stops the next one; note and journal stay.
func TestWriteInPlaceRefusesLeftoverJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	os.WriteFile(path, []byte("old\n"), 0o644)
	os.WriteFile(JournalPath(path), []byte("half"), 0o600)
	if err := WriteInPlace(path, []byte("new\n")); err == nil {
		t.Fatal("a leftover journal must stop the write")
	}
	if data, _ := os.ReadFile(path); string(data) != "old\n" {
		t.Errorf("note changed: %q", data)
	}
	if data, _ := os.ReadFile(JournalPath(path)); string(data) != "half" {
		t.Error("the leftover journal must stay for the user to compare")
	}
}

// A note that does not exist yet is created, with no journal left.
func TestWriteInPlaceCreates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.md")
	if err := WriteInPlace(path, []byte("### x\n")); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "### x\n" {
		t.Errorf("got %q", data)
	}
	if _, err := os.Lstat(JournalPath(path)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("journal left behind: %v", err)
	}
}

// Through a symlink the target is written, and the link stays a link.
func TestWriteInPlaceThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.md")
	os.WriteFile(target, []byte("old\n"), 0o644)
	link := filepath.Join(dir, "link.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteInPlace(link, []byte("new\n")); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink was replaced by a file")
	}
	if data, _ := os.ReadFile(target); string(data) != "new\n" {
		t.Errorf("target reads %q", data)
	}
}
