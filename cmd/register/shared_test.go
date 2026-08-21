package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeFixture creates a temp dir with ≥3 binders.
func makeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	collectionsDir := filepath.Join(dir, "collections")
	notesDir := filepath.Join(dir, "notes")
	if err := os.MkdirAll(collectionsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(notesDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Binder "Photos": 2 files
	writeFile(t, filepath.Join(collectionsDir, "photos.jsonl"),
		`{"type":"ref","id":"100000001","binder":["Photos"],"filename":"vacation.jpg"}`+"\n"+
			`{"type":"ref","id":"100000002","binder":["Photos"],"filename":"beach.jpg"}`+"\n")

	// Binder "Documents": 3 files
	writeFile(t, filepath.Join(collectionsDir, "docs.jsonl"),
		`{"type":"ref","id":"200000001","binder":["Documents"],"filename":"report.pdf"}`+"\n"+
			`{"type":"ref","id":"200000002","binder":["Documents"],"filename":"summary.pdf"}`+"\n"+
			`{"type":"ref","id":"200000003","binder":["Documents"],"filename":"notes.md"}`+"\n")

	// Binder "Archive": 1 file
	writeFile(t, filepath.Join(collectionsDir, "archive.jsonl"),
		`{"type":"ref","id":"300000001","binder":["Archive"],"filename":"old.pdf"}`+"\n")

	// Binder "videos": 2 files (lowercase, tests sort order)
	writeFile(t, filepath.Join(collectionsDir, "videos.jsonl"),
		`{"type":"ref","id":"400000001","binder":["videos"],"filename":"clip1.mp4"}`+"\n"+
			`{"type":"ref","id":"400000002","binder":["videos"],"filename":"clip2.mp4"}`+"\n")

	// Markdown files are present but must NOT be read by default list
	writeFile(t, filepath.Join(notesDir, "annotated_docs.md"),
		"# Some note\n\n```yaml\ntype: ref\nid: 200000001\nbinder: Documents\nfilename: report.pdf\n```\n")

	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func captureListAll(t *testing.T, notes string) string {
	t.Helper()
	// Redirect stdout
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	dir, dirErr := func() (string, error) {
		prev := os.Getenv("GRUBBER_NOTES")
		os.Setenv("GRUBBER_NOTES", notes)
		defer func() {
			if prev == "" {
				os.Unsetenv("GRUBBER_NOTES")
			} else {
				os.Setenv("GRUBBER_NOTES", prev)
			}
		}()
		return notesDir()
	}()

	if dirErr == nil {
		listAll(dir)
	}

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	r.Close()
	return buf.String()
}

func filterEnv(env []string, exclude string) []string {
	var out []string
	for _, e := range env {
		if !strings.HasPrefix(e, exclude+"=") {
			out = append(out, e)
		}
	}
	return out
}

// TestListPlainNoSuffix: the default list carries no [inbox] / [annotated] suffix
// (a self-contained invariant, independent of the golden text).
func TestListPlainNoSuffix(t *testing.T) {
	out := captureListAll(t, makeFixture(t))
	if strings.Contains(out, "[inbox]") || strings.Contains(out, "[annotated]") {
		t.Errorf("default list must not contain annotation suffixes, got:\n%s", out)
	}
}

// TestListNumericID: numeric JSON ids are stringified to plain decimal strings.
func TestListNumericID(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "collections"), 0755)
	writeFile(t, filepath.Join(dir, "collections", "test.jsonl"),
		`{"type":"ref","id":100000002,"binder":["Test"],"filename":"a.pdf"}`+"\n")
	assertGolden(t, "list_numeric-id", captureListAll(t, dir))
}
