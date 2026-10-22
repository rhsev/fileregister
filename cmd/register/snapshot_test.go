package main

import (
	"github.com/rhsev/fileregister/v2/internal/index"

	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// snapEnv is a sandbox: its own HOME (the home base and the bookmark store)
// and notes folder.
func snapEnv(home, notes, anchor string) []string {
	env := filterEnv(os.Environ(), "GRUBBER_SET")
	for _, k := range []string{"GRUBBER_NOTES", "HOME", "FILEANCHOR", "REGISTER_BINDER"} {
		env = filterEnv(env, k)
	}
	return append(env, "GRUBBER_NOTES="+notes, "HOME="+home, "FILEANCHOR="+anchor)
}

// runIn runs one command in-process in env and returns stdout, stderr, code.
func runIn(t *testing.T, env []string, cmd func([]string) int, args ...string) (string, string, int) {
	t.Helper()
	restore := setEnv(t, env)
	defer restore()
	index.ResetEngine()
	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	var ob, eb bytes.Buffer
	done := make(chan bool, 2)
	go func() { io.Copy(&ob, outR); done <- true }()
	go func() { io.Copy(&eb, errR); done <- true }()
	code := cmd(args)
	index.ResetEngine()
	outW.Close()
	errW.Close()
	<-done
	<-done
	os.Stdout, os.Stderr = oldOut, oldErr
	return ob.String(), eb.String(), code
}

// put writes a file, making its folders.
func put(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0755)
	writeFile(t, path, content)
}

// engineCall sends one request to a fresh engine and returns its answer.
func engineCall(t *testing.T, anchor string, req map[string]any) map[string]any {
	t.Helper()
	line, _ := json.Marshal(req)
	cmd := exec.Command(anchor, "--sync-name", "com.fileregister.id#S")
	cmd.Stdin = bytes.NewReader(append(line, '\n'))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("engine %v: %v", req, err)
	}
	var resp map[string]any
	json.Unmarshal(out, &resp)
	return resp
}

// writeComment sets a Finder comment without asking Finder: on macOS the
// xattr directly (a binary-plist string), elsewhere through the engine.
func writeComment(t *testing.T, anchor, path, comment string) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		engineCall(t, anchor, map[string]any{"op": "set_meta", "path": path, "key": "comment", "value": comment, "mode": "set"})
		return
	}
	dir := t.TempDir()
	xml := filepath.Join(dir, "c.plist")
	writeFile(t, xml, `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><string>`+comment+`</string></plist>`)
	bin := filepath.Join(dir, "c.bin")
	if out, err := exec.Command("plutil", "-convert", "binary1", "-o", bin, xml).CombinedOutput(); err != nil {
		t.Fatalf("plutil: %v %s", err, out)
	}
	data, _ := os.ReadFile(bin)
	if out, err := exec.Command("xattr", "-wx", "com.apple.metadata:kMDItemFinderComment", hex.EncodeToString(data), path).CombinedOutput(); err != nil {
		t.Fatalf("xattr: %v %s", err, out)
	}
}

func engineTags(t *testing.T, anchor, path string) []string {
	t.Helper()
	resp := engineCall(t, anchor, map[string]any{"op": "tags", "path": path})
	var out []string
	for _, v := range resp["tags"].([]any) {
		out = append(out, v.(string))
	}
	sort.Strings(out)
	return out
}

func fileComment(t *testing.T, anchor, path string) string {
	t.Helper()
	return index.AsString(engineCall(t, anchor, map[string]any{"op": "get_meta", "path": path, "key": "comment"})["value"])
}

// watchComments replaces the comment writer for one test and records what
// would be written, so no test sends Finder an Apple Event.
func watchComments(t *testing.T) map[string]string {
	t.Helper()
	got := map[string]string{}
	old := setFileComment
	setFileComment = func(path, comment string) error {
		got[filepath.Base(path)] = comment
		return nil
	}
	t.Cleanup(func() { setFileComment = old })
	return got
}

// snapFixture is a register in a sandbox home: two files in the notes folder,
// one in ~/Ablage/Steuer, one in no binder, a URL record and a binder note.
type snapFixture struct {
	home, notes string
	env         []string
	ids         map[string]string // file name → id
}

