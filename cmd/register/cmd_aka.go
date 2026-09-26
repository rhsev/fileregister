package main

// cmd_aka — add or remove a record's aka handles after add time. The identity
// verb annotate refuses to be: aka lives on the index record (all binders at
// once), must stay unique across the index, and is one of the keys Markdown
// blocks are matched by (id ∪ aka).
//
// On remove, the record's blocks are cleaned up: the handle leaves their aka:
// list (a later re-use of it cannot match them), and a block that reached the
// record only through the handle — hand-written, aka in the id: slot or an
// aka-only block — gets the real id. Without it the block would lose its
// record, and reindex would mint a phantom record from an aka in the id: slot.
// A block carrying the handle that is NOT the record's (another record's id,
// an id no index record knows) is ambiguous: both directions refuse on it.

import (
	"github.com/rhsev/fileregister/internal/index"

	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const akaUsage = "Usage: register aka <id|aka> [--add <handle>]... [--remove <handle>]..."

func cmdAka(args []string) int {
	vals, bools, pos, unk := parseFlagsMulti(args,
		map[string]bool{"--add": true, "--remove": true}, nil)
	if unk != "" {
		return unknownOption("aka", unk)
	}
	if bools["--help"] {
		fmt.Println(akaUsage)
		return 0
	}
	if bools["--version"] {
		fmt.Println("register aka " + registerVersion)
		return 0
	}
	adds, removes := uniqStrings(vals["--add"], false), uniqStrings(vals["--remove"], false)
	if len(pos) != 1 || (len(adds) == 0 && len(removes) == 0) {
		fmt.Fprintln(os.Stderr, akaUsage)
		return 1
	}
	key := pos[0]
	for _, h := range append(append([]string{}, adds...), removes...) {
		if strings.TrimSpace(h) == "" || strings.ContainsAny(h, "\n\r") {
			fmt.Fprintf(os.Stderr, "Error: '%s' is not a usable handle\n", h)
			return 1
		}
	}
	for _, h := range adds {
		if containsStr(removes, h) {
			fmt.Fprintf(os.Stderr, "Error: '%s' given to both --add and --remove\n", h)
			return 1
		}
	}

	nd, err := notesDir()
	if err != nil {
		return 1
	}
	refs := mustRefs(nd)
	rec := index.ResolveKey(refs, key)
	if rec == nil {
		fmt.Fprintf(os.Stderr, "Error: '%s' does not match any record id or aka\n", key)
		return 1
	}
	id := index.AsString(rec["id"])
	label := refLabel(rec)
	current := index.AkaList(rec)

	// Plan: sort each handle into apply / noop, refusing index clashes.
	var toAdd, toRemove []string
	for _, h := range adds {
		if containsStr(current, h) {
			fmt.Printf("aka '%s' already on %s\n", h, label)
			continue
		}
		if clash := index.ResolveKey(refs, h); clash != nil {
			if index.AsString(clash["id"]) == id {
				fmt.Fprintf(os.Stderr, "Error: '%s' is the record's id, not a handle\n", h)
			} else {
				fmt.Fprintf(os.Stderr, "Error: aka '%s' already resolves to record %s (binder '%s')\n",
					h, index.AsString(clash["id"]), strings.Join(index.AsStrings(clash["binder"]), ", "))
			}
			return 1
		}
		toAdd = append(toAdd, h)
	}
	for _, h := range removes {
		if !containsStr(current, h) {
			fmt.Printf("aka '%s' not on %s — nothing to remove\n", h, label)
			continue
		}
		toRemove = append(toRemove, h)
	}
	if len(toAdd) == 0 && len(toRemove) == 0 {
		return 0
	}

	// Markdown guard: no block may depend on a handle that changes owner.
	blocks := readAnnotations(nd)
	if conflicts := akaBlockConflicts(blocks, id, current, append(append([]string{}, toAdd...), toRemove...)); len(conflicts) > 0 {
		fmt.Fprintln(os.Stderr, "Error: Markdown blocks carry the handle but belong to no or another record:")
		for _, c := range conflicts {
			fmt.Fprintln(os.Stderr, "  "+c)
		}
		fmt.Fprintln(os.Stderr, "Fix their id: by hand, then retry.")
		return 1
	}

	// Markdown first: a block that lost the handle while the index still has
	// it is harmless (the id matches); the reverse would leave a stale handle.
	mdEdits := 0
	if len(toRemove) > 0 {
		seen := map[string]bool{}
		for _, b := range blocks {
			f := index.AsString(b["_note_file"])
			if seen[f] || !akaBlockOwned(b, id, current) || !akaBlockTouched(b, toRemove) {
				continue
			}
			seen[f] = true
			n, werr := mdStripAka(f, id, current, toRemove)
			if werr != nil {
				fmt.Fprintf(os.Stderr, "Error: %s: %v\n", filepath.Base(f), werr)
				return 1
			}
			mdEdits += n
		}
	}

	// Index: every JSONL file holding the id (normally one).
	next := []any{}
	for _, a := range current {
		if !containsStr(toRemove, a) {
			next = append(next, a)
		}
	}
	for _, a := range toAdd {
		next = append(next, a)
	}
	files := map[string]bool{}
	for _, r := range refs {
		if index.AsString(r["id"]) == id {
			files[index.AsString(r["_note_file"])] = true
		}
	}
	for f := range files {
		if _, werr := index.JSONLRewrite(f, func(r map[string]any) (map[string]any, bool) {
			if index.AsString(r["id"]) != id {
				return r, false
			}
			out := map[string]any{}
			for k, v := range r {
				out[k] = v
			}
			if len(next) == 0 {
				delete(out, "aka")
			} else {
				out["aka"] = next
			}
			return out, true
		}); werr != nil {
			fmt.Fprintf(os.Stderr, "Error: writing %s failed: %v\n", f, werr)
			return 1
		}
	}

	for _, h := range toAdd {
		fmt.Printf("aka '%s' added to %s\n", h, label)
	}
	for _, h := range toRemove {
		fmt.Printf("aka '%s' removed from %s\n", h, label)
	}
	if mdEdits > 0 {
		fmt.Printf("  %d Markdown block(s) updated\n", mdEdits)
	}
	return 0
}

// akaBlockOwned reports whether a ref block belongs to the record: its id:
// is the record's id or one of its handles, or it has no id and reaches the
// record through a handle. current is the handle list BEFORE the change.
func akaBlockOwned(b map[string]any, id string, current []string) bool {
	bid := index.AsString(b["id"])
	if bid == id || (bid != "" && containsStr(current, bid)) {
		return true
	}
	return bid == "" && anyIn(index.AsStrings(b["aka"]), current)
}

// akaBlockTouched reports whether a block carries one of the handles, in its
// id: slot or its aka: list.
func akaBlockTouched(b map[string]any, handles []string) bool {
	return containsStr(handles, index.AsString(b["id"])) || anyIn(index.AsStrings(b["aka"]), handles)
}

// akaBlockConflicts lists the blocks that carry one of the changing handles
// but are not the record's — blocks the change would silently adopt (add) or
// hand to nobody (remove).
func akaBlockConflicts(blocks []map[string]any, id string, current, handles []string) []string {
	var out []string
	for _, b := range blocks {
		if !akaBlockTouched(b, handles) || akaBlockOwned(b, id, current) {
			continue
		}
		bid := index.AsString(b["id"])
		if bid == "" {
			bid = "(none)"
		}
		out = append(out, fmt.Sprintf("%s: binder '%s', id: %s",
			filepath.Base(index.AsString(b["_note_file"])), index.AsString(b["binder"]), bid))
	}
	return out
}

// mdStripAka cleans the record's blocks in one note: the handles leave the
// aka: list (an emptied list drops the key), and a block whose id: is one of
// them — or that has none — gets the real id. All other lines stay verbatim.
func mdStripAka(path, id string, current, handles []string) (int, error) {
	orphaned := 0
	n, err := mdTransformFile(path, func(lines []string, parsed map[string]any) (string, bool) {
		if !akaBlockOwned(parsed, id, current) || !akaBlockTouched(parsed, handles) {
			return "", false
		}
		keep := []any{}
		for _, a := range index.AsStrings(parsed["aka"]) {
			if !containsStr(handles, a) {
				keep = append(keep, a)
			}
		}
		wantID := index.AsString(parsed["id"])
		setID := wantID == "" || containsStr(handles, wantID)
		if setID {
			wantID = id
		}

		var out []string
		skip, idDone := false, !setID
		for _, l := range lines {
			// The aka: key swallows its block-sequence items up to the next key line.
			if skip {
				if !yamlKeyLineRe.MatchString(l) {
					continue
				}
				skip = false
			}
			switch {
			case keyLine(l, "aka"):
				if len(keep) > 0 {
					out = append(out, strings.Split(strings.TrimSuffix(yamlFieldVal("aka", keep), "\n"), "\n")...)
				}
				skip = true
			case setID && keyLine(l, "id") && l == strings.TrimLeft(l, " \t"):
				out = append(out, yamlLine("id", id))
				idDone = true
			default:
				out = append(out, l)
			}
		}
		if !idDone {
			// aka-only block: the id goes right after type:, where promote puts it.
			at := 0
			for i, l := range out {
				if keyLine(l, "type") {
					at = i + 1
					break
				}
			}
			out = append(out[:at], append([]string{yamlLine("id", id)}, out[at:]...)...)
		}
		body := strings.Join(out, "\n")
		// keyLine is indentation-tolerant — verify the surgery still parses to
		// the intended id and reduced list before accepting it.
		var check map[string]any
		if yaml.Unmarshal([]byte(body), &check) != nil || index.AsString(check["id"]) != wantID ||
			strings.Join(index.AsStrings(check["aka"]), "\x00") != strings.Join(index.AsStrings(keep), "\x00") {
			fmt.Fprintf(os.Stderr, "  Warning: block for id %s in %s left unchanged (unusual layout) — edit by hand\n",
				id, filepath.Base(path))
			if setID {
				orphaned++
			}
			return "", false
		}
		return "```yaml\n" + body + "\n```", true
	})
	if err == nil && orphaned > 0 {
		// The block still needs the handle — keep it in the index.
		err = fmt.Errorf("%d block(s) could not be given the id; index left unchanged", orphaned)
	}
	return n, err
}

// anyIn reports whether any element of xs is in set.
func anyIn(xs, set []string) bool {
	for _, x := range xs {
		if containsStr(set, x) {
			return true
		}
	}
	return false
}

// warnAkaNotApplied tells add's caller when --aka did not land: an existing
// record keeps its fields (the index only backfills an absent aka on a new
// membership), so the handle needs the identity verb instead.
func warnAkaNotApplied(nd, handle, id string) {
	if rec := index.ResolveKey(mustRefs(nd), handle); rec != nil && index.AsString(rec["id"]) == id {
		return
	}
	fmt.Fprintf(os.Stderr, "  Note: record %s already existed — --aka '%s' not applied; use: register aka %s --add %s\n",
		id, handle, id, handle)
}
