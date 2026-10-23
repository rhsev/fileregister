package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every registered file carries one star: ☆ while its record is in no binder,
// ★ while it is in one. The star follows the record through add, remove,
// refresh and forget, and audit reports a missing or a wrong one.
func TestStarFollowsTheRecord(t *testing.T) {
	anchor := engineBin(t)
	notes := t.TempDir()
	os.MkdirAll(filepath.Join(notes, "collections"), 0755)
	env := forgetEnv(t, notes, anchor)
	f := filepath.Join(t.TempDir(), "f.txt")
	writeFile(t, f, "f")
	tags := func() string { return strings.Join(fileTags(t, anchor, f), ",") }

	if _, e, code := runGoAdd(t, env, f); code != 0 {
		t.Fatalf("add: %d %s", code, e)
	}
	if got := tags(); got != "☆" {
		t.Errorf("added in no binder: tags %q, want ☆", got)
	}
	if _, e, code := runGoAdd(t, env, f, "--binder", "B"); code != 0 {
		t.Fatalf("add --binder: %d %s", code, e)
	}
	if got := tags(); got != "★" {
		t.Errorf("in a binder: tags %q, want ★ alone", got)
	}
	if _, e, code := runGoAdd(t, env, f); code != 0 {
		t.Fatalf("add again: %d %s", code, e)
	}
	if got := tags(); got != "★" {
		t.Errorf("added again without --binder, still in B: tags %q, want ★", got)
	}
	if _, e, code := runGoRemove(t, env, f, "--binder", "B"); code != 0 {
		t.Fatalf("remove: %d %s", code, e)
	}
	if got := tags(); got != "☆" {
		t.Errorf("out of its last binder: tags %q, want ☆", got)
	}

	// The wrong star is reported and refresh swaps it.
	engineExec(t, anchor, `{"op":"untag","path":`+jsonString(f)+`,"value":"☆"}`)
	tagFile(t, anchor, f, "★")
	if out, _, _ := runGoAudit(t, env); !strings.Contains(out, "MISSING ID") || !strings.Contains(out, "☆") {
		t.Errorf("audit did not report the wrong star:\n%s", out)
	}
	if _, e, code := runGoRefresh(t, env); code != 0 {
		t.Fatalf("refresh: %d %s", code, e)
	}
	if got := tags(); got != "☆" {
		t.Errorf("after refresh: tags %q, want ☆", got)
	}
	if out, _, _ := runGoAudit(t, env); strings.Contains(out, "MISSING ID") {
		t.Errorf("audit still reports after refresh:\n%s", out)
	}

	id := inboxByFilename(t, notes)["f.txt"]["id"].(string)
	if _, e, code := runGoForget(t, env, id); code != 0 {
		t.Fatalf("forget: %d %s", code, e)
	}
	if got := tags(); got != "" {
		t.Errorf("forgotten: tags %q, want none", got)
	}
}
