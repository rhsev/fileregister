package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rhsev/fileregister/v2/internal/index"
)

// Reader and writers see the same blocks. grubber reads the notes, register
// edits them, and both take the rule for what a block is from
// entry.DataBlocks: register through the module, grubber in the binary it
// runs. Each note here is read once by that binary and once by the writers'
// mdBlocks. A grubber binary with another rule than the module's shows up
// here, whatever the difference is.
//
// A block inside another code fence is an example and no data: neither side
// takes it, so a note that shows what a block looks like is not edited.
//
// The same holds for what stands in a block. A block that is not valid YAML
// grubber reads line by line, and so do the writers (entry.Fields).
func TestReaderAndWritersSeeTheSameBlocks(t *testing.T) {
	bin := grubberTestBin(t)
	block := func(id string) string { return "```yaml\ntype: ref\nid: " + id + "\nbinder: Docs\n```\n" }
	notes := map[string]string{
		"plain":                "### a\n" + block("a") + "\n### b\n" + block("b"),
		"no heading":           block("a") + "\n" + block("b"),
		"frontmatter":          "---\ntitle: T\n---\n\n### a\n" + block("a"),
		"empty block first":    "### empty\n```yaml\n```\n\n### a\n" + block("a"),
		"indented fences":      "- item\n\n  ```yaml\n  type: ref\n  id: a\n  binder: Docs\n  ```\n\n" + block("b"),
		"blanks after fences":  "```yaml \ntype: ref\nid: a\nbinder: Docs\n``` \n\n" + block("b"),
		"tab after fence":      "```yaml\t\ntype: ref\nid: a\nbinder: Docs\n```\t\n",
		"crlf":                 strings.ReplaceAll("### a\n"+block("a")+"\n"+block("b"), "\n", "\r\n"),
		"bom":                  "\xef\xbb\xbf" + block("a"),
		"bom and crlf":         "\xef\xbb\xbf" + strings.ReplaceAll(block("a")+block("b"), "\n", "\r\n"),
		"no final newline":     strings.TrimSuffix(block("a"), "\n"),
		"in a longer fence":    "````\n" + block("a") + "````\n\n" + block("b"),
		"in a tilde fence":     "~~~\n" + block("a") + "~~~\n\n" + block("b"),
		"after an open fence":  "```python\nprint(1)\n\n" + block("a") + "\n" + block("b"),
		"yml is no data":       "```yml\ntype: ref\nid: x\nbinder: Docs\n```\n\n" + block("a"),
		"fence with more text": "```yaml title\ntype: ref\nid: x\nbinder: Docs\n```\n\n" + block("a"),
		"unclosed at the end":  block("a") + "\n```yaml\ntype: ref\nid: x\nbinder: Docs\n",
		"fence not on a line":  "text ```yaml\ntype: ref\nid: x\nbinder: Docs\n\n" + block("a"),
		"longer fence, named":  "````markdown\n" + block("x") + "````\n\n" + block("a"),
		"fence never closed":   block("a") + "\n~~~\n" + block("x"),
		"rule between":         block("a") + "\n---\n\n" + block("b"),
		"not a mapping":        "```yaml\n- one\n- two\n```\n\n" + block("a"),
		"opener in the block":  "```yaml\ntype: ref\nid: a\nbinder: Docs\n```yaml\n```\n\n" + block("b"),
		"colon in a value":     "```yaml\ntype: ref\nid: '41'\nbinder: Docs\ncomment: see: there\n```\n\n" + block("b"),
		"open quote":           "```yaml\ntype: ref\nid: \"42\"\nbinder: Docs\ncomment: \"open\n```\n\n" + block("b"),
		"key twice":            "```yaml\ntype: ref\nid: a\nbinder: Docs\nid: c\n```\n\n" + block("b"),
	}
	dir := t.TempDir()
	for name, text := range notes {
		path := filepath.Join(dir, strings.ReplaceAll(name, " ", "_")+".md")
		if err := os.WriteFile(path, []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
		records, err := grubberExtract(bin, path, "-b", "--no-fill", "--inherit=", "--extensions=.md")
		if err != nil {
			t.Errorf("%s: grubber: %v", name, err)
			continue
		}
		var read []string
		for _, r := range records {
			read = append(read, index.AsString(r["id"]))
		}
		content, _, err := readNote(path)
		if err != nil {
			t.Fatal(err)
		}
		var written []string
		for _, b := range mdBlocks(content) {
			if rec := blockFields(b.body); len(rec) > 0 {
				written = append(written, index.AsString(rec["id"]))
			}
		}
		if strings.Join(read, ",") != strings.Join(written, ",") {
			t.Errorf("%s: grubber reads the blocks %v, the writers find %v", name, read, written)
		}
		if len(read) == 0 {
			t.Errorf("%s: no block read, the case proves nothing", name)
		}
	}
}

// A block that is not valid YAML is still the record's block: grubber shows
// it, so promote must not append a second one, and forget and delete find it.
func TestWritersFindABlockThatIsNotValidYAML(t *testing.T) {
	note := filepath.Join(t.TempDir(), "binder_Docs.md")
	broken := "### a.pdf\n```yaml\ntype: ref\nid: '1'\nbinder: Docs\ncomment: see: there\n```\n"
	writeFile(t, note, broken)
	rec := index.RefRecord{ID: "1", Binder: "Docs", Filename: "a.pdf"}
	if got := mdFileWriteMany([]index.RefRecord{rec}, note); len(got) != 1 || got[0] != "noop" {
		t.Errorf("promote: got %v, want a noop", got)
	}
	if got := mustRead(t, note); got != broken {
		t.Errorf("promote changed the note:\n%s", got)
	}
	if n, err := mdForgetIDs(note, map[string]bool{"1": true}); err != nil || n != 1 {
		t.Errorf("forget changed %d block(s), err %v", n, err)
	}
	if got := mustRead(t, note); strings.Contains(got, "id:") || !strings.Contains(got, "comment: see: there") {
		t.Errorf("forget should take the id line and nothing else:\n%s", got)
	}
	writeFile(t, note, broken)
	if n, err := mdDeleteBlock(note, "1", "Docs"); err != nil || n != 1 {
		t.Errorf("delete removed %d block(s), err %v", n, err)
	}
}

// An example of a block, shown in a longer fence, is the note's text. The
// writers leave it alone: rename does not rewrite it, delete does not cut it
// out, and promote does not take it for the record's block.
func TestWritersLeaveAnExampleBlockAlone(t *testing.T) {
	note := filepath.Join(t.TempDir(), "howto.md")
	example := "A block looks like this:\n\n````\n### a.pdf\n```yaml\ntype: ref\nid: '1'\nbinder: Docs\n```\n````\n"
	writeFile(t, note, example)
	if n, err := mdRenameBinder(note, "Docs", "Papers"); err != nil || n != 0 {
		t.Errorf("rename changed %d block(s), err %v", n, err)
	}
	if n, err := mdDeleteBlock(note, "1", "Docs"); err != nil || n != 0 {
		t.Errorf("delete removed %d block(s), err %v", n, err)
	}
	if n, err := mdForgetIDs(note, map[string]bool{"1": true}); err != nil || n != 0 {
		t.Errorf("forget changed %d block(s), err %v", n, err)
	}
	if got := mustRead(t, note); got != example {
		t.Errorf("the note changed:\n%s", got)
	}
	rec := index.RefRecord{ID: "1", Binder: "Docs", Filename: "a.pdf"}
	if got := mdFileWriteMany([]index.RefRecord{rec}, note); len(got) != 1 || got[0] != "appended" {
		t.Errorf("promote: got %v, want the block appended", got)
	}
	if got := mustRead(t, note); !strings.HasPrefix(got, example) {
		t.Errorf("promote changed the example:\n%s", got)
	}
}

// promote gives a block that lost its id to forget the id back. An example
// of such a block in a ~~~ fence is not that block, and a # line in a code
// fence is no heading.
func TestReattachLeavesAnExampleAlone(t *testing.T) {
	note := filepath.Join(t.TempDir(), "binder_Docs.md")
	lost := "```yaml\ntype: ref\nbinder: Docs\n```\n"
	example := "### a.pdf\n~~~\n" + lost + "~~~\n"
	writeFile(t, note, example)
	if done, err := mdReattachBlock(note, "Docs", "a.pdf", "1"); err != nil || done {
		t.Errorf("reattached to the example: %v, err %v", done, err)
	}
	if got := mustRead(t, note); got != example {
		t.Errorf("the note changed:\n%s", got)
	}

	real := "### a.pdf\n```sh\n# a.pdf is not here\n```\n" + lost
	writeFile(t, note, example+"\n### b.pdf\n```sh\n### a.pdf\n```\n"+lost+"\n"+real)
	if done, err := mdReattachBlock(note, "Docs", "a.pdf", "1"); err != nil || !done {
		t.Fatalf("reattach: %v, err %v", done, err)
	}
	want := example + "\n### b.pdf\n```sh\n### a.pdf\n```\n" + lost + "\n" +
		strings.Replace(real, "type: ref\n", "type: ref\nid: '1'\n", 1)
	if got := mustRead(t, note); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}
