package main

import (
	"github.com/rhsev/fileregister/v2/internal/index"

	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubDyingOn wraps the real engine and ends the moment it is asked op, as an
// engine that crashes in the middle of a run does.
func stubDyingOn(t *testing.T, real, op string) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "fileanchor-stub")
	body := `#!/usr/bin/env python3
import json, subprocess, sys
engine = subprocess.Popen([` + quotePy(real) + `] + sys.argv[1:], stdin=subprocess.PIPE,
                          stdout=subprocess.PIPE, text=True, bufsize=1)
for line in sys.stdin:
    try:
        op = json.loads(line).get("op")
    except Exception:
        op = None
    if op == ` + quotePy(op) + `:
        sys.exit(1)
    engine.stdin.write(line)
    engine.stdin.flush()
    print(engine.stdout.readline().rstrip("\n"), flush=True)
`
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
	return script
}

// withEngine is env with another engine: same HOME, same notes.
func withEngine(env []string, anchor string) []string {
	return append(filterEnv(env, "FILEANCHOR"), "FILEANCHOR="+anchor)
}

// forget that cannot learn where the files are forgets nothing: it would
// drop the record and leave its id on the file.
func TestForgetStopsWhenThePathsAreUnknown(t *testing.T) {
	anchor := engineBin(t)
	notes := seedNotes(t)
	env := forgetEnv(t, notes, anchor)
	f := filepath.Join(t.TempDir(), "f.txt")
	writeFile(t, f, "f")
	if _, e, code := runGoAdd(t, env, f); code != 0 {
		t.Fatalf("add: %d %s", code, e)
	}
	id := index.AsString(inboxByFilename(t, notes)["f.txt"]["id"])
	before := mustRead(t, filepath.Join(notes, "collections", "inbox.jsonl"))

	_, e, code := runGoForget(t, withEngine(env, stubDyingOn(t, anchor, "resolve")), id)
	if code != 1 || !strings.Contains(e, "nothing forgotten") {
		t.Errorf("forget with a failing engine: exit %d\n%s", code, e)
	}
	if got := mustRead(t, filepath.Join(notes, "collections", "inbox.jsonl")); got != before {
		t.Errorf("the index changed:\n%s", got)
	}
	if got := queryStampedID(t, anchor, f); !strings.Contains(got, id) {
		t.Errorf("the id is off the file: %s", got)
	}
}

// cleanup --prune that could not check the entries says so and fails. It
// does not report that there is nothing to prune.
func TestPruneFailsWhenNothingWasChecked(t *testing.T) {
	needGrubber(t)
	anchor := engineBin(t)
	notes := seedNotes(t)
	env := forgetEnv(t, notes, anchor)
	f := filepath.Join(t.TempDir(), "f.txt")
	writeFile(t, f, "f")
	if _, e, code := runGoAdd(t, env, f); code != 0 {
		t.Fatalf("add: %d %s", code, e)
	}

	for _, args := range [][]string{{"--prune"}, {"--prune", "--dry-run"}} {
		so, se, code := runGoCleanup(t, withEngine(env, stubDyingOn(t, anchor, "resolve")), args...)
		if code != 1 || strings.Contains(so, "Nothing to prune") || !strings.Contains(se, "nothing pruned") {
			t.Errorf("cleanup %v with a failing engine: exit %d\n%s\n%s", args, code, so, se)
		}
	}
}

// marshal packs the rest of a binder when one member is a folder or package,
// and the exit code says that a file is not in the container.
func TestMarshalPacksAroundAFolderMember(t *testing.T) {
	needGrubber(t)
	anchor := engineBin(t)
	notes := seedNotes(t)
	env := forgetEnv(t, notes, anchor)
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.txt")
	pkg := filepath.Join(dir, "plan.graffle")
	writeFile(t, plain, "plain")
	os.MkdirAll(pkg, 0755)
	writeFile(t, filepath.Join(pkg, "data.plist"), "x")
	if _, e, code := runGoAdd(t, env, plain, pkg, "--binder", "Ship"); code != 0 {
		t.Fatalf("add: %d %s", code, e)
	}
	if len(inboxByFilename(t, notes)) != 2 {
		t.Skip("this engine does not register a folder")
	}

	out := filepath.Join(t.TempDir(), "ship.tar.gz")
	so, se, code := runGoMarshal(t, env, "--binder", "Ship", "--out", out)
	if code != 1 || !strings.Contains(se, "plan.graffle is a folder or package") || !strings.Contains(so, "missing files: 1") {
		t.Fatalf("marshal with a folder member: exit %d\n%s\n%s", code, so, se)
	}
	unpacked := t.TempDir()
	untar(t, out, unpacked)
	if m, _ := filepath.Glob(filepath.Join(unpacked, "*", "files", "plain.txt")); len(m) != 1 {
		t.Errorf("plain.txt is not in the container")
	}
	manifest, _ := filepath.Glob(filepath.Join(unpacked, "*", "manifest.jsonl"))
	if len(manifest) != 1 || !strings.Contains(mustRead(t, manifest[0]), "plan.graffle") {
		t.Errorf("the folder's record is not in the manifest")
	}
}

// The manifest is the one at the top of the container. A registered file of
// the same name lies under files/ and sorts before it.
func TestFindManifestIgnoresAPackedFileOfThatName(t *testing.T) {
	root := t.TempDir()
	top := filepath.Join(root, "akte")
	os.MkdirAll(filepath.Join(top, "files"), 0755)
	writeFile(t, filepath.Join(top, "files", "manifest.jsonl"), `{"type":"ref","id":"evil"}`+"\n")
	if got := findManifest(root); got != "" {
		t.Errorf("a packed file was taken for the manifest: %s", got)
	}
	writeFile(t, filepath.Join(top, "manifest.jsonl"), `{"type":"head"}`+"\n")
	if got := findManifest(root); got != filepath.Join(top, "manifest.jsonl") {
		t.Errorf("findManifest = %s", got)
	}
	writeFile(t, filepath.Join(root, "manifest.jsonl"), `{"type":"head"}`+"\n")
	if got := findManifest(root); got != filepath.Join(root, "manifest.jsonl") {
		t.Errorf("findManifest with one at the root = %s", got)
	}
}

// collections/ is protected under every spelling the file system takes for
// it: through a symlink, and in another case where the volume ignores case.
func TestCollectionsIsProtectedUnderAnotherSpelling(t *testing.T) {
	notes := t.TempDir()
	col := filepath.Join(notes, "collections")
	os.MkdirAll(filepath.Join(col, "sub"), 0755)
	alias := filepath.Join(notes, "alias")
	if err := os.Symlink(col, alias); err != nil {
		t.Fatal(err)
	}
	if !underDirOnDisk(filepath.Join(alias, "inbox.jsonl"), col) {
		t.Errorf("a path through a symlink to collections/ is not seen as under it")
	}
	if !underDirOnDisk(filepath.Join(alias, "sub", "new", "inbox.jsonl"), col) {
		t.Errorf("a deeper path through the symlink is not seen as under it")
	}
	if underDirOnDisk(filepath.Join(notes, "elsewhere", "inbox.jsonl"), col) {
		t.Errorf("a path beside collections/ is seen as under it")
	}
	upper := filepath.Join(notes, "Collections")
	if _, err := os.Stat(upper); err != nil {
		t.Log("this volume tells Collections from collections; case not tested")
		return
	}
	target := filepath.Join(upper, "inbox.jsonl")
	if why := protectedTarget(target, underDirOnDisk(target, col)); why == "" {
		t.Errorf("Collections/inbox.jsonl is not protected on a volume that ignores case")
	}
}
