package main

// cmd_refresh — push index (Markdown/JSONL) state back to macOS metadata:
// the syncable id xattr, the ★ managed marker, and the binder xattr layer.
//

import (
	"github.com/rhsev/fileregister/internal/index"

	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// nonEmptyBinders returns a record's binder set as non-empty strings (no uniq,
// matching Array(rec["binder"]).map(&:to_s).reject(&:empty?)).
func nonEmptyBinders(rec map[string]any) []string {
	var out []string
	for _, b := range index.AsStrings(rec["binder"]) {
		if b != "" {
			out = append(out, b)
		}
	}
	return out
}

func cmdRefresh(args []string) int {
	dryRun := false
	for _, a := range args {
		switch a {
		case "--dry-run":
			dryRun = true
		case "-h", "--help":
			fmt.Println("Usage: register refresh [options]")
			return 0
		case "-v", "--version":
			fmt.Println("register refresh " + registerVersion)
			return 0
		default:
			if strings.HasPrefix(a, "-") {
				return unknownOption("refresh", a)
			}
		}
	}

	nd, err := notesDir()
	if err != nil {
		return 1
	}

	fmt.Fprintln(os.Stderr, "Collecting type:ref records…")
	allRefs, refsOK := loadRefs(nd)
	if !refsOK {
		return 1
	}
	if len(allRefs) == 0 {
		fmt.Println("No ref records found.")
		return 0
	}
	fmt.Fprintf(os.Stderr, "Found %d ref record(s). Resolving bookmarks…\n", len(allRefs))

	resolved, rerr := resolveRecordPaths(allRefs)
	if rerr != nil {
		fmt.Fprintf(os.Stderr, "Error: fileanchor batch resolve failed: %v\n", rerr)
		return 1
	}

	type brokenEntry struct {
		binders         []string
		noteFile, label string
	}
	type missingEntry struct {
		binders        []string
		refPath, label string
	}
	var broken []brokenEntry
	var missing []missingEntry
	var skippedTrash, skippedShared []string
	shared := sharedPaths(allRefs, resolved)
	refreshed, noop, failed, skippedNone, skippedURL := 0, 0, 0, 0, 0

	for _, rec := range allRefs {
		id := index.AsString(rec["id"])
		binders := nonEmptyBinders(rec)
		noteFile := index.AsString(rec["_note_file"])
		backend := index.XattrBackend(rec)

		if index.URLRef(rec) {
			skippedURL++
			continue
		}

		refPath := resolved[id]
		if refPath == "" {
			broken = append(broken, brokenEntry{binders, noteFile, refLabel(rec)})
			continue
		}
		if !index.FileExists(refPath) {
			missing = append(missing, missingEntry{binders, refPath, refLabel(rec)})
			continue
		}
		// A file in the Trash is not re-marked as a member, and a file two
		// records resolve to would get their ids written in turn — audit
		// reports both.
		if trashOrBackup(refPath) != "" {
			skippedTrash = append(skippedTrash, refLabel(rec))
			continue
		}
		if len(shared[refPath]) > 1 {
			skippedShared = append(skippedShared, refLabel(rec))
			continue
		}

		// Identity layer for every record (idempotent): syncable id xattr; ★ for
		// members only (a bookmark, empty binder set, does not get it).
		if !dryRun {
			index.SetSyncXattr(refPath, id)
			if len(binders) > 0 {
				index.ManagedMark(refPath)
			}
		}

		if len(binders) == 0 {
			continue // bookmark: identity refreshed, no binder layer
		}
		if backend == "none" {
			skippedNone++
			continue
		}

		for _, binder := range binders {
			if dryRun {
				fmt.Printf("[dry-run] (%s) %s → %s\n", backend, filepath.Base(refPath), binder)
				refreshed++
				continue
			}
			switch index.XattrBackendAdd(refPath, binder, backend) {
			case "added":
				refreshed++
				fmt.Printf("Refreshed (%s): %s → %s\n", backend, filepath.Base(refPath), binder)
			case "noop":
				noop++
			case "failed":
				failed++
				fmt.Fprintf(os.Stderr, "Warning: xattr failed (%s) for %s\n", backend, filepath.Base(refPath))
			}
		}
	}

	fmt.Println("")
	dr := ""
	if dryRun {
		dr = " (dry-run)"
	}
	fmt.Printf("Refresh complete%s:\n", dr)
	fmt.Printf("  Refreshed    : %d\n", refreshed)
	fmt.Printf("  Already ok   : %d\n", noop)
	if skippedNone > 0 {
		fmt.Printf("  Skipped (none): %d\n", skippedNone)
	}
	if skippedURL > 0 {
		fmt.Printf("  Skipped (url) : %d\n", skippedURL)
	}
	if failed > 0 {
		fmt.Printf("  Failed       : %d\n", failed)
	}
	if len(skippedTrash)+len(skippedShared) > 0 {
		fmt.Printf("  Skipped      : %d in the Trash or a backup, %d on a file shared with another record — see 'register audit'\n",
			len(skippedTrash), len(skippedShared))
	}

	if len(broken) > 0 {
		fmt.Println("")
		fmt.Printf("Broken bookmarks (%d) — run 'register repair' to fix:\n", len(broken))
		for _, b := range broken {
			fmt.Printf("  [%s] %s (in: %s)\n", strings.Join(b.binders, ", "), b.label, filepath.Base(b.noteFile))
		}
	}

	if len(missing) > 0 {
		fmt.Println("")
		fmt.Printf("Missing files (%d) — file no longer exists at resolved path:\n", len(missing))
		for _, m := range missing {
			fmt.Printf("  [%s] %s → %s\n", strings.Join(m.binders, ", "), m.label, m.refPath)
		}
	}

	return 0
}