func makeSnapFixture(t *testing.T, anchor string) snapFixture {
	t.Helper()
	needGrubber(t)
	home := t.TempDir()
	notes := filepath.Join(home, "Notes")
	os.MkdirAll(filepath.Join(notes, "collections"), 0755)
	os.MkdirAll(filepath.Join(home, "Ablage", "Steuer"), 0755)
	fx := snapFixture{home: home, notes: notes, env: snapEnv(home, notes, anchor), ids: map[string]string{}}
	writeFile(t, filepath.Join(notes, "a.txt"), "Rechnung A\n")
	writeFile(t, filepath.Join(notes, "b.txt"), "Beleg B\n")
	writeFile(t, filepath.Join(home, "Ablage", "Steuer", "vertrag.pdf"), "Vertrag\n")
	writeFile(t, filepath.Join(home, "Ablage", "lose.txt"), "lose\n")
	if _, se, code := runIn(t, fx.env, cmdAdd, filepath.Join(notes, "a.txt"), filepath.Join(notes, "b.txt"),
		filepath.Join(home, "Ablage", "Steuer", "vertrag.pdf"), "--binder", "Akte"); code != 0 {
		t.Fatalf("add: %s", se)
	}
	if _, se, code := runIn(t, fx.env, cmdAdd, filepath.Join(home, "Ablage", "lose.txt")); code != 0 {
		t.Fatalf("add loose: %s", se)
	}
	if _, se, code := runIn(t, fx.env, cmdAdd, "--url", "x-devonthink-item://ABC", "--binder", "Akte", "--label", "DT"); code != 0 {
		t.Fatalf("add url: %s", se)
	}
	if _, se, code := runIn(t, fx.env, cmdPromote, "--binder", "Akte"); code != 0 {
		t.Fatalf("promote: %s", se)
	}
	for name, rec := range inboxByFilename(t, notes) {
		fx.ids[name] = index.AsString(rec["id"])
	}
	engineCall(t, anchor, map[string]any{"op": "tag", "path": filepath.Join(home, "Ablage", "Steuer", "vertrag.pdf"), "value": "Steuer 2026"})
	writeComment(t, anchor, filepath.Join(home, "Ablage", "Steuer", "vertrag.pdf"), "Original im Ordner")
	return fx
}

// snapshotOf runs snapshot in fx and returns the container.
func snapshotOf(t *testing.T, fx snapFixture, args ...string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "snap.tar.gz")
	so, se, code := runIn(t, fx.env, cmdSnapshot, append([]string{"--out", out}, args...)...)
	if code != 0 {
		t.Fatalf("snapshot exit %d\n%s\n%s", code, so, se)
	}
	return out
}

// containerFile reads one file below the top folder of a tar.gz.
func containerFile(t *testing.T, tgz, name string) string {
	t.Helper()
	dir := t.TempDir()
	untar(t, tgz, dir)
	matches, _ := filepath.Glob(filepath.Join(dir, "*", name))
	if len(matches) != 1 {
		t.Fatalf("%s: %d matches for %s", tgz, len(matches), name)
	}
	return mustRead(t, matches[0])
}

// fileLines maps files.jsonl lines by filename.
func fileLines(t *testing.T, tgz string) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, l := range strings.Split(strings.TrimSpace(containerFile(t, tgz, "files.jsonl")), "\n") {
		m := index.ParseJSONObject(l)
		out[index.AsString(m["filename"])] = m
	}
	return out
}

