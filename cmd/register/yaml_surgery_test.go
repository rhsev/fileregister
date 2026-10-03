package main

import (
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// A multi-line value's lines are indented; they are the value, not keys. Edits
// used to take `  Meeting: 3pm` or `  place: …` inside a comment for a key.

const scalarBlock = "### a.pdf\n```yaml\ntype: ref\nid: '1'\nbinder: proj\n" +
	"note: |\n  Meeting: 3pm\n  place: unknown yet\n  sort: by date later\ncomment: keep\n```\n"

func parseOnlyBlock(t *testing.T, content string) map[string]any {
	t.Helper()
	m := mdYamlBlockRe.FindString(content)
	body := strings.TrimSuffix(strings.TrimPrefix(m, "```yaml\n"), "\n```")
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("block no longer parses: %v\n%s", err, content)
	}
	return parsed
}

func TestAnnotateUnsetTakesTheWholeMultiLineValue(t *testing.T) {
	out, n := annotateEditBlocks(scalarBlock, map[string]bool{"1": true}, "proj", nil, []string{"note"})
	p := parseOnlyBlock(t, out)
	if n != 1 || p["note"] != nil || p["comment"] != "keep" || strings.Contains(out, "Meeting") {
		t.Errorf("unset note: n=%d parsed=%v\n%s", n, p, out)
	}
}

func TestAnnotateSetLeavesIndentedProseAlone(t *testing.T) {
	out, _ := annotateEditBlocks(scalarBlock, map[string]bool{"1": true}, "proj",
		[]annotateSet{{key: "place", value: "Rome"}}, nil)
	p := parseOnlyBlock(t, out)
	if p["place"] != "Rome" || !strings.Contains(p["note"].(string), "place: unknown yet") {
		t.Errorf("set place: parsed=%v\n%s", p, out)
	}
}

func TestOrderMoveLeavesIndentedProseAlone(t *testing.T) {
	note := filepath.Join(t.TempDir(), "binder_proj.md")
	writeFile(t, note, scalarBlock)
	rec := map[string]any{"id": "1", "filename": "a.pdf"}
	if got := orderWriteOverride(note, "proj", rec, "sort", "m"); got != "updated" {
		t.Fatalf("orderWriteOverride = %s", got)
	}
	p := parseOnlyBlock(t, mustRead(t, note))
	if p["sort"] != "m" || !strings.Contains(p["note"].(string), "sort: by date later") {
		t.Errorf("order move: parsed=%v", p)
	}
}

// YAML lets a hand-written root mapping sit indented; edits keep its layout.
func TestEditsKeepAnIndentedBlockValid(t *testing.T) {
	block := "```yaml\n  type: ref\n  id: '1'\n  binder: proj\n  place: Paris\n```\n"
	out, n := annotateEditBlocks(block, map[string]bool{"1": true}, "proj",
		[]annotateSet{{key: "place", value: "Rome"}, {key: "amount", value: "12"}}, nil)
	p := parseOnlyBlock(t, out)
	if n != 1 || p["place"] != "Rome" || p["amount"] != 12 || p["binder"] != "proj" {
		t.Errorf("indented block: n=%d parsed=%v\n%s", n, p, out)
	}
}
