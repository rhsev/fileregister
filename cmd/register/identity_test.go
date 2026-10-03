package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// identityEnv keeps ONE home (one bookmark db) across add and remove calls.
func identityEnv(t *testing.T, notes, anchor string) []string {
	t.Helper()
	env := filterEnv(os.Environ(), "GRUBBER_SET")
	for _, k := range []string{"GRUBBER_NOTES", "HOME", "FILEANCHOR", "REGISTER_BINDER"} {
		env = filterEnv(env, k)
	}
	return append(env, "GRUBBER_NOTES="+notes, "HOME="+t.TempDir(), "FILEANCHOR="+anchor)
}

// fileMeta reads one fileanchor meta key: values for id/groups, value for sync.
func fileMeta(t *testing.T, anchor, path, key string) []string {
	t.Helper()
	cmd := exec.Command(anchor, "--sync-name", "com.fileregister.id#S")
	cmd.Stdin = strings.NewReader(`{"op":"get_meta","path":` + jsonString(path) + `,"key":"` + key + `"}` + "\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("get_meta %s: %v", key, err)
	}
	var resp struct {
		Value  *string  `json:"value"`
		Values []string `json:"values"`
	}
	json.Unmarshal(out, &resp)
	if resp.Value != nil {
		return []string{*resp.Value}
	}
	return resp.Values
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// inboxByFilename returns the inbox records keyed by filename.
func inboxByFilename(t *testing.T, notes string) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, line := range strings.Split(mustRead(t, filepath.Join(notes, "collections", "inbox.jsonl")), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("inbox line %q: %v", line, err)
		}
		out[rec["filename"].(string)] = rec
	}
	return out
}

func binders(rec map[string]any) string {
	var names []string
	for _, b := range rec["binder"].([]any) {
		names = append(names, b.(string))
	}
	return strings.Join(names, ",")
}

// A copy (cp, Finder's Duplicate) carries the original's id xattrs. It must
// not be taken for the registered file: removing the copy used to remove the
// original from its binder, and adding it used to add the original instead.
func TestCopyIsNotTheOriginal(t *testing.T) {
	anchor := engineBin(t)
	notes := t.TempDir()
	env := identityEnv(t, notes, anchor)
	dir := t.TempDir()
	orig := filepath.Join(dir, "a.txt")
	writeFile(t, orig, "a")

	if _, e, code := runGoAdd(t, env, orig, "--binder", "alpha"); code != 0 {
		t.Fatalf("add: %d %s", code, e)
	}
	origID := fileMeta(t, anchor, orig, "id")
	dup := filepath.Join(dir, "copy.txt")
	if out, err := exec.Command("cp", orig, dup).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v %s", err, out)
	}
	if got := fileMeta(t, anchor, dup, "id"); strings.Join(got, " ") != strings.Join(origID, " ") {
		t.Fatalf("precondition: cp should copy the id xattr, got %v want %v", got, origID)
	}

	_, errOut, _ := runGoRemove(t, env, dup, "--binder", "alpha")
	if !strings.Contains(errOut, "Not a member of binder 'alpha'") {
		t.Errorf("remove on the copy: stderr %q", errOut)
	}
	if got := binders(inboxByFilename(t, notes)["a.txt"]); got != "alpha" {
		t.Errorf("the original lost its binder through the copy: %q", got)
	}

	if _, e, code := runGoAdd(t, env, dup, "--binder", "beta"); code != 0 {
		t.Fatalf("add copy: %d %s", code, e)
	}
	recs := inboxByFilename(t, notes)
	if got := binders(recs["a.txt"]); got != "alpha" {
		t.Errorf("adding the copy changed the original's record: %q", got)
	}
	cp, ok := recs["copy.txt"]
	if !ok || binders(cp) != "beta" || cp["id"] == recs["a.txt"]["id"] {
		t.Fatalf("the copy was not registered on its own: %v", cp)
	}
	if got := fileMeta(t, anchor, dup, "id"); len(got) != 1 || got[0] != cp["id"] {
		t.Errorf("the copy still carries the original's id: %v (own id %v)", got, cp["id"])
	}
}

// iCloud Drive strips kMDItemInformation but keeps the #S copy of the id; a
// re-add must find the file's record through it, not mint a second identity.
func TestAddFindsTheIdThroughSyncCopy(t *testing.T) {
	anchor := engineBin(t)
	notes := t.TempDir()
	env := identityEnv(t, notes, anchor)
	f := filepath.Join(t.TempDir(), "d.txt")
	writeFile(t, f, "d")

	if _, e, code := runGoAdd(t, env, f, "--binder", "x"); code != 0 {
		t.Fatalf("add: %d %s", code, e)
	}
	id := fileMeta(t, anchor, f, "sync")[0]
	if out, err := exec.Command("xattr", "-d", "com.apple.metadata:kMDItemInformation", f).CombinedOutput(); err != nil {
		t.Fatalf("xattr -d: %v %s", err, out)
	}

	if _, e, code := runGoAdd(t, env, f, "--binder", "y"); code != 0 {
		t.Fatalf("re-add: %d %s", code, e)
	}
	recs := inboxByFilename(t, notes)
	if len(recs) != 1 || recs["d.txt"]["id"] != id || binders(recs["d.txt"]) != "x,y" {
		t.Errorf("re-add forked the identity: %v", recs)
	}
	if got := fileMeta(t, anchor, f, "sync"); got[0] != id {
		t.Errorf("#S was overwritten: %v, want %s", got, id)
	}
}
