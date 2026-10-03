package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A container can come from someone else. These build hostile ones by hand.

// buildContainer writes files (path → content; content "->target" makes a
// symlink) under ship/ and packs them as name inside a fresh dir.
func buildContainer(t *testing.T, name string, files map[string]string) string {
	t.Helper()
	staging := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(staging, "ship", rel)
		os.MkdirAll(filepath.Dir(p), 0755)
		if target, ok := strings.CutPrefix(content, "->"); ok {
			if err := os.Symlink(target, p); err != nil {
				t.Fatal(err)
			}
			continue
		}
		writeFile(t, p, content)
	}
	out := filepath.Join(t.TempDir(), name)
	if b, err := exec.Command("tar", "-czf", out, "-C", staging, "ship").CombinedOutput(); err != nil {
		t.Fatalf("tar: %v %s", err, b)
	}
	return out
}

const keptLine = `{"type":"ref","id":"100000001","binder":["keep"],"url":"https://example.com/keep"}`

func TestUnmarshalContainerNameCannotEscapeStaging(t *testing.T) {
	anchor := engineBin(t)
	for _, name := range []string{"...tar.gz", ".tar.gz", "..tgz"} {
		notes := seedInbox(t, keptLine)
		c := buildContainer(t, name, map[string]string{
			"manifest.jsonl": `{"type":"ref","id":"200000002","binder":["b"],"url":"https://example.com/b"}` + "\n",
		})
		runGoUnmarshal(t, unmarshalEnv(t, notes, anchor), c)
		inbox, err := os.ReadFile(filepath.Join(notes, "collections", "inbox.jsonl"))
		if err != nil || !strings.Contains(string(inbox), "100000001") {
			t.Errorf("%s: the local index did not survive (err %v)", name, err)
		}
	}
}

func TestUnmarshalNeverWritesTheIndex(t *testing.T) {
	anchor := engineBin(t)
	notes := seedInbox(t, keptLine)
	c := buildContainer(t, "evil.tar.gz", map[string]string{
		"manifest.jsonl": strings.Join([]string{
			`{"type":"ref","id":"300000003","binder":["b"],"filename":"inbox.jsonl","file":"files/a","origin":"inbox.jsonl"}`,
			`{"type":"ref","id":"300000004","binder":["b"],"filename":"evil.jsonl","file":"files/b","origin":"evil.jsonl"}`,
			`{"type":"ref","id":"300000005","binder":["b"],"filename":"SCHEMA","file":"files/c","origin":"SCHEMA"}`,
			`{"type":"note","file":"notes/x.jsonl","origin":"x.jsonl"}`,
		}, "\n") + "\n",
		"files/a":       "CLOBBERED\n",
		"files/b":       `{"type":"ref","id":"999","binder":["evil"],"url":"https://evil/"}` + "\n",
		"files/c":       "1\n",
		"notes/x.jsonl": `{"type":"ref","id":"998","binder":["evil"],"url":"https://evil/"}` + "\n",
	})
	runGoUnmarshal(t, unmarshalEnv(t, notes, anchor), c, "--force")

	col := filepath.Join(notes, "collections")
	inbox := mustRead(t, filepath.Join(col, "inbox.jsonl"))
	if !strings.Contains(inbox, "100000001") || strings.Contains(inbox, "CLOBBERED") {
		t.Errorf("--force let the container overwrite inbox.jsonl:\n%s", inbox)
	}
	for _, f := range []string{"evil.jsonl", "x.jsonl"} {
		if _, err := os.Stat(filepath.Join(col, f)); err == nil {
			t.Errorf("the container placed %s into collections/", f)
		}
	}
	if got := strings.TrimSpace(mustRead(t, filepath.Join(col, "SCHEMA"))); got != "3" {
		t.Errorf("SCHEMA was overwritten: %q", got)
	}
}

func TestUnmarshalDoesNotFollowSymlinks(t *testing.T) {
	anchor := engineBin(t)
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "secret.txt"), "TOP SECRET")

	notes := seedInbox(t, keptLine)
	c := buildContainer(t, "links.tar.gz", map[string]string{
		"manifest.jsonl": strings.Join([]string{
			`{"type":"ref","id":"400000004","binder":["m"],"filename":"leak.txt","file":"files/leak.txt","origin":"leak.txt"}`,
			`{"type":"ref","id":"400000005","binder":["m"],"filename":"secret.txt","file":"files/dir/secret.txt","origin":"secret.txt"}`,
			`{"type":"note","file":"notes/binder_m.md","origin":"binder_m.md"}`,
		}, "\n") + "\n",
		"files/leak.txt":    "->" + filepath.Join(outside, "secret.txt"),
		"files/dir":         "->" + outside,
		"notes/binder_m.md": "->" + filepath.Join(outside, "secret.txt"),
	})
	runGoUnmarshal(t, unmarshalEnv(t, notes, anchor), c)

	col := filepath.Join(notes, "collections")
	for _, f := range []string{"leak.txt", "secret.txt", "binder_m.md"} {
		if data, err := os.ReadFile(filepath.Join(col, f)); err == nil {
			t.Errorf("a symlink in the container imported a host file as %s: %q", f, data)
		}
	}
}
