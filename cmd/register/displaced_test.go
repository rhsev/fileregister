package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// displacedCase registers doc.txt, moves it away and puts another file in its
// place. It returns the env of an engine that tells the two apart, the env of
// the real engine, and the two paths.
func displacedCase(t *testing.T) (stub, real []string, notes, moved, newcomer string) {
	t.Helper()
	anchor := engineBin(t)
	home := t.TempDir()
	notes = t.TempDir()
	os.MkdirAll(filepath.Join(notes, "collections"), 0755)
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	newcomer = filepath.Join(dir, "doc.txt")
	writeFile(t, newcomer, "original")
	blob := engineSave(t, anchor, newcomer)
	share := filepath.Join(home, ".local", "share", "fileregister")
	os.MkdirAll(share, 0755)
	writeFile(t, filepath.Join(share, "bookmarks.json"), `{"800000001":"`+blob+`"}`)
	writeFile(t, filepath.Join(notes, "collections", "inbox.jsonl"),
		`{"type":"ref","id":"800000001","binder":["Docs"],"filename":"doc.txt"}`+"\n")

	real = refreshEnv(t, notes, home, anchor)
	if _, stderr, code := runGoRefresh(t, real); code != 0 {
		t.Fatalf("first refresh: exit %d\n%s", code, stderr)
	}

	os.MkdirAll(filepath.Join(dir, "elsewhere"), 0755)
	moved = filepath.Join(dir, "elsewhere", "renamed.txt")
	if err := os.Rename(newcomer, moved); err != nil {
		t.Fatal(err)
	}
	writeFile(t, newcomer, "another file")

	script := engineStub{Real: anchor, Displaced: map[string][2]string{blob: {moved, newcomer}}}.script(t)
	return refreshEnv(t, notes, home, script), real, notes, moved, newcomer
}

// A file that was moved, with another file under its old name: refresh
// writes nothing, audit reports it, and repair binds the record to the moved
// file, which alone carries the id.
func TestDisplacedFileIsSettledByRepair(t *testing.T) {
	stub, real, notes, moved, newcomer := displacedCase(t)
	anchor := engineBin(t)

	stdout, _, code := runGoRefresh(t, stub)
	if code != 1 || !strings.Contains(stdout, "Displaced (1)") {
		t.Errorf("refresh: exit %d\n%s", code, stdout)
	}
	if got := fileMeta(t, anchor, newcomer, "sync"); len(got) != 0 {
		t.Errorf("refresh wrote the id on the file in the old place: %v", got)
	}
	indexFile, _ := os.ReadFile(filepath.Join(notes, "collections", "inbox.jsonl"))
	if !strings.Contains(string(indexFile), `"doc.txt"`) {
		t.Errorf("refresh changed the file name in the index: %s", indexFile)
	}

	stdout, _, code = runGoAudit(t, stub)
	if code != 1 || !strings.Contains(stdout, "DISPLACED (1)") || !strings.Contains(stdout, newcomer) {
		t.Errorf("audit: exit %d\n%s", code, stdout)
	}

	stdout, _, code = runGoRepair(t, stub)
	if code != 0 || !strings.Contains(stdout, "The file carries the id") {
		t.Errorf("repair: exit %d\n%s", code, stdout)
	}
	indexFile, _ = os.ReadFile(filepath.Join(notes, "collections", "inbox.jsonl"))
	if !strings.Contains(string(indexFile), `"renamed.txt"`) {
		t.Errorf("repair did not write the new file name: %s", indexFile)
	}
	if got := fileMeta(t, anchor, newcomer, "sync"); len(got) != 0 {
		t.Errorf("repair wrote the id on the file in the old place: %v", got)
	}

	// The renewed bookmark records the new place, so nothing is left to settle.
	if stdout, _, code = runGoRefresh(t, real); code != 0 || strings.Contains(stdout, "Displaced") {
		t.Errorf("refresh after repair: exit %d\n%s", code, stdout)
	}
	if got, _, _ := runGoResolve(t, real, "800000001"); strings.TrimSpace(got) != moved {
		t.Errorf("resolve after repair = %q, want %q", got, moved)
	}
}

// When the file in the old place carries the id too, a copy for instance,
// repair does not choose by itself.
func TestDisplacedFileWithTwoCarriersNeedsAnAnswer(t *testing.T) {
	stub, _, _, _, newcomer := displacedCase(t)
	anchor := engineBin(t)
	cmd := exec.Command(anchor, "--sync-name", "com.fileregister.id#S")
	cmd.Stdin = strings.NewReader(`{"op":"set_meta","path":` + jsonString(newcomer) + `,"key":"sync","value":"800000001","mode":"set"}` + "\n")
	if out, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(out), `"ok":true`) {
		t.Fatalf("stamping the copy: %v %s", err, out)
	}

	stdout, _, code := runGoRepair(t, stub)
	if code != 1 || !strings.Contains(stdout, "Both files carry the id") {
		t.Errorf("repair: exit %d\n%s", code, stdout)
	}

	// --interactive, answer 2: the file in the old place.
	r, w, _ := os.Pipe()
	w.WriteString("2\n")
	w.Close()
	oldIn := os.Stdin
	os.Stdin = r
	stdout, _, code = runGoRepair(t, stub, "--interactive")
	os.Stdin = oldIn
	if code != 0 || !strings.Contains(stdout, "Repaired") {
		t.Errorf("repair --interactive: exit %d\n%s", code, stdout)
	}
	if got := fileMeta(t, anchor, newcomer, "id"); len(got) != 1 || got[0] != "800000001" {
		t.Errorf("the chosen file did not get the id: %v", got)
	}
}
