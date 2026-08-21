package index

// Regression tests for the line-preserving rewrite: JSONLWriteMany must honor
// the wire contract (foreign/unparseable/blank lines pass through verbatim,
// binder stays an array, duplicate-id lines are not collapsed).

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestJSONLWriteManyPreservesForeignLines(t *testing.T) {
	target := filepath.Join(t.TempDir(), "inbox.jsonl")
	content := `{"type":"note","text":"foreign no id"}` + "\n" +
		`{this line is not JSON}` + "\n" +
		"\n" +
		`{"type":"ref","id":"111","binder":[]}` + "\n" +
		`{"type":"ref","id":"222","binder":["a"],"filename":"x.pdf"}` + "\n"
	if err := os.WriteFile(target, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	actions, err := JSONLWriteMany([]RefRecord{{ID: "222", Binder: "b"}}, target)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0] != "updated" {
		t.Fatalf("actions = %v, want [updated]", actions)
	}

	got, _ := os.ReadFile(target)
	s := string(got)
	for _, want := range []string{
		`{"type":"note","text":"foreign no id"}`,
		`{this line is not JSON}`,
		`{"type":"ref","id":"111","binder":[]}`, // untouched line stays byte-identical
	} {
		if !strings.Contains(s, want) {
			t.Errorf("rewrite lost %q; file now:\n%s", want, s)
		}
	}
	if strings.Contains(s, `"binder":null`) {
		t.Errorf("rewrite emitted binder:null; file now:\n%s", s)
	}
	if !strings.Contains(s, "\n\n") {
		t.Errorf("rewrite dropped the blank separator line; file now:\n%s", s)
	}
	if !strings.Contains(s, `"b"`) {
		t.Errorf("set-insert missing; file now:\n%s", s)
	}
}

func TestJSONLWriteManyDuplicateIDLines(t *testing.T) {
	target := filepath.Join(t.TempDir(), "inbox.jsonl")
	content := `{"type":"ref","id":"42","binder":["a"]}` + "\n" +
		`{"type":"ref","id":"42","binder":["b"]}` + "\n"
	if err := os.WriteFile(target, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := JSONLWriteMany([]RefRecord{{ID: "42", Binder: "c"}}, target); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(target)
	lines := strings.Split(strings.TrimRight(string(got), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("line count = %d, want 2:\n%s", len(lines), got)
	}
	if lines[0] != `{"type":"ref","id":"42","binder":["a"]}` {
		t.Errorf("first duplicate line not preserved verbatim: %s", lines[0])
	}
	if !strings.Contains(lines[1], `"b"`) || !strings.Contains(lines[1], `"c"`) {
		t.Errorf("last duplicate line should carry b and c: %s", lines[1])
	}
}

func TestNormalizeBindersNeverNil(t *testing.T) {
	if NormalizeBinders(nil) == nil {
		t.Error("NormalizeBinders(nil) must return an empty slice, not nil (nil marshals as JSON null)")
	}
}

func TestPathsEqualUnicodeForms(t *testing.T) {
	// Both nonexistent -> string-fallback path: NFC normalization applies on
	// every platform, case folding only where the default filesystem folds.
	nfc := "/nonexistent/Café.pdf"  // e-acute precomposed (shell input)
	nfd := "/nonexistent/Café.pdf" // e + combining acute (Foundation output)
	if !PathsEqual(nfc, nfd) {
		t.Error("NFC and NFD forms of the same path must compare equal")
	}
	caseFolded := PathsEqual("/nonexistent/File.pdf", "/nonexistent/fILE.pdf")
	if runtime.GOOS == "darwin" && !caseFolded {
		t.Error("darwin: case fold must apply in the string fallback")
	}
	if runtime.GOOS != "darwin" && caseFolded {
		t.Error("non-darwin: paths differing in case are different files")
	}
}

func TestPathsEqualUsesFilesystemIdentity(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "File.pdf")
	if err := os.WriteFile(real, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.pdf")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if !PathsEqual(real, link) {
		t.Error("a symlink and its target are the same file")
	}
	if PathKey(real) != PathKey(link) {
		t.Error("PathKey must agree for a symlink and its target")
	}
	other := filepath.Join(dir, "Other.pdf")
	if err := os.WriteFile(other, []byte("y"), 0644); err != nil {
		t.Fatal(err)
	}
	if PathsEqual(real, other) || PathKey(real) == PathKey(other) {
		t.Error("distinct files must not compare equal")
	}
	// One side exists, the other names nothing on this filesystem -> the
	// filesystem's own lookup already decided; never equal.
	if PathsEqual(real, filepath.Join(dir, "missing.pdf")) {
		t.Error("an existing and a nonexistent path are never the same file")
	}
}
