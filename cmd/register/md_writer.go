package main

// md_writer — the Markdown annotation write layer: emit a lean annotation block
// for a record, and read existing blocks back for dedup.
//
// Note: the emitted YAML is canonical for these records, but scalar quoting is
// not normative — any YAML parser round-trips it to the same record.

import (
	"github.com/rhsev/fileregister/internal/index"

	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// defaultPromoteTarget is the binder's default annotation note (also its default
// ordering file): <notes_dir>/collections/binder_<sanitized>.md.
func defaultPromoteTarget(notesDir, binder string) string {
	return filepath.Join(notesDir, "collections", "binder_"+sanitizeBinderName(binder)+".md")
}

var yamlNumRe = regexp.MustCompile(`^[-+]?(\d+\.?\d*|\.\d+)([eE][-+]?\d+)?$`)

// yamlScalar renders a string as a YAML scalar, single-quoting when a plain
// scalar would be ambiguous (numbers, keywords, indicator-led, etc.) — close to
// stable across writes, and always valid YAML.
//
// The heuristic keeps the common output stable; what decides is a round trip:
// anything YAML would not read back as exactly s (YAML 1.2 numbers like
// 2024_05 or 0x1F, a control character, a line separator) is written
// double-quoted with escapes. 2024_05 used to read back as the number 202405,
// so promote never recognized its own block and appended it again.
func yamlScalar(s string) string {
	out := s
	if needsYAMLQuote(s) {
		out = "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
	if strings.IndexFunc(s, yamlUnsafeRune) < 0 && yamlReadsBack(out, s) {
		return out
	}
	node := yaml.Node{Kind: yaml.ScalarNode, Style: yaml.DoubleQuotedStyle, Value: s}
	b, err := yaml.Marshal(&node)
	if err != nil {
		return out
	}
	return strings.TrimSuffix(string(b), "\n")
}

// yamlUnsafeRune: characters a plain or single-quoted scalar cannot carry
// reliably — control characters, and the Unicode line/paragraph separators.
func yamlUnsafeRune(r rune) bool {
	return unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || r == '\uFEFF'
}

// yamlReadsBack reports whether repr, as a mapping value, parses to the
// string s.
func yamlReadsBack(repr, s string) bool {
	var m map[string]any
	if yaml.Unmarshal([]byte("k: "+repr), &m) != nil {
		return false
	}
	got, ok := m["k"].(string)
	return ok && got == s
}

func needsYAMLQuote(s string) bool {
	if s == "" {
		return true
	}
	if yamlNumRe.MatchString(s) {
		return true
	}
	switch strings.ToLower(s) {
	case "true", "false", "yes", "no", "on", "off", "null", "~", "nan", ".inf", "-.inf":
		return true
	}
	if strings.IndexByte("!&*?{}[],#|>@`\"'%-:= ", s[0]) >= 0 {
		return true
	}
	if s[len(s)-1] == ' ' || s[len(s)-1] == ':' {
		return true
	}
	if strings.Contains(s, ": ") || strings.Contains(s, " #") {
		return true
	}
	if strings.ContainsAny(s, "\n\t") {
		return true
	}
	return false
}

// headingText makes s safe as a one-line heading: a line break in a file name
// would end the heading and turn the rest of the name into prose.
func headingText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ' '
		}
		return r
	}, s)
}

// readNote reads a Markdown note with its line endings as \n: every editor
// here matches \n, and a note saved with \r\n (Windows, some sync setups)
// looked empty — its blocks were invisible, and promote appended duplicates.
// crlf tells the file's own style, so writeNote can put it back.
func readNote(path string) (content string, crlf bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
	}
	s := string(data)
	if !strings.Contains(s, "\r\n") {
		return s, false, nil
	}
	return strings.ReplaceAll(s, "\r\n", "\n"), true, nil
}

// writeNote writes a note read by readNote back in its own line endings.
func writeNote(path, content string, crlf bool) error {
	if crlf {
		content = strings.ReplaceAll(content, "\n", "\r\n")
	}
	return index.AtomicWrite(path, []byte(content))
}