func TestSnapshotRecordsBasesAndFacts(t *testing.T) {
	anchor := engineBin(t)
	fx := makeSnapFixture(t, anchor)
	snap := snapshotOf(t, fx)

	lines := fileLines(t, snap)
	if len(lines) != 4 {
		t.Fatalf("want 4 file lines (URL records have none), got %d: %v", len(lines), lines)
	}
	want := map[string][2]string{
		"a.txt":       {"notes", "a.txt"},
		"vertrag.pdf": {"home", "Ablage/Steuer/vertrag.pdf"},
		"lose.txt":    {"home", "Ablage/lose.txt"},
	}
	for name, bp := range want {
		l := lines[name]
		if l["base"] != bp[0] || l["path"] != bp[1] {
			t.Errorf("%s: base/path = %v/%v, want %v", name, l["base"], l["path"], bp)
		}
		if l["file"] == nil || index.AsString(l["sha256"]) == "" || index.AsString(l["size"]) == "" {
			t.Errorf("%s: missing content, size or sha256: %v", name, l)
		}
	}
	v := lines["vertrag.pdf"]
	if index.AsString(v["finder_comment"]) != "Original im Ordner" || index.AsString(v["finder_tags"]) != `["Steuer 2026"]` {
		t.Errorf("vertrag.pdf: finder fields = %v / %v (★ and binder names are not free tags)", v["finder_tags"], v["finder_comment"])
	}

	// The index travels byte for byte, the note by its path.
	if got, want := containerFile(t, snap, "index/inbox.jsonl"), mustRead(t, filepath.Join(fx.notes, "collections", "inbox.jsonl")); got != want {
		t.Errorf("index changed in the snapshot:\n%s\nwant\n%s", got, want)
	}
	containerFile(t, snap, "notes/collections/binder_Akte.md")

	var head map[string]any
	json.Unmarshal([]byte(containerFile(t, snap, "snapshot.json")), &head)
	bases, _ := head["bases"].(map[string]any)
	if bases["home"] == nil || bases["notes"] == nil || head["system"] != runtime.GOOS || head["files"] != true {
		t.Errorf("snapshot.json head: %v", head)
	}

	// --no-files keeps size and sha256 but leaves the content out;
	// --no-finder leaves tags and comment out.
	bare := fileLines(t, snapshotOf(t, fx, "--no-files", "--no-finder"))
	if l := bare["vertrag.pdf"]; l["file"] != nil || l["sha256"] == nil || l["finder_tags"] != nil || l["finder_comment"] != nil {
		t.Errorf("--no-files --no-finder line: %v", l)
	}
}

func TestSnapshotRestoreIntoAnotherRoot(t *testing.T) {
	anchor := engineBin(t)
	fx := makeSnapFixture(t, anchor)
	snap := snapshotOf(t, fx)
	comments := watchComments(t)

	home := t.TempDir()
	notes := filepath.Join(home, "Notes")
	os.MkdirAll(notes, 0755)
	into := filepath.Join(t.TempDir(), "elsewhere")
	env := snapEnv(home, notes, anchor)
	so, se, code := runIn(t, env, cmdRestore, snap, "--into", into)
	if code != 0 {
		t.Fatalf("restore exit %d\n%s\n%s", code, so, se)
	}
	if !strings.Contains(so, "created          : 4") {
		t.Errorf("restore report:\n%s", so)
	}

	// The index comes back unchanged, the note at its path.
	if got, want := mustRead(t, filepath.Join(notes, "collections", "inbox.jsonl")), mustRead(t, filepath.Join(fx.notes, "collections", "inbox.jsonl")); got != want {
		t.Errorf("restored index differs:\n%s\nwant\n%s", got, want)
	}
	mustRead(t, filepath.Join(notes, "collections", "binder_Akte.md"))

	// notes base into the notes folder, home below --into.
	vertrag := filepath.Join(into, "Ablage", "Steuer", "vertrag.pdf")
	for p, content := range map[string]string{
		filepath.Join(notes, "a.txt"): "Rechnung A\n",
		vertrag:                       "Vertrag\n",
		filepath.Join(into, "Ablage", "lose.txt"): "lose\n",
	} {
		if got := mustRead(t, p); got != content {
			t.Errorf("%s = %q, want %q", p, got, content)
		}
	}
	if got := queryStampedID(t, anchor, vertrag); !strings.Contains(got, fx.ids["vertrag.pdf"]) {
		t.Errorf("vertrag.pdf not stamped with its id %s: %s", fx.ids["vertrag.pdf"], got)
	}
	if got := strings.Join(engineTags(t, anchor, vertrag), ","); got != "Steuer 2026,★" {
		t.Errorf("vertrag.pdf tags = %s, want the free tag and ★", got)
	}
	if got := strings.Join(engineTags(t, anchor, filepath.Join(into, "Ablage", "lose.txt")), ","); got != "" {
		t.Errorf("a record in no binder got tags: %s", got)
	}
	if comments["vertrag.pdf"] != "Original im Ordner" || len(comments) != 1 {
		t.Errorf("comment writes = %v", comments)
	}

	// The restored register resolves every file record.
	if so, se, code := runIn(t, env, cmdAudit); code != 0 {
		t.Errorf("audit after restore exit %d\n%s\n%s", code, so, se)
	}

	// A second run of the same snapshot finds everything in place.
	if so, se, code := runIn(t, env, cmdRestore, snap, "--into", into); code != 0 ||
		!strings.Contains(so, "already restored : 4") || strings.Contains(so, "created") {
		t.Errorf("second restore: exit %d\n%s\n%s", code, so, se)
	}
}

