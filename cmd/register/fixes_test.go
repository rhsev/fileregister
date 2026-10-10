package main

// Regression tests for the 2026-08 review fixes: YAML quoting, block deletion,
// promote dedup scope, nested-section prose replacement, ordering-config
// placement, and reindex membership folding.

import (
	"github.com/rhsev/fileregister/v2/internal/index"

	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNeedsYAMLQuoteTrailingColon(t *testing.T) {
	if !needsYAMLQuote("notes:") {
		t.Error("a value ending in ':' must be quoted — plain it is unparseable YAML")
	}
}

func TestMdDeleteBlockKeepsAdjacentTitle(t *testing.T) {
	p := filepath.Join(t.TempDir(), "n.md")
	content := "# My Notes\n### item.pdf\n```yaml\ntype: ref\nid: '9'\nbinder: b\n```\n"
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	n, err := mdDeleteBlock(p, "9", "b")
	if err != nil || n != 1 {
		t.Fatalf("deleted = %d, err = %v", n, err)
	}
	got, _ := os.ReadFile(p)
	if !strings.Contains(string(got), "# My Notes") {
		t.Errorf("document title deleted along with the block:\n%s", got)
	}
	if strings.Contains(string(got), "### item.pdf") {
		t.Errorf("block heading survived the delete:\n%s", got)
	}
}

func TestPromoteDedupIsBinderScoped(t *testing.T) {
	p := filepath.Join(t.TempDir(), "shared.md")
	rec := map[string]any{"id": "5", "filename": "f.pdf"}
	if got, _, _ := promoteRecords([]map[string]any{rec}, p, "A"); got != 1 {
		t.Fatalf("promote A = %d, want 1", got)
	}
	if got, _, _ := promoteRecords([]map[string]any{rec}, p, "B"); got != 1 {
		t.Fatalf("promote B = %d, want 1 — a block for binder A must not suppress (id, B)", got)
	}
	if got, noop, _ := promoteRecords([]map[string]any{rec}, p, "B"); got != 0 || noop != 1 {
		t.Fatalf("second promote B = (%d, %d), want (0, 1)", got, noop)
	}
}

func TestAnnotateReplaceProseNestedSections(t *testing.T) {
	keys := map[string]bool{"7": true}
	content := "## X\nold outer\n```yaml\ntype: ref\nid: '7'\nbinder: b\n```\n" +
		"### Y\nold inner\n```yaml\ntype: ref\nid: '7'\nbinder: b\n```\n"
	out, n, err := annotateReplaceProse(content, keys, "b", "new prose")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("sections changed = %d, want 2\n%s", n, out)
	}
	if !strings.Contains(out, "### Y") {
		t.Errorf("nested heading destroyed:\n%s", out)
	}
	if c := strings.Count(out, "```yaml"); c != 2 {
		t.Errorf("yaml block count = %d, want 2:\n%s", c, out)
	}
	if c := strings.Count(out, "new prose"); c != 2 {
		t.Errorf("prose count = %d, want 2:\n%s", c, out)
	}
	if strings.Contains(out, "old outer") || strings.Contains(out, "old inner") {
		t.Errorf("old prose survived:\n%s", out)
	}
}

// TestReindexFoldsMemberships: one id annotated in two binders rebuilds as ONE
// record carrying both memberships (recovery must not drop the second binder).
func TestReindexFoldsMemberships(t *testing.T) {
	dir := t.TempDir()
	col := filepath.Join(dir, "collections")
	if err := os.MkdirAll(col, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(col, "binder_A.md"),
		"### f.pdf\n```yaml\ntype: ref\nid: '9'\nbinder: A\nfilename: f.pdf\n```\n")
	writeFile(t, filepath.Join(col, "binder_B.md"),
		"### f.pdf\n```yaml\ntype: ref\nid: '9'\nbinder: B\nfilename: f.pdf\n```\n")

	_, stderr, code := runGoReindex(t, reindexEnv(t, dir))
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	got := mustRead(t, filepath.Join(col, "inbox.jsonl"))
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("index lines = %d, want 1 record:\n%s", len(lines), got)
	}
	if !strings.Contains(lines[0], `"A"`) || !strings.Contains(lines[0], `"B"`) {
		t.Errorf("memberships not folded into one record: %s", lines[0])
	}
}

// TestMarshalUniqueNameFoldsForms: container/album names must stay unique on
// case- and normalization-insensitive filesystems, wherever they get unpacked.
func TestMarshalUniqueNameFoldsForms(t *testing.T) {
	taken := map[string]bool{}
	if got := marshalUniqueName("Report.pdf", taken); got != "Report.pdf" {
		t.Fatalf("first = %q", got)
	}
	if got := marshalUniqueName("report.pdf", taken); got != "report-1.pdf" {
		t.Errorf("case-colliding name = %q, want report-1.pdf", got)
	}
	nfc := "Café.jpg"  // e-acute precomposed
	nfd := "Café.jpg" // e + combining acute
	if got := marshalUniqueName(nfc, taken); got != nfc {
		t.Fatalf("nfc = %q", got)
	}
	if got := marshalUniqueName(nfd, taken); got == nfd {
		t.Errorf("normalization-colliding name not suffixed: %q", got)
	}
}

// buildScatterContainer hand-builds a container whose file origin escapes
// collections/ via "..".
func buildScatterContainer(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	root := filepath.Join(tmp, "Scatter")
	if err := os.MkdirAll(filepath.Join(root, "files"), 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "files", "out.txt"), "outside")
	writeFile(t, filepath.Join(root, "manifest.jsonl"),
		`{"type":"ref","id":"900000002","binder":["Docs"],"filename":"out.txt","kind":"txt","file":"files/out.txt","origin":"../out.txt"}`+"\n")
	out := filepath.Join(t.TempDir(), "scatter.tar.gz")
	cmd := exec.Command("tar", "--no-mac-metadata", "-czf", out, "-C", tmp, "Scatter")
	if o, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build container: %v\n%s", err, o)
	}
	return out
}

