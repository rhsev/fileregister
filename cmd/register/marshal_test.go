package main

import (
	"github.com/rhsev/fileregister/internal/index"

	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// makeMarshalFixture builds binder "Ship": two url refs, both annotated by one
// binder note inside collections/, plus a scattered note in the notes root.
func makeMarshalFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	col := filepath.Join(dir, "collections")
	if err := os.MkdirAll(col, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(col, "ship.jsonl"),
		`{"type":"ref","id":"1","binder":["Ship"],"url":"x-devonthink-item://AAA","filename":"a.md","kind":"doc","aka":"alpha"}`+"\n"+
			`{"type":"ref","id":"2","binder":["Ship"],"url":"x-devonthink-item://BBB","filename":"b.md"}`+"\n")

	// Binder note inside collections/ (origin has no ".." → always packed).
	writeFile(t, filepath.Join(col, "ship-notes.md"),
		"# Ship notes\n\n```yaml\ntype: ref\nid: 1\nbinder: Ship\nfilename: a.md\n```\n\n"+
			"```yaml\ntype: ref\nid: 2\nbinder: Ship\nfilename: b.md\n```\n")

	// Scattered note in the notes root (origin escapes via ".." → needs --all-notes).
	writeFile(t, filepath.Join(dir, "scattered.md"),
		"```yaml\ntype: ref\nid: 1\nbinder: Ship\nfilename: a.md\n```\n")

	return dir
}

func marshalEnv(t *testing.T, notes, anchor string) []string {
	t.Helper()
	env := filterEnv(os.Environ(), "GRUBBER_SET")
	for _, k := range []string{"GRUBBER_NOTES", "HOME", "FILEANCHOR", "REGISTER_BINDER"} {
		env = filterEnv(env, k)
	}
	return append(env,
		"GRUBBER_NOTES="+notes,
		"HOME="+t.TempDir(),
		"FILEANCHOR="+anchor,
	)
}

func runGoMarshal(t *testing.T, env []string, args ...string) (string, string, int) {
	t.Helper()
	restore := setEnv(t, env)
	defer restore()
	index.ResetEngine()

	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	code := cmdMarshal(args)

	index.ResetEngine()
	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr

	var ob, eb bytes.Buffer
	io.Copy(&ob, outR)
	io.Copy(&eb, errR)
	return ob.String(), eb.String(), code
}

// untar extracts a tar.gz into dest.
func untar(t *testing.T, tgz, dest string) {
	t.Helper()
	if err := os.MkdirAll(dest, 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("tar", "-xzf", tgz, "-C", dest)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("untar %s: %v\n%s", tgz, err, out)
	}
}

// manifestMultiset returns the manifest's lines as canonical (key-sorted) JSON,
// sorted — so key order and line order don't affect equality.
func manifestMultiset(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest %s: %v", path, err)
	}
	var lines []string
	for _, ln := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(ln), &obj); err != nil {
			t.Fatalf("manifest line not JSON: %q: %v", ln, err)
		}
		canon, _ := json.Marshal(obj) // Go sorts map keys → canonical form
		lines = append(lines, string(canon))
	}
	sort.Strings(lines)
	return lines
}

// treeContents maps each file path relative to <root>/<name> to its contents,
// excluding manifest.jsonl (compared separately).
func treeContents(t *testing.T, containerRoot string) map[string]string {
	t.Helper()
	out := map[string]string{}
	filepath.WalkDir(containerRoot, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(containerRoot, p)
		if rel == "manifest.jsonl" {
			return nil
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			t.Fatal(rerr)
		}
		out[rel] = string(data)
		return nil
	})
	return out
}

func keysOf(m map[string]string) []string {
	var k []string
	for key := range m {
		k = append(k, key)
	}
	sort.Strings(k)
	return k
}

// containerGolden untars a marshal container and renders its manifest (as a
// sorted multiset) + files as one deterministic golden string.
func containerGolden(t *testing.T, tgz string) string {
	t.Helper()
	d := t.TempDir()
	untar(t, tgz, d)
	root := filepath.Join(d, "Ship")
	var b strings.Builder
	b.WriteString("=== manifest ===\n")
	for _, l := range manifestMultiset(t, filepath.Join(root, "manifest.jsonl")) {
		b.WriteString(l + "\n")
	}
	tree := treeContents(t, root)
	keys := keysOf(tree)
	for _, k := range keys {
		b.WriteString("=== " + k + " ===\n" + tree[k])
	}
	return b.String()
}

func assertMarshal(t *testing.T, label string, env []string, extraArgs ...string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "ship.tar.gz")
	args := append([]string{"--binder", "Ship", "--out", out}, extraArgs...)
	so, se, code := runGoMarshal(t, env, args...)
	repl := map[string]string{out: "<OUT>"}
	assertGolden(t, "marshal_"+label, cliResult(norm(so, repl), norm(se, repl), code))
	if code == 0 {
		assertGolden(t, "marshal_"+label+"_container", containerGolden(t, out))
	}
}

func TestMarshal(t *testing.T) {
	anchor := engineBin(t)
	env := marshalEnv(t, makeMarshalFixture(t), anchor)
	assertMarshal(t, "default", env) // binder notes only (scattered skipped)
	assertMarshal(t, "all-notes", env, "--all-notes")

	// missing binder → "Nothing to marshal", exit 0 (no container).
	so, se, code := runGoMarshal(t, env, "--binder", "Ghost", "--out", filepath.Join(t.TempDir(), "g.tar.gz"))
	assertGolden(t, "marshal_missing-binder", cliResult(so, se, code))
}