// leanAnnotation renders a lean annotation block: H3 = filename (or id), YAML =
// type/id/binder (the join key). Custom fields + prose are added by the human.
func leanAnnotation(rec map[string]any, binder string) string {
	header := index.AsString(rec["filename"])
	if header == "" {
		header = index.AsString(rec["id"])
	}
	yaml := "type: " + yamlScalar("ref") + "\n" +
		"id: " + yamlScalar(index.AsString(rec["id"])) + "\n" +
		"binder: " + yamlScalar(binder) + "\n"
	return "### " + headingText(header) + "\n```yaml\n" + yaml + "```\n"
}

// mdParseBlocks parses every fenced yaml block in one Markdown file — one read
// serves all block consumers. Nil on read error or unparseable blocks skipped.
func mdParseBlocks(path string) []map[string]any {
	content, _, err := readNote(path)
	if err != nil {
		return nil
	}
	var out []map[string]any
	for _, m := range mdYamlBlockRe.FindAllStringSubmatch(content, -1) {
		var rec map[string]any
		if yaml.Unmarshal([]byte(m[1]), &rec) != nil || rec == nil {
			continue
		}
		out = append(out, rec)
	}
	return out
}

// mdAllRefs parses the type:ref YAML blocks in one Markdown file (MdEditor.all_refs).
func mdAllRefs(path string) []map[string]any {
	var out []map[string]any
	for _, rec := range mdParseBlocks(path) {
		if t, _ := rec["type"].(string); t == "ref" {
			out = append(out, rec)
		}
	}
	return out
}

// chomp removes a single trailing line separator.
func chomp(s string) string {
	if strings.HasSuffix(s, "\r\n") {
		return s[:len(s)-2]
	}
	if strings.HasSuffix(s, "\n") || strings.HasSuffix(s, "\r") {
		return s[:len(s)-1]
	}
	return s
}

// promoteRecords writes lean per-binder annotations into mdTarget (one block per
// id), keeping the index untouched. Idempotent per (id, binder): an id (or aka)
// already present in the target under the same binder is skipped — a block for a
// different binder does not suppress this one (SPEC keys blocks by (id, binder)).
// Returns (promoted, noop, failed).
func promoteRecords(records []map[string]any, mdTarget, binder string) (int, int, int) {
	promoted, noop, failed := 0, 0, 0

	existing := map[string]bool{}
	if index.FileExists(mdTarget) {
		for _, r := range mdAllRefs(mdTarget) {
			if !index.SameBinder(index.AsString(r["binder"]), binder) {
				continue
			}
			if id := index.AsString(r["id"]); id != "" {
				existing[id] = true
			}
			for _, a := range index.AsStrings(r["aka"]) {
				if a != "" {
					existing[a] = true
				}
			}
		}
	}

	var blocks []string
	for _, rec := range records {
		id := index.AsString(rec["id"])
		if id == "" {
			os.Stderr.WriteString("  Warning: skipping record without id\n")
			failed++
			continue
		}
		keys := []string{id}
		for _, a := range index.AsStrings(rec["aka"]) {
			if a != "" {
				keys = append(keys, a)
			}
		}
		dup := false
		for _, k := range keys {
			if existing[k] {
				dup = true
				break
			}
		}
		if dup {
			noop++
			continue
		}
		blocks = append(blocks, leanAnnotation(rec, binder))
		existing[id] = true
		promoted++
	}

	if len(blocks) > 0 {
		os.MkdirAll(filepath.Dir(mdTarget), 0755)
		body := strings.Join(blocks, "\n")
		content := body
		existingNote, crlf, err := readNote(mdTarget)
		if err == nil {
			content = chomp(existingNote) + "\n\n" + body
		} else if !os.IsNotExist(err) {
			// The note exists but can't be read — appending blind would
			// replace it with only the new blocks. Refuse.
			fmt.Fprintf(os.Stderr, "  Error: reading %s failed: %v\n", mdTarget, err)
			return 0, noop, failed + promoted
		}
		if err := writeNote(mdTarget, content, crlf); err != nil {
			fmt.Fprintf(os.Stderr, "  Error: writing %s failed: %v\n", mdTarget, err)
			return 0, noop, failed + promoted
		}
	}
	return promoted, noop, failed
}

