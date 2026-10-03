package main

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func fileTags(t *testing.T, anchor, path string) []string {
	t.Helper()
	cmd := exec.Command(anchor)
	cmd.Stdin = strings.NewReader(`{"op":"tags","path":` + jsonString(path) + `}` + "\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("tags: %v", err)
	}
	var resp struct{ Tags []string }
	json.Unmarshal(out, &resp)
	sort.Strings(resp.Tags)
	return resp.Tags
}

func tagFile(t *testing.T, anchor, path, tag string) {
	t.Helper()
	engineExec(t, anchor, `{"op":"tag","path":`+jsonString(path)+`,"value":`+jsonString(tag)+`}`)
}

// With the tags backend a binder is a Finder tag. A tag of the same name the
// user had set before is theirs: remove and rename must leave it.
func TestTagsBackendKeepsTheUsersOwnTags(t *testing.T) {
	anchor := engineBin(t)
	notes := t.TempDir()
	env := identityEnv(t, notes, anchor)
	dir := t.TempDir()
	mine := filepath.Join(dir, "mine.txt")
	plain := filepath.Join(dir, "plain.txt")
	writeFile(t, mine, "m")
	writeFile(t, plain, "p")
	tagFile(t, anchor, mine, "Wichtig")

	for _, f := range []string{mine, plain} {
		if _, e, code := runGoAdd(t, env, f, "--binder", "Wichtig", "--xattr", "tags"); code != 0 {
			t.Fatalf("add %s: %d %s", f, code, e)
		}
	}
	recs := inboxByFilename(t, notes)
	if got := strings.Join(toStrings(recs["mine.txt"]["kept_tags"]), ","); got != "Wichtig" {
		t.Errorf("kept_tags of the file that had the tag: %q", got)
	}
	if _, ok := recs["plain.txt"]["kept_tags"]; ok {
		t.Errorf("kept_tags on a file that did not have the tag: %v", recs["plain.txt"])
	}

	for _, f := range []string{mine, plain} {
		if _, e, code := runGoRemove(t, env, f, "--binder", "Wichtig"); code != 0 {
			t.Fatalf("remove %s: %d %s", f, code, e)
		}
	}
	if got := fileTags(t, anchor, mine); strings.Join(got, ",") != "Wichtig" {
		t.Errorf("remove took the user's own tag: %v", got)
	}
	if got := fileTags(t, anchor, plain); len(got) != 0 {
		t.Errorf("remove left fileregister's tag behind: %v", got)
	}

	// rename: the old name's tag stays where it was the user's.
	if _, e, code := runGoAdd(t, env, mine, "--binder", "Wichtig", "--xattr", "tags"); code != 0 {
		t.Fatalf("re-add: %d %s", code, e)
	}
	if _, e, code := runGoRename(t, env, "Wichtig", "Dringend"); code != 0 {
		t.Fatalf("rename: %d %s", code, e)
	}
	if got := strings.Join(fileTags(t, anchor, mine), ","); got != "Dringend,Wichtig,★" {
		t.Errorf("after rename: %q", got)
	}
}

func toStrings(v any) []string {
	var out []string
	if arr, ok := v.([]any); ok {
		for _, x := range arr {
			out = append(out, x.(string))
		}
	}
	return out
}
