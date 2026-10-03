package main

// cmd_rename — rename a binder across index records, Markdown annotation
// blocks, and the file xattr layer.

import (
	"github.com/rhsev/fileregister/internal/index"

	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// refLabel is a record's display label: "<filename> (<id>)", or the bare id.
func refLabel(rec map[string]any) string {
	fn := index.AsString(rec["filename"])
	id := index.AsString(rec["id"])
	if fn == "" {
		return id
	}
	return fn + " (" + id + ")"
}

// annotationsForBinder returns the Markdown annotation records for a binder
// (binder compared as a scalar, as md blocks carry one binder each).
func annotationsForBinder(notesDir, binder string) []map[string]any {
	var out []map[string]any
	for _, r := range readAnnotations(notesDir) {
		if index.AsString(r["binder"]) == binder {
			out = append(out, r)
		}
	}
	return out
}

func cmdRename(args []string) int {
	var pos []string
	merge := false
	for _, a := range args {
		switch a {
		case "--merge":
			merge = true
		case "-h", "--help":
			fmt.Println("Usage: register rename <old-name> <new-name> [--merge]")
			return 0
		case "-v", "--version":
			fmt.Println("register rename " + registerVersion)
			return 0
		default:
			if strings.HasPrefix(a, "-") {
				return unknownOption("rename", a)
			}
			pos = append(pos, a)
		}
	}

	if len(pos) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: register rename <old-name> <new-name>")
		return 1
	}
	oldName, newName := pos[0], pos[1]

	nd, err := notesDir()
	if err != nil {
		return 1
	}
	if oldName == newName {
		fmt.Fprintln(os.Stderr, "Error: old and new names are the same")
		return 1
	}
	// Only the new name is checked: an old binder that breaks the rule must
	// stay renamable.
	if p := index.BinderNameProblem(newName); p != "" {
		fmt.Fprintf(os.Stderr, "Error: binder name '%s' %s\n", newName, p)
		return 1
	}

	fmt.Fprintf(os.Stderr, "Collecting refs for binder '%s'…\n", oldName)
	// A record's binder lives in the index (jsonl) and in any annotation copies
	// (markdown). Update both; jsonl records come first so they win on dedup.
	refs, refsOK := loadRefs(nd)
	if !refsOK {
		return 1
	}
	records := append(recordsForBinder(refs, oldName), annotationsForBinder(nd, oldName)...)
	if len(records) == 0 {
		fmt.Printf("No refs found for binder '%s'\n", oldName)
		return 0
	}

	// Renaming onto an existing binder merges two sets — irreversible, so it
	// has to be asked for explicitly.
	if existing := recordsForBinder(refs, newName); len(existing) > 0 && !merge {
		fmt.Fprintf(os.Stderr, "Error: binder '%s' already exists (%d record(s)) — renaming would merge the two sets.\n", newName, len(existing))
		fmt.Fprintln(os.Stderr, "Rerun with --merge to do that on purpose.")
		return 1
	}

	fmt.Printf("Found %d ref(s). Renaming '%s' → '%s'…\n", len(records), oldName, newName)
	fmt.Println("")

	recordChanges := 0
	writeFailed := false
	for _, note := range groupByNoteFile(records) {
		var count int
		var werr error
		if strings.HasSuffix(strings.ToLower(note.file), ".jsonl") {
			count, werr = index.JSONLRenameBinder(note.file, oldName, newName)
		} else {
			count, werr = mdRenameBinder(note.file, oldName, newName)
		}
		if werr != nil {
			fmt.Fprintf(os.Stderr, "  Error: writing %s failed: %v\n", note.file, werr)
			writeFailed = true
			continue
		}
		recordChanges += count
		if count > 0 {
			fmt.Printf("  %s: %d block(s) updated\n", filepath.Base(note.file), count)
		}
	}

	// Carry the ordering layer along: the canonical note file is addressed by
	// binder name, and its type:ordering config block carries the name too —
	// left behind, `order` would silently fall back to rule order.
	oldNote := defaultPromoteTarget(nd, oldName)
	newNote := defaultPromoteTarget(nd, newName)
	if index.FileExists(oldNote) {
		if n, oerr := mdRenameOrderingBinder(oldNote, oldName, newName); oerr != nil {
			fmt.Fprintf(os.Stderr, "  Error: updating ordering config in %s failed: %v\n", filepath.Base(oldNote), oerr)
			writeFailed = true
		} else if n > 0 {
			fmt.Printf("  %s: ordering config updated\n", filepath.Base(oldNote))
		}
		if index.FileExists(newNote) {
			fmt.Fprintf(os.Stderr, "  Note: %s already exists — blocks stay in %s; review by hand\n",
				filepath.Base(newNote), filepath.Base(oldNote))
		} else if mvErr := os.Rename(oldNote, newNote); mvErr != nil {
			fmt.Fprintf(os.Stderr, "  Error: renaming %s failed: %v\n", filepath.Base(oldNote), mvErr)
			writeFailed = true
		} else {
			fmt.Printf("  %s → %s\n", filepath.Base(oldNote), filepath.Base(newNote))
		}
	}

	fmt.Println("")
	resolved, rerr := resolveRecordPaths(records)
	if rerr != nil {
		// The index rename above already happened — don't fail the command,
		// but say why the file layer stayed behind.
		fmt.Fprintf(os.Stderr, "Warning: fileanchor batch resolve failed: %v — xattrs not updated; run register refresh\n", rerr)
	}

	xattrChanges := 0
	xattrFailed := 0
	seenIDs := map[string]bool{}
	keptChanges := map[string]map[string]map[string]bool{} // index file → id → tag → keep
	for _, rec := range records {
		id := index.AsString(rec["id"])
		if seenIDs[id] {
			continue // one xattr update per file
		}
		seenIDs[id] = true
		label := refLabel(rec)
		backend := index.XattrBackend(rec)
		layer := "kMDItemProjects"
		if backend == "tags" {
			layer = "kMDItemUserTags"
		}

		if index.URLRef(rec) {
			xattrChanges++
			continue
		}

		refPath := resolved[id]
		if refPath == "" {
			fmt.Fprintf(os.Stderr, "  Warning: bookmark does not resolve for id=%s (%s) — xattr not updated\n", id, label)
			continue
		}
		if !index.FileExists(refPath) {
			fmt.Fprintf(os.Stderr, "  Warning: file not found at %s — xattr not updated\n", refPath)
			continue
		}
		if backend == "none" {
			xattrChanges++
			continue
		}

		// With the tags backend, the old name's tag stays if it was the
		// user's own; the new name's tag is the user's if it was already there.
		keepOld := backend == "tags" && index.IsKeptTag(rec, oldName)
		if !keepOld && index.XattrBackendRemove(refPath, oldName, backend) == "failed" {
			fmt.Fprintf(os.Stderr, "  Warning: failed to remove '%s' from %s of %s\n", oldName, layer, filepath.Base(refPath))
			xattrFailed++
			continue
		}
		result := index.XattrBackendAdd(refPath, newName, backend)
		if file := index.AsString(rec["_note_file"]); backend == "tags" && strings.HasSuffix(strings.ToLower(file), ".jsonl") &&
			(result == "noop" || index.IsKeptTag(rec, newName)) {
			if keptChanges[file] == nil {
				keptChanges[file] = map[string]map[string]bool{}
			}
			keptChanges[file][id] = map[string]bool{newName: result == "noop"}
		}
		if result == "failed" {
			fmt.Fprintf(os.Stderr, "  Warning: failed to add '%s' to %s of %s\n", newName, layer, filepath.Base(refPath))
			xattrFailed++
		} else {
			xattrChanges++
			fmt.Printf("  %s updated: %s\n", layer, filepath.Base(refPath))
		}
	}

	for file, changes := range keptChanges {
		if _, err := index.JSONLSetKeptTags(file, changes); err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: recording kept tags in %s failed: %v\n", filepath.Base(file), err)
		}
	}

	fmt.Println("")
	fmt.Println("Rename complete:")
	fmt.Printf("  YAML blocks updated : %d\n", recordChanges)
	fmt.Printf("  xattr files updated : %d\n", xattrChanges)
	if xattrFailed > 0 {
		fmt.Printf("  xattr failures      : %d\n", xattrFailed)
	}
	fmt.Println("")
	if xattrFailed > 0 {
		fmt.Println("Hint: run 'register refresh' to reconcile any residue.")
	}
	if writeFailed {
		return 1
	}
	return 0
}
