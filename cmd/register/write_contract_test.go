package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLine(fields string) string { return "{" + fields + "}\n" }

func TestWriteKeepsTheIndexContract(t *testing.T) {
	anchor := engineBin(t)
	notes := t.TempDir()
	col := filepath.Join(notes, "collections")
	os.MkdirAll(col, 0755)
	writeFile(t, filepath.Join(col, "archive.jsonl"),
		`{"type":"ref","id":"500","binder":["old"],"aka":["mykey"],"filename":"x.pdf"}`+"\n")
	inbox := filepath.Join(col, "inbox.jsonl")
	env := append(writeEnv(t, notes), "FILEANCHOR="+anchor)

	out, _, code := runGoWrite(t, env,
		writeLine(`"_note_file":"`+inbox+`","type":"ref","id":"501","binder":["alpha"]`)+
			writeLine(`"_note_file":"`+inbox+`","type":"ref","id":"500","binder":"new"`)+
			writeLine(`"_note_file":"`+inbox+`","type":"ref","id":"502","binder":"b","aka":"mykey"`))
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != 1 || len(lines) != 3 {
		t.Fatalf("code %d, out:\n%s", code, out)
	}
	for i, want := range []string{"one string per line", "recorded in archive.jsonl", "aka 'mykey' already resolves"} {
		if !strings.Contains(lines[i], `"ok":false`) || !strings.Contains(lines[i], want) {
			t.Errorf("line %d: %s (want %q)", i+1, lines[i], want)
		}
	}
	if _, err := os.Stat(inbox); err == nil {
		t.Errorf("records were written despite the errors:\n%s", mustRead(t, inbox))
	}
}

func TestWriteMembershipGoesThroughTheBackend(t *testing.T) {
	anchor := engineBin(t)
	notes := t.TempDir()
	col := filepath.Join(notes, "collections")
	os.MkdirAll(col, 0755)
	inbox := filepath.Join(col, "inbox.jsonl")
	dir := t.TempDir()
	tagged, bare, noted := filepath.Join(dir, "t.txt"), filepath.Join(dir, "b.txt"), filepath.Join(dir, "n.txt")
	for _, f := range []string{tagged, bare, noted} {
		writeFile(t, f, "x")
	}
	env := append(writeEnv(t, notes), "FILEANCHOR="+anchor)
	_, _, code := runGoWrite(t, env,
		writeLine(`"_note_file":"`+inbox+`","type":"ref","id":"601","binder":"fotos","xattr":"tags","_ref_path":"`+tagged+`"`)+
			writeLine(`"_note_file":"`+inbox+`","type":"ref","id":"602","_ref_path":"`+bare+`"`)+
			writeLine(`"_note_file":"`+filepath.Join(col, "binder_x.md")+`","type":"ref","id":"603","binder":"x","_ref_path":"`+noted+`"`))
	if code != 0 {
		t.Fatalf("write: code %d", code)
	}
	if got := strings.Join(fileTags(t, anchor, tagged), ","); got != "fotos,★" {
		t.Errorf("tags backend: tags %q", got)
	}
	if got := fileMeta(t, anchor, tagged, "groups"); len(got) != 0 {
		t.Errorf("tags backend also wrote kMDItemProjects: %v", got)
	}
	for _, f := range []string{bare, noted} {
		if g, tg := fileMeta(t, anchor, f, "groups"), fileTags(t, anchor, f); len(g) != 0 || len(tg) != 0 {
			t.Errorf("%s got membership metadata: groups %v tags %v", filepath.Base(f), g, tg)
		}
	}
}

func TestWriteStampsSchemaOnlyInAnIndex(t *testing.T) {
	notes := t.TempDir()
	proj := t.TempDir()
	writeFile(t, filepath.Join(proj, "SCHEMA"), "my schema notes\n")
	target := filepath.Join(proj, "data.jsonl")
	runGoWrite(t, writeEnv(t, notes), writeLine(`"_note_file":"`+target+`","type":"ref","id":"700","binder":"b"`))
	if got := mustRead(t, filepath.Join(proj, "SCHEMA")); got != "my schema notes\n" {
		t.Errorf("SCHEMA outside collections/ was overwritten: %q", got)
	}
}
