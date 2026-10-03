package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A hand-written block may list its binders; each is one membership.
func TestReindexSplitsAListBinder(t *testing.T) {
	notes := t.TempDir()
	col := filepath.Join(notes, "collections")
	os.MkdirAll(col, 0755)
	writeFile(t, filepath.Join(col, "binder_proj.md"),
		"### a.pdf\n```yaml\ntype: ref\nid: '800'\nbinder: [proj, other]\nfilename: a.pdf\n```\n\n"+
			"### b.pdf\n```yaml\ntype: ref\nid: '801'\nbinder: 'Müller, Hans'\nfilename: b.pdf\n```\n")

	_, errOut, _ := runGoReindex(t, cleanupEnv(t, notes))
	inbox := mustRead(t, filepath.Join(col, "inbox.jsonl"))
	if !strings.Contains(inbox, `"binder":["proj","other"]`) || strings.Contains(inbox, `[proj other]`) {
		t.Errorf("list binder:\n%s", inbox)
	}
	if strings.Contains(inbox, "801") || !strings.Contains(errOut, "contains a comma") {
		t.Errorf("a binder breaking the name rule was indexed:\n%s\nstderr: %s", inbox, errOut)
	}
}

func TestReindexNamesBlocksItCannotIndex(t *testing.T) {
	notes := t.TempDir()
	col := filepath.Join(notes, "collections")
	os.MkdirAll(col, 0755)
	writeFile(t, filepath.Join(col, "binder_proj.md"),
		"### x.pdf\n```yaml\ntype: ref\naka: [unknown-handle]\nbinder: proj\n```\n")
	out, errOut, _ := runGoReindex(t, cleanupEnv(t, notes))
	if strings.Contains(out, "Index is complete") || !strings.Contains(errOut, "unknown-handle in binder_proj.md") {
		t.Errorf("out %q\nerr %q", out, errOut)
	}
}
