package main

import (
	"github.com/rhsev/fileregister/internal/index"

	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func unmarshalEnv(t *testing.T, notes, anchor string) []string {
	t.Helper()
	env := filterEnv(os.Environ(), "GRUBBER_SET")
	for _, k := range []string{"GRUBBER_NOTES", "HOME", "FILEANCHOR"} {
		env = filterEnv(env, k)
	}
	return append(env,
		"GRUBBER_NOTES="+notes,
		"HOME="+t.TempDir(),
		"FILEANCHOR="+anchor,
	)
}

func runGoUnmarshal(t *testing.T, env []string, args ...string) (string, string, int) {
	t.Helper()
	restore := setEnv(t, env)
	defer restore()
	index.ResetEngine()

	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	code := cmdUnmarshal(args)

	index.ResetEngine()
	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr

	var ob, eb bytes.Buffer
	io.Copy(&ob, outR)
	io.Copy(&eb, errR)
	return ob.String(), eb.String(), code
}

// goMarshalToContainer produces a container from the Ship fixture via Go marshal.
func goMarshalToContainer(t *testing.T, anchor string, extraArgs ...string) string {
	t.Helper()
	fx := makeMarshalFixture(t)
	out := filepath.Join(t.TempDir(), "ship.tar.gz")
	env := marshalEnv(t, fx, anchor)
	args := append([]string{"--binder", "Ship", "--out", out}, extraArgs...)
	_, _, code := runGoMarshal(t, env, args...)
	if code != 0 {
		t.Fatalf("marshal to container failed: code=%d", code)
	}
	return out
}

// TestUnmarshal: unpack a container, then golden the recreated index / SCHEMA / notes.

func TestUnmarshal(t *testing.T) {
	anchor := engineBin(t)
	container := goMarshalToContainer(t, anchor)

	dest := t.TempDir()
	os.MkdirAll(filepath.Join(dest, "collections"), 0755)
	gOut, gErr, gCode := runGoUnmarshal(t, unmarshalEnv(t, dest, anchor), container)

	repl := map[string]string{dest: "<NOTES>"}
	assertGolden(t, "unmarshal_stdout", cliResult(norm(gOut, repl), norm(gErr, repl), gCode))
	assertGolden(t, "unmarshal_index.jsonl",
		strings.Join(manifestMultiset(t, filepath.Join(dest, "collections", "inbox.jsonl")), "\n")+"\n")
	assertGolden(t, "unmarshal_SCHEMA", mustRead(t, filepath.Join(dest, "collections", "SCHEMA")))
	assertGolden(t, "unmarshal_ship-notes.md", mustRead(t, filepath.Join(dest, "collections", "ship-notes.md")))
}

// TestUnmarshalRoundTrip: Go marshal then Go unmarshal reconstructs the original
// records (id + binder set), proving the two compose.
func TestUnmarshalRoundTrip(t *testing.T) {
	anchor := engineBin(t)
	container := goMarshalToContainer(t, anchor)

	dest := t.TempDir()
	os.MkdirAll(filepath.Join(dest, "collections"), 0755)
	_, _, code := runGoUnmarshal(t, unmarshalEnv(t, dest, anchor), container)
	if code != 0 {
		t.Fatalf("unmarshal failed: %d", code)
	}
	idx := manifestMultiset(t, filepath.Join(dest, "collections", "inbox.jsonl"))
	joined := strings.Join(idx, "\n")
	for _, want := range []string{`"id":"1"`, `"id":"2"`, `"binder":["Ship"]`, `"url":"x-devonthink-item://AAA"`} {
		if !strings.Contains(joined, want) {
			t.Errorf("round-tripped index missing %s:\n%s", want, joined)
		}
	}
}

// buildFileRefContainer hand-builds a container with one file ref + its staged
// file, exercising the placement / bookmark / xattr / ★ path.
func buildFileRefContainer(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	root := filepath.Join(tmp, "Pack")
	if err := os.MkdirAll(filepath.Join(root, "files"), 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "files", "doc.txt"), "hello world")
	writeFile(t, filepath.Join(root, "manifest.jsonl"),
		`{"type":"ref","id":"900000001","binder":["Docs"],"filename":"doc.txt","kind":"txt","file":"files/doc.txt","origin":"sub/doc.txt"}`+"\n")

	out := filepath.Join(t.TempDir(), "pack.tar.gz")
	cmd := exec.Command("tar", "--no-mac-metadata", "-czf", out, "-C", tmp, "Pack")
	if o, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build container: %v\n%s", err, o)
	}
	return out
}

