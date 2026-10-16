package main

// normalization_test — one Unicode form in the index (internal/index/normalize.go).
//
// macOS hands the same text over in two byte forms, and the writers that feed
// this index differ: a name typed on the keyboard arrives precomposed (NFC),
// while a path from the Finder, Apple Mail or DEVONthink arrives decomposed
// (NFD). APFS resolves either on lookup, so a mismatch is invisible until
// something compares bytes. These tests pin the three places where that used
// to go wrong: what gets written, what counts as the same binder, and what a
// typed handle finds.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	unorm "golang.org/x/text/unicode/norm"
)

// indexFilenames returns the filename field of every ref record, in file order.
func indexFilenames(t *testing.T, notes string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(notes, "collections", "inbox.jsonl"))
	if err != nil {
		t.Fatalf("reading the index: %v", err)
	}
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("index line is not JSON: %v", err)
		}
		if s, _ := rec["filename"].(string); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// indexBinders returns the binder set of the record with this filename.
func indexBinders(t *testing.T, notes, filename string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(notes, "collections", "inbox.jsonl"))
	if err != nil {
		t.Fatalf("reading the index: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		if s, _ := rec["filename"].(string); s != filename {
			continue
		}
		var out []string
		arr, _ := rec["binder"].([]any)
		for _, b := range arr {
			if s, ok := b.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	t.Fatalf("no record with filename %q", filename)
	return nil
}

// seedNotes makes an empty notes directory with a collections dir.
func seedNotes(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "collections"), 0755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// touchNFD creates a file whose name is stored decomposed, the way the Finder,
// Apple Mail and DEVONthink create theirs, and returns its path (also NFD).
func touchNFD(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, unorm.NFD.String(name))
	if err := os.WriteFile(p, []byte("# x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

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

// Two spellings of one binder name are one binder: `list` shows it once with
// every member, not once per form with half the members each.
func TestOneBinderInTwoFormsIsOneBinder(t *testing.T) {
	anchor := engineBin(t)
	notes := seedNotes(t)
	files := t.TempDir()
	env := addEnv(t, notes, anchor)

	nfc := "Übung"
	nfd := unorm.NFD.String(nfc)
	if nfc == nfd {
		t.Fatal("the fixture has no decomposable character, so this test cannot fire")
	}

	a := filepath.Join(files, "a.md")
	b := filepath.Join(files, "b.md")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("# x\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if _, se, code := runGoAdd(t, env, a, "--binder", nfc); code != 0 {
		t.Fatalf("add failed (%d): %s", code, se)
	}
	if _, se, code := runGoAdd(t, env, b, "--binder", nfd); code != 0 {
		t.Fatalf("add failed (%d): %s", code, se)
	}

	// The binder list names it once.
	so, se, code := runGoList(t, env)
	if code != 0 {
		t.Fatalf("list failed (%d): %s", code, se)
	}
	// Counted on the normalized output: the two spellings differ in bytes, so
	// counting the raw text would find one of each and call that a single hit.
	if n := strings.Count(unorm.NFC.String(so), nfc); n != 1 {
		t.Errorf("binder listed %d times, want 1:\n%s", n, so)
	}

	// And either spelling lists both members.
	for _, q := range []string{nfc, nfd} {
		so, se, code := runGoList(t, env, q)
		if code != 0 {
			t.Fatalf("list %q failed (%d): %s", q, code, se)
		}
		for _, want := range []string{"a.md", "b.md"} {
			if !strings.Contains(so, want) {
				t.Errorf("list %q is missing %s:\n%s", q, want, so)
			}
		}
	}
}

// Adding a file to a binder it is already in, naming the binder in the other
// form, is a noop. Before, it appended a second membership and the record sat
// in one binder twice.
func TestAddingTheOtherFormIsNotASecondMembership(t *testing.T) {
	anchor := engineBin(t)
	notes := seedNotes(t)
	files := t.TempDir()
	env := addEnv(t, notes, anchor)

	nfc := "Übung"
	nfd := unorm.NFD.String(nfc)
	p := filepath.Join(files, "a.md")
	if err := os.WriteFile(p, []byte("# x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, se, code := runGoAdd(t, env, p, "--binder", nfc); code != 0 {
		t.Fatalf("add failed (%d): %s", code, se)
	}
	if _, se, code := runGoAdd(t, env, p, "--binder", nfd); code != 0 {
		t.Fatalf("second add failed (%d): %s", code, se)
	}

	binders := indexBinders(t, notes, "a.md")
	if len(binders) != 1 {
		t.Fatalf("binder set is %q, want one entry", binders)
	}
	if binders[0] != nfc {
		t.Errorf("binder stored as %q, want the precomposed %q", binders[0], nfc)
	}
}

// An aka handle is found whichever form the key is typed in. The handle is
// stored precomposed; a key pasted out of a file name arrives decomposed.
func TestResolveFindsAkaInEitherForm(t *testing.T) {
	anchor := engineBin(t)
	notes := seedNotes(t)
	files := t.TempDir()
	env := addEnv(t, notes, anchor)

	handle := "grüße-käse"
	nfd := unorm.NFD.String(handle)
	if handle == nfd {
		t.Fatal("the fixture has no decomposable character, so this test cannot fire")
	}

	p := filepath.Join(files, "a.md")
	if err := os.WriteFile(p, []byte("# x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, se, code := runGoAdd(t, env, p, "--binder", "Akte", "--aka", handle); code != 0 {
		t.Fatalf("add failed (%d): %s", code, se)
	}

	for _, key := range []string{handle, nfd} {
		so, se, code := runGoResolve(t, env, key)
		if code != 0 {
			t.Fatalf("resolve %q failed (%d): %s %s", key, code, so, se)
		}
		if !strings.Contains(so, "a.md") {
			t.Errorf("resolve %q did not find the file: %q", key, so)
		}
	}
}

// Renaming a decomposed binder name onto its own precomposed spelling is
// canonicalization, not a merge of two binders, so it does not demand --merge.
// This is the remedy `register audit` prints for a NOT CANONICAL entry; if it
// needed a flag, that advice would be one no run could carry out.
func TestRenameOntoOwnPrecomposedFormNeedsNoMerge(t *testing.T) {
	anchor := engineBin(t)
	notes := seedNotes(t)
	files := t.TempDir()
	env := addEnv(t, notes, anchor)

	nfc := "Übung"
	nfd := unorm.NFD.String(nfc)
	p := filepath.Join(files, "a.md")
	if err := os.WriteFile(p, []byte("# x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, se, code := runGoAdd(t, env, p, "--binder", nfc); code != 0 {
		t.Fatalf("add failed (%d): %s", code, se)
	}
	// Put the record back into the state a pre-rule write left behind.
	jsonl := filepath.Join(notes, "collections", "inbox.jsonl")
	data, err := os.ReadFile(jsonl)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jsonl, []byte(strings.ReplaceAll(string(data), nfc, nfd)), 0644); err != nil {
		t.Fatal(err)
	}
	if got := indexBinders(t, notes, "a.md"); len(got) != 1 || got[0] != nfd {
		t.Fatalf("fixture is %q, want the decomposed name, so this test cannot fire", got)
	}

	so, se, code := runGoRename(t, env, nfd, nfc)
	if code != 0 {
		t.Fatalf("rename failed (%d): %s %s", code, so, se)
	}
	if got := indexBinders(t, notes, "a.md"); len(got) != 1 || got[0] != nfc {
		t.Errorf("binder set is %q, want the single precomposed %q", got, nfc)
	}
}

// The note a binder gets is created inside the notes directory, which is a
// synced vault. iOS does not reconstruct a decomposed path, so a note written
// under a decomposed name is invisible on the iPhone and iPad. Its file name is
// precomposed even when the binder name arrived decomposed.
func TestBinderNoteFilenameIsNFC(t *testing.T) {
	anchor := engineBin(t)
	notes := seedNotes(t)
	files := t.TempDir()
	env := addEnv(t, notes, anchor)

	nfd := unorm.NFD.String("Behörde")
	p := filepath.Join(files, "a.md")
	if err := os.WriteFile(p, []byte("# x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, se, code := runGoAdd(t, env, p, "--binder", nfd, "--md"); code != 0 {
		t.Fatalf("add failed (%d): %s", code, se)
	}

	entries, err := os.ReadDir(filepath.Join(notes, "collections"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "binder_") {
			continue
		}
		found = true
		if name != unorm.NFC.String(name) {
			t.Errorf("note file name is decomposed: %q", name)
		}
	}
	if !found {
		t.Fatal("no binder note was written, so this test cannot fire")
	}
}