func TestRestoreFromFileBackup(t *testing.T) {
	anchor := engineBin(t)
	fx := makeSnapFixture(t, anchor)
	snap := snapshotOf(t, fx, "--no-files")
	comments := watchComments(t)

	// A file backup without attributes: a.txt in place, b.txt moved, vertrag.pdf
	// renamed and given its own comment, lose.txt changed, nothing else.
	home := t.TempDir()
	notes := filepath.Join(home, "Notes")
	put(t, filepath.Join(notes, "a.txt"), "Rechnung A\n")
	put(t, filepath.Join(notes, "Archiv", "b.txt"), "Beleg B\n")
	renamed := filepath.Join(home, "Ablage", "Steuer", "vertrag-2026.pdf")
	put(t, renamed, "Vertrag\n")
	writeComment(t, anchor, renamed, "eigener Kommentar")
	put(t, filepath.Join(home, "Ablage", "lose.txt"), "lose, geändert\n")

	env := snapEnv(home, notes, anchor)
	so, se, code := runIn(t, env, cmdRestore, snap)
	if code != 0 {
		t.Fatalf("restore exit %d\n%s\n%s", code, so, se)
	}
	for _, want := range []string{
		"taken over       : 1",
		"found moved      : 2",
		"b.txt → " + filepath.Join(notes, "Archiv", "b.txt"),
		"vertrag.pdf → " + renamed,
		"changed          : 1",
		"lose.txt → " + filepath.Join(home, "Ablage", "lose.txt") + " (content differs",
		`keeps its own comment "eigener Kommentar"`,
	} {
		if !strings.Contains(so, want) {
			t.Errorf("report lacks %q:\n%s", want, so)
		}
	}
	if len(comments) != 0 {
		t.Errorf("a file with its own comment got another: %v", comments)
	}
	if !strings.Contains(so, "1 file(s) were found under another name; run register refresh") {
		t.Errorf("no refresh hint for the renamed file:\n%s", so)
	}

	// refresh writes the new name; a second restore still takes the index
	// as the snapshot's.
	if _, se, code := runIn(t, env, cmdRefresh); code != 0 {
		t.Fatalf("refresh: %s", se)
	}
	if got := index.AsString(inboxByFilename(t, notes)["vertrag-2026.pdf"]["id"]); got != fx.ids["vertrag.pdf"] {
		t.Errorf("index has no vertrag-2026.pdf for id %s after refresh", fx.ids["vertrag.pdf"])
	}
	if so, se, code := runIn(t, env, cmdRestore, snap); code != 0 || !strings.Contains(so, "already restored : 4") {
		t.Errorf("restore after refresh: exit %d\n%s\n%s", code, so, se)
	}
	if got := strings.Join(engineTags(t, anchor, renamed), ","); got != "Steuer 2026,★" {
		t.Errorf("found file tags = %s", got)
	}
	if so, se, code := runIn(t, env, cmdAudit); code != 0 {
		t.Errorf("audit after restore exit %d\n%s\n%s", code, so, se)
	}
}

func TestRestoreMissingAndAmbiguous(t *testing.T) {
	anchor := engineBin(t)
	fx := makeSnapFixture(t, anchor)
	snap := snapshotOf(t, fx, "--no-files")
	watchComments(t)

	// b.txt twice elsewhere, the rest gone.
	home := t.TempDir()
	notes := filepath.Join(home, "Notes")
	put(t, filepath.Join(notes, "A", "b.txt"), "Beleg B\n")
	put(t, filepath.Join(notes, "B", "kopie.txt"), "Beleg B\n")

	so, _, code := runIn(t, snapEnv(home, notes, anchor), cmdRestore, snap)
	if code != 1 {
		t.Errorf("restore with missing files: exit %d, want 1", code)
	}
	for _, want := range []string{"without file     : 3", "parked           : 1", "2 files match its size and checksum"} {
		if !strings.Contains(so, want) {
			t.Errorf("report lacks %q:\n%s", want, so)
		}
	}
	// The records are in the index all the same.
	if n := len(inboxByFilename(t, notes)); n != 5 {
		t.Errorf("index has %d records, want 5", n)
	}
}