// keyLine reports whether a line is a top-level YAML key line (\A\s*key\s*:),
// as tolerant as the parser so edits don't silently no-op on hand-formatted blocks.
// keyLine reports whether line opens the top-level key `key` of a block whose
// top-level keys sit at indent (blockIndent). A deeper-indented line belongs
// to the value above it — a `place:` inside a multi-line comment is prose, and
// matching it used to replace or delete that prose.
func keyLine(line, key, indent string) bool {
	if !opensKey(line, indent) {
		return false
	}
	s := line[len(indent):]
	if !strings.HasPrefix(s, key) {
		return false
	}
	s = strings.TrimLeft(s[len(key):], " \t")
	return strings.HasPrefix(s, ":")
}

// opensKey reports whether line starts a new top-level entry at indent: not
// deeper, not a sequence item of the key above, not blank.
func opensKey(line, indent string) bool {
	if !strings.HasPrefix(line, indent) || len(line) == len(indent) {
		return false
	}
	c := line[len(indent)]
	return c != ' ' && c != '\t' && c != '-'
}

// blockIndent returns the indentation of a block's top-level keys: the leading
// whitespace of its first content line. fileregister writes column 0, but YAML
// lets a hand-written root mapping sit indented.
func blockIndent(lines []string) string {
	for _, l := range lines {
		t := strings.TrimLeft(l, " \t")
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		return l[:len(l)-len(t)]
	}
	return ""
}

// yamlLine renders one "key: value" YAML line (MdEditor.yaml_line).
func yamlLine(key, value string) string {
	return key + ": " + yamlScalar(value)
}

// mdTransformFile applies fn to every type:ref YAML block via line surgery,
// preserving unrelated lines (prose, custom fields). fn returns (replacement,
// changed). Writes back only when something changed; returns the change count.
// A failed write returns the error and counts as zero changes applied.
func mdTransformFile(path string, fn func(lines []string, parsed map[string]any) (string, bool)) (int, error) {
	content, crlf, err := readNote(path)
	if err != nil {
		return 0, err
	}
	changes := 0
	modified := mdYamlBlockRe.ReplaceAllStringFunc(content, func(match string) string {
		body := strings.TrimSuffix(strings.TrimPrefix(match, "```yaml\n"), "\n```")
		var parsed map[string]any
		if yaml.Unmarshal([]byte(body), &parsed) != nil || parsed == nil {
			return match
		}
		if t, _ := parsed["type"].(string); t != "ref" {
			return match
		}
		repl, changed := fn(strings.Split(body, "\n"), parsed)
		if changed && repl != match {
			changes++
			return repl
		}
		return match
	})
	if changes > 0 {
		if err := writeNote(path, modified, crlf); err != nil {
			return 0, err
		}
	}
	return changes, nil
}

// mdRenameBinder rewrites the binder: line in every block whose binder == oldName.
func mdRenameBinder(path, oldName, newName string) (int, error) {
	return mdTransformFile(path, func(lines []string, parsed map[string]any) (string, bool) {
		if !index.SameBinder(index.AsString(parsed["binder"]), oldName) {
			return "", false
		}
		out := make([]string, len(lines))
		ind := blockIndent(lines)
		for i, l := range lines {
			if keyLine(l, "binder", ind) {
				out[i] = ind + yamlLine("binder", newName)
			} else {
				out[i] = l
			}
		}
		body := strings.Join(out, "\n")
		// Verify the surgery still parses to the renamed record before
		// accepting it (a binder: key with a multi-line value, say).
		var check map[string]any
		if yaml.Unmarshal([]byte(body), &check) != nil || !index.SameBinder(index.AsString(check["binder"]), newName) {
			fmt.Fprintf(os.Stderr, "  Warning: block for id %s in %s left unrenamed (nested binder: key) — edit by hand\n",
				index.AsString(parsed["id"]), filepath.Base(path))
			return "", false
		}
		return "```yaml\n" + body + "\n```", true
	})
}

