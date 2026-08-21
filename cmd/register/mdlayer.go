package main

// mdlayer — the optional Markdown annotation layer: which ids carry an
// annotation, and where those notes live. Consulted only for the list
// --inbox/--curated cosmetic filter; the core read path is the index.

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// mdYamlBlockRe matches fenced YAML blocks: ```yaml\n…\n``` (multiline, dotall,
// non-greedy) — the Go equivalent of MdEditor::YAML_BLOCK_RE.
var mdYamlBlockRe = regexp.MustCompile("(?ms)^```yaml\n(.*?)\n^```")

// mdFiles returns the *.md files worth parsing for ref blocks. A ripgrep
// prefilter narrows a large vault to just the files that actually contain a
// `type: ref` line (a pure optimization — a file with no ref block yields no
// records, so the parsed result is identical); it falls back to a full *.md
// walk when rg is unavailable or errors.
func mdFiles(notesDir string) []string {
	if files, ok := mdRefFilesRg(notesDir); ok {
		return files
	}
	return mdFilesWalk(notesDir)
}

// mdRefFilesRg lists *.md files containing a ref block via ripgrep. The second
// return is false when rg is missing or errored (caller falls back to a walk).
func mdRefFilesRg(notesDir string) ([]string, bool) {
	cmd := exec.Command("rg", "--no-ignore", "--null", "-l", "-g", "*.md",
		"-e", `type:\s*['"]?ref\b`, notesDir)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return nil, true // ran clean, zero matches
		}
		return nil, false // rg not installed or a real error → fall back
	}
	var files []string
	for _, f := range strings.Split(string(out), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files, true
}

// mdFilesWalk returns every *.md under notesDir, skipping hidden files and dirs
// (a recursive *.md walk).
func mdFilesWalk(notesDir string) []string {
	var out []string
	filepath.WalkDir(notesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if path != notesDir && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasPrefix(name, ".") && strings.HasSuffix(name, ".md") {
			out = append(out, path)
		}
		return nil
	})
	return out
}

// annotatedIDs is the set of record ids that carry a Markdown ref block somewhere
// under notesDir — the cosmetic inbox-vs-annotated distinction for list.
func annotatedIDs(notesDir string) map[string]bool {
	ids := map[string]bool{}
	for _, md := range mdFiles(notesDir) {
		data, err := os.ReadFile(md)
		if err != nil {
			continue
		}
		for _, m := range mdYamlBlockRe.FindAllStringSubmatch(string(data), -1) {
			var rec map[string]any
			if yaml.Unmarshal([]byte(m[1]), &rec) != nil {
				continue
			}
			if t, _ := rec["type"].(string); t != "ref" {
				continue
			}
			ids[scalarToS(rec["id"])] = true
		}
	}
	return ids
}

// readAnnotations returns the Markdown layer's ref blocks as records, each with
// "_note_file" set to its source path — the Go equivalent of read_annotations.
// Files are visited in sorted order, so the output is deterministic.
func readAnnotations(notesDir string) []map[string]any {
	files := mdFiles(notesDir)
	sort.Strings(files)
	var out []map[string]any
	for _, md := range files {
		data, err := os.ReadFile(md)
		if err != nil {
			continue
		}
		for _, m := range mdYamlBlockRe.FindAllStringSubmatch(string(data), -1) {
			var rec map[string]any
			if yaml.Unmarshal([]byte(m[1]), &rec) != nil {
				continue
			}
			if t, _ := rec["type"].(string); t != "ref" {
				continue
			}
			rec["_note_file"] = md
			out = append(out, rec)
		}
	}
	return out
}

// scalarToS stringifies a YAML scalar for display (yaml.v3 decodes
// integers as int, unlike the JSON path's json.Number).
func scalarToS(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", x)
	}
}
