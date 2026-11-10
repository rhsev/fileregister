package main

import (
	"github.com/rhsev/fileregister/v2/internal/index"

	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stubResolve wraps the real engine. searchable is answered with yes, so a
// miss counts where the test's folders are not indexed. A resolve of a blob in
// noPath is answered without last_path, one of a blob in gonePath as resolving
// to that path.
func stubResolve(t *testing.T, real string, noPath []string, gonePath map[string]string) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "fileanchor-stub")
	var np, gp []string
	for _, b := range noPath {
		np = append(np, quotePy(b))
	}
	for b, p := range gonePath {
		gp = append(gp, quotePy(b)+": "+quotePy(p))
	}
	body := `#!/usr/bin/env python3
import json, subprocess, sys
engine = subprocess.Popen([` + quotePy(real) + `] + sys.argv[1:], stdin=subprocess.PIPE,
                          stdout=subprocess.PIPE, text=True, bufsize=1)
no_path = {` + strings.Join(np, ", ") + `}
gone = {` + strings.Join(gp, ", ") + `}
for line in sys.stdin:
    try:
        req = json.loads(line)
    except Exception:
        req = {}
    op = req.get("op")
    if op == "searchable":
        print('{"ok":true,"searchable":true}', flush=True)
        continue
    if op == "resolve" and req.get("blob") in gone:
        print(json.dumps({"ok": True, "path": gone[req.get("blob")]}), flush=True)
        continue
    engine.stdin.write(line)
    engine.stdin.flush()
    reply = engine.stdout.readline().rstrip("\n")
    if op == "resolve" and req.get("blob") in no_path:
        r = json.loads(reply)
        r.pop("last_path", None)
        reply = json.dumps(r)
    print(reply, flush=True)
`
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
	return script
}

