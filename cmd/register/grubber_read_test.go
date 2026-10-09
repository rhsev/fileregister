package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A hand-written block may carry its id unquoted. That is a YAML integer, and
// decoded from grubber's JSON as float64 it became "2.70450536e+08", matching
// no record: the member lost its fields in the album and its sort key in the
// ordering. Both read through grubber now, so both are pinned here.
func TestUnquotedIDReachesAlbumAndOrder(t *testing.T) {
	anchor := engineBin(t)
	dir := t.TempDir()
	col := filepath.Join(dir, "collections")
	if err := os.MkdirAll(col, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(col, "inbox.jsonl"),
		`{"type":"ref","id":"100000001","binder":["B"],"url":"https://example.com/a","filename":"a.md","kind":"web"}`+"\n"+
			`{"type":"ref","id":"270450536","binder":["B"],"url":"https://example.com/z","filename":"z.md","kind":"web"}`+"\n")
	writeFile(t, filepath.Join(col, "binder_B.md"),
		"### z\n```yaml\ntype: ref\nid: 270450536\nbinder: B\ntitle: Hand\nsort: a\n```\n")
	env := albumEnv(t, dir, anchor)

	so, se, code := runGoAlbum(t, env, "B")
	if code != 0 {
		t.Fatalf("album: exit %d: %s", code, se)
	}
	first := strings.SplitN(so, "\n", 2)[0]
	var line map[string]any
	if err := json.Unmarshal([]byte(first), &line); err != nil {
		t.Fatalf("album line %q: %v", first, err)
	}
	// Keyed, so first, although z.md sorts after a.md by name.
	if line["id"] != "270450536" {
		t.Errorf("first line is %v, want the keyed member 270450536", line["id"])
	}
	if f, _ := line["fields"].(map[string]any); f["title"] != "Hand" {
		t.Errorf("fields lost: %v", line["fields"])
	}

	oo, oe, ocode := runGoOrder(t, env, "show", "B", "--json")
	if ocode != 0 || !strings.HasPrefix(oo, `{"position":1,"id":"270450536"`) {
		t.Errorf("order show: exit %d, %q %s", ocode, oo, oe)
	}
}

// rename finds the binder's blocks through grubber before it changes anything.
// When grubber cannot read the notes, nothing is renamed: not the index, not
// the blocks, not the note.
func TestRenameKeepsNoteWhenGrubberFails(t *testing.T) {
	anchor := engineBin(t)
	n, h := setupRenameWithMd(t, anchor, "700000901", "Old")
	stub := filepath.Join(t.TempDir(), "grubber")
	writeFile(t, stub, "#!/bin/sh\necho 'grubber: broken on purpose' >&2\nexit 1\n")
	if err := os.Chmod(stub, 0755); err != nil {
		t.Fatal(err)
	}
	env := append(filterEnv(renameEnvHome(t, n, h, anchor), "GRUBBER_BIN"), "GRUBBER_BIN="+stub)

	before := mustRead(t, filepath.Join(n, "collections", "inbox.jsonl"))
	_, se, code := runGoRename(t, env, "Old", "New")
	if code != 1 || !strings.Contains(se, "broken on purpose") || !strings.Contains(se, "nothing renamed") {
		t.Errorf("exit %d, stderr does not say why nothing was renamed: %q", code, se)
	}
	if _, err := os.Stat(filepath.Join(n, "collections", "binder_Old.md")); err != nil {
		t.Errorf("the note was moved although its blocks could not be read")
	}
	if mustRead(t, filepath.Join(n, "collections", "inbox.jsonl")) != before {
		t.Errorf("the index changed although rename stopped")
	}
}
