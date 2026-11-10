package main

import (
	"github.com/rhsev/fileregister/v2/internal/index"

	"os"
	"path/filepath"
	"strings"
	"testing"

	uninorm "golang.org/x/text/unicode/norm"
)

// A snapshot that cannot be written leaves the one already at --out as it
// was, and a good run leaves nothing beside it.
func TestSnapshotKeepsTheOldOneWhenItFails(t *testing.T) {
	anchor := engineBin(t)
	fx := makeSnapFixture(t, anchor)
	out := filepath.Join(t.TempDir(), "snap.tar.gz")
	writeFile(t, out, "the old snapshot")

	// Something in the way of the partial file: the run cannot write.
	os.MkdirAll(out+".partial", 0755)
	writeFile(t, filepath.Join(out+".partial", "x"), "in the way")
	if _, _, code := runIn(t, fx.env, cmdSnapshot, "--out", out); code != 1 {
		t.Fatalf("snapshot with a blocked partial file: exit %d, want 1", code)
	}
	if got := mustRead(t, out); got != "the old snapshot" {
		t.Errorf("the failed run touched the old snapshot: %q", got)
	}

	os.RemoveAll(out + ".partial")
	if so, se, code := runIn(t, fx.env, cmdSnapshot, "--out", out); code != 0 {
		t.Fatalf("snapshot over an old one: exit %d\n%s\n%s", code, so, se)
	}
	if len(fileLines(t, out)) == 0 {
		t.Errorf("the new snapshot holds no file lines")
	}
	if index.FileExists(out + ".partial") {
		t.Errorf("a good run left %s.partial behind", out)
	}
}

func TestMarshalKeepsTheOldOneWhenItFails(t *testing.T) {
	anchor := engineBin(t)
	env := marshalEnv(t, makeMarshalFixture(t), anchor)
	out := filepath.Join(t.TempDir(), "ship.tar.gz")
	writeFile(t, out, "the old container")

	os.MkdirAll(out+".partial", 0755)
	writeFile(t, filepath.Join(out+".partial", "x"), "in the way")
	if _, _, code := runGoMarshal(t, env, "--binder", "Ship", "--out", out); code != 1 {
		t.Fatalf("marshal with a blocked partial file: exit %d, want 1", code)
	}
	if got := mustRead(t, out); got != "the old container" {
		t.Errorf("the failed run touched the old container: %q", got)
	}

	os.RemoveAll(out + ".partial")
	if so, se, code := runGoMarshal(t, env, "--binder", "Ship", "--out", out); code != 0 {
		t.Fatalf("marshal over an old container: exit %d\n%s\n%s", code, so, se)
	}
	dir := t.TempDir()
	untar(t, out, dir)
	if m, _ := filepath.Glob(filepath.Join(dir, "*", "*")); len(m) == 0 {
		t.Errorf("the new container is empty")
	}
	if index.FileExists(out + ".partial") {
		t.Errorf("a good run left %s.partial behind", out)
	}
}

// Two lines of a snapshot that name the same place: the first file is
// placed, the second is parked, and neither overwrites the other.
func TestRestoreParksASecondFileForTheSameTarget(t *testing.T) {
	anchor := engineBin(t)
	fx := makeSnapFixture(t, anchor)
	snap := snapshotOf(t, fx)

	// Repack with a second line for a.txt's place, carrying b.txt's content.
	dir := t.TempDir()
	untar(t, snap, dir)
	tops, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(tops) != 1 {
		t.Fatalf("container top folders: %v", tops)
	}
	manifest := filepath.Join(tops[0], "files.jsonl")
	var lines []string
	var a, b map[string]any
	for _, l := range strings.Split(strings.TrimSpace(mustRead(t, manifest)), "\n") {
		lines = append(lines, l)
		switch m := index.ParseJSONObject(l); index.AsString(m["filename"]) {
		case "a.txt":
			a = m
		case "b.txt":
			b = m
		}
	}
	if a == nil || b == nil {
		t.Fatalf("a.txt or b.txt missing from the manifest")
	}
	var kept []string
	for _, l := range lines {
		if index.AsString(index.ParseJSONObject(l)["filename"]) != "b.txt" {
			kept = append(kept, l)
		}
	}
	b["base"], b["path"] = a["base"], a["path"]
	kept = append(kept, index.JSONVal(b))
	writeFile(t, manifest, strings.Join(kept, "\n")+"\n")
	twice := filepath.Join(t.TempDir(), "twice.tar.gz")
	if err := tarCreate(twice, dir, filepath.Base(tops[0])); err != nil {
		t.Fatal(err)
	}

	home := t.TempDir()
	notes := filepath.Join(home, "Notes")
	os.MkdirAll(notes, 0755)
	into := filepath.Join(t.TempDir(), "elsewhere")
	so, se, _ := runIn(t, snapEnv(home, notes, anchor), cmdRestore, twice, "--into", into)
	if !strings.Contains(so+se, "another file of this snapshot goes to") {
		t.Errorf("the second file for the same place is not reported:\n%s\n%s", so, se)
	}
	if got := mustRead(t, filepath.Join(notes, "a.txt")); got != "Rechnung A\n" {
		t.Errorf("a.txt = %q, want the first file's content", got)
	}
}

