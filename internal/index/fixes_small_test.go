package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"
)

// One JSON object and nothing after it.
func TestParseJSONObjectRefusesTrailingBrackets(t *testing.T) {
	for _, bad := range []string{`{"id":"1"}}`, `{"id":"1"}]`, `{"id":"1"}{"id":"2"}`, `{"id":"1"} x`} {
		if ParseJSONObject(bad) != nil {
			t.Errorf("%s was taken for a record", bad)
		}
	}
	for _, good := range []string{`{"id":"1"}`, ` {"id":"1"} `} {
		if ParseJSONObject(good) == nil {
			t.Errorf("%s was refused", good)
		}
	}
}

// A line that is no ref passes through untouched, also when it carries the
// id of a record.
func TestWriteLeavesAForeignLineWithTheSameID(t *testing.T) {
	target := filepath.Join(t.TempDir(), "collections", "inbox.jsonl")
	os.MkdirAll(filepath.Dir(target), 0755)
	foreign := `{"type":"ordering","id":"111","note":"x"}`
	if err := os.WriteFile(target, []byte(`{"type":"ref","id":"111","binder":["a"],"filename":"f.pdf"}`+"\n"+foreign+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := JSONLWriteMany([]RefRecord{{ID: "111", Binder: "b"}}, target); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(target)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 || lines[1] != foreign {
		t.Fatalf("the foreign line changed:\n%s", data)
	}
	if rec := ParseJSONObject(lines[0]); strings.Join(AsStrings(rec["binder"]), ",") != "a,b" {
		t.Errorf("the ref line did not get the binder: %s", lines[0])
	}
}

// kept_tags: a tag named decomposed is the one stored composed.
func TestSetKeptTagsComparesLabels(t *testing.T) {
	nfc, nfd := norm.NFC.String("Bücher"), norm.NFD.String("Bücher")
	target := filepath.Join(t.TempDir(), "inbox.jsonl")
	os.WriteFile(target, []byte(`{"type":"ref","id":"1","binder":["x"],"kept_tags":["`+nfc+`"]}`+"\n"), 0644)

	if _, err := JSONLSetKeptTags(target, map[string]map[string]bool{"1": {nfd: true}}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(target)
	if got := KeptTags(ParseJSONObject(strings.TrimSpace(string(data)))); len(got) != 1 {
		t.Errorf("kept_tags after keeping the same tag again: %q", got)
	}
	if _, err := JSONLSetKeptTags(target, map[string]map[string]bool{"1": {nfd: false}}); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(target)
	if got := KeptTags(ParseJSONObject(strings.TrimSpace(string(data)))); len(got) != 0 {
		t.Errorf("kept_tags after dropping the tag by its decomposed name: %q", got)
	}
}
