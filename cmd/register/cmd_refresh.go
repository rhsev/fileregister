package main

// cmd_refresh — push index (Markdown/JSONL) state back to macOS metadata: the
// id (kMDItemInformation, Spotlight's, and its syncable copy), the ★ managed
// marker, and the binder xattr layer.
//
// One field goes the other way: the file name. After a rename the bookmark
// follows the file, and refresh writes the name it has now into the index,
// where repair and add look for it.

import (
	"github.com/rhsev/fileregister/v2/internal/index"

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

const refreshUsage = "Usage: register refresh [--dry-run] [--snapshot <file>]"

func cmdRefresh(args []string) int {
	dryRun := false
	snapshotPath := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--dry-run":
			dryRun = true
		case "--snapshot":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, refreshUsage)
				return 1
			}
			i++
			snapshotPath = args[i]
		case "-h", "--help":
			fmt.Println(refreshUsage)
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

	// With --snapshot, a file that lost its id lost every attribute (a program
	// replaced it, or a copy stripped it): its free Finder tags and comment
	// come back from the snapshot. A file that still has its id keeps the tags
	// it has, since a missing tag there was taken off on purpose.
	var finder map[string]finderEntry
	if snapshotPath != "" {
		finder, err = finderFromSnapshot(snapshotPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			return 1
		}
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

	resolutions, rerr := resolveRecords(allRefs)
	if rerr != nil {
		fmt.Fprintf(os.Stderr, "Error: fileanchor batch resolve failed: %v\n", rerr)
		return 1
	}
	resolved := map[string]string{}
	for id, r := range resolutions {
		resolved[id] = r.Path
	}
	renewed := 0
	renames := map[string]map[string]string{} // index file → id → new name
	renamedNames := 0

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
	refreshed, noop, failed, skippedNone, skippedURL, restoredIDs := 0, 0, 0, 0, 0, 0
	starsSet, finderBack := 0, 0

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

		if now := renamedTo(rec, refPath); now != "" {
			old := index.AsString(rec["filename"])
			if dryRun {
				fmt.Printf("[dry-run] file name in the index: %s → %s\n", old, now)
				renamedNames++
			} else {
				if renames[noteFile] == nil {
					renames[noteFile] = map[string]string{}
				}
				renames[noteFile][id] = now
				fmt.Printf("File name in the index: %s → %s\n", old, now)
			}
		}

		// Whether the file lost its id is read before anything writes it back:
		// renewing a stale bookmark stamps the id again.
		fe, inSnap := finder[id]
		lost := inSnap && (len(fe.tags) > 0 || fe.comment != "") &&
			!index.HasDescriptionID(refPath, id) && index.SyncID(refPath) != id

		// A stale bookmark still resolves, but macOS asks for it to be saved
		// anew; left alone it degrades until it no longer resolves at all.
		if resolutions[id].Stale && !dryRun {
			if _, err := index.Rebind(id, refPath); err == nil {
				renewed++
			}
		}

		// Identity layer for every record (idempotent): the id in both its
		// carriers, and the star, ★ for a member and ☆ for a record in no
		// binder. kMDItemInformation is the one Spotlight searches and iCloud
		// strips; the #S copy is the one that travels.
		member := len(binders) > 0
		starOK := index.StatusMarked(refPath, member)
		if dryRun {
			if !index.HasDescriptionID(refPath, id) {
				fmt.Printf("[dry-run] id %s → %s of %s\n", id, idLayerName, filepath.Base(refPath))
				restoredIDs++
			}
			if !starOK {
				fmt.Printf("[dry-run] %s → %s\n", index.StatusStar(member), filepath.Base(refPath))
				starsSet++
			}
			if lost {
				fmt.Printf("[dry-run] Finder tags and comment from the snapshot → %s\n", filepath.Base(refPath))
				finderBack++
			}
		} else {
			index.SetSyncXattr(refPath, id)
			if restored, why := index.RestoreDescriptionID(refPath, id); why != "" {
				failed++
				fmt.Fprintf(os.Stderr, "Warning: writing the id to %s failed for %s: %s\n", idLayerName, filepath.Base(refPath), why)
			} else if restored {
				restoredIDs++
				fmt.Printf("Restored id %s → %s of %s\n", id, idLayerName, filepath.Base(refPath))
			}
			if !starOK {
				if index.StatusMark(refPath, member) == "failed" {
					failed++
					fmt.Fprintf(os.Stderr, "Warning: setting %s on %s failed: %s\n", index.StatusStar(member), filepath.Base(refPath), index.LastEngineError())
				} else {
					starsSet++
					fmt.Printf("%s → %s\n", index.StatusStar(member), filepath.Base(refPath))
				}
			}
			if lost {
				wrote, remarks := writeFinder(refPath, fe.tags, fe.comment)
				if wrote {
					finderBack++
					fmt.Printf("Finder tags and comment from the snapshot → %s\n", filepath.Base(refPath))
				}
				for _, r := range remarks {
					fmt.Fprintf(os.Stderr, "Warning: %s: %s\n", filepath.Base(refPath), r)
				}
			}
		}

		if len(binders) == 0 {
			continue // in no binder: identity refreshed, no binder layer
		}
		if backend == "none" {
			skippedNone++
			continue
		}

		for _, binder := range binders {
			if dryRun {
				// Report what the real run would write: a name already on the
				// file is a noop there, so it is one here.
				if index.XattrBackendIncludes(refPath, binder, backend) {
					noop++
					continue
				}
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
				fmt.Fprintf(os.Stderr, "Warning: xattr failed (%s) for %s: %s\n", backend, filepath.Base(refPath), index.LastEngineError())
			}
		}
	}

	for f, names := range renames {
		n, err := setIndexFilenames(f, names)
		renamedNames += n
		if err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "Warning: updating file names in %s failed: %v\n", filepath.Base(f), err)
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
	if renewed > 0 {
		fmt.Printf("  Bookmarks renewed (stale): %d\n", renewed)
	}
	if restoredIDs > 0 {
		fmt.Printf("  IDs restored : %d %s\n", restoredIDs, idRestoredNote)
	}
	if starsSet > 0 {
		fmt.Printf("  Stars set    : %d (★ in a binder, ☆ in none)\n", starsSet)
	}
	if finderBack > 0 {
		fmt.Printf("  From snapshot: %d (Finder tags and comment of files that had lost their id)\n", finderBack)
	}
	if renamedNames > 0 {
		fmt.Printf("  File names updated in the index: %d\n", renamedNames)
	}
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

	// Non-zero while something needs the user, so a monitor can tell.
	if failed > 0 || len(broken) > 0 || len(missing) > 0 {
		return 1
	}
	return 0
}
