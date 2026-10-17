package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A marshal container is plain tar and has to pack with the tar of the system
// it is made on. tarCreateArgs differs per platform: GNU tar exits 64 on
// bsdtar's --no-mac-metadata, which is what this pins on Linux.
func TestTarCreateMakesAPlainArchive(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "box"), 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "box", "a.txt"), "hi")
	out := filepath.Join(dir, "box.tar.gz")
	if err := tarCreate(out, dir, "box"); err != nil {
		t.Fatalf("tarCreate: %v", err)
	}
	list, err := exec.Command("tar", "-tzf", out).Output()
	if err != nil {
		t.Fatalf("tar -tzf: %v", err)
	}
	if !strings.Contains(string(list), "box/a.txt") {
		t.Errorf("archive lacks box/a.txt:\n%s", list)
	}
}
