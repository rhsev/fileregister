//go:build darwin || linux

package index

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// A note's Finder tags and comment live in xattrs on the file; a rewrite
// replaces the file and must carry them over.
func TestAtomicWriteKeepsXattrs(t *testing.T) {
	p := filepath.Join(t.TempDir(), "binder_proj.md")
	if err := os.WriteFile(p, []byte("old\n"), 0644); err != nil {
		t.Fatal(err)
	}
	attrs := map[string]string{
		"com.apple.metadata:_kMDItemUserTags": "bplist-stand-in",
		"com.example.note":                    "keep me",
	}
	for name, value := range attrs {
		if err := unix.Setxattr(p, name, []byte(value), 0); err != nil {
			t.Skipf("filesystem without xattrs: %v", err)
		}
	}

	if err := AtomicWrite(p, []byte("new\n")); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); string(got) != "new\n" {
		t.Fatalf("content = %q", got)
	}
	for name, want := range attrs {
		buf := make([]byte, 256)
		n, err := unix.Getxattr(p, name, buf)
		if err != nil || string(buf[:n]) != want {
			t.Errorf("%s after rewrite: %q, %v; want %q", name, buf[:max(n, 0)], err, want)
		}
	}
}
