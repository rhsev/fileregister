package main

// cmd_audit — read-only consistency report (index ↔ files ↔ xattr).

import (
	"github.com/rhsev/fileregister/internal/index"

	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ghostReport prints a ghost entry (a file tagged with a binder that the index
// doesn't record) and returns "ghost", or "" if it's a known record. A copy of a
// registered file (cp, Finder's Duplicate, a copied folder) carries the
// original's metadata; adding it would only register a duplicate, so it is
// named as a copy instead, and "copy" returned.
func ghostReport(fpath, binderName, layer string, recordIndex map[string]bool, db map[string]string) string {
	fpath = strings.TrimSpace(fpath)
	if fpath == "" {
		return ""
	}
	key := index.PathKey(fpath) + "|" + binderName
	if recordIndex[key] {
		return ""
	}
	if id, orig := index.CopiedFrom(db, fpath); id != "" {
		fmt.Printf("  COPY (%s) [%s] %s\n", layer, binderName, filepath.Base(fpath))
		fmt.Printf("    path: %s\n", fpath)
		fmt.Printf("    copy of record %s: %s\n", id, orig)
		fmt.Println("    → nothing to do; it carries the original's metadata. Add it only if it is meant to be a file of its own.")
		return "copy"
	}
	fmt.Printf("  GHOST (%s) [%s] %s\n", layer, binderName, filepath.Base(fpath))
	fmt.Printf("    path: %s\n", fpath)
	fmt.Printf("    → run: register add %s --binder %s\n", shellQuote(fpath), shellQuote(binderName))
	return "ghost"
}

// shellQuote quotes s for a command line the user may paste into a shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// binderTag renders a record's binder list for a report line; a binderless
// record shows as "bookmark".
func binderTag(binders []string) string {
	if len(binders) == 0 {
		return "bookmark"
	}
	return strings.Join(binders, ", ")
}

func cmdAudit(args []string) int {
	binderFilter := ""
	hasBinderFilter := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--binder" && i+1 < len(args):
			i++
			binderFilter = args[i]
			hasBinderFilter = true
		case strings.HasPrefix(a, "--binder="):
			binderFilter = strings.TrimPrefix(a, "--binder=")
			hasBinderFilter = true
		case a == "-h" || a == "--help":
			fmt.Println("Usage: register audit [options]")
			return 0
		case a == "-v" || a == "--version":
			fmt.Println("register audit " + registerVersion)
			return 0
		default:
			if strings.HasPrefix(a, "-") {
				return unknownOption("audit", a)
			}
		}
	}

	nd, err := notesDir()
	if err != nil {
		return 1
	}

	fmt.Fprintln(os.Stderr, "Collecting type:ref records…")
	all, refsOK := loadRefs(nd)
	if !refsOK {
		return 1
	}
	db, err := index.LoadDB()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}
	var active, bookmarks []map[string]any
	for _, r := range all {
		binders := nonEmptyBinders(r)
		if hasBinderFilter {
			match := false
			for _, b := range binders {
				if b == binderFilter {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		if len(binders) > 0 {
			active = append(active, r)
		} else {
			// Binderless bookmarks are first-class: their bookmark must still
			// resolve, they just carry no xattr expectations.
			bookmarks = append(bookmarks, r)
		}
	}
	checked := append(append([]map[string]any{}, active...), bookmarks...)
	if len(checked) == 0 {
		fmt.Println("No active ref records found.")
		return 0
	}

	resolved, rerr := resolveRecordPaths(checked)
	if rerr != nil {
		// With the engine failing, every record would report as broken.
		fmt.Fprintf(os.Stderr, "Error: fileanchor batch resolve failed: %v — cannot audit\n", rerr)
		return 1
	}

	type brokenT struct {
		binders         []string
		noteFile, label string
	}
	type missingFileT struct {
		binders                  []string
		noteFile, refPath, label string
	}
	type missingXattrT struct {
		binder, noteFile, refPath, label, backend, layer string
	}
	var brokenBookmark []brokenT
	var missingFile []missingFileT
	var inTrash []missingFileT
	shared := sharedPaths(checked, resolved)
	var missingXattr []missingXattrT
	okCount := 0

	fmt.Printf("=== Direction 1: Record → File (%d record(s)) ===\n", len(checked))
	fmt.Println("")

	for _, rec := range checked {
		id := index.AsString(rec["id"])
		binders := nonEmptyBinders(rec)
		noteFile := index.AsString(rec["_note_file"])
		backend := index.XattrBackend(rec)
		label := refLabel(rec)

		if index.URLRef(rec) {
			okCount++
			continue
		}

		refPath := resolved[id]
		if refPath == "" {
			brokenBookmark = append(brokenBookmark, brokenT{binders, noteFile, label})
			continue
		}
		if !index.FileExists(refPath) {
			missingFile = append(missingFile, missingFileT{binders, noteFile, refPath, label})
			continue
		}
		if trashOrBackup(refPath) != "" {
			inTrash = append(inTrash, missingFileT{binders, noteFile, refPath, label})
			continue
		}
		if len(binders) == 0 || backend == "none" {
			okCount++
			continue
		}
		for _, binder := range binders {
			if index.XattrBackendIncludes(refPath, binder, backend) {
				okCount++
			} else {
				layer := "kMDItemProjects"
				if backend == "tags" {
					layer = "kMDItemUserTags"
				}
				missingXattr = append(missingXattr, missingXattrT{binder, noteFile, refPath, label, backend, layer})
			}
		}
	}

	if len(brokenBookmark) == 0 && len(missingFile) == 0 && len(missingXattr) == 0 && len(inTrash) == 0 && len(shared) == 0 {
		fmt.Printf("All %d record(s) are consistent.\n", okCount)
	} else {
		if okCount > 0 {
			fmt.Printf("OK: %d record(s)\n", okCount)
		}
		if len(brokenBookmark) > 0 {
			fmt.Println("")
			fmt.Printf("BROKEN BOOKMARK (%d) — bookmark does not resolve:\n", len(brokenBookmark))
			for _, b := range brokenBookmark {
				fmt.Printf("  [%s] %s\n", binderTag(b.binders), b.label)
				fmt.Printf("    in: %s\n", b.noteFile)
				fmt.Println("    → run: register repair")
			}
		}
		if len(missingFile) > 0 {
			fmt.Println("")
			fmt.Printf("MISSING FILE (%d) — bookmark resolves but file not found:\n", len(missingFile))
			for _, m := range missingFile {
				fmt.Printf("  [%s] %s\n", binderTag(m.binders), m.label)
				fmt.Printf("    expected: %s\n", m.refPath)
			}
		}
		if len(inTrash) > 0 {
			fmt.Println("")
			fmt.Printf("IN TRASH OR BACKUP (%d) — the bookmark followed the file there:\n", len(inTrash))
			for _, m := range inTrash {
				fmt.Printf("  [%s] %s\n", binderTag(m.binders), m.label)
				fmt.Printf("    file: %s\n", m.refPath)
				fmt.Println("    → put it back, or remove the record")
			}
		}
		if len(shared) > 0 {
			fmt.Println("")
			fmt.Printf("SHARED FILE (%d) — several records resolve to one file:\n", len(shared))
			paths := make([]string, 0, len(shared))
			for p := range shared {
				paths = append(paths, p)
			}
			sort.Strings(paths)
			for _, p := range paths {
				ids := shared[p]
				fmt.Printf("  %s\n", p)
				fmt.Printf("    records: %s\n", strings.Join(ids, ", "))
				fmt.Println("    → keep one: the others belong to files that are gone or were copies")
			}
		}
		if len(missingXattr) > 0 {
			fmt.Println("")
			fmt.Printf("MISSING XATTR (%d) — file lacks expected metadata:\n", len(missingXattr))
			for _, mx := range missingXattr {
				fmt.Printf("  [%s] %s (%s)\n", mx.binder, mx.label, mx.backend)
				fmt.Printf("    file: %s\n", mx.refPath)
				fmt.Printf("    expected in: %s\n", mx.layer)
				fmt.Println("    → run: register refresh")
			}
		}
	}

	// Direction 2: File → Record (ghost xattr scan via mdfind).
	var binderNames []string
	if hasBinderFilter {
		binderNames = []string{binderFilter}
	} else {
		bseen := map[string]bool{}
		for _, r := range active {
			for _, b := range nonEmptyBinders(r) {
				if !bseen[b] {
					bseen[b] = true
					binderNames = append(binderNames, b)
				}
			}
		}
	}
	if len(binderNames) == 0 {
		if !hasBinderFilter {
			auditBookmarkDirection(db, checked)
		}
		return 0
	}

	fmt.Println("")
	fmt.Printf("=== Direction 2: File → Record (%d binder(s) via mdfind) ===\n", len(binderNames))
	fmt.Println("")

	recordIndex := map[string]bool{}
	for _, rec := range active {
		rp := resolved[index.AsString(rec["id"])]
		if rp == "" {
			continue
		}
		for _, b := range index.AsStrings(rec["binder"]) {
			recordIndex[index.PathKey(rp)+"|"+b] = true
		}
		// A kept tag is the user's own, not a leftover of a membership.
		for _, t := range index.KeptTags(rec) {
			recordIndex[index.PathKey(rp)+"|"+t] = true
		}
	}

	found := map[string]int{}
	for _, binderName := range binderNames {
		for _, f := range index.ByXattrItemProjects(binderName, "") {
			found[ghostReport(f, binderName, "itemprojects", recordIndex, db)]++
		}
		for _, f := range index.ByXattrTags(binderName, "") {
			found[ghostReport(f, binderName, "tags", recordIndex, db)]++
		}
	}

	if found["ghost"]+found["copy"] == 0 {
		fmt.Println("No ghost xattr entries found.")
	} else {
		fmt.Println("")
		fmt.Printf("%d ghost entry(ies), %d copy(ies) of registered files found.\n", found["ghost"], found["copy"])
	}

	if !hasBinderFilter {
		auditBookmarkDirection(db, checked)
	}
	return 0
}

// auditBookmarkDirection prints Direction 3: the identity layer against the
// index. The judging lives in bookmark_health.go, shared with cleanup.
func auditBookmarkDirection(db map[string]string, records []map[string]any) {
	if len(db) == 0 {
		return
	}
	rep, err := classifyBookmarks(db, records)
	findings, ids := rep.findings, rep.ids
	fmt.Println("")
	if err != nil {
		fmt.Println("=== Direction 3: Bookmark → Record ===")
		fmt.Println("")
		fmt.Printf("  Engine error: %v — the identity layer was not checked\n", err)
		return
	}
	fmt.Printf("=== Direction 3: Bookmark → Record (%d entry(ies)) ===\n", len(ids))
	fmt.Println("")
	if len(findings) == 0 {
		fmt.Printf("All %d bookmark(s) are claimed by a record and resolve.\n", len(ids))
		return
	}
	tally := map[string]int{}
	for _, f := range findings {
		tally[f.kind]++
		bookmarkFindingLine(f)
	}
	fmt.Println("")
	fmt.Printf("%d of %d bookmark(s) need attention: %d orphan, %d broken, %d dead, %d malformed, %d unreachable.\n",
		len(findings), len(ids), tally[bmOrphan], tally[bmBroken], tally[bmDead], tally[bmMalformed], tally[bmUnreachable])
	if tally[bmBroken] > 0 {
		fmt.Println("Broken ones are repairable: register repair")
	}
	if tally[bmUnreachable] > 0 {
		if rep.blind {
			fmt.Println("Unreachable ones were not judged: this fileanchor does not report a failed resolve's")
			fmt.Println("recorded path, so a gone file cannot be told from an absent volume. Update to 1.2.0.")
		} else {
			fmt.Println("Unreachable ones were not judged — mount the volume and run again.")
		}
	}
}
