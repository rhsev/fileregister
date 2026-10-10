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

// cleanupDeleteMdBlock deletes one per-binder Markdown context block. The
// items come from the notes (readAnnotations reads .md only), so this is only
// ever a block; a record is deleted by forget and nothing else.
func cleanupDeleteMdBlock(item cleanupItem) (int, error) {
	return mdDeleteBlock(item.noteFile, index.AsString(item.rec["id"]), index.AsString(item.rec["binder"]))
}

func cmdCleanup(args []string) int {
	interactive := false
	dryRun := false
	prune := false
	for _, a := range args {
		switch a {
		case "--interactive":
			interactive = true
		case "--dry-run":
			dryRun = true
		case "--prune":
			prune = true
		case "-h", "--help":
			fmt.Println(cleanupUsage)
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
	annos, aerr := readAnnotations(nd)
	if aerr != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", aerr)
		return 1
	}

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
		if id == "" {
			continue
		}
		item := cleanupItem{rec: r, noteFile: index.AsString(r["_note_file"])}
		if !indexIDs[id] {
			unindexed = append(unindexed, item)
			continue
		}
		// A block that names no binder claims no membership, so it cannot be
		// stale — it is context for the record as a whole. A list names one
		// membership per element; any one that is gone makes it stale.
		for _, nb := range index.NormalizeBinders(r["binder"]) {
			if !memberships[id+"|"+index.AsString(nb)] {
				stale = append(stale, item)
				break
			}
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

	// The identity layer. The three sections above are about records and
	// Markdown; this one is about ~/.local/share/bookmarks.json, which no other
	// section can see into. Only the verdicts beyond repair are offered: an
	// orphan may be held on purpose, a broken one belongs to `repair`, and an
	// unreachable one was never judged. `register audit` shows all five.
	db, dbErr := index.LoadDB()
	var prunable, unreachable []bookmarkFinding
	if dbErr != nil {
		fmt.Println("")
		fmt.Println("=== Unrepairable bookmark entries ===")
		fmt.Printf("    bookmarks.json unreadable: %v\n", dbErr)
	} else if len(db) > 0 {
		rep, cerr := classifyBookmarks(db, refs)
		for _, f := range rep.findings {
			if f.prunable() {
				prunable = append(prunable, f)
			} else if f.kind == bmUnreachable {
				unreachable = append(unreachable, f)
			}
		}
		fmt.Println("")
		fmt.Printf("=== Unrepairable bookmark entries (%d) ===\n", len(prunable))
		fmt.Println("    Entries in bookmarks.json that no repair can fix: the file is gone,")
		fmt.Println("    or the key was never a minted id. Orphans and repairable ones are")
		fmt.Println("    left out — see 'register audit' for the full picture.")
		if cerr != nil {
			fmt.Printf("    Engine error: %v — not checked\n", cerr)
		}
		for _, f := range prunable {
			lbl := f.label
			if lbl == "" {
				lbl = "(no name)"
			}
			fmt.Printf("  • %s [%s] %s\n", f.kind, f.id, lbl)
		}
		if len(prunable) == 0 {
			fmt.Println("(none)")
		}
	}

	if prune {
		if dryRun {
			fmt.Println("")
			fmt.Printf("[dry-run] would drop %d unrepairable bookmark entry(ies).\n", len(prunable))
			return 0
		}
		return cleanupPrune(unreachable, prunable)
	}

	if !(interactive && !dryRun) {
		return 0
	}

	stdin := bufio.NewReader(os.Stdin)
	total := 0
	total += cleanupInteractiveReview(stdin, stale, "Stale context blocks")
	total += cleanupInteractiveReview(stdin, unindexed, "Unindexed annotations")
	dropped := cleanupReviewBookmarks(stdin, prunable)

	fmt.Println("")
	fmt.Printf("Cleanup complete. %d block(s) deleted, %d bookmark entry(ies) dropped. Records in no binder left untouched.\n", total, dropped)
	return 0
}

// cleanupReviewBookmarks walks the unrepairable entries and drops the ones the
// user confirms. One save at the end: the store is a single JSON file, and a
// write per answer would rewrite it dozens of times for no gain.
func cleanupReviewBookmarks(stdin *bufio.Reader, items []bookmarkFinding) int {
	if len(items) == 0 {
		return 0
	}
	// See cleanupPrune: the write phase re-reads under the lock.
	db, lerr := lockedBookmarkDB()
	if lerr != nil {
		fmt.Fprintf(os.Stderr, "  Error: %v — nothing was removed\n", lerr)
		return 0
	}
	fmt.Println("")
	fmt.Printf("=== Unrepairable bookmark entries (%d) ===\n", len(items))
	dropped := 0
	for i, f := range items {
		fmt.Println("")
		fmt.Printf("%d/%d [%s]\n", i+1, len(items), f.kind)
		fmt.Printf("  id      : %s\n", f.id)
		if f.label != "" {
			fmt.Printf("  filename: %s\n", f.label)
		}
		if f.path != "" {
			fmt.Printf("  last    : %s\n", f.path)
		}
		fmt.Printf("  why     : %s\n", f.hint)

		fmt.Fprint(os.Stderr, "  Drop (d) / Keep (k) / Skip (s)? [d/k/s]: ")
		line, _ := stdin.ReadString('\n')
		ch := "s"
		if t := strings.ToLower(strings.TrimSpace(line)); t != "" {
			ch = t[:1]
		}
		if ch != "d" {
			fmt.Println("  kept")
			continue
		}
		delete(db, f.id)
		dropped++
		fmt.Println("  dropped")
	}
	if dropped > 0 {
		if err := index.SaveDB(db); err != nil {
			fmt.Fprintf(os.Stderr, "  Error: saving bookmarks.json failed: %v — nothing was removed\n", err)
			return 0
		}
	}
	return dropped
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

const cleanupUsage = "Usage: register cleanup [--interactive] [--prune] [--dry-run]"

// cleanupPrune removes the unrepairable bookmark entries without asking, and
// refuses to do so while any entry could not be judged.
//
// The refusal is the whole point. "Dead" means the blob does not resolve and no
// file carries the id — true only if the question could be asked. An unmounted
// volume, or a mounted one Spotlight does not index, makes every bookmark on it
// look dead, and an unattended run would then delete healthy entries. So the
// mode does not trust the system; it trusts a condition the system can check.
func cleanupPrune(unreachable, prunable []bookmarkFinding) int {
	fmt.Println("")
	if len(unreachable) > 0 {
		fmt.Printf("Refusing to prune: %d bookmark entry(ies) could not be judged.\n", len(unreachable))
		for _, f := range unreachable {
			lbl := f.label
			if lbl == "" {
				lbl = "(no name)"
			}
			fmt.Printf("  %s [%s] %s\n", f.kind, f.id, lbl)
			if f.path != "" {
				fmt.Printf("    last seen: %s\n", f.path)
			}
			fmt.Printf("    → %s\n", f.hint)
		}
		fmt.Println("")
		fmt.Println("Mount the volume (or let Spotlight index it) and run again.")
		return 1
	}
	if len(prunable) == 0 {
		fmt.Println("Nothing to prune.")
		return 0
	}
	// The report read the store without the lock, which is fine for a report.
	// The write must not: take the lock and re-read under it, so an entry added
	// between the two is not lost when the whole map is saved back. Verdicts are
	// keyed by id, and an id does not change, so the decisions still apply.
	db, lerr := lockedBookmarkDB()
	if lerr != nil {
		fmt.Fprintf(os.Stderr, "Error: %v — nothing was removed\n", lerr)
		return 1
	}
	for _, f := range prunable {
		lbl := f.label
		if lbl == "" {
			lbl = "(no name)"
		}
		fmt.Printf("  dropped %s [%s] %s\n", f.kind, f.id, lbl)
		delete(db, f.id)
	}
	if err := index.SaveDB(db); err != nil {
		fmt.Fprintf(os.Stderr, "Error: saving bookmarks.json failed: %v — nothing was removed\n", err)
		return 1
	}
	fmt.Println("")
	fmt.Printf("Pruned %d unrepairable bookmark entry(ies).\n", len(prunable))
	return 0
}

// lockedBookmarkDB takes the bookmark lock and reads the store under it — the
// order SPEC.md requires of anything that loads, modifies and saves the whole
// map. The lock is process-wide and idempotent, so SaveDB's own take is a
// no-op afterwards.
func lockedBookmarkDB() (map[string]string, error) {
	if err := index.LockBookmarks(); err != nil {
		return nil, err
	}
	return index.LoadDB()
}