func TestRestoreGoesOnAfterAnEarlierRun(t *testing.T) {
	anchor := engineBin(t)
	fx := makeSnapFixture(t, anchor)
	snap := snapshotOf(t, fx, "--no-files")
	watchComments(t)

	// First run: only a.txt is there.
	home := t.TempDir()
	notes := filepath.Join(home, "Notes")
	put(t, filepath.Join(notes, "a.txt"), "Rechnung A\n")
	env := snapEnv(home, notes, anchor)
	if so, _, code := runIn(t, env, cmdRestore, snap); code != 1 || !strings.Contains(so, "without file     : 3") {
		t.Fatalf("first run: exit %d\n%s", code, so)
	}

	// b.txt turns up; the second run binds it and leaves a.txt alone.
	put(t, filepath.Join(notes, "b.txt"), "Beleg B\n")
	so, se, code := runIn(t, env, cmdRestore, snap)
	for _, want := range []string{"already restored : 1", "taken over       : 1", "b.txt → " + filepath.Join(notes, "b.txt"), "without file     : 2"} {
		if !strings.Contains(so, want) {
			t.Errorf("second run lacks %q (exit %d):\n%s\n%s", want, code, so, se)
		}
	}
	if !strings.Contains(se, "going on with the files an earlier restore left") {
		t.Errorf("second run does not say it goes on:\n%s", se)
	}
	if got := queryStampedID(t, anchor, filepath.Join(notes, "b.txt")); !strings.Contains(got, fx.ids["b.txt"]) {
		t.Errorf("b.txt not stamped with %s: %s", fx.ids["b.txt"], got)
	}

	// An index that changed since is not the snapshot's any more.
	f, _ := os.OpenFile(filepath.Join(notes, "collections", "inbox.jsonl"), os.O_APPEND|os.O_WRONLY, 0644)
	f.WriteString(`{"type":"ref","id":"1","binder":[],"url":"https://example.org"}` + "\n")
	f.Close()
	if _, se, code := runIn(t, env, cmdRestore, snap); code != 1 || !strings.Contains(se, "differ from the snapshot") {
		t.Errorf("changed index: exit %d, %s", code, se)
	}
}

func TestRestoreDryRunWritesNothing(t *testing.T) {
	anchor := engineBin(t)
	fx := makeSnapFixture(t, anchor)
	snap := snapshotOf(t, fx)

	home := t.TempDir()
	notes := filepath.Join(home, "Notes")
	os.MkdirAll(notes, 0755)
	so, se, code := runIn(t, snapEnv(home, notes, anchor), cmdRestore, snap, "--dry-run")
	if code != 0 || !strings.Contains(so, "Would restore") || !strings.Contains(so, "created          : 4") {
		t.Errorf("dry run exit %d\n%s\n%s", code, so, se)
	}
	entries, _ := os.ReadDir(notes)
	if len(entries) != 0 {
		t.Errorf("dry run wrote into the notes folder: %v", entries)
	}
	if _, err := os.Stat(filepath.Join(home, "Ablage")); err == nil {
		t.Errorf("dry run placed files in the home folder")
	}
}

func TestSnapshotNamesAFileItCannotReach(t *testing.T) {
	anchor := engineBin(t)
	fx := makeSnapFixture(t, anchor)
	os.Remove(filepath.Join(fx.home, "Ablage", "lose.txt"))

	out := filepath.Join(t.TempDir(), "snap.tar.gz")
	so, _, code := runIn(t, fx.env, cmdSnapshot, "--out", out)
	if code != 1 || !strings.Contains(so, "files not reached (recorded without content): 1") || !strings.Contains(so, "lose.txt") {
		t.Errorf("snapshot with a gone file: exit %d\n%s", code, so)
	}
	l := fileLines(t, out)["lose.txt"]
	if l["file"] != nil || l["sha256"] != nil || l["base"] != "home" || l["path"] != "Ablage/lose.txt" {
		t.Errorf("line of the gone file: %v", l)
	}
}

