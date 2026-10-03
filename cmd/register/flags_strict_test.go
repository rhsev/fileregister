package main

import (
	"strings"
	"testing"
)

func TestFlagsDoNotSwallowFlags(t *testing.T) {
	vf := map[string]bool{"--binder": true}
	bf := map[string]bool{"--edit": true}

	_, bools, _, unknown := parseFlagsMulti([]string{"--binder", "--edit"}, vf, bf)
	if unknown != "--binder"+missingValue || !bools["--edit"] {
		t.Errorf("--binder --edit: unknown %q, bools %v", unknown, bools)
	}
	if _, _, _, unknown := parseFlagsMulti([]string{"--binder"}, vf, bf); unknown != "--binder"+missingValue {
		t.Errorf("--binder at the end: unknown %q", unknown)
	}
	vals, _, _, unknown := parseFlagsMulti([]string{"--binder", "-draft"}, vf, bf)
	if unknown != "" || vals["--binder"][0] != "-draft" {
		t.Errorf("a value starting with one dash: %v %q", vals, unknown)
	}
	_, _, pos, unknown := parseFlagsMulti([]string{"--", "-draft", "--final"}, vf, bf)
	if unknown != "" || strings.Join(pos, " ") != "-draft --final" {
		t.Errorf("after --: pos %v, unknown %q", pos, unknown)
	}
}

func TestRenameTakesNamesAfterDoubleDash(t *testing.T) {
	anchor := engineBin(t)
	notes := seedInbox(t, `{"type":"ref","id":"1","binder":["-draft"],"url":"https://example.com/"}`)
	env := renameEnvHome(t, notes, t.TempDir(), anchor)
	if _, e, code := runGoRename(t, env, "--", "-draft", "final"); code != 0 {
		t.Fatalf("rename -- -draft final: %d %s", code, e)
	}
	if inbox := mustRead(t, notes+"/collections/inbox.jsonl"); !strings.Contains(inbox, `"final"`) {
		t.Errorf("inbox: %s", inbox)
	}
}