// mdRenameOrderingBinder rewrites the binder: line of a type:ordering config
// block whose binder == oldName — mdTransformFile is ref-only, and a rename
// must carry the ordering layer along.
func mdRenameOrderingBinder(path, oldName, newName string) (int, error) {
	const blockType = "ordering"
	content, crlf, err := readNote(path)
	if err != nil {
		return 0, err
	}
	changes := 0
	modified := mdYamlBlockRe.ReplaceAllStringFunc(content, func(match string) string {
		body := strings.TrimSuffix(strings.TrimPrefix(match, "```yaml\n"), "\n```")
		var parsed map[string]any
		if yaml.Unmarshal([]byte(body), &parsed) != nil || parsed == nil {
			return match
		}
		if t, _ := parsed["type"].(string); t != blockType {
			return match
		}
		if !index.SameBinder(index.AsString(parsed["binder"]), oldName) {
			return match
		}
		lines := strings.Split(body, "\n")
		ind := blockIndent(lines)
		for i, l := range lines {
			if keyLine(l, "binder", ind) {
				lines[i] = ind + yamlLine("binder", newName)
			}
		}
		changes++
		return "```yaml\n" + strings.Join(lines, "\n") + "\n```"
	})
	if changes > 0 {
		if err := writeNote(path, modified, crlf); err != nil {
			return 0, err
		}
	}
	return changes, nil
}

// yamlField renders one scalar "key: value" line.
func yamlField(key, value string) string {
	return key + ": " + yamlScalar(value) + "\n"
}

// yamlFieldVal renders a field that may be a scalar or an array (block sequence),
// the canonical dump order for the record fields.
func yamlFieldVal(key string, v any) string {
	if arr, ok := v.([]any); ok {
		var b strings.Builder
		b.WriteString(key + ":\n")
		for _, e := range arr {
			b.WriteString("- " + yamlScalar(index.AsString(e)) + "\n")
		}
		return b.String()
	}
	return yamlField(key, index.AsString(v))
}

// mdToMarkdown renders the full annotation block for a record
// (index.RefRecord#to_markdown): H3 = filename, then a YAML block with every present
// field. A plain function — index.RefRecord lives in the index core, and the Markdown
// rendering stays on this side of the seam.
func mdToMarkdown(r index.RefRecord) string {
	var b strings.Builder
	if r.Filename != "" {
		b.WriteString("### " + headingText(r.Filename) + "\n")
	}
	b.WriteString("```yaml\n")
	b.WriteString(yamlField("type", "ref"))
	b.WriteString(yamlField("id", r.ID))
	b.WriteString(yamlField("binder", r.Binder))
	if r.URL != "" {
		b.WriteString(yamlField("url", r.URL))
	}
	if r.Filename != "" {
		b.WriteString(yamlField("filename", r.Filename))
	}
	if r.Kind != "" {
		b.WriteString(yamlField("kind", r.Kind))
	}
	if index.NotEmptyVal(r.Aka) {
		b.WriteString(yamlFieldVal("aka", r.Aka))
	}
	if index.NotEmptyVal(r.Tags) {
		b.WriteString(yamlFieldVal("tags", r.Tags))
	}
	if r.Xattr != "" && r.Xattr != "itemprojects" {
		b.WriteString(yamlField("xattr", r.Xattr))
	}
	b.WriteString("```\n")
	return b.String()
}

// blockMatchesKeys reports whether a parsed ref block carries any of the
// record's identity keys (id ∪ aka) — in its id: or its aka: list. The one
// matching rule for "is this block the record's": mdAlreadyPresent,
// orderWriteOverride and annotate all match this way.
func blockMatchesKeys(parsed map[string]any, keys map[string]bool) bool {
	if keys[index.AsString(parsed["id"])] {
		return true
	}
	for _, a := range index.AsStrings(parsed["aka"]) {
		if a != "" && keys[a] {
			return true
		}
	}
	return false
}

// mdAlreadyPresent reports whether a block with the same binder and an overlapping
// id/aka identifier is already in the content (MdFileWriter#already_present?).
func mdAlreadyPresent(rec index.RefRecord, content string) bool {
	recIdents := map[string]bool{}
	if rec.ID != "" {
		recIdents[rec.ID] = true
	}
	for _, a := range index.AsStrings(rec.Aka) {
		if a != "" {
			recIdents[a] = true
		}
	}
	for _, m := range mdYamlBlockRe.FindAllStringSubmatch(content, -1) {
		var parsed map[string]any
		if yaml.Unmarshal([]byte(m[1]), &parsed) != nil {
			continue
		}
		if t, _ := parsed["type"].(string); t != "ref" {
			continue
		}
		if !index.SameBinder(index.AsString(parsed["binder"]), rec.Binder) {
			continue
		}
		for _, k := range append([]string{index.AsString(parsed["id"])}, index.AsStrings(parsed["aka"])...) {
			if k != "" && recIdents[k] {
				return true
			}
		}
	}
	return false
}