func TestUnmarshalPlacesBasesAndTakesOver(t *testing.T) {
	anchor := engineBin(t)
	fx := makeSnapFixture(t, anchor)
	watchComments(t)
	container := filepath.Join(t.TempDir(), "akte.tar.gz")
	if so, se, code := runIn(t, fx.env, cmdMarshal, "--binder", "Akte", "--out", container); code != 0 {
		t.Fatalf("marshal exit %d\n%s\n%s", code, so, se)
	}
	data := containerFile(t, container, "manifest.jsonl")
	if !strings.Contains(data, `"type":"head"`) || !strings.Contains(data, `"base":"home"`) || !strings.Contains(data, `"finder_tags":["Steuer 2026"]`) {
		t.Errorf("manifest lacks head, base or Finder fields:\n%s", data)
	}

	// Without --into or --map only the notes folder is written.
	home := t.TempDir()
	notes := filepath.Join(home, "Notes")
	os.MkdirAll(notes, 0755)
	so, _, code := runIn(t, snapEnv(home, notes, anchor), cmdUnmarshal, container)
	if code != 0 || !strings.Contains(so, "vertrag.pdf — outside the notes folder; use --into") {
		t.Errorf("unmarshal without --into: exit %d\n%s", code, so)
	}
	if _, err := os.Stat(filepath.Join(notes, "a.txt")); err != nil {
		t.Errorf("a.txt (notes base) not placed: %v", err)
	}

	// With --into the home base lands below the folder; a file already there
	// with the same content is taken over.
	home2 := t.TempDir()
	notes2 := filepath.Join(home2, "Notes")
	os.MkdirAll(notes2, 0755)
	into := filepath.Join(t.TempDir(), "neu")
	vertrag := filepath.Join(into, "Ablage", "Steuer", "vertrag.pdf")
	put(t, vertrag, "Vertrag\n")
	so, se, code := runIn(t, snapEnv(home2, notes2, anchor), cmdUnmarshal, container, "--into", into)
	if code != 0 || !strings.Contains(so, "Took over 1 file(s)") || strings.Contains(so, "Parked") {
		t.Errorf("unmarshal --into: exit %d\n%s\n%s", code, so, se)
	}
	if got := strings.Join(engineTags(t, anchor, vertrag), ","); got != "Steuer 2026,★" {
		t.Errorf("taken-over file tags = %s", got)
	}

	// A different file at the target is parked, not overwritten.
	home3 := t.TempDir()
	notes3 := filepath.Join(home3, "Notes")
	put(t, filepath.Join(notes3, "a.txt"), "something else\n")
	so, _, _ = runIn(t, snapEnv(home3, notes3, anchor), cmdUnmarshal, container)
	if !strings.Contains(so, "a.txt — a different file exists at") || mustRead(t, filepath.Join(notes3, "a.txt")) != "something else\n" {
		t.Errorf("different file at target:\n%s", so)
	}

	// The same content, but registered here under its own id: taking it over
	// would give the file two identities.
	home4 := t.TempDir()
	notes4 := filepath.Join(home4, "Notes")
	put(t, filepath.Join(notes4, "a.txt"), "Rechnung A\n")
	env4 := snapEnv(home4, notes4, anchor)
	if _, se, code := runIn(t, env4, cmdAdd, filepath.Join(notes4, "a.txt")); code != 0 {
		t.Fatalf("add: %s", se)
	}
	so, _, _ = runIn(t, env4, cmdUnmarshal, container)
	if !strings.Contains(so, "is already registered here as id") {
		t.Errorf("file registered under another id was taken over:\n%s", so)
	}
}

