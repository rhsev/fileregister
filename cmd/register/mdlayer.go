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
	"strings"

	"github.com/rhsev/grubber/entry"
)

// mdBlock is one data block in a note's text: the bytes from the opening
// fence line's first to the closing fence line's last, and the lines between
// the fences without a trailing newline.
type mdBlock struct {
	start, end int
	body       string
}

// mdBlocks returns the data blocks of a note. The writers find the block they
// edit with it; reading the layer is grubber's, and the two must agree on what
// a block is, or a writer misses a block the reader shows and appends a second
// one. So the rule is not register's: entry.DataBlocks is the function grubber
// reads its records with.
func mdBlocks(content string) []mdBlock {
	// Below the frontmatter, as grubber looks: a fence line in a header
	// value that runs over several lines opens no block.
	fm := frontmatterLen(content)
	lines := strings.Split(content[fm:], "\n")
	spans := entry.DataBlocks(lines)
	if len(spans) == 0 {
		return nil
	}
	starts := make([]int, len(lines))
	off := fm
	for i, l := range lines {
		starts[i] = off
		off += len(l) + 1
	}
	blocks := make([]mdBlock, len(spans))
	for i, s := range spans {
		blocks[i] = mdBlock{
			start: starts[s.Open],
			end:   starts[s.Close] + len(lines[s.Close]),
			body:  strings.Join(lines[s.Open+1:s.Close], "\n"),
		}
	}
	return blocks
}

// blockFields reads the text between a block's fences as grubber reads it:
// a key that stands twice counts once, and a block that is not valid YAML is
// read line by line. A writer that took such a block for none would not find
// the record grubber shows, and promote would append a second block for it.
// nil when the block holds no record.
func blockFields(body string) map[string]any {
	fields, _ := entry.Fields([]byte(body))
	return fields
}

// mdReplaceBlocks returns content with every data block replaced by what fn
// makes of it. fn gets the block from fence to fence and returns it unchanged
// to keep it.
func mdReplaceBlocks(content string, fn func(block string) string) string {
	var b strings.Builder
	last := 0
	for _, blk := range mdBlocks(content) {
		b.WriteString(content[last:blk.start])
		b.WriteString(fn(content[blk.start:blk.end]))
		last = blk.end
	}
	b.WriteString(content[last:])
	return b.String()
}

// splitFence splits a block from fence to fence into its opening fence line (with
// its newline), the body without a trailing newline, and the closing fence
// line.
func splitFence(match string) (open, body, closing string) {
	i := strings.IndexByte(match, '\n')
	j := strings.LastIndexByte(match, '\n')
	open, closing = match[:i+1], match[j+1:]
	if j > i {
		body = match[i+1 : j]
	}
	return
}

// canonBlock is a block as register writes it. The editors build their
// replacement in this form; refence puts it back between the fence lines the
// note had, so an indented fence stays indented.
func canonBlock(body string) string { return "```yaml\n" + body + "\n```" }

func refence(repl, open, closing string) string {
	return open + strings.TrimSuffix(strings.TrimPrefix(repl, "```yaml\n"), "```") + closing
}

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
// under notesDir — the inbox-vs-annotated distinction for list. A block
// reaches its record as it does for annotate, aka and order: by the id, by a
// handle in the id: slot, or, without an id, by a handle in its aka: list.
func annotatedIDs(notesDir string) (map[string]bool, error) {
	blocks, err := readAnnotations(notesDir)
	if err != nil {
		return nil, err
	}
	refs, err := index.ReadAllRefs(notesDir)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	akaToID := map[string]string{}
	for _, r := range refs {
		id := index.AsString(r["id"])
		known[id] = true
		for _, a := range index.AsStrings(r["aka"]) {
			akaToID[index.CanonLabel(a)] = id
		}
	}
	ids := map[string]bool{}
	for _, b := range blocks {
		bid := index.AsString(b["id"])
		switch {
		case bid != "" && known[bid]:
			ids[bid] = true
		case bid != "":
			if real, ok := akaToID[index.CanonLabel(bid)]; ok {
				ids[real] = true
			} else {
				ids[bid] = true // a block of no record; harmless here
			}
		default:
			for _, a := range index.AsStrings(b["aka"]) {
				if real, ok := akaToID[index.CanonLabel(a)]; ok {
					ids[real] = true
				}
			}
		}
	}
	return ids, nil
}
