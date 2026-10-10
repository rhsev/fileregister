package main

// mdlayer — the Markdown annotation layer as register's own commands need it:
// which ref blocks exist, for which id and binder, in which note. Read through
// grubber, the one reader of the Markdown layer; register only writes notes.
// Every ref block carries type and binder itself (promote writes both): read
// without inheritance, a block that took either from the note's header would
// not be found.

import (
	"github.com/rhsev/fileregister/v2/internal/index"

	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// mdYamlBlockRe matches fenced YAML blocks: ```yaml\n…\n``` (multiline, dotall,
// non-greedy). The writers use it to find the block they edit; reading the
// layer is grubber's.
var mdYamlBlockRe = regexp.MustCompile("(?ms)^```yaml\n(.*?)\n^```")

// importDir: where unmarshal parks what it did not place — a container's notes
// wait there for the user, and are not part of the notes.
func importDir(notesDir string) string { return filepath.Join(notesDir, "collections", "import") }

// readAnnotations returns the Markdown layer's ref blocks as records, each with
// "_note_file" set to its source note, in grubber's (deterministic) order: one
// grubber run, ref blocks in *.md notes, none in hidden files or the import
// staging folder.
//
// Here a block is a record (reindex rebuilds index lines from it, rename and
// cleanup act on it), so it is read as it stands: --inherit= passes nothing
// down from the frontmatter, where a note's url: would otherwise become every
// block's. -b keeps out notes without blocks, whose frontmatter grubber would
// return as a record of its own. The album reads with inheritance; see
// grubberRecordsFor.
func readAnnotations(notesDir string) ([]map[string]any, error) {
	bin, err := grubberBin()
	if err != nil {
		return nil, err
	}
	blocks, err := grubberExtract(bin, notesDir, "-b", "--no-fill", "--inherit=", "--extensions=.md", "-f", "type=ref")
	if err != nil {
		return nil, fmt.Errorf("grubber failed reading the notes: %w", err)
	}
	parked := importDir(notesDir) + string(filepath.Separator)
	var out []map[string]any
	for _, b := range blocks {
		note := index.AsString(b["_note_file"])
		if t, _ := b["type"].(string); t != "ref" { // grubber's filter ignores case
			continue
		}
		if strings.HasPrefix(note, parked) || strings.HasPrefix(filepath.Base(note), ".") {
			continue
		}
		delete(b, "_mtime")
		out = append(out, b)
	}
	return out, nil
}

// annotatedIDs is the set of record ids that carry a Markdown ref block somewhere
// under notesDir — the inbox-vs-annotated distinction for list.
func annotatedIDs(notesDir string) (map[string]bool, error) {
	blocks, err := readAnnotations(notesDir)
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, b := range blocks {
		ids[index.AsString(b["id"])] = true
	}
	return ids, nil
}
