package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// refresh --snapshot gives a file that lost every attribute its free Finder
// tags and comment back. A file that kept its id keeps the tags it has: a tag
// missing there was taken off on purpose.
func TestRefreshBringsBackFinderFromASnapshot(t *testing.T) {
	anchor := engineBin(t)
	fx := makeSnapFixture(t, anchor)
	vertrag := filepath.Join(fx.home, "Ablage", "Steuer", "vertrag.pdf")
	a := filepath.Join(fx.notes, "a.txt")
	engineCall(t, anchor, map[string]any{"op": "tag", "path": a, "value": "Rot"})
	snap := snapshotOf(t, fx, "--no-files")

	// A program saves vertrag.pdf through a new file: new inode, no attributes.
	tmp := vertrag + ".tmp"
	writeFile(t, tmp, "Vertrag, unterschrieben\n")
	if err := os.Rename(tmp, vertrag); err != nil {
		t.Fatal(err)
	}
	// The tag on a.txt is taken off by hand; its id stays.
	engineCall(t, anchor, map[string]any{"op": "untag", "path": a, "value": "Rot"})
	comments := watchComments(t)

	so, se, _ := runIn(t, fx.env, cmdRefresh, "--dry-run", "--snapshot", snap)
	if !strings.Contains(so, "[dry-run] Finder tags and comment from the snapshot → vertrag.pdf") ||
		strings.Contains(so, "snapshot → a.txt") {
		t.Fatalf("dry run:\n%s\n%s", so, se)
	}
	if got := strings.Join(engineTags(t, anchor, vertrag), ","); got != "" {
		t.Errorf("the dry run wrote tags: %s", got)
	}

	so, se, _ = runIn(t, fx.env, cmdRefresh, "--snapshot", snap)
	if !strings.Contains(so, "From snapshot: 1") {
		t.Errorf("refresh:\n%s\n%s", so, se)
	}
	if got := strings.Join(engineTags(t, anchor, vertrag), ","); got != "Steuer 2026,★" {
		t.Errorf("vertrag.pdf tags = %s, want the free tag back and ★", got)
	}
	if comments["vertrag.pdf"] != "Original im Ordner" || len(comments) != 1 {
		t.Errorf("comment writes = %v", comments)
	}
	if got := strings.Join(engineTags(t, anchor, a), ","); got != "★" {
		t.Errorf("a.txt tags = %s, want only ★: the removed tag stays removed", got)
	}

	if _, se, code := runIn(t, fx.env, cmdRefresh, "--snapshot", filepath.Join(t.TempDir(), "none.tar.gz")); code == 0 || !strings.Contains(se, "Error") {
		t.Errorf("a missing snapshot: exit %d, %q", code, se)
	}
}
