package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rhsev/fileregister/v2/internal/index"
)

// goneBookmark registers a file and removes it, leaving a bookmark that does
// not resolve, a recorded path and a record that claims it.
func goneBookmark(t *testing.T) (db map[string]string, records []map[string]any, id string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "gone.pdf")
	writeFile(t, file, "x")
	res, err := index.AddMany([]string{file}, nil, nil)
	if err != nil || len(res) != 1 || res[0].ID == "" {
		t.Fatalf("AddMany: %v %v", res, err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	db, err = index.LoadDB()
	if err != nil {
		t.Fatal(err)
	}
	id = res[0].ID
	return db, []map[string]any{{"type": "ref", "id": id, "filename": "gone.pdf"}}, id
}

// A bookmark that does not resolve is dead only where the engine says its
// search covers the place the file was. Where it says no, and where it cannot
// say because it predates the op, the verdict is withheld.
func TestDeadNeedsTheEngineToCoverThePlace(t *testing.T) {
	real := engineBin(t)
	cases := []struct {
		name, reply, wantKind, wantHint string
		noCoverage                      bool
	}{
		{"covered", `{"ok":true,"searchable":true}`, bmDead, "nothing to repair", false},
		{"not covered", `{"ok":true,"searchable":false}`, bmUnreachable, noIndexHint, false},
		{"engine predates the op", `{"ok":false,"error":"unknown op: searchable"}`, bmUnreachable, "update it", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			restore := setEnv(t, identityEnv(t, t.TempDir(), stubSearchable(t, real, c.reply)))
			defer restore()
			index.ResetEngine()
			defer index.ResetEngine()

			db, records, id := goneBookmark(t)
			rep, err := classifyBookmarks(db, records)
			if err != nil {
				t.Fatal(err)
			}
			if rep.blind {
				t.Fatal("the engine reported no last_path, so this test cannot reach the coverage question")
			}
			var got *bookmarkFinding
			for i := range rep.findings {
				if rep.findings[i].id == id {
					got = &rep.findings[i]
				}
			}
			if got == nil {
				t.Fatalf("no finding for %s: %+v", id, rep.findings)
			}
			if got.kind != c.wantKind || !strings.Contains(got.hint, c.wantHint) {
				t.Errorf("got %s %q, want %s with %q", got.kind, got.hint, c.wantKind, c.wantHint)
			}
			if rep.noCoverage != c.noCoverage {
				t.Errorf("noCoverage = %v, want %v", rep.noCoverage, c.noCoverage)
			}
		})
	}
}
