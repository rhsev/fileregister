package main

import (
	"github.com/rhsev/fileregister/internal/index"

	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeEnv(t *testing.T, notes string) []string {
	t.Helper()
	env := filterEnv(os.Environ(), "GRUBBER_SET")
	for _, k := range []string{"GRUBBER_NOTES", "HOME", "REGISTER_BINDER"} {
		env = filterEnv(env, k)
	}
	return append(env, "GRUBBER_NOTES="+notes, "HOME="+t.TempDir())
}

func buildWriteInput(notes string) string {
	col := filepath.Join(notes, "collections")
	md := filepath.Join(col, "binder_Test.md")
	jsonl := filepath.Join(col, "extra.jsonl")
	bad := filepath.Join(col, "bad.txt")
	return strings.Join([]string{
		`{"_note_file":"` + md + `","type":"ref","id":"5","binder":"Docs","filename":"d.pdf","kind":"pdf"}`,
		`{"_note_file":"` + md + `","type":"ref","id":"5","binder":"Docs","filename":"d.pdf","kind":"pdf"}`,
		`{"_note_file":"` + jsonl + `","type":"ref","id":"6","binder":"Photos","filename":"e.jpg"}`,
		`{"_note_file":"` + md + `","type":"note"}`,
		`{"_note_file":"` + md + `","type":"ref","id":""}`,
		`{"type":"ref","id":"7"}`,
		`{"_note_file":"` + bad + `","type":"ref","id":"8","binder":"X"}`,
	}, "\n") + "\n"
}

func runGoWrite(t *testing.T, env []string, input string) (string, string, int) {
	t.Helper()
	inFile := filepath.Join(t.TempDir(), "in.jsonl")
	if err := os.WriteFile(inFile, []byte(input), 0644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(inFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	restore := setEnv(t, env)
	defer restore()
	index.ResetEngine()

	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldIn, oldOut, oldErr := os.Stdin, os.Stdout, os.Stderr
	os.Stdin, os.Stdout, os.Stderr = f, outW, errW

	code := cmdWrite(nil)

	index.ResetEngine()
	outW.Close()
	errW.Close()
	os.Stdin, os.Stdout, os.Stderr = oldIn, oldOut, oldErr

	var ob, eb bytes.Buffer
	io.Copy(&ob, outR)
	io.Copy(&eb, errR)
	return ob.String(), eb.String(), code
}

func TestWrite(t *testing.T) {
	n := t.TempDir()
	os.MkdirAll(filepath.Join(n, "collections"), 0755)
	gOut, gErr, gCode := runGoWrite(t, writeEnv(t, n), buildWriteInput(n))

	repl := map[string]string{n: "<N>"}
	assertGolden(t, "write_status", cliResult(norm(gOut, repl), norm(gErr, repl), gCode))
	assertGolden(t, "write_binder_Test.md", mustRead(t, filepath.Join(n, "collections", "binder_Test.md")))
	assertGolden(t, "write_extra.jsonl",
		strings.Join(manifestMultiset(t, filepath.Join(n, "collections", "extra.jsonl")), "\n")+"\n")
}

// Labels are annotation and live in the note's block; the index knows identity
// and membership. A line that still carries tags is written without them, and
// write says so once instead of dropping them silently.
func TestWriteIgnoresTags(t *testing.T) {
	n := t.TempDir()
	col := filepath.Join(n, "collections")
	os.MkdirAll(col, 0755)
	jsonl := filepath.Join(col, "inbox.jsonl")
	md := filepath.Join(col, "binder_Docs.md")
	input := `{"_note_file":"` + jsonl + `","type":"ref","id":"5","binder":"Docs","filename":"d.pdf","tags":["draft","v2"]}` + "\n" +
		`{"_note_file":"` + md + `","type":"ref","id":"5","binder":"Docs","filename":"d.pdf","tags":"draft"}` + "\n"

	_, se, code := runGoWrite(t, writeEnv(t, n), input)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, se)
	}
	if !strings.Contains(se, "2 line(s) carry tags") {
		t.Errorf("no word about the dropped tags: %q", se)
	}
	for _, f := range []string{jsonl, md} {
		if got := mustRead(t, f); strings.Contains(got, "tags") || strings.Contains(got, "draft") {
			t.Errorf("%s still carries tags:\n%s", filepath.Base(f), got)
		}
	}
}
