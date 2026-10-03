package main

// cmd_remove — remove a file (or URL) from a binder.
//
// Set model: the index is the membership truth — set-delete the binder there.
// Markdown context blocks are left untouched (register cleanup reviews them).

import (
	"github.com/rhsev/fileregister/internal/index"

	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func cmdRemove(args []string) int {
	binder := ""
	hasBinder := false
	var positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--binder" && i+1 < len(args):
			i++
			binder = args[i]
			hasBinder = true
		case strings.HasPrefix(a, "--binder="):
			binder = strings.TrimPrefix(a, "--binder=")
			hasBinder = true
		case a == "-h" || a == "--help":
			fmt.Println("Usage: register remove <file>... --binder <name>")
			return 0
		case a == "-v" || a == "--version":
			fmt.Println("register remove " + registerVersion)
			return 0
		default:
			if strings.HasPrefix(a, "-") {
				return unknownOption("remove", a)
			}
			positional = append(positional, a)
		}
	}

	if !hasBinder {
		binder = os.Getenv("REGISTER_BINDER")
		hasBinder = binder != ""
	}
	if !hasBinder {
		fmt.Fprintln(os.Stderr, "Error: --binder is required (or set REGISTER_BINDER env var)")
		fmt.Fprintln(os.Stderr, "Usage: register remove <file>... --binder <name>")
		return 1
	}
	if len(positional) == 0 {
		fmt.Fprintln(os.Stderr, "Error: file argument required")
		fmt.Fprintln(os.Stderr, "Usage: register remove <file>... --binder <name>")
		return 1
	}

	nd, err := notesDir()
	if err != nil {
		return 1
	}

	// Each target runs the full flow; the index is re-read per target so a
	// removal is visible to the next one (e.g. the ★ membership check).
	worst := 0
	for _, targetArg := range positional {
		if code := removeTarget(nd, binder, targetArg); code > worst {
			worst = code
		}
	}
	return worst
}

// removeTarget removes one file (or URL) from the binder; returns the exit code.
// anyKeptTag reports whether any of the records keeps tag as the user's own.
func anyKeptTag(recs []map[string]any, tag string) bool {
	for _, r := range recs {
		if index.IsKeptTag(r, tag) {
			return true
		}
	}
	return false
}