// queryStampedID reads the file's kMDItemInformation id via a one-shot engine call.
func queryStampedID(t *testing.T, anchor, path string) string {
	t.Helper()
	cmd := exec.Command(anchor, "--sync-name", "com.fileregister.id#S")
	cmd.Stdin = strings.NewReader(fmt.Sprintf(`{"op":"get_meta","path":%q,"key":"id"}`+"\n", path))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("queryStampedID: %v", err)
	}
	return string(out)
}

// TestUnmarshalFileRef: the file-placement path (bookmark, xattr, ★) — golden stdout
// + recreated index, and Go actually stamps
// the placed file's id.
func TestUnmarshalFileRef(t *testing.T) {
	anchor := engineBin(t)
	container := buildFileRefContainer(t)

	dest := t.TempDir()
	os.MkdirAll(filepath.Join(dest, "collections"), 0755)
	gOut, gErr, gCode := runGoUnmarshal(t, unmarshalEnv(t, dest, anchor), container)

	repl := map[string]string{dest: "<NOTES>"}
	assertGolden(t, "unmarshal_fileref_stdout", cliResult(norm(gOut, repl), norm(gErr, repl), gCode))
	assertGolden(t, "unmarshal_fileref_index.jsonl",
		strings.Join(manifestMultiset(t, filepath.Join(dest, "collections", "inbox.jsonl")), "\n")+"\n")

	// The file was mirrored to sub/doc.txt with its content.
	assertGolden(t, "unmarshal_fileref_placed", mustRead(t, filepath.Join(dest, "collections", "sub", "doc.txt")))

	// Go actually stamped the placed file's id xattr.
	if got := queryStampedID(t, anchor, filepath.Join(dest, "collections", "sub", "doc.txt")); !strings.Contains(got, "900000001") {
		t.Errorf("placed file not stamped with id; get_meta returned: %s", got)
	}
}

// A container whose files all lie outside collections/ imports nothing without
// --scatter. Its note then waits in staging rather than landing alone, with
// blocks that point at ids the index lacks; the rerun with --scatter places
// records, file and note together and leaves nothing behind.
func TestUnmarshalNoteWaitsForItsRecords(t *testing.T) {
	anchor := engineBin(t)
	tmp := t.TempDir()
	root := filepath.Join(tmp, "Pack")
	os.MkdirAll(filepath.Join(root, "files"), 0755)
	os.MkdirAll(filepath.Join(root, "notes"), 0755)
	writeFile(t, filepath.Join(root, "files", "doc.txt"), "hello")
	writeFile(t, filepath.Join(root, "notes", "binder_Docs.md"),
		"### doc.txt\n```yaml\ntype: ref\nid: '900000002'\nbinder: Docs\n```\n")
	writeFile(t, filepath.Join(root, "manifest.jsonl"),
		`{"type":"ref","id":"900000002","binder":["Docs"],"filename":"doc.txt","kind":"txt","file":"files/doc.txt","origin":"../outside/doc.txt"}`+"\n"+
			`{"type":"note","file":"notes/binder_Docs.md","origin":"binder_Docs.md"}`+"\n")
	container := filepath.Join(t.TempDir(), "pack.tar.gz")
	if o, err := exec.Command("tar", "--no-mac-metadata", "-czf", container, "-C", tmp, "Pack").CombinedOutput(); err != nil {
		t.Fatalf("build container: %v\n%s", err, o)
	}

	base := t.TempDir()
	dest := filepath.Join(base, "notes")
	os.MkdirAll(filepath.Join(dest, "collections"), 0755)
	env := unmarshalEnv(t, dest, anchor)
	note := filepath.Join(dest, "collections", "binder_Docs.md")

	out, _, code := runGoUnmarshal(t, env, container)
	if code != 0 || !strings.Contains(out, "Imported 0 record(s)") {
		t.Fatalf("first run: exit %d\n%s", code, out)
	}
	if index.FileExists(note) {
		t.Errorf("note placed although no record was imported:\n%s", out)
	}

	out, _, code = runGoUnmarshal(t, env, container, "--scatter")
	if code != 0 || !strings.Contains(out, "Imported 1 record(s)") || !strings.Contains(out, "Placed 1 annotation note(s)") {
		t.Fatalf("rerun with --scatter: exit %d\n%s", code, out)
	}
	if !index.FileExists(note) || !index.FileExists(filepath.Join(dest, "outside", "doc.txt")) {
		t.Errorf("note or file missing after --scatter:\n%s", out)
	}
	if index.FileExists(filepath.Join(dest, "collections", "import", "pack")) {
		t.Errorf("staging left behind after a complete import:\n%s", out)
	}
}