// A registered file that is itself a note is packed as a file and as a note.
// restore places it once and counts the note as there.
func TestRestoreOfARegisteredNote(t *testing.T) {
	anchor := engineBin(t)
	fx := makeSnapFixture(t, anchor)
	idea := filepath.Join(fx.notes, "idee.md")
	ideaText := "# Idee\n\n```yaml\ntype: ref\nid: '" + fx.ids["a.txt"] + "'\nbinder: Akte\n```\n"
	writeFile(t, idea, ideaText)
	if _, se, code := runIn(t, fx.env, cmdAdd, idea, "--binder", "Akte"); code != 0 {
		t.Fatalf("add: %s", se)
	}
	snap := snapshotOf(t, fx)

	home := t.TempDir()
	notes := filepath.Join(home, "Notes")
	os.MkdirAll(notes, 0755)
	into := filepath.Join(t.TempDir(), "elsewhere")
	so, se, code := runIn(t, snapEnv(home, notes, anchor), cmdRestore, snap, "--into", into)
	if code != 0 || strings.Contains(so+se, "could not place") || strings.Contains(so+se, "different note") {
		t.Errorf("restore exit %d\n%s\n%s", code, so, se)
	}
	if got := mustRead(t, filepath.Join(notes, "idee.md")); got != ideaText {
		t.Errorf("idee.md = %q", got)
	}
}

// audit: a binder stored decomposed in the index and composed on the file is
// one binder, and the file is no ghost. The same holds for a kept tag.
func TestAuditSeesNoGhostInADecomposedBinder(t *testing.T) {
	nfc, nfd := uninorm.NFC.String("Bücher"), uninorm.NFD.String("Bücher")
	if nfc == nfd {
		t.Fatal("the two forms are the same")
	}
	file := filepath.Join(t.TempDir(), "buch.txt")
	writeFile(t, file, "x")
	records := []map[string]any{
		{"type": "ref", "id": "1", "binder": []any{nfd}},
		{"type": "ref", "id": "2", "binder": []any{"Other"}, "kept_tags": []any{nfd}},
	}
	other := filepath.Join(filepath.Dir(file), "other.txt")
	writeFile(t, other, "y")
	recordIndex := auditRecordIndex(records, map[string]string{"1": file, "2": other})
	for _, f := range []string{file, other} {
		if got := ghostReport(f, nfc, "tags", recordIndex, map[string]string{}); got != "" {
			t.Errorf("%s under the composed name: %q, want a known record", filepath.Base(f), got)
		}
	}
	if got := ghostReport(file, "Anders", "tags", recordIndex, map[string]string{}); got != "ghost" {
		t.Errorf("a file under a binder the index does not record: %q, want ghost", got)
	}
}

// rename writes the new name onto the binder's members, and a member is a
// record of the index. A note block left from a removed membership names a
// file that is not in the binder.
func TestRenameLeavesAFileNamedOnlyByANoteBlock(t *testing.T) {
	anchor := engineBin(t)
	notes := seedNotes(t)
	env := identityEnv(t, notes, anchor)
	dir := t.TempDir()
	member, former := filepath.Join(dir, "member.txt"), filepath.Join(dir, "former.txt")
	writeFile(t, member, "m")
	writeFile(t, former, "f")
	if _, e, code := runGoAdd(t, env, member, "--binder", "Old"); code != 0 {
		t.Fatalf("add member: %d %s", code, e)
	}
	if _, e, code := runGoAdd(t, env, former, "--binder", "Other"); code != 0 {
		t.Fatalf("add former: %d %s", code, e)
	}
	id := index.AsString(inboxByFilename(t, notes)["former.txt"]["id"])
	writeFile(t, filepath.Join(notes, "collections", "binder_Old.md"),
		"### former.txt\n```yaml\ntype: ref\nid: '"+id+"'\nbinder: Old\n```\n")

	if _, e, code := runGoRename(t, env, "Old", "New"); code != 0 {
		t.Fatalf("rename: %d %s", code, e)
	}
	if got := strings.Join(fileMeta(t, anchor, member, "groups"), ","); !strings.Contains(got, "New") {
		t.Errorf("the member did not get the new name: %s", got)
	}
	if got := strings.Join(fileMeta(t, anchor, former, "groups"), ","); strings.Contains(got, "New") {
		t.Errorf("a file that is not in the binder got the new name: %s", got)
	}
}

