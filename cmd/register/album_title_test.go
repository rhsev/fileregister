package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The album name is one frontmatter field; these pin how it is written,
// replaced and taken away again, and that the heading follows.

func TestRemoveFrontmatterField(t *testing.T) {
	cases := []struct {
		name, in, want, what string
	}{
		{"other keys stay",
			"---\nauthor: me\nalbum: Safari\n---\n\nbody\n",
			"---\nauthor: me\n---\n\nbody\n", "removed"},
		{"last key takes the header with it",
			"---\nalbum: Safari\n---\n\nbody\n",
			"body\n", "removed"},
		{"a multi-line value goes whole",
			"---\nalbum: >\n  Safari,\n\n  Namibia\nauthor: me\n---\nbody\n",
			"---\nauthor: me\n---\nbody\n", "removed"},
		{"no such key",
			"---\nauthor: me\n---\nbody\n",
			"---\nauthor: me\n---\nbody\n", "absent"},
		{"no header",
			"body\n",
			"body\n", "absent"},
		{"a block's album key is not the header's",
			"### x\n```yaml\nalbum: Safari\n```\n",
			"### x\n```yaml\nalbum: Safari\n```\n", "absent"},
	}
	for _, c := range cases {
		p := filepath.Join(t.TempDir(), "n.md")
		writeFile(t, p, c.in)
		what, err := removeFrontmatterField(p, "album")
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if what != c.what {
			t.Errorf("%s: what = %q, want %q", c.name, what, c.what)
		}
		if got := mustRead(t, p); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}

	// Taking a name away must not create the note it was never in.
	missing := filepath.Join(t.TempDir(), "binder_x.md")
	if what, err := removeFrontmatterField(missing, "album"); err != nil || what != "absent" {
		t.Errorf("missing note: what = %q, err = %v", what, err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Errorf("missing note was created")
	}
}

// Replacing a hand-written multi-line value used to swap only its first line,
// leaving the continuation behind as broken YAML.
func TestUpsertFrontmatterFieldReplacesMultiline(t *testing.T) {
	p := filepath.Join(t.TempDir(), "n.md")
	writeFile(t, p, "---\nalbum: >\n  Safari,\n  Namibia\nauthor: me\n---\nbody\n")
	if _, err := upsertFrontmatterField(p, "album", "Kalahari"); err != nil {
		t.Fatal(err)
	}
	if got, want := mustRead(t, p), "---\nalbum: Kalahari\nauthor: me\n---\nbody\n"; got != want {
		t.Errorf("\n got %q\nwant %q", got, want)
	}
}

func TestAlbumTitleSetAndClear(t *testing.T) {
	anchor := engineBin(t)
	n := seedAlbumFixture(t)
	note := filepath.Join(n, "collections", "binder_Alb.md")
	env := albumEnv(t, n, anchor)

	// albumNames returns the album field of every line that has fields.
	albumNames := func(out string) []any {
		var names []any
		for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
			var m map[string]any
			if err := json.Unmarshal([]byte(l), &m); err != nil {
				t.Fatalf("stdout is not JSON lines: %q", l)
			}
			if f, ok := m["fields"].(map[string]any); ok {
				names = append(names, f["album"])
			}
		}
		return names
	}

	so, se, code := runGoAlbum(t, env, "Alb", "--title", "Safari, Namibia 2026")
	if code != 0 {
		t.Fatalf("set: exit %d: %s", code, se)
	}
	if !strings.Contains(mustRead(t, note), "album: Safari, Namibia 2026") {
		t.Fatalf("field not written:\n%s", mustRead(t, note))
	}
	for _, name := range albumNames(so) {
		if name != "Safari, Namibia 2026" {
			t.Errorf("a member did not inherit the name: %v", name)
		}
	}

	so, se, code = runGoAlbum(t, env, "Alb", "--title", "")
	if code != 0 {
		t.Fatalf("clear: exit %d: %s", code, se)
	}
	if !strings.Contains(se, "Album field removed") {
		t.Errorf("clear: stderr %q", se)
	}
	if got := mustRead(t, note); strings.Contains(got, "album:") || strings.HasPrefix(got, "---") {
		t.Errorf("field or empty header left behind:\n%s", got)
	}
	for _, name := range albumNames(so) {
		if name != nil {
			t.Errorf("a member still carries a name: %v", name)
		}
	}
}

// grubber says why it failed on stderr; the exit status alone does not.
func TestAlbumKeepsGrubberStderr(t *testing.T) {
	anchor := engineBin(t)
	n := seedAlbumFixture(t)
	stub := filepath.Join(t.TempDir(), "grubber")
	writeFile(t, stub, "#!/bin/sh\necho 'grubber: config set not found: foo' >&2\nexit 1\n")
	if err := os.Chmod(stub, 0755); err != nil {
		t.Fatal(err)
	}
	env := append(filterEnv(albumEnv(t, n, anchor), "GRUBBER_BIN"), "GRUBBER_BIN="+stub)

	_, se, code := runGoAlbum(t, env, "Alb")
	if code == 0 {
		t.Fatal("album succeeded with a failing grubber")
	}
	if !strings.Contains(se, "exit status 1: grubber: config set not found: foo") {
		t.Errorf("stderr lost grubber's reason: %q", se)
	}
}