func removeTarget(nd, binder, targetArg string) int {
	urlMode := urlRe.MatchString(targetArg)
	filePath := ""
	display := targetArg
	if !urlMode {
		filePath = index.ExpandPath(targetArg)
		display = filepath.Base(filePath)
	}

	if !urlMode && !index.FileExists(filePath) {
		fmt.Fprintf(os.Stderr, "Error: file not found: %s\n", filePath)
		return 1
	}

	// The bookmark db tells a registered file from a copy of it (OwnIDs).
	var db map[string]string
	if !urlMode {
		var err error
		if db, err = index.LoadDB(); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			return 1
		}
	}

	allRefs, refsOK := loadRefs(nd)
	if !refsOK {
		return 1
	}
	records := recordsForBinder(allRefs, binder)
	if len(records) == 0 {
		fmt.Fprintf(os.Stderr, "No refs found for binder '%s'\n", binder)
		return 0
	}

	// URL refs match on url; files match by the id stamped on the file, falling
	// back to a batched resolve + path compare when the file carries no id xattr.
	var matching []map[string]any
	if urlMode {
		for _, rec := range records {
			if index.AsString(rec["url"]) == targetArg {
				matching = append(matching, rec)
			}
		}
	} else if fileIDs := index.OwnIDs(db, filePath); len(fileIDs) > 0 {
		idSet := map[string]bool{}
		for _, id := range fileIDs {
			idSet[id] = true
		}
		for _, rec := range records {
			if idSet[index.AsString(rec["id"])] {
				matching = append(matching, rec)
			}
		}
	} else {
		resolved, rerr := resolveRecordPaths(records)
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "Error: fileanchor batch resolve failed: %v\n", rerr)
			return 1
		}
		fileKey := index.PathKey(filePath) // one symlink walk, not one per record
		for _, rec := range records {
			if path := resolved[index.AsString(rec["id"])]; path != "" && index.PathKey(path) == fileKey {
				matching = append(matching, rec)
			}
		}
	}

	if len(matching) == 0 {
		fmt.Fprintf(os.Stderr, "Not a member of binder '%s': %s\n", binder, display)
		return 0
	}

	// Derive the xattr backend(s) from the index records.
	backendsUsed := uniqStrings(mapBackends(matching), false)
	if len(backendsUsed) == 0 {
		backendsUsed = []string{"itemprojects"}
	}

	totalRemoved := 0
	writeFailed := false
	for _, note := range groupByNoteFile(matching) {
		idSet := map[string]bool{}
		for _, rec := range note.recs {
			idSet[index.AsString(rec["id"])] = true
		}
		count, err := index.JSONLRemoveBinderMany(note.file, idSet, binder)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: writing %s failed: %v\n", note.file, err)
			writeFailed = true
			continue
		}
		totalRemoved += count
		if count > 0 {
			fmt.Printf("Removed '%s' from %d record(s) in %s\n", binder, count, filepath.Base(note.file))
		}
	}
	if writeFailed {
		return 1
	}

	// URL refs carry no xattr layer and no ★ — the set-delete is everything.
	if !urlMode {
		for _, backend := range backendsUsed {
			// A tag the user had set before the file joined the binder stays.
			if backend == "tags" && anyKeptTag(matching, binder) {
				fmt.Printf("kMDItemUserTags: '%s' is your own tag (kept)\n", binder)
				continue
			}
			result := index.XattrBackendRemove(filePath, binder, backend)
			layer := "kMDItemProjects"
			if backend == "tags" {
				layer = "kMDItemUserTags"
			}
			switch result {
			case "removed":
				fmt.Printf("Removed '%s' from %s\n", binder, layer)
			case "noop":
				fmt.Printf("%s: '%s' was not present (skipped)\n", layer, binder)
			case "skipped":
				// none backend — nothing to do
			case "failed":
				fmt.Fprintf(os.Stderr, "Warning: failed to update %s on %s\n", layer, filepath.Base(filePath))
			}
		}

		// ★ managed marker: drop it only when the file has no remaining binder
		// membership across ALL its ids. The id xattr is multi-valued, so a file
		// may carry ids that were never in the removed binder (and so aren't in
		// `matching`) yet still hold the file in another binder — enumerate the
		// file's own ids, not just the matched ones. Index re-read post-removal.
		fileIDs := map[string]bool{}
		for _, id := range index.OwnIDs(db, filePath) {
			fileIDs[id] = true
		}
		for _, m := range matching { // fallback when the id xattr was stripped
			fileIDs[index.AsString(m["id"])] = true
		}
		stillMember := false
		after, refsOK := loadRefs(nd) // re-read: this removal must be visible
		if !refsOK {
			return 1
		}
		for _, r := range after {
			if fileIDs[index.AsString(r["id"])] && len(index.NormalizeBinders(r["binder"])) > 0 {
				stillMember = true
				break
			}
		}
		if !stillMember && index.ManagedUnmark(filePath) == "removed" {
			fmt.Println("Unmarked ★ (no remaining membership)")
		}
	}

	if totalRemoved > 0 {
		fmt.Printf("Done: %s removed from '%s'\n", display, binder)
	} else {
		fmt.Printf("Not a member of '%s' (nothing to remove)\n", binder)
	}
	return 0
}

// recordsForBinder filters records to those whose binder set includes binder.
func recordsForBinder(all []map[string]any, binder string) []map[string]any {
	var out []map[string]any
	for _, r := range all {
		for _, b := range index.AsStrings(r["binder"]) {
			if b == binder {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

func mapBackends(recs []map[string]any) []string {
	out := make([]string, len(recs))
	for i, r := range recs {
		out[i] = index.XattrBackend(r)
	}
	return out
}

type noteGroup struct {
	file string
	recs []map[string]any
}

// groupByNoteFile groups records by their _note_file, preserving first-encounter
// order, so the report is deterministic.
func groupByNoteFile(recs []map[string]any) []noteGroup {
	byFile := map[string][]map[string]any{}
	var order []string
	for _, r := range recs {
		nf := index.AsString(r["_note_file"])
		if _, ok := byFile[nf]; !ok {
			order = append(order, nf)
		}
		byFile[nf] = append(byFile[nf], r)
	}
	out := make([]noteGroup, 0, len(order))
	for _, f := range order {
		out = append(out, noteGroup{file: f, recs: byFile[f]})
	}
	return out
}
