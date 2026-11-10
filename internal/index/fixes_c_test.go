package index

import (
	"os"
	"path/filepath"
	"testing"
)

// An index file that cannot be read is not an absent one: writing stops
// instead of appending a second line for an id the file already holds.
func TestWriteStopsOnAnUnreadableIndex(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads every file")
	}
	target := filepath.Join(t.TempDir(), "collections", "inbox.jsonl")
	os.MkdirAll(filepath.Dir(target), 0755)
	line := `{"type":"ref","id":"1","binder":["A"],"filename":"a.txt"}` + "\n"
	if err := os.WriteFile(target, []byte(line), 0200); err != nil {
		t.Fatal(err)
	}
	_, err := JSONLWriteMany([]RefRecord{{ID: "1", Binder: "B", Filename: "a.txt"}}, target)
	if err == nil {
		t.Errorf("writing to an unreadable index reported no error")
	}
	os.Chmod(target, 0644)
	if data, _ := os.ReadFile(target); string(data) != line {
		t.Errorf("the index changed:\n%s", data)
	}
}