// mdFileWriteMany appends the records' annotation blocks to one Markdown file
// in a single read + write (idempotent: a duplicate id/aka under the same
// binder is a noop). Returns "appended"/"noop"/"failed" per record in order.
func mdFileWriteMany(recs []index.RefRecord, targetPath string) []string {
	actions := make([]string, len(recs))
	fail := func() []string {
		for i := range actions {
			actions[i] = "failed"
		}
		return actions
	}

	content, crlf, err := readNote(targetPath)
	if err != nil {
		if !os.IsNotExist(err) {
			// Existing note, unreadable — writing now would clobber it.
			fmt.Fprintf(os.Stderr, "  Error: reading %s failed: %v\n", targetPath, err)
			return fail()
		}
		os.MkdirAll(filepath.Dir(targetPath), 0755)
	}

	appended := 0
	for i, rec := range recs {
		if mdAlreadyPresent(rec, content) {
			actions[i] = "noop"
			continue
		}
		block := mdToMarkdown(rec)
		if content == "" {
			content = block
		} else {
			content = chomp(content) + "\n\n" + block
		}
		actions[i] = "appended"
		appended++
	}
	if appended == 0 {
		return actions
	}
	if err := writeNote(targetPath, content, crlf); err != nil {
		fmt.Fprintf(os.Stderr, "  Error: writing %s failed: %v\n", targetPath, err)
		return fail()
	}
	return actions
}

// mdDeleteBlockRe matches an optional heading + blank lines, a ```yaml block, and
// the trailing blank lines — so deleting a block leaves no gap (MdEditor.delete_block).
// Exactly ONE heading line, and only an entry heading (H3/H4): H1/H2 are
// document structure (SPEC), so a section title directly above a block that
// has no heading of its own stays.
var mdDeleteBlockRe = regexp.MustCompile("(?ms)((?:^#{3,4}[ \t][^\n]*\n)\n*)?^```yaml\n(.*?)\n^```[ \t]*(?:\n(?:[ \t]*\n)*)?")

// mdDeleteBlock removes the block(s) matching (id, binder) — and their preceding
// heading + trailing blanks — keeping the rest of the document byte-for-byte.
// The binder must match exactly: a blank binder matches only blocks that name
// none, never every block of the id. Returns the count of deleted blocks.
func mdDeleteBlock(path, id, binder string) (int, error) {
	content, crlf, err := readNote(path)
	if err != nil {
		return 0, err
	}
	locs := mdDeleteBlockRe.FindAllStringSubmatchIndex(content, -1)
	if len(locs) == 0 {
		return 0, nil
	}
	var b strings.Builder
	last := 0
	changes := 0
	for _, m := range locs {
		start, end := m[0], m[1]
		body := ""
		if m[4] >= 0 {
			body = content[m[4]:m[5]] // capture group 2 = yaml body
		}
		var parsed map[string]any
		del := false
		if yaml.Unmarshal([]byte(body), &parsed) == nil && parsed != nil {
			if t, _ := parsed["type"].(string); t == "ref" &&
				index.AsString(parsed["id"]) == id &&
				index.SameBinder(index.AsString(parsed["binder"]), binder) {
				del = true
			}
		}
		b.WriteString(content[last:start])
		if !del {
			b.WriteString(content[start:end])
		} else {
			changes++
		}
		last = end
	}
	b.WriteString(content[last:])
	if changes > 0 {
		if err := writeNote(path, b.String(), crlf); err != nil {
			return 0, err
		}
	}
	return changes, nil
}

