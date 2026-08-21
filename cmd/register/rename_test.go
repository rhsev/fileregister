package main

import (
	"github.com/rhsev/fileregister/internal/index"

	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// renameEnvHome builds the env with an explicit HOME (so a seeded bookmarks.json
// under it is used).
func renameEnvHome(t *testing.T, notes, home, anchor string) []string {
	t.Helper()
	env := filterEnv(os.Environ(), "GRUBBER_SET")
	for _, k := range []string{"GRUBBER_NOTES", "HOME", "FILEANCHOR", "REGISTER_BINDER"} {
		env = filterEnv(env, k)
	}
	return append(env, "GRUBBER_NOTES="+notes, "HOME="+home, "FILEANCHOR="+anchor)
}

func runGoRename(t *testing.T, env []string, args ...string) (string, string, int) {
	t.Helper()
	restore := setEnv(t, env)
	defer restore()
	index.ResetEngine()

	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	code := cmdRename(args)

	index.ResetEngine()
	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr

	var ob, eb bytes.Buffer
	io.Copy(&ob, outR)
	io.Copy(&eb, errR)
	return ob.String(), eb.String(), code
}

// engineSave saves a bookmark blob for path via a one-shot engine call.
func engineSave(t *testing.T, anchor, path string) string {
	t.Helper()
	cmd := exec.Command(anchor, "--sync-name", "com.fileregister.id#S")
	cmd.Stdin = strings.NewReader(`{"op":"save","path":"` + path + `"}` + "\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("engineSave: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(out), &resp); err != nil {
		t.Fatalf("engineSave parse %q: %v", out, err)
	}
	blob, _ := resp["blob"].(string)
	if blob == "" {
		t.Fatalf("engineSave empty blob: %s", out)
	}
	return blob
}

// setupRenameCase creates a bookmarked, group-stamped "doc.txt" and an index that
// references it, all under a dedicated HOME so the bookmark resolves.
func setupRenameCase(t *testing.T, anchor, id, binder string) (notes, home, file string) {
	home = t.TempDir()
	notes = t.TempDir()
	if err := os.MkdirAll(filepath.Join(notes, "collections"), 0755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "doc.txt")
	if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(f)

	blob := engineSave(t, anchor, real)
	share := filepath.Join(home, ".local", "share")
	os.MkdirAll(share, 0755)
	dbJSON, _ := json.Marshal(map[string]string{id: blob})
	writeFile(t, filepath.Join(share, "bookmarks.json"), string(dbJSON))

	engineExec(t, anchor,
		`{"op":"set_meta","path":"`+real+`","key":"id","value":"`+id+`","mode":"add"}`,
		`{"op":"set_meta","path":"`+real+`","key":"groups","value":"`+binder+`","mode":"add"}`)

	writeFile(t, filepath.Join(notes, "collections", "inbox.jsonl"),
		`{"type":"ref","id":"`+id+`","binder":["`+binder+`"],"filename":"doc.txt"}`+"\n")
	return notes, home, real
}

func TestRename(t *testing.T) {
	anchor := engineBin(t)
	n, h, _ := setupRenameCase(t, anchor, "700000800", "Old")
	gOut, gErr, gCode := runGoRename(t, renameEnvHome(t, n, h, anchor), "Old", "New")
	assertGolden(t, "rename_full", cliResult(gOut, gErr, gCode))
	assertGolden(t, "rename_index.jsonl",
		strings.Join(manifestMultiset(t, filepath.Join(n, "collections", "inbox.jsonl")), "\n")+"\n")
}

// setupRenameWithMd is setupRenameCase plus a binder_<binder>.md annotation block,
// so rename must update both the index and the Markdown block.
func setupRenameWithMd(t *testing.T, anchor, id, binder string) (notes, home string) {
	notes, home, _ = setupRenameCase(t, anchor, id, binder)
	writeFile(t, filepath.Join(notes, "collections", "binder_"+binder+".md"),
		"### doc.txt\n```yaml\ntype: ref\nid: '"+id+"'\nbinder: "+binder+"\n```\n")
	return notes, home
}

// TestRenameWithMd: a binder carrying a Markdown annotation renames natively
// now (index block + md block + xattr); line surgery preserves the rest.
func TestRenameWithMd(t *testing.T) {
	anchor := engineBin(t)
	n, h := setupRenameWithMd(t, anchor, "700000900", "Old")
	gOut, gErr, gCode := runGoRename(t, renameEnvHome(t, n, h, anchor), "Old", "New")
	assertGolden(t, "rename_md", cliResult(gOut, gErr, gCode))
	assertGolden(t, "rename_md_index.jsonl",
		strings.Join(manifestMultiset(t, filepath.Join(n, "collections", "inbox.jsonl")), "\n")+"\n")
	// The Markdown block's binder line is rewritten (line surgery preserves the
	// rest), and the canonical note file is renamed along with the binder.
	if index.FileExists(filepath.Join(n, "collections", "binder_Old.md")) {
		t.Errorf("binder_Old.md still exists — canonical note not renamed")
	}
	assertGolden(t, "rename_binder_New.md", mustRead(t, filepath.Join(n, "collections", "binder_New.md")))
}

func TestRenameArg(t *testing.T) {
	anchor := engineBin(t)
	seed := func() string {
		d := t.TempDir()
		os.MkdirAll(filepath.Join(d, "collections"), 0755)
		writeFile(t, filepath.Join(d, "collections", "inbox.jsonl"),
			`{"type":"ref","id":"1","binder":["Real"],"filename":"a.txt"}`+"\n"+
				`{"type":"ref","id":"2","binder":["Other"],"filename":"b.txt"}`+"\n")
		return d
	}
	cases := map[string][]string{
		"no-refs":     {"Ghost", "NewGhost"},
		"same-name":   {"Same", "Same"},
		"missing-arg": {"OnlyOne"},
		"merge-guard": {"Real", "Other"}, // target exists → refuse without --merge
	}
	for label, args := range cases {
		gOut, gErr, gCode := runGoRename(t, renameEnvHome(t, seed(), t.TempDir(), anchor), args...)
		assertGolden(t, "rename_arg_"+label, cliResult(gOut, gErr, gCode))
	}
}
