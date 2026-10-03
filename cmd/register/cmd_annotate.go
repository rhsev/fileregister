package main

// cmd_annotate — edit an existing Markdown annotation block: set/unset custom
// fields in the YAML, optionally replace the section's prose. The GUI-safe
// counterpart of "the user edits it directly" (SPEC §Record Identity); no
// ancestor. Reserved keys are refused — identity and membership have their own
// verbs, the ordering key belongs to `order move`.
//
// Prose replacement is a deliberate policy change: register used to never
// touch prose. It may here, on explicit request (--prose), because register is
// the only Markdown writer of the family — see SPEC §register annotate.

import (
	"github.com/rhsev/fileregister/internal/index"

	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const annotateUsage = "Usage: register annotate <binder> <id|aka> [--set k=v]... [--unset k]... [--prose <text>|-]"

// reservedAnnotateKeys maps each refused key to the verb that owns it.
var reservedAnnotateKeys = map[string]string{
	"id":     "identity — register assigns it at add time",
	"type":   "record identity — never editable",
	"binder": "membership — use register add/remove",
	"aka":    "identity handle — use register aka --add/--remove",
	"sort":   "ordering key — use register order move",
}

type annotateSet struct{ key, value string }

func cmdAnnotate(args []string) int {
	vals, bools, pos, unk := parseFlagsMulti(args,
		map[string]bool{"--set": true, "--unset": true, "--prose": true}, nil)
	if unk != "" {
		return unknownOption("annotate", unk)
	}
	if bools["--help"] {
		fmt.Println(annotateUsage)
		return 0
	}
	if bools["--version"] {
		fmt.Println("register annotate " + registerVersion)
		return 0
	}
	if len(pos) > 2 {
		fmt.Fprintln(os.Stderr, annotateUsage)
		return 1
	}
	var binder, key string
	if len(pos) > 0 {
		binder = pos[0]
	}
	if len(pos) > 1 {
		key = pos[1]
	}
	var sets []annotateSet
	for _, v := range vals["--set"] {
		eq := strings.Index(v, "=")
		if eq <= 0 {
			fmt.Fprintf(os.Stderr, "Error: --set expects key=value, got '%s'\n", v)
			return 1
		}
		sets = append(sets, annotateSet{key: v[:eq], value: v[eq+1:]})
	}
	unsets := vals["--unset"]
	prose := ""
	hasProse := len(vals["--prose"]) > 0
	if hasProse {
		prose = vals["--prose"][len(vals["--prose"])-1]
	}

	if binder == "" || key == "" || (len(sets) == 0 && len(unsets) == 0 && !hasProse) {
		fmt.Fprintln(os.Stderr, annotateUsage)
		return 1
	}

	for _, s := range sets {
		if !validAnnotateKey(s.key, "--set") {
			return 1
		}
		if strings.ContainsAny(s.value, "\n\r") {
			fmt.Fprintf(os.Stderr, "Error: --set values are scalars — use --prose for text\n")
			return 1
		}
	}
	for _, k := range unsets {
		if !validAnnotateKey(k, "--unset") {
			return 1
		}
	}

	if hasProse && prose == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: reading prose from stdin: %v\n", err)
			return 1
		}
		prose = string(data)
	}
	prose = strings.TrimRight(prose, " \t\n")

	nd, err := notesDir()
	if err != nil {
		return 1
	}

	// Resolve the user key against the index; blocks are matched over id ∪ aka,
	// like every other block lookup (hand-written blocks may carry an aka in
	// their id: slot).
	rec := index.ResolveKey(mustRefs(nd), key)
	if rec == nil {
		fmt.Fprintf(os.Stderr, "Error: '%s' does not match any record id or aka\n", key)
		return 1
	}
	id := index.AsString(rec["id"])
	keys := map[string]bool{id: true}
	for _, a := range index.AkaList(rec) {
		if a != "" {
			keys[a] = true
		}
	}

	// Candidate files: every note carrying a matching (id ∪ aka, binder) block.
	seen := map[string]bool{}
	var files []string
	for _, r := range readAnnotations(nd) {
		if blockMatchesKeys(r, keys) && index.AsString(r["binder"]) == binder {
			f := index.AsString(r["_note_file"])
			if f != "" && !seen[f] {
				seen[f] = true
				files = append(files, f)
			}
		}
	}
	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "No annotation block for '%s' in binder '%s'.\n", key, binder)
		fmt.Fprintf(os.Stderr, "Create one first: register promote --binder %s --id %s\n", binder, id)
		return 1
	}

	fieldEdits := 0
	proseEdits := 0
	for _, f := range files {
		data, rerr := os.ReadFile(f)
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "Error: reading %s: %v\n", f, rerr)
			return 1
		}
		content := string(data)

		if len(sets) > 0 || len(unsets) > 0 {
			edited, n := annotateEditBlocks(content, keys, binder, sets, unsets)
			if n > 0 && edited != content {
				content = edited
				fieldEdits += n
			}
		}
		if hasProse {
			edited, n, perr := annotateReplaceProse(content, keys, binder, prose)
			if perr != nil {
				fmt.Fprintf(os.Stderr, "Error: %s: %v\n", filepath.Base(f), perr)
				return 1
			}
			if n > 0 {
				content = edited
				proseEdits += n
			}
		}
		if content != string(data) {
			if werr := index.AtomicWrite(f, []byte(content)); werr != nil {
				fmt.Fprintf(os.Stderr, "Error: writing %s failed: %v\n", f, werr)
				return 1
			}
		}
	}

	var parts []string
	if len(sets) > 0 || len(unsets) > 0 {
		parts = append(parts, fmt.Sprintf("%d block(s) edited", fieldEdits))
	}
	if hasProse {
		verb := "replaced"
		if prose == "" {
			verb = "cleared"
		}
		parts = append(parts, fmt.Sprintf("prose %s in %d section(s)", verb, proseEdits))
	}
	fmt.Printf("Annotated '%s' in binder '%s': %s\n", key, binder, strings.Join(parts, ", "))
	return 0
}

var annotateKeyRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

func validAnnotateKey(k, flag string) bool {
	if why, reserved := reservedAnnotateKeys[k]; reserved {
		fmt.Fprintf(os.Stderr, "Error: '%s' is reserved: %s\n", k, why)
		return false
	}
	if strings.HasPrefix(k, "_") {
		fmt.Fprintf(os.Stderr, "Error: '_'-prefixed keys are injected at read time, never stored\n")
		return false
	}
	if !annotateKeyRe.MatchString(k) {
		fmt.Fprintf(os.Stderr, "Error: %s key '%s' is not a plain YAML key\n", flag, k)
		return false
	}
	return true
}

// annotateValue renders a --set value: numbers and booleans stay plain (the
// user's `amount=129.50` means a number), everything else goes through the
// same quoting promote uses. Leading-zero numerals stay strings — YAML would
// reparse them as something else.
func annotateValue(v string) string {
	if v == "true" || v == "false" {
		return v
	}
	if _, err := strconv.ParseFloat(v, 64); err == nil {
		if !(len(v) > 1 && v[0] == '0' && v[1] != '.') &&
			!(len(v) > 2 && (v[0] == '-' || v[0] == '+') && v[1] == '0' && v[2] != '.') {
			return v
		}
	}
	return yamlScalar(v)
}

// annotateEditBlocks applies set/unset to every matching (id ∪ aka, binder)
// ref block, preserving unrelated lines byte-identically. Returns the edited
// content and the count of changed blocks.
func annotateEditBlocks(content string, keys map[string]bool, binder string, sets []annotateSet, unsets []string) (string, int) {
	changed := 0
	edited := mdYamlBlockRe.ReplaceAllStringFunc(content, func(match string) string {
		body := strings.TrimSuffix(strings.TrimPrefix(match, "```yaml\n"), "\n```")
		var parsed map[string]any
		if yaml.Unmarshal([]byte(body), &parsed) != nil || parsed == nil {
			return match
		}
		if t, _ := parsed["type"].(string); t != "ref" {
			return match
		}
		if !blockMatchesKeys(parsed, keys) || index.AsString(parsed["binder"]) != binder {
			return match
		}

		lines := strings.Split(body, "\n")
		ind := blockIndent(lines)
		var out []string
		replaced := map[string]bool{}
		skip := false
		for _, l := range lines {
			// A removed/replaced key swallows its value lines (a sequence, a
			// multi-line scalar) up to the next top-level key.
			if skip {
				if !opensKey(l, ind) {
					continue
				}
				skip = false
			}
			handled := false
			for _, s := range sets {
				if keyLine(l, s.key, ind) {
					out = append(out, ind+s.key+": "+annotateValue(s.value))
					replaced[s.key] = true
					skip = true
					handled = true
					break
				}
			}
			if handled {
				continue
			}
			for _, k := range unsets {
				if keyLine(l, k, ind) {
					skip = true
					handled = true
					break
				}
			}
			if handled {
				continue
			}
			out = append(out, l)
		}
		for _, s := range sets {
			if !replaced[s.key] {
				out = append(out, ind+s.key+": "+annotateValue(s.value))
			}
		}

		// Verify the surgery touched only the keys asked for; otherwise leave
		// the block as it was (an unusual layout is better edited by hand).
		newBody := strings.Join(out, "\n")
		if !annotateOnlyTargetsChanged(parsed, newBody, sets, unsets) {
			fmt.Fprintf(os.Stderr, "  Warning: block for id %s left unchanged (unusual layout) — edit by hand\n",
				index.AsString(parsed["id"]))
			return match
		}
		repl := "```yaml\n" + newBody + "\n```"
		if repl != match {
			changed++
		}
		return repl
	})
	return edited, changed
}

