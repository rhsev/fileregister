package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rhsev/fileregister/internal/index"
)

func TestUnmountedVolume(t *testing.T) {
	cases := map[string]string{
		"/Volumes/no-such-volume-4711/a/b.pdf": "no-such-volume-4711",
		"/Users/someone/doc.pdf":               "",
		"/Volumes/":                            "",
		"":                                     "",
	}
	for p, want := range cases {
		if got := unmountedVolume(p); got != want {
			t.Errorf("unmountedVolume(%q) = %q, want %q", p, got, want)
		}
	}
	if volumeOf("/Volumes/lightning/x") != "/Volumes/lightning" || volumeOf("/Users/x") != "/" {
		t.Error("volumeOf")
	}
}

// A file found by name or id is not taken when it is in the Trash or a
// backup, or when it is another record's file.
func TestCandidateProblem(t *testing.T) {
	for _, p := range []string{
		"/Users/me/.Trash/rechnung.pdf",
		"/Volumes/Backup/Backups.backupdb/Mac/2026/Users/me/rechnung.pdf",
		"/Volumes/.timemachine/abc/2026/Data/Users/me/rechnung.pdf",
	} {
		if candidateProblem(p, "111", map[string]string{}, map[string]bool{}) == "" {
			t.Errorf("%s was accepted", p)
		}
	}

	anchor := engineBin(t)
	restore := setEnv(t, identityEnv(t, t.TempDir(), anchor))
	defer restore()
	index.ResetEngine()
	defer index.ResetEngine()

	other := filepath.Join(t.TempDir(), "rechnung.pdf")
	writeFile(t, other, "someone else's")
	res, err := index.AddMany([]string{other}, nil, nil)
	if err != nil || len(res) != 1 || res[0].ID == "" {
		t.Fatalf("AddMany: %v %v", res, err)
	}
	db, _ := index.LoadDB()
	if got := candidateProblem(other, "999999999", db, map[string]bool{}); got != "it is record "+res[0].ID {
		t.Errorf("another record's file: %q", got)
	}
	if got := candidateProblem(other, res[0].ID, db, map[string]bool{}); got != "" {
		t.Errorf("the record's own file: %q", got)
	}
}

// repair renews the bookmark even when the file refuses its metadata, and
// says what is missing instead of reporting a clean repair.
func TestRepairRecordReportsWhatItCouldNotWrite(t *testing.T) {
	anchor := engineBin(t)
	restore := setEnv(t, identityEnv(t, t.TempDir(), anchor))
	defer restore()
	index.ResetEngine()
	defer index.ResetEngine()

	f := filepath.Join(t.TempDir(), "locked.pdf")
	writeFile(t, f, "x")
	os.Chmod(f, 0444)
	defer os.Chmod(f, 0644)

	rec := map[string]any{"id": "123456789", "binder": []any{"proj"}}
	id, failed := repairRecord(rec, f)
	if id != "123456789" {
		t.Fatalf("the bookmark was not renewed: %q", id)
	}
	if len(failed) != 2 {
		t.Errorf("failed = %v, want the binder and the ★ marker", failed)
	}
}