// TestUnmarshalScatterGuard: a vault-external origin is parked without
// --scatter and placed with it.
func TestUnmarshalScatterGuard(t *testing.T) {
	anchor := engineBin(t)
	container := buildScatterContainer(t)

	dest := t.TempDir()
	os.MkdirAll(filepath.Join(dest, "collections"), 0755)
	outside := filepath.Join(dest, "out.txt") // origin ../out.txt relative to collections/

	gOut, _, code := runGoUnmarshal(t, unmarshalEnv(t, dest, anchor), container)
	if code != 0 {
		t.Fatalf("unmarshal (guarded) exit = %d", code)
	}
	if index.FileExists(outside) {
		t.Fatalf("file placed outside collections/ without --scatter")
	}
	if !strings.Contains(gOut, "--scatter") {
		t.Errorf("park reason should point at --scatter:\n%s", gOut)
	}

	container2 := buildScatterContainer(t)
	_, _, code = runGoUnmarshal(t, unmarshalEnv(t, dest, anchor), container2, "--scatter")
	if code != 0 {
		t.Fatalf("unmarshal --scatter exit = %d", code)
	}
	if !index.FileExists(outside) {
		t.Errorf("--scatter did not place the vault-external file")
	}
}

// TestAnnotateReplaceProseYamlComment: a column-0 # line inside a matched
// block is a YAML comment, not a heading — the block must survive prose replace.
func TestAnnotateReplaceProseYamlComment(t *testing.T) {
	keys := map[string]bool{"7": true}
	content := "## H\nold\n```yaml\ntype: ref\n# a yaml comment\nid: '7'\nbinder: b\n```\n"
	out, n, err := annotateReplaceProse(content, keys, "b", "X")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("sections changed = %d, want 1\n%s", n, out)
	}
	if o := strings.Count(out, "```yaml"); o != 1 {
		t.Errorf("opening fence count = %d, want 1:\n%s", o, out)
	}
	if c := strings.Count(out, "\n```\n"); c != 1 {
		t.Errorf("bare closing fence count = %d, want 1:\n%s", c, out)
	}
	if !strings.Contains(out, "type: ref") || !strings.Contains(out, "# a yaml comment") {
		t.Errorf("yaml body damaged:\n%s", out)
	}
	if !strings.Contains(out, "\nX\n") {
		t.Errorf("prose not written:\n%s", out)
	}
}

// buildTraversalContainer hand-builds a container whose manifest `file` field
// escapes the staging dir via `..`.
func buildTraversalContainer(t *testing.T, fileField, origin string) string {
	t.Helper()
	tmp := t.TempDir()
	root := filepath.Join(tmp, "Evil")
	if err := os.MkdirAll(filepath.Join(root, "files"), 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "files", "decoy.txt"), "decoy")
	writeFile(t, filepath.Join(root, "manifest.jsonl"),
		`{"type":"ref","id":"900000003","binder":["Docs"],"filename":"x.txt","kind":"txt","file":`+
			jsonStr(fileField)+`,"origin":`+jsonStr(origin)+`}`+"\n")
	out := filepath.Join(t.TempDir(), "evil.tar.gz")
	cmd := exec.Command("tar", "--no-mac-metadata", "-czf", out, "-C", tmp, "Evil")
	if o, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build container: %v\n%s", err, o)
	}
	return out
}

func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestUnmarshalRejectsTraversal: a manifest `file` escaping the container is
// parked (read-side guard), and an `origin` with interior `..` escaping the
// vault is parked without --scatter (post-Clean write-side guard). Neither
// touches a host file outside the vault.
func TestUnmarshalRejectsTraversal(t *testing.T) {
	anchor := engineBin(t)

	// A sentinel the manifest would read/delete if the read guard were bypassed.
	secretDir := t.TempDir()
	secret := filepath.Join(secretDir, "secret.txt")
	writeFile(t, secret, "top secret")

	dest := t.TempDir()
	os.MkdirAll(filepath.Join(dest, "collections"), 0755)
	rel, _ := filepath.Rel(filepath.Join(dest, "collections"), secret)

	// Read-side: file field climbs out of the container to the sentinel. The
	// entry is parked (parking is not itself an error → exit 0); what matters
	// is that the sentinel survives and nothing is imported from outside.
	c1 := buildTraversalContainer(t, rel, "docs/x.txt")
	runGoUnmarshal(t, unmarshalEnv(t, dest, anchor), c1)
	if !index.FileExists(secret) {
		t.Errorf("read guard let the sentinel be deleted")
	}
	if index.FileExists(filepath.Join(dest, "collections", "docs", "x.txt")) {
		t.Errorf("read guard let an out-of-container file be imported")
	}

	// Write-side: interior `..` in origin collapses above the vault; must be
	// parked without --scatter despite not starting with "../".
	escaped := filepath.Join(dest, "escaped.txt")
	c2 := buildTraversalContainer(t, "files/decoy.txt", "sub/../../escaped.txt")
	if _, _, code := runGoUnmarshal(t, unmarshalEnv(t, dest, anchor), c2); code != 0 {
		t.Fatalf("write-traversal (parked) exit = %d", code)
	}
	if index.FileExists(escaped) {
		t.Errorf("interior-.. origin escaped the vault without --scatter")
	}
}