// annotateOnlyTargetsChanged re-parses an edited block and checks that every
// key not being set or unset kept its value, unset keys are gone and set keys
// are present.
func annotateOnlyTargetsChanged(before map[string]any, newBody string, sets []annotateSet, unsets []string) bool {
	var after map[string]any
	if yaml.Unmarshal([]byte(newBody), &after) != nil || after == nil {
		return false
	}
	touched := map[string]bool{}
	for _, s := range sets {
		touched[s.key] = true
		if _, ok := after[s.key]; !ok {
			return false
		}
	}
	for _, k := range unsets {
		touched[k] = true
		if _, ok := after[k]; ok && !isSetKey(sets, k) {
			return false
		}
	}
	for k, v := range before {
		if touched[k] {
			continue
		}
		if !reflect.DeepEqual(after[k], v) {
			return false
		}
	}
	for k := range after {
		if _, ok := before[k]; !ok && !touched[k] {
			return false
		}
	}
	return true
}

func isSetKey(sets []annotateSet, k string) bool {
	for _, s := range sets {
		if s.key == k {
			return true
		}
	}
	return false
}

// headingLevelOf returns the Markdown heading level of a line (1–6), or 0.
func headingLevelOf(line string) int {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	if n < 1 || n > 6 {
		return 0
	}
	if n == len(line) || line[n] == ' ' {
		return n
	}
	return 0
}

// annotateReplaceProse rewrites the section around every matching block:
// heading and fenced yaml/yml blocks stay verbatim (in order), everything
// else — the prose, wherever it sat — is replaced by the given text, placed
// between heading and blocks. A section runs from the block's nearest
// preceding heading to the NEXT heading of any level: stopping at any
// heading keeps sections disjoint, so a nested subsection is never rebuilt
// (and its heading destroyed) from an enclosing one, and the back-to-front
// rebuild never works on stale indexes. Returns (content, sections changed,
// error).
func annotateReplaceProse(content string, keys map[string]bool, binder, prose string) (string, int, error) {
	trailingNL := strings.HasSuffix(content, "\n")
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")

	// Fence spans: [start, end] line indexes of ```yaml/```yml blocks.
	type span struct {
		start, end int
		matched    bool
	}
	var spans []span
	for i := 0; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if t != "```yaml" && t != "```yml" {
			continue
		}
		j := i + 1
		for j < len(lines) && strings.TrimSpace(lines[j]) != "```" {
			j++
		}
		if j >= len(lines) {
			break // unterminated fence — leave the tail alone
		}
		body := strings.Join(lines[i+1:j], "\n")
		var parsed map[string]any
		matched := false
		if yaml.Unmarshal([]byte(body), &parsed) == nil && parsed != nil {
			if t, _ := parsed["type"].(string); t == "ref" &&
				blockMatchesKeys(parsed, keys) &&
				index.AsString(parsed["binder"]) == binder {
				matched = true
			}
		}
		spans = append(spans, span{start: i, end: j, matched: matched})
		i = j
	}

	// A column-0 `#` line INSIDE a fenced block is a YAML comment, not a
	// heading — the heading scans must skip fence interiors, or a `# TODO`
	// line in a matched block would be read as an H1 and split the section
	// mid-fence, erasing the block on rebuild.
	insideFence := func(i int) bool {
		for _, s := range spans {
			if i >= s.start && i <= s.end {
				return true
			}
		}
		return false
	}
	headingAt := func(i int) int {
		if insideFence(i) {
			return 0
		}
		return headingLevelOf(lines[i])
	}

	// Section ranges around matched blocks, deduped (two matched blocks under
	// one heading rebuild once).
	type section struct{ start, end int }
	var sections []section
	for _, s := range spans {
		if !s.matched {
			continue
		}
		headIdx := -1
		for i := s.start - 1; i >= 0; i-- {
			if headingAt(i) > 0 {
				headIdx = i
				break
			}
		}
		if headIdx < 0 {
			return "", 0, fmt.Errorf("block for the record has no preceding heading — edit the note by hand")
		}
		end := len(lines)
		for i := headIdx + 1; i < len(lines); i++ {
			if headingAt(i) > 0 {
				end = i
				break
			}
		}
		if n := len(sections); n > 0 && sections[n-1].start == headIdx {
			continue
		}
		sections = append(sections, section{start: headIdx, end: end})
	}
	if len(sections) == 0 {
		return content, 0, nil
	}

	// Rebuild back-to-front so earlier indexes stay valid.
	for si := len(sections) - 1; si >= 0; si-- {
		sec := sections[si]
		var rebuilt []string
		rebuilt = append(rebuilt, lines[sec.start]) // heading
		rebuilt = append(rebuilt, "")
		if prose != "" {
			rebuilt = append(rebuilt, strings.Split(prose, "\n")...)
			rebuilt = append(rebuilt, "")
		}
		first := true
		for _, s := range spans {
			if s.start < sec.start || s.end >= sec.end {
				continue
			}
			if !first {
				rebuilt = append(rebuilt, "")
			}
			rebuilt = append(rebuilt, lines[s.start:s.end+1]...)
			first = false
		}
		if sec.end < len(lines) {
			rebuilt = append(rebuilt, "") // blank line before the next section
		}
		lines = append(lines[:sec.start], append(rebuilt, lines[sec.end:]...)...)
	}

	out := strings.Join(lines, "\n")
	if trailingNL {
		out += "\n"
	}
	return out, len(sections), nil
}