// bookmarkBlobs reads the identity store of env's HOME.
func bookmarkBlobs(t *testing.T, env []string) map[string]string {
	t.Helper()
	restore := setEnv(t, env)
	defer restore()
	db, err := index.LoadDB()
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// list --inbox/--curated: a block that names its record by a handle is that
// record's annotation, as it is for annotate, aka and order.
func TestListCountsABlockThatNamesItsRecordByHandle(t *testing.T) {
	needGrubber(t)
	anchor := engineBin(t)
	n := seedAkaFixture(t, "")
	env := listEnv(t, n, anchor)
	curated, _, code := runGoList(t, env, "Anno", "--curated", "--json")
	if code != 0 || !strings.Contains(curated, `"id":"33"`) {
		t.Errorf("gamma.pdf (block with the handle in the id slot) is not listed as curated: exit %d\n%s", code, curated)
	}
	inbox, _, _ := runGoList(t, env, "Anno", "--inbox", "--json")
	if strings.Contains(inbox, `"id":"33"`) {
		t.Errorf("gamma.pdf is listed in the inbox:\n%s", inbox)
	}
	if !strings.Contains(inbox, `"id":"22"`) {
		t.Errorf("beta.pdf has no block and is missing from the inbox:\n%s", inbox)
	}
}

// forget takes a member whose bookmark resolves to a place where no file is:
// remove cannot (no file), so nothing else gets the record out.
func TestForgetTakesAMemberWhoseBookmarkPointsAtNothing(t *testing.T) {
	needGrubber(t)
	anchor := engineBin(t)
	notes := seedNotes(t)
	env := forgetEnv(t, notes, anchor)
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	f := filepath.Join(dir, "f.txt")
	writeFile(t, f, "f")
	if _, e, code := runGoAdd(t, env, f, "--binder", "B"); code != 0 {
		t.Fatalf("add: %d %s", code, e)
	}
	id := index.AsString(inboxByFilename(t, notes)["f.txt"]["id"])
	blob := bookmarkBlobs(t, env)[id]
	os.Remove(f)

	stub := stubResolve(t, anchor, nil, map[string]string{blob: f})
	so, se, code := runGoForget(t, withEngine(env, stub), id)
	if code != 0 || !strings.Contains(so, "Forgot") {
		t.Fatalf("forget: exit %d\n%s\n%s", code, so, se)
	}
	if len(inboxByFilename(t, notes)) != 0 {
		t.Errorf("the record is still in the index")
	}
}

// A bookmark that does not say where its file was is not judged, also when
// other entries do say it. cleanup --prune then refuses.
func TestABookmarkWithoutAPathIsNotCalledDead(t *testing.T) {
	needGrubber(t)
	anchor := engineBin(t)
	notes := seedNotes(t)
	env := forgetEnv(t, notes, anchor)
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	writeFile(t, a, "a")
	writeFile(t, b, "b")
	if _, e, code := runGoAdd(t, env, a, b); code != 0 {
		t.Fatalf("add: %d %s", code, e)
	}
	recs := inboxByFilename(t, notes)
	blobs := bookmarkBlobs(t, env)
	os.Remove(a)
	os.Remove(b)
	// The records go, the bookmark entries stay: two entries no record needs.
	os.Remove(filepath.Join(notes, "collections", "inbox.jsonl"))

	noPath := blobs[index.AsString(recs["a.txt"]["id"])]
	so, se, code := runGoCleanup(t, withEngine(env, stubResolve(t, anchor, []string{noPath}, nil)), "--prune")
	if code != 1 || !strings.Contains(so, "Refusing to prune: 1") {
		t.Errorf("cleanup --prune with one entry without a path: exit %d\n%s\n%s", code, so, se)
	}
	if got := bookmarkBlobs(t, env); len(got) != 2 {
		t.Errorf("%d bookmark entries left, want both", len(got))
	}
}

// write: two lines of one batch must not fork an id over two index files,
// nor give one handle to two records.
func TestWriteChecksTheBatchAgainstItself(t *testing.T) {
	anchor := engineBin(t)
	notes := t.TempDir()
	col := filepath.Join(notes, "collections")
	os.MkdirAll(col, 0755)
	a, b := filepath.Join(col, "a.jsonl"), filepath.Join(col, "b.jsonl")
	env := append(writeEnv(t, notes), "FILEANCHOR="+anchor)

	out, _, code := runGoWrite(t, env,
		writeLine(`"_note_file":"`+a+`","type":"ref","id":"601","url":"x://1","binder":"x","aka":"key"`)+
			writeLine(`"_note_file":"`+b+`","type":"ref","id":"601","url":"x://1","binder":"y"`)+
			writeLine(`"_note_file":"`+a+`","type":"ref","id":"602","url":"x://2","binder":"x","aka":"key"`))
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != 1 || len(lines) != 3 {
		t.Fatalf("code %d, out:\n%s", code, out)
	}
	if !strings.Contains(lines[0], `"ok":true`) {
		t.Errorf("first line: %s", lines[0])
	}
	for i, want := range map[int]string{1: "earlier in this batch", 2: "earlier in this batch"} {
		if !strings.Contains(lines[i], `"ok":false`) || !strings.Contains(lines[i], want) {
			t.Errorf("line %d: %s (want %q)", i+1, lines[i], want)
		}
	}
	if index.FileExists(b) {
		t.Errorf("the forked id was written to b.jsonl")
	}
}

// Deleting one of two blocks under a shared heading leaves the heading to
// the other.
func TestDeleteBlockLeavesASharedHeading(t *testing.T) {
	note := filepath.Join(t.TempDir(), "n.md")
	writeFile(t, note, "### doc.pdf\n```yaml\ntype: ref\nid: '1'\nbinder: B\n```\n```yaml\ntype: ref\nid: '1'\nbinder: C\n```\n\n### other\n```yaml\ntype: ref\nid: '2'\nbinder: B\n```\n")
	if n, err := mdDeleteBlock(note, "1", "B"); err != nil || n != 1 {
		t.Fatalf("delete: %d %v", n, err)
	}
	want := "### doc.pdf\n```yaml\ntype: ref\nid: '1'\nbinder: C\n```\n\n### other\n```yaml\ntype: ref\nid: '2'\nbinder: B\n```\n"
	if got := mustRead(t, note); got != want {
		t.Errorf("note:\n%s\nwant:\n%s", got, want)
	}
	// A heading of its own goes with its block, as before.
	if n, err := mdDeleteBlock(note, "2", "B"); err != nil || n != 1 {
		t.Fatalf("delete: %d %v", n, err)
	}
	if got := mustRead(t, note); strings.Contains(got, "### other") {
		t.Errorf("the block's own heading stayed:\n%s", got)
	}
}

// A Spotlight value with a line break in it is read whole.
func TestAlbumReadsAMultiLineSpotlightValue(t *testing.T) {
	raw := "kMDItemCity                = \"Okaukuejo\"\n" +
		"kMDItemDescription         = \"Am Wasserloch.\nZweite Zeile.\"\n" +
		"kMDItemTitle               = \"Sunset\"\n"
	got := albumSpotlightFields(raw)
	if got["title"] != "Sunset" {
		t.Errorf("title = %v", got["title"])
	}
	found := false
	for _, v := range got {
		if v == "Am Wasserloch.\nZweite Zeile." {
			found = true
		}
	}
	if !found {
		t.Errorf("the two-line description is not read whole: %v", got)
	}
}

// snapshot --no-files without --out does not take the day's default name,
// under which it would replace a full snapshot.
func TestSnapshotWithoutFilesHasANameOfItsOwn(t *testing.T) {
	anchor := engineBin(t)
	fx := makeSnapFixture(t, anchor)
	t.Chdir(t.TempDir())
	if so, se, code := runIn(t, fx.env, cmdSnapshot); code != 0 {
		t.Fatalf("snapshot: %d\n%s\n%s", code, so, se)
	}
	if so, se, code := runIn(t, fx.env, cmdSnapshot, "--no-files"); code != 0 {
		t.Fatalf("snapshot --no-files: %d\n%s\n%s", code, so, se)
	}
	day := time.Now().Format("2006-01-02")
	full := "register-snapshot-" + day + ".tar.gz"
	if !index.FileExists("register-snapshot-" + day + "-no-files.tar.gz") {
		t.Errorf("no snapshot under the -no-files name")
	}
	if l := fileLines(t, full)["a.txt"]; l["file"] == nil {
		t.Errorf("the full snapshot lost its files: %v", l)
	}
}

func TestOrderVersion(t *testing.T) {
	out, _, code := runGoOrder(t, orderEnv(t, t.TempDir()), "--version")
	if code != 0 || !strings.HasPrefix(out, "register order ") {
		t.Errorf("order --version: exit %d %q", code, out)
	}
}
