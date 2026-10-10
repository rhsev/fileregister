package main

import (
	"github.com/rhsev/fileregister/v2/internal/index"

	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.jsonl")

	// Create.
	if err := index.AtomicWrite(p, []byte("one\n")); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, p); got != "one\n" {
		t.Fatalf("got %q", got)
	}

	// Replace.
	if err := index.AtomicWrite(p, []byte("two\n")); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, p); got != "two\n" {
		t.Fatalf("got %q", got)
	}

	// Mode is the layer's usual 0644, and no temp files are left behind.
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0644 {
		t.Fatalf("mode %v, want 0644", st.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}

	// A missing parent directory fails loudly instead of silently.
	if err := index.AtomicWrite(filepath.Join(dir, "no", "such", "dir.md"), []byte("x")); err == nil {
		t.Fatal("expected error for missing parent dir")
	}
}
