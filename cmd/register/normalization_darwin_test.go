package main

// APFS treats both Unicode forms of a name as one file, so on macOS add takes
// a decomposed path and records it precomposed. On Linux the two forms are
// two files and add refuses the decomposed one (TestAddRefusesADecomposedName
// in platform_other_test.go).

import (
	"path/filepath"
	"testing"

	unorm "golang.org/x/text/unicode/norm"
)

// A file whose name is decomposed on disk is recorded precomposed, whichever
// form the caller's argument had. Before, the index kept the caller's bytes, so
// two files from the same directory could land in two different forms.
func TestAddRecordsTheFilenameNFC(t *testing.T) {
	anchor := engineBin(t)
	notes := seedNotes(t)
	files := t.TempDir()
	env := addEnv(t, notes, anchor)

	nfdPath := touchNFD(t, files, "Grüße Käse.md")
	if base := filepath.Base(nfdPath); base == unorm.NFC.String(base) {
		t.Fatal("the fixture is not decomposed on disk, so this test cannot fire")
	}

	// The decomposed path, as shell completion or the Quick Action hands it over.
	if _, se, code := runGoAdd(t, env, nfdPath, "--binder", "Akte"); code != 0 {
		t.Fatalf("add failed (%d): %s", code, se)
	}
	// A second file, also decomposed on disk, added by its precomposed path the
	// way it is typed. APFS resolves that to the same file.
	touchNFD(t, files, "Lörrach Tür.md")
	typed := filepath.Join(files, unorm.NFC.String("Lörrach Tür.md"))
	if _, se, code := runGoAdd(t, env, typed, "--binder", "Akte"); code != 0 {
		t.Fatalf("add failed (%d): %s", code, se)
	}

	for _, got := range indexFilenames(t, notes) {
		if got != unorm.NFC.String(got) {
			t.Errorf("index holds a decomposed filename %q", got)
		}
	}
}
