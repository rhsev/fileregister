package main

import (
	"github.com/rhsev/fileregister/v2/internal/index"

	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// snapshot --no-files --since reads only the files whose size or modification
// time changed; the others keep the earlier snapshot's sha256 unread.
func TestSnapshotSinceReadsOnlyChangedFiles(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file without permissions")
	}
	anchor := engineBin(t)
	fx := makeSnapFixture(t, anchor)
	first := snapshotOf(t, fx, "--no-files")
	before := fileLines(t, first)
	if index.AsString(before["a.txt"]["mtime"]) == "" {
		t.Fatalf("a.txt has no mtime: %v", before["a.txt"])
	}

	b := filepath.Join(fx.notes, "b.txt")
	writeFile(t, b, "Beleg B, korrigiert\n")
	// Unreadable: a snapshot that read a.txt would have to say so.
	a := filepath.Join(fx.notes, "a.txt")
	if err := os.Chmod(a, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(a, 0644) })

	out := filepath.Join(t.TempDir(), "second.tar.gz")
	so, se, code := runIn(t, fx.env, cmdSnapshot, "--no-files", "--since", first, "--out", out)
	if code != 0 || strings.Contains(se, "cannot read") {
		t.Fatalf("exit %d\n%s\n%s", code, so, se)
	}
	if !strings.Contains(so, "checksums    : 1 read, 3 unchanged since the earlier snapshot") {
		t.Errorf("summary:\n%s", so)
	}
	after := fileLines(t, out)
	if after["a.txt"]["sha256"] != before["a.txt"]["sha256"] {
		t.Errorf("a.txt sha256 %v, want the earlier %v", after["a.txt"]["sha256"], before["a.txt"]["sha256"])
	}
	sum := sha256.Sum256([]byte("Beleg B, korrigiert\n"))
	if got := index.AsString(after["b.txt"]["sha256"]); got != hex.EncodeToString(sum[:]) {
		t.Errorf("b.txt sha256 %s, want the changed content's", got)
	}

	if _, se, code := runIn(t, fx.env, cmdSnapshot, "--since", first, "--out", filepath.Join(t.TempDir(), "x.tar.gz")); code == 0 || !strings.Contains(se, "--since only goes with --no-files") {
		t.Errorf("--since with the files: exit %d, %q", code, se)
	}
}