// rename --merge onto a binder the file is in already: the tag there is the
// one register set, so it is not recorded as the user's own, and leaving the
// binder takes it off again.
func TestRenameMergeDoesNotKeepRegistersOwnTag(t *testing.T) {
	anchor := engineBin(t)
	notes := seedNotes(t)
	env := identityEnv(t, notes, anchor)
	f := filepath.Join(t.TempDir(), "both.txt")
	writeFile(t, f, "x")
	for _, b := range []string{"Old", "New"} {
		if _, e, code := runGoAdd(t, env, f, "--binder", b, "--xattr", "tags"); code != 0 {
			t.Fatalf("add %s: %d %s", b, code, e)
		}
	}
	if _, e, code := runGoRename(t, env, "Old", "New", "--merge"); code != 0 {
		t.Fatalf("rename --merge: %d %s", code, e)
	}
	if rec := inboxByFilename(t, notes)["both.txt"]; len(toStrings(rec["kept_tags"])) != 0 {
		t.Errorf("register's own tag was recorded as kept: %v", rec["kept_tags"])
	}
	if _, e, code := runGoRemove(t, env, f, "--binder", "New"); code != 0 {
		t.Fatalf("remove: %d %s", code, e)
	}
	if got := strings.Join(fileTags(t, anchor, f), ","); strings.Contains(got, "New") {
		t.Errorf("the binder tag stayed after remove: %s", got)
	}
}

// add --xattr on a record that has its backend already is ignored, and the
// record says the same as before.
func TestAddIgnoredXattrStaysOutOfTheRecord(t *testing.T) {
	anchor := engineBin(t)
	notes := seedNotes(t)
	env := identityEnv(t, notes, anchor)
	f := filepath.Join(t.TempDir(), "doc.txt")
	writeFile(t, f, "x")
	if _, e, code := runGoAdd(t, env, f, "--binder", "One"); code != 0 {
		t.Fatalf("add: %d %s", code, e)
	}
	before := inboxByFilename(t, notes)["doc.txt"]["xattr"]
	if _, e, code := runGoAdd(t, env, f, "--binder", "Two", "--xattr", "tags"); code != 0 {
		t.Fatalf("second add: %d %s", code, e)
	}
	rec := inboxByFilename(t, notes)["doc.txt"]
	if rec["xattr"] != before {
		t.Errorf("xattr was %v, is %v after an ignored --xattr", before, rec["xattr"])
	}
	if got := strings.Join(fileTags(t, anchor, f), ","); strings.Contains(got, "Two") {
		t.Errorf("the ignored backend was used all the same: %s", got)
	}
}

// annotate --prose on a note that starts with a BOM: the block is found and
// the BOM stays where it was.
func TestAnnotateProseKeepsTheBOM(t *testing.T) {
	needGrubber(t)
	notes := t.TempDir()
	col := filepath.Join(notes, "collections")
	os.MkdirAll(col, 0755)
	writeFile(t, filepath.Join(col, "inbox.jsonl"),
		`{"type":"ref","id":"11","binder":["Anno"],"url":"x-devonthink-item://A","filename":"a.md"}`+"\n")
	note := filepath.Join(col, "binder_Anno.md")
	writeFile(t, note, utf8BOM+"### a\n\nOld prose.\n\n```yaml\ntype: ref\nid: '11'\nbinder: Anno\n```\n")

	if _, e, code := runGoAnnotate(t, annotateEnv(t, notes), "", "Anno", "11", "--prose", "New prose."); code != 0 {
		t.Fatalf("annotate: %d %s", code, e)
	}
	want := utf8BOM + "### a\n\nNew prose.\n\n```yaml\ntype: ref\nid: '11'\nbinder: Anno\n```\n"
	if got := mustRead(t, note); got != want {
		t.Errorf("note:\n%q\nwant\n%q", got, want)
	}
}