func TestPlacementRoots(t *testing.T) {
	notes, home, into := "/n", "/h", "/x"
	pl := &placement{notesDir: notes, home: home, defaults: true, srcSystem: "plan9",
		srcRoots: map[string]string{"volume:Gone": "/no/such/volume"}}
	cases := []struct {
		base, want, why string
	}{
		{"notes", "/n", ""},
		{"home", "/h", ""},
		{"system", "", "not guessed"},
		{"volume:Gone", "", "volume:Gone is not here"},
		{"volume:..", "", "unknown base"},
	}
	for _, c := range cases {
		got, why := pl.root(c.base)
		if got != c.want || !strings.Contains(why, c.why) {
			t.Errorf("defaults root(%s) = %q, %q", c.base, got, why)
		}
	}
	pl.into = into
	pl.maps = map[string]string{"home": "/m"}
	for base, want := range map[string]string{"home": "/m", "system": "/x/system", "volume:Gone": "/x/Gone", "notes": "/n"} {
		if got, why := pl.root(base); got != want || why != "" {
			t.Errorf("into root(%s) = %q, %q; want %q", base, got, why, want)
		}
	}
	if _, why := pl.target("home", "../../etc/passwd"); !strings.Contains(why, "escapes") {
		t.Errorf("a path leaving its base: %q", why)
	}
	if _, err := parseMaps([]string{"notes=/tmp"}); err == nil {
		t.Errorf("--map notes= accepted")
	}
	// Without defaults (unmarshal, no --scatter) an unmapped base is not guessed.
	strict := &placement{notesDir: notes, home: home}
	if _, why := strict.root("home"); !strings.Contains(why, "outside the notes folder") {
		t.Errorf("strict root(home): %q", why)
	}
}

func TestClassifyBases(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	notes := filepath.Join(home, "Notes")
	os.MkdirAll(notes, 0755)
	c := newBaseClassifier(notes)
	real := realOrSelf(home)
	for p, want := range map[string][2]string{
		filepath.Join(notes, "x", "a.txt"): {"notes", "x/a.txt"},
		filepath.Join(home, "Ablage", "b"): {"home", "Ablage/b"},
		filepath.Join(real, "gone", "c"):   {"home", "gone/c"},
	} {
		if b, r := c.classify(p); b != want[0] || r != want[1] {
			t.Errorf("classify(%s) = %s %s, want %v", p, b, r, want)
		}
	}
	if b, _ := c.classify("/usr/share/x"); b != "system" && !strings.HasPrefix(b, "volume:") {
		t.Errorf("classify(/usr/share/x) = %s", b)
	}
}

func TestRefreshWritesTheNewFileName(t *testing.T) {
	anchor := engineBin(t)
	fx := makeSnapFixture(t, anchor)
	old := filepath.Join(fx.home, "Ablage", "Steuer", "vertrag.pdf")
	now := filepath.Join(fx.home, "Ablage", "Steuer", "Vertrag 2026.pdf")
	if err := os.Rename(old, now); err != nil {
		t.Fatal(err)
	}

	// audit names it and does not fail on it.
	so, se, code := runIn(t, fx.env, cmdAudit)
	if code != 0 || !strings.Contains(so, "RENAMED (1)") || !strings.Contains(so, "vertrag.pdf ("+fx.ids["vertrag.pdf"]+") → Vertrag 2026.pdf") {
		t.Errorf("audit after a rename: exit %d\n%s\n%s", code, so, se)
	}

	// --dry-run tells, the real run writes.
	if so, _, _ := runIn(t, fx.env, cmdRefresh, "--dry-run"); !strings.Contains(so, "[dry-run] file name in the index: vertrag.pdf → Vertrag 2026.pdf") {
		t.Errorf("refresh --dry-run:\n%s", so)
	}
	if _, ok := inboxByFilename(t, fx.notes)["vertrag.pdf"]; !ok {
		t.Errorf("refresh --dry-run changed the index")
	}
	so, se, code = runIn(t, fx.env, cmdRefresh)
	if code != 0 || !strings.Contains(so, "File names updated in the index: 1") {
		t.Errorf("refresh: exit %d\n%s\n%s", code, so, se)
	}
	recs := inboxByFilename(t, fx.notes)
	if index.AsString(recs["Vertrag 2026.pdf"]["id"]) != fx.ids["vertrag.pdf"] {
		t.Errorf("index after refresh: %v", recs)
	}
	if so, _, _ := runIn(t, fx.env, cmdAudit); strings.Contains(so, "RENAMED") {
		t.Errorf("audit still reports the rename:\n%s", so)
	}
}
