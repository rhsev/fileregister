package main

// cmd_cleanup — human-judged review of drift between the index (membership truth)
// and the optional per-binder Markdown context blocks.
//
// Report mode (default) lists findings; --interactive (without --dry-run) prompts
// to delete stale/unindexed blocks. Bookmarks (empty binder set) are never deleted.

import (
	"github.com/rhsev/fileregister/internal/index"

	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type cleanupItem struct {
	rec      map[string]any
	noteFile string
}

// cleanupShortDesc: "<label>" or "<label> (<binders>)".
func cleanupShortDesc(rec map[string]any) string {
	label := refLabel(rec)
	b := strings.Join(nonEmptyBinders(rec), ", ")
	if b == "" {
		return label
	}
	return label + " (" + b + ")"
}

// cleanupDeleteMdBlock deletes one per-binder Markdown context block.
func cleanupDeleteMdBlock(item cleanupItem) (int, error) {
	id := index.AsString(item.rec["id"])
	binder := index.AsString(item.rec["binder"])
	// Annotations are Markdown; dispatch by extension for safety.
	if strings.HasSuffix(strings.ToLower(item.noteFile), ".jsonl") {
		if binder != "" {
			// Set model: deleting an (id, binder) pair set-deletes that one
			// membership — the record stays (an emptied set is a bookmark).
			return index.JSONLRemoveBinder(item.noteFile, id, binder)
		}
		return index.JSONLRewrite(item.noteFile, func(rec map[string]any) (map[string]any, bool) {
			if index.AsString(rec["id"]) == id {
				return nil, true
			}
			return rec, false
		})
	}
	return mdDeleteBlock(item.noteFile, id, binder)
}

func cmdCleanup(args []string) int {
	interactive := false
	dryRun := false
	for _, a := range args {
		switch a {
		case "--interactive":
			interactive = true
		case "--dry-run":
			dryRun = true
		case "-h", "--help":
			fmt.Println("Usage: register cleanup [options]")
			return 0
		case "-v", "--version":
			fmt.Println("register cleanup " + registerVersion)
			return 0
		default:
			if strings.HasPrefix(a, "-") {
				return unknownOption("cleanup", a)
			}
		}
	}

	if !interactive {
		fmt.Println("(report mode — rerun with --interactive to act on findings)")
	}

	nd, err := notesDir()
	if err != nil {
		return 1
	}

	fmt.Fprintln(os.Stderr, "Collecting index records and annotations…")
	refs, refsOK := loadRefs(nd)
	if !refsOK {
		return 1
	}
	annos := readAnnotations(nd)

	indexIDs := map[string]bool{}
	memberships := map[string]bool{}
	for _, r := range refs {
		id := index.AsString(r["id"])
		indexIDs[id] = true
		for _, b := range index.AsStrings(r["binder"]) {
			memberships[id+"|"+b] = true
		}
	}

	var stale, unindexed []cleanupItem
	for _, r := range annos {
		id := index.AsString(r["id"])
		b := index.AsString(r["binder"])
		if id == "" {
			continue
		}
		item := cleanupItem{rec: r, noteFile: index.AsString(r["_note_file"])}
		if !indexIDs[id] {
			unindexed = append(unindexed, item)
		} else if b != "" && !memberships[id+"|"+b] {
			// A block that names no binder claims no membership, so it
			// cannot be stale — it is context for the record as a whole.
			stale = append(stale, item)
		}
	}

	var bookmarks []map[string]any
	for _, r := range refs {
		if len(nonEmptyBinders(r)) == 0 {
			bookmarks = append(bookmarks, r)
		}
	}

	fmt.Printf("=== Stale context blocks (%d) ===\n", len(stale))
	fmt.Println("    Markdown block whose binder is no longer in the index (e.g. after remove).")
	fmt.Println("    Default action: delete the block, or re-add the binder.")
	for _, it := range stale {
		fmt.Printf("  • %s in %s\n", cleanupShortDesc(it.rec), filepath.Base(it.noteFile))
	}
	if len(stale) == 0 {
		fmt.Println("(none)")
	}

	fmt.Println("")
	fmt.Printf("=== Unindexed annotations (%d) ===\n", len(unindexed))
	fmt.Println("    Markdown block whose id has no index record (legacy / hand-written).")
	fmt.Println("    Usually: run 'register reindex'. Or delete here if obsolete.")
	for _, it := range unindexed {
		fmt.Printf("  • %s in %s\n", cleanupShortDesc(it.rec), filepath.Base(it.noteFile))
	}
	if len(unindexed) == 0 {
		fmt.Println("(none)")
	}

	fmt.Println("")
	fmt.Printf("=== Bookmarks (%d) ===\n", len(bookmarks))
	fmt.Println("    Records in no binder. A bookmark is not cruft — listed for review only,")
	fmt.Println("    never deleted automatically.")
	for _, r := range bookmarks {
		fmt.Printf("  • %s (id=%s)\n", refLabel(r), index.AsString(r["id"]))
	}
	if len(bookmarks) == 0 {
		fmt.Println("(none)")
	}

	if !(interactive && !dryRun) {
		return 0
	}

	stdin := bufio.NewReader(os.Stdin)
	total := 0
	total += cleanupInteractiveReview(stdin, stale, "Stale context blocks")
	total += cleanupInteractiveReview(stdin, unindexed, "Unindexed annotations")

	fmt.Println("")
	fmt.Printf("Cleanup complete. %d block(s) deleted. Bookmarks left untouched.\n", total)
	return 0
}

func cleanupInteractiveReview(stdin *bufio.Reader, items []cleanupItem, typeLabel string) int {
	if len(items) == 0 {
		return 0
	}
	fmt.Println("")
	fmt.Printf("=== %s (%d) ===\n", typeLabel, len(items))
	deleted := 0
	for i, item := range items {
		rec := item.rec
		fmt.Println("")
		fmt.Printf("%d/%d [%s]\n", i+1, len(items), typeLabel)
		fmt.Printf("  File    : %s\n", filepath.Base(item.noteFile))
		fmt.Printf("  id      : %s\n", index.AsString(rec["id"]))
		fmt.Printf("  filename: %s\n", orNone(rec, "filename"))
		if aka := index.AsStrings(rec["aka"]); len(aka) > 0 {
			fmt.Printf("  aka     : %s\n", strings.Join(aka, ", "))
		}
		fmt.Printf("  kind    : %s\n", orNone(rec, "kind"))
		fmt.Printf("  binder  : %s\n", index.AsString(rec["binder"]))
		fmt.Printf("  note    : %s\n", item.noteFile)

		fmt.Fprint(os.Stderr, "  Delete (d) / Keep (k) / Skip (s)? [d/k/s]: ")
		line, _ := stdin.ReadString('\n')
		ch := "s"
		if s := strings.ToLower(strings.TrimSpace(line)); s != "" {
			ch = s[:1]
		}
		switch ch {
		case "d":
			count, err := cleanupDeleteMdBlock(item)
			switch {
			case err != nil:
				fmt.Fprintf(os.Stderr, "  Error: delete failed: %v\n", err)
			case count == 0:
				fmt.Println("  → no matching block found (nothing deleted)")
			default:
				deleted++
				fmt.Println("  → deleted")
			}
		case "k":
			fmt.Println("  → kept")
		default:
			fmt.Println("  → skipped")
		}
	}
	fmt.Println("")
	fmt.Printf("%s: %d deleted, %d kept/skipped.\n", typeLabel, deleted, len(items)-deleted)
	return deleted
}

// orNone returns the field's value, or "(none)" when nil/false.
func orNone(rec map[string]any, key string) string {
	if v, ok := rec[key]; ok && v != nil && v != false {
		return index.AsString(v)
	}
	return "(none)"
}
