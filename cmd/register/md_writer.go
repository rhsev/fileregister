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
func yamlScalar(s string) string {
	if needsYAMLQuote(s) {
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
	return s
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
	return "### " + header + "\n```yaml\n" + yaml + "```\n"
}

// mdParseBlocks parses every fenced yaml block in one Markdown file — one read
// serves all block consumers. Nil on read error or unparseable blocks skipped.
func mdParseBlocks(path string) []map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []map[string]any
	for _, m := range mdYamlBlockRe.FindAllStringSubmatch(string(data), -1) {
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
			if index.AsString(r["binder"]) != binder {
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
		if data, err := os.ReadFile(mdTarget); err == nil {
			content = chomp(string(data)) + "\n\n" + body
		} else if !os.IsNotExist(err) {
			// The note exists but can't be read — appending blind would
			// replace it with only the new blocks. Refuse.
			fmt.Fprintf(os.Stderr, "  Error: reading %s failed: %v\n", mdTarget, err)
			return 0, noop, failed + promoted
		}
		if err := index.AtomicWrite(mdTarget, []byte(content)); err != nil {
			fmt.Fprintf(os.Stderr, "  Error: writing %s failed: %v\n", mdTarget, err)
			return 0, noop, failed + promoted
		}
	}
	return promoted, noop, failed
}

// keyLine reports whether a line is a top-level YAML key line (\A\s*key\s*:),
// as tolerant as the parser so edits don't silently no-op on hand-formatted blocks.
func keyLine(line, key string) bool {
	s := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(s, key) {
		return false
	}
	s = strings.TrimLeft(s[len(key):], " \t")
	return strings.HasPrefix(s, ":")
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
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	changes := 0
	modified := mdYamlBlockRe.ReplaceAllStringFunc(string(data), func(match string) string {
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
		if err := index.AtomicWrite(path, []byte(modified)); err != nil {
			return 0, err
		}
	}
	return changes, nil
}

// mdRenameBinder rewrites the binder: line in every block whose binder == oldName.
func mdRenameBinder(path, oldName, newName string) (int, error) {
	return mdTransformFile(path, func(lines []string, parsed map[string]any) (string, bool) {
		if index.AsString(parsed["binder"]) != oldName {
			return "", false
		}
		out := make([]string, len(lines))
		for i, l := range lines {
			if keyLine(l, "binder") {
				out[i] = yamlLine("binder", newName)
			} else {
				out[i] = l
			}
		}
		body := strings.Join(out, "\n")
		// keyLine is indentation-tolerant, so a binder: nested inside a custom
		// sub-mapping matches too — verify the surgery still parses to the
		// renamed record before accepting it.
		var check map[string]any
		if yaml.Unmarshal([]byte(body), &check) != nil || index.AsString(check["binder"]) != newName {
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
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	changes := 0
	modified := mdYamlBlockRe.ReplaceAllStringFunc(string(data), func(match string) string {
		body := strings.TrimSuffix(strings.TrimPrefix(match, "```yaml\n"), "\n```")
		var parsed map[string]any
		if yaml.Unmarshal([]byte(body), &parsed) != nil || parsed == nil {
			return match
		}
		if t, _ := parsed["type"].(string); t != "ordering" {
			return match
		}
		if index.AsString(parsed["binder"]) != oldName {
			return match
		}
		lines := strings.Split(body, "\n")
		for i, l := range lines {
			if keyLine(l, "binder") {
				lines[i] = yamlLine("binder", newName)
			}
		}
		changes++
		return "```yaml\n" + strings.Join(lines, "\n") + "\n```"
	})
	if changes > 0 {
		if err := index.AtomicWrite(path, []byte(modified)); err != nil {
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
		b.WriteString("### " + r.Filename + "\n")
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
		if index.AsString(parsed["binder"]) != rec.Binder {
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

	content := ""
	if data, err := os.ReadFile(targetPath); err == nil {
		content = string(data)
	} else if os.IsNotExist(err) {
		os.MkdirAll(filepath.Dir(targetPath), 0755)
	} else {
		// Existing note, unreadable — writing now would clobber it.
		fmt.Fprintf(os.Stderr, "  Error: reading %s failed: %v\n", targetPath, err)
		return fail()
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
	if err := index.AtomicWrite(targetPath, []byte(content)); err != nil {
		fmt.Fprintf(os.Stderr, "  Error: writing %s failed: %v\n", targetPath, err)
		return fail()
	}
	return actions
}

// mdDeleteBlockRe matches an optional heading + blank lines, a ```yaml block, and
// the trailing blank lines — so deleting a block leaves no gap (MdEditor.delete_block).
// Exactly ONE heading line: the block's own header. A greedy (^#…\n)+ would also
// swallow a section title sitting directly above it.
var mdDeleteBlockRe = regexp.MustCompile("(?ms)((?:^#[^\n]*\n)\n*)?^```yaml\n(.*?)\n^```[ \t]*(?:\n(?:[ \t]*\n)*)?")

// mdDeleteBlock removes the block(s) matching (id, binder) — and their preceding
// heading + trailing blanks — keeping the rest of the document byte-for-byte.
// A blank binder matches any. Returns the count of deleted blocks.
func mdDeleteBlock(path, id, binder string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	content := string(data)
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
				(binder == "" || index.AsString(parsed["binder"]) == binder) {
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
		if err := index.AtomicWrite(path, []byte(b.String())); err != nil {
			return 0, err
		}
	}
	return changes, nil
}

// orderingBlock renders the ordering config block (no trailing newline).
func orderingBlock(binder, rule string) string {
	yaml := yamlField("type", "ordering") + yamlField("binder", binder) +
		yamlField("rule", rule)
	return "### · " + binder + " (ordering)\n```yaml\n" + yaml + "```"
}

// upsertOrderingConfig creates or updates the ordering config block. Returns
// "created" or "updated". A legacy kind: line in an existing block is left
// alone — the field is inert since orderings became absolute-only.
func upsertOrderingConfig(path, binder, rule string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			// Existing note, unreadable — creating now would clobber it.
			return "", err
		}
		os.MkdirAll(filepath.Dir(path), 0755)
		if werr := index.AtomicWrite(path, []byte(orderingBlock(binder, orDefault(rule, "name"))+"\n")); werr != nil {
			return "", werr
		}
		return "created", nil
	}
	content := string(data)

	found := false
	modified := mdYamlBlockRe.ReplaceAllStringFunc(content, func(match string) string {
		if found {
			return match
		}
		body := strings.TrimSuffix(strings.TrimPrefix(match, "```yaml\n"), "\n```")
		var parsed map[string]any
		if yaml.Unmarshal([]byte(body), &parsed) != nil || parsed == nil {
			return match
		}
		if t, _ := parsed["type"].(string); t != "ordering" {
			return match
		}
		found = true
		if rule == "" {
			return match
		}
		lines := strings.Split(body, "\n")
		replaced := false
		for i, l := range lines {
			if keyLine(l, "rule") {
				lines[i] = yamlLine("rule", rule)
				replaced = true
			}
		}
		if !replaced {
			lines = append(lines, yamlLine("rule", rule))
		}
		return "```yaml\n" + strings.Join(lines, "\n") + "\n```"
	})

	if found {
		if modified != content {
			if werr := index.AtomicWrite(path, []byte(modified)); werr != nil {
				return "", werr
			}
		}
		return "updated", nil
	}
	// No ordering block yet — prepend one, below any YAML frontmatter (a block
	// above the opening --- would stop the frontmatter being parsed at all).
	block := orderingBlock(binder, orDefault(rule, "name"))
	out := block + "\n\n" + content
	if fm := frontmatterLen(content); fm > 0 {
		out = content[:fm] + "\n" + block + "\n\n" + content[fm:]
	}
	if werr := index.AtomicWrite(path, []byte(out)); werr != nil {
		return "", werr
	}
	return "created", nil
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
