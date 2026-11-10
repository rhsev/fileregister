package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The same folder under two spellings is one lock. The second call used to
// wait for the first, in the same process, forever.
func TestLockSameDirUnderTwoSpellings(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	link := filepath.Join(dir, "link")
	if err := os.Mkdir(real, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skip("no symlinks here:", err)
	}
	done := make(chan error, 1)
	go func() {
		if err := LockIndexDir(real); err != nil {
			done <- err
			return
		}
		done <- LockIndexDir(link)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the second lock on the same folder never returned")
	}
}

func TestIsKeptTagComparesLabels(t *testing.T) {
	rec := map[string]any{"kept_tags": []any{"Bücher"}}
	if !IsKeptTag(rec, "Bücher") {
		t.Error("a kept tag must be found under its decomposed spelling")
	}
	if IsKeptTag(rec, "Buch") {
		t.Error("another tag is not kept")
	}
}

// A record without an xattr field uses the default backend. A later write
// naming another backend must not put that name into the record.
func TestWriteDoesNotBackfillXattr(t *testing.T) {
	target := filepath.Join(t.TempDir(), "collections", "inbox.jsonl")
	os.MkdirAll(filepath.Dir(target), 0755)
	if err := os.WriteFile(target, []byte(`{"type":"ref","id":"5","binder":["A"],"filename":"f.pdf"}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	rec := NewRefRecord(map[string]any{"type": "ref", "id": "5", "binder": "B", "filename": "f.pdf", "xattr": "tags"})
	if _, err := JSONLWriteMany([]RefRecord{rec}, target); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(target)
	if strings.Contains(string(data), "xattr") || !strings.Contains(string(data), `"B"`) {
		t.Errorf("got %s", data)
	}
}
