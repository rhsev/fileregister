package main

// cmd_annotate — edit an existing Markdown annotation block: set/unset custom
// fields in the YAML, optionally replace the section's prose. The GUI-safe
// counterpart of "the user edits it directly" (SPEC §Record Identity); no
// ancestor. Reserved keys are refused — identity and membership have their own
// verbs, the ordering key belongs to `order move`.
//
// Prose replacement follows grubber's package entry, the rules commenter
// applies as well, so register and every other tool agree on where an entry's
// prose starts and ends — see SPEC §register annotate.

import (
	"github.com/rhsev/fileregister/v2/internal/index"
	"github.com/rhsev/grubber/entry"

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
	allRefs, refsOK := loadRefs(nd)
	if !refsOK {
		return 1
	}
	rec := index.ResolveKey(allRefs, key)
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
	annos, aerr := readAnnotations(nd)
	if aerr != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", aerr)
		return 1
	}
	for _, r := range annos {
		if blockMatchesKeys(r, keys) && index.SameBinder(index.AsString(r["binder"]), binder) {
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
		original, crlf, rerr := readNote(f)
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "Error: reading %s: %v\n", f, rerr)
			return 1
		}
		content := original

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
		if content != original {
			if werr := writeNote(f, content, crlf); werr != nil {
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
		parts = append(parts, fmt.Sprintf("prose %s for %d block(s)", verb, proseEdits))
	}
	fmt.Printf("Annotated '%s' in binder '%s': %s\n", key, binder, strings.Join(parts, ", "))
	return 0
}

var annotateKeyRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// annotateDateRe: a calendar date, which YAML reads as a date when plain.
var annotateDateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

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

// annotateValue renders a --set value: numbers, booleans and dates stay plain
// (the user's `amount=129.50` means a number, `due=2026-08-01` a date),
// everything else goes through the same quoting promote uses. Leading-zero
// numerals stay strings — YAML would reparse them as something else.
func annotateValue(v string) string {
	if v == "true" || v == "false" || annotateDateRe.MatchString(v) {
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
		if !blockMatchesKeys(parsed, keys) || !index.SameBinder(index.AsString(parsed["binder"]), binder) {
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

// annotateReplaceProse replaces the prose of every matching (id ∪ aka,
// binder) ref block. Where an entry's prose starts and ends is grubber's
// package entry: the text between the block and the nearest boundary above it
// (a heading of any level, another yaml block, a --- line, the frontmatter's
// end). Text after a block belongs to no entry and stays. A prose holding a
// heading, a --- line or a yaml block is refused, because it would move a
// boundary. Returns (content, blocks changed, error).
func annotateReplaceProse(content string, keys map[string]bool, binder, prose string) (string, int, error) {
	n := &entry.Note{Lines: strings.Split(content, "\n")}
	blocks := entry.ParseBlocks(n.Lines)
	var hits []int
	for i, b := range blocks {
		if t, _ := b.Fields["type"].(string); t == "ref" &&
			blockMatchesKeys(b.Fields, keys) &&
			index.SameBinder(index.AsString(b.Fields["binder"]), binder) {
			hits = append(hits, i)
		}
	}
	if len(hits) == 0 {
		return content, 0, nil
	}
	out, _, err := n.SetProse(blocks, hits, prose)
	if err != nil {
		return "", 0, err
	}
	return strings.Join(out, "\n"), len(hits), nil
}
