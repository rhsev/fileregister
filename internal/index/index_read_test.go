package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeIndex(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	col := filepath.Join(dir, "collections")
	os.MkdirAll(col, 0755)
	if err := os.WriteFile(filepath.Join(col, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// A partial index is worse than none: add and reindex mint duplicates of the
// records they cannot see.
func TestReadAllRefsRefusesAPartialIndex(t *testing.T) {
	glued := writeIndex(t, "merged.jsonl",
		`{"type":"ref","id":"200","binder":["a"]}{"type":"ref","id":"300","binder":["a"]}`+"\n")
	if _, err := ReadAllRefs(glued); err == nil || !strings.Contains(err.Error(), "merged.jsonl:1") {
		t.Errorf("two records on one line: err = %v", err)
	}

	broken := writeIndex(t, "inbox.jsonl", `{"type":"ref","id":"1"}`+"\n"+`{"type":"ref","id":`+"\n")
	if _, err := ReadAllRefs(broken); err == nil || !strings.Contains(err.Error(), "inbox.jsonl:2") {
		t.Errorf("a broken line: err = %v", err)
	}

	locked := writeIndex(t, "archive.jsonl", `{"type":"ref","id":"1"}`+"\n")
	p := filepath.Join(locked, "collections", "archive.jsonl")
	os.Chmod(p, 0)
	defer os.Chmod(p, 0644)
	if _, err := ReadAllRefs(locked); err == nil {
		t.Error("an unreadable index file was skipped")
	}
}

// Only ref records are edited; everything else in the file stays byte for byte.
func TestRewriteKeepsWhatItDoesNotEdit(t *testing.T) {
	content := `{"type":"ref","id":"1","binder":["old"]}` + "\n\n" +
		`{"type":"view","binder":["old"]}` + "\n" +
		`{"type":"ref","id":"200"}{"type":"ref","id":"300"}` + "\n"
	dir := writeIndex(t, "inbox.jsonl", content)
	p := filepath.Join(dir, "collections", "inbox.jsonl")
	if n, err := JSONLRenameBinder(p, "old", "new"); err != nil || n != 1 {
		t.Fatalf("rename: %d, %v", n, err)
	}
	got, _ := os.ReadFile(p)
	want := `{"binder":["new"],"id":"1","type":"ref"}` + "\n\n" +
		`{"type":"view","binder":["old"]}` + "\n" +
		`{"type":"ref","id":"200"}{"type":"ref","id":"300"}` + "\n"
	if string(got) != want {
		t.Errorf("rewrite:\n%s\nwant:\n%s", got, want)
	}
}