// upsertFrontmatterField sets one key in a note's YAML frontmatter, creating
// the frontmatter when there is none. Returns "created" or "updated".
//
// The frontmatter is the note's header, and grubber passes every key in it down
// into each block of the file. That is what makes it the right place for a field
// all members share: nobody looks the value up, it arrives with every record.
// Only top-level scalar keys are touched; the rest of the header is left as it
// stands, including keys we know nothing about.
func upsertFrontmatterField(path, key, value string) (string, error) {
	content, crlf, err := readNote(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		os.MkdirAll(filepath.Dir(path), 0755)
		fm := "---\n" + yamlLine(key, value) + "\n---\n"
		if werr := index.AtomicWrite(path, []byte(fm)); werr != nil {
			return "", werr
		}
		return "created", nil
	}

	if fmLen := frontmatterLen(content); fmLen > 0 {
		head := content[:fmLen]
		lines := strings.Split(strings.TrimSuffix(head, "\n"), "\n")
		for i, l := range lines {
			if keyLine(l, key, "") {
				end := frontmatterKeyEnd(lines, i, len(lines)-1)
				lines = append(lines[:i], append([]string{yamlLine(key, value)}, lines[end:]...)...)
				out := strings.Join(lines, "\n") + "\n" + content[fmLen:]
				if out == content {
					return "updated", nil
				}
				if werr := writeNote(path, out, crlf); werr != nil {
					return "", werr
				}
				return "updated", nil
			}
		}
		// No such key yet: insert above the closing fence.
		closing := len(lines) - 1
		lines = append(lines[:closing], append([]string{yamlLine(key, value)}, lines[closing:]...)...)
		out := strings.Join(lines, "\n") + "\n" + content[fmLen:]
		if werr := writeNote(path, out, crlf); werr != nil {
			return "", werr
		}
		return "created", nil
	}

	// No frontmatter at all: give the note one.
	out := "---\n" + yamlLine(key, value) + "\n---\n\n" + content
	if werr := writeNote(path, out, crlf); werr != nil {
		return "", werr
	}
	return "created", nil
}

// removeFrontmatterField drops one key from a note's frontmatter, and the
// frontmatter with it when nothing else is left. Returns "removed", or "absent"
// when there was nothing to drop (no note, no header, no such key); the file is
// then left untouched.
func removeFrontmatterField(path, key string) (string, error) {
	content, crlf, err := readNote(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "absent", nil
		}
		return "", err
	}
	fmLen := frontmatterLen(content)
	if fmLen == 0 {
		return "absent", nil
	}
	lines := strings.Split(strings.TrimSuffix(content[:fmLen], "\n"), "\n")
	closing := len(lines) - 1
	for i := 1; i < closing; i++ {
		if !keyLine(lines[i], key, "") {
			continue
		}
		lines = append(lines[:i], lines[frontmatterKeyEnd(lines, i, closing):]...)
		body := content[fmLen:]
		out := strings.Join(lines, "\n") + "\n" + body
		if strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "")) == "" {
			// Nothing left in the header: drop it, and the blank line that
			// upsertFrontmatterField puts between header and body.
			out = strings.TrimPrefix(body, "\n")
		}
		if werr := writeNote(path, out, crlf); werr != nil {
			return "", werr
		}
		return "removed", nil
	}
	return "absent", nil
}

// frontmatterKeyEnd returns the index just past the key on line i and its
// value's continuation lines (indented, sequence items, blank lines between
// them), so that replacing or removing a key never leaves half of a multi-line
// value behind as broken YAML. closing is the index of the closing fence.
func frontmatterKeyEnd(lines []string, i, closing int) int {
	end := i + 1
	for j := i + 1; j < closing; j++ {
		if strings.TrimSpace(lines[j]) == "" {
			continue
		}
		if opensKey(lines[j], "") {
			break
		}
		end = j + 1
	}
	return end
}

// frontmatterLen returns the byte length of a leading YAML frontmatter
// (---\n…\n---\n) including the closing fence line, or 0 when there is none.
func frontmatterLen(content string) int {
	if !strings.HasPrefix(content, "---\n") {
		return 0
	}
	rest := content[4:]
	if idx := strings.Index(rest, "\n---\n"); idx >= 0 {
		return 4 + idx + len("\n---\n")
	}
	if strings.HasSuffix(rest, "\n---") {
		return len(content)
	}
	return 0
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
