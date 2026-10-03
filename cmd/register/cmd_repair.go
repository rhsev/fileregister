package main

// cmd_repair — restore broken bookmarks via Spotlight (register repair).

import (
	"github.com/rhsev/fileregister/internal/index"

	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// repairLabel is the best human label for a record: filename, then title, then id.
func repairLabel(rec map[string]any) string {
	if fn := index.AsString(rec["filename"]); fn != "" {
		return fn
	}
	if title := index.AsString(rec["title"]); title != "" {
		return title
	}
	return index.AsString(rec["id"])
}

// locateCandidates finds candidate paths for a relocated file, by reliability:
// id xattr → filename → xattr-backend + basename. Each stage runs only while
// nothing has been found. Returns unique candidates and whether they came
// from the id stage — the only one that identifies the file, not just a file
// with the same name.
func locateCandidates(id, binder, filename, backend string) ([]string, bool) {
	if c := index.ByDescriptionID(id); len(c) > 0 {
		return uniqStrings(c, false), true
	}
	var candidates []string
	if filename != "" {
		candidates = index.ByFilename(filename)
	}
	if len(candidates) == 0 && filename != "" {
		if backend == "tags" {
			candidates = index.ByXattrTags(binder, filename)
		} else {
			candidates = index.ByXattrItemProjects(binder, filename)
		}
	}
	return uniqStrings(candidates, false), false
}

// unmountedVolume returns the name of the volume p lies on when that volume
// is not mounted, or "". A file there is not gone, just not reachable now.
func unmountedVolume(p string) string {
	rest, ok := strings.CutPrefix(p, "/Volumes/")
	if !ok {
		return ""
	}
	name, _, _ := strings.Cut(rest, "/")
	if name == "" {
		return ""
	}
	if _, err := os.Stat(filepath.Join("/Volumes", name)); err == nil {
		return ""
	}
	return name
}

// volumeOf is the mount a path lies on as far as its path shows: /Volumes/<name>, or /.
func volumeOf(p string) string {
	if rest, ok := strings.CutPrefix(p, "/Volumes/"); ok {
		name, _, _ := strings.Cut(rest, "/")
		return "/Volumes/" + name
	}
	return "/"
}

// candidateProblem says why a found file must not be re-bound under id, or "".
// A copy in the Trash or a backup, and a file that is another record's, carry
// the same name — or even the same id — as the file that went missing.
func candidateProblem(path, id string, db map[string]string, indexIDs map[string]bool) string {
	for _, marker := range []string{"/.Trash/", "/.Trashes/", "/Backups.backupdb/", "/.MobileBackups/"} {
		if strings.Contains(path, marker) {
			return "in the Trash or a backup"
		}
	}
	if strings.HasPrefix(path, "/Volumes/.timemachine/") {
		return "in a Time Machine backup"
	}
	for _, own := range index.OwnIDs(db, path) {
		if own == id {
			continue
		}
		if _, ok := db[own]; ok || indexIDs[own] {
			return "it is record " + own
		}
	}
	return ""
}

// repairRecord re-binds the relocated file under its EXISTING id, refreshing the
// binder xattr + ★. Returns the (unchanged) id, or "" on failure.
func repairRecord(rec map[string]any, newPath string) string {
	id := index.AsString(rec["id"])
	binders := nonEmptyBinders(rec)
	backend := index.XattrBackend(rec)

	newID, err := index.Rebind(id, newPath)
	if err != nil || newID == "" {
		return ""
	}
	for _, b := range binders {
		index.XattrBackendAdd(newPath, b, backend)
	}
	if len(binders) > 0 {
		index.ManagedMark(newPath)
	}
	return id
}

var digitsRe = regexp.MustCompile(`^\d+$`)

func cmdRepair(args []string) int {
	interactive := false
	for _, a := range args {
		switch a {
		case "--interactive":
			interactive = true
		case "-h", "--help":
			fmt.Println("Usage: register repair [options]")
			return 0
		case "-v", "--version":
			fmt.Println("register repair " + registerVersion)
			return 0
		default:
			if strings.HasPrefix(a, "-") {
				return unknownOption("repair", a)
			}
		}
	}

	nd, err := notesDir()
	if err != nil {
		return 1
	}

	fmt.Fprintln(os.Stderr, "Collecting type:ref records…")
	active, refsOK := loadRefs(nd) // repair the bookmark blob for every record, bookmarks included
	if !refsOK {
		return 1
	}
	if len(active) == 0 {
		fmt.Println("No ref records found.")
		return 0
	}

	db, err := index.LoadDB()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}
	indexIDs := map[string]bool{}
	for _, r := range active {
		indexIDs[index.AsString(r["id"])] = true
	}

	fmt.Fprintf(os.Stderr, "Resolving bookmarks for %d record(s)…\n", len(active))
	resolved, rerr := resolveRecords(active)
	if rerr != nil {
		// With the engine failing, every record looks broken — repairing now
		// would re-bind healthy bookmarks to whatever Spotlight finds.
		fmt.Fprintf(os.Stderr, "Error: fileanchor batch resolve failed: %v — refusing to repair\n", rerr)
		return 1
	}

	var broken []map[string]any
	for _, r := range active {
		if index.URLRef(r) || resolved[index.AsString(r["id"])].Path != "" {
			continue
		}
		broken = append(broken, r)
	}
	if len(broken) == 0 {
		fmt.Println("All bookmarks resolve correctly. Nothing to repair.")
		return 0
	}

	fmt.Printf("Found %d broken bookmark(s).\n", len(broken))
	fmt.Println("")

	stdin := bufio.NewReader(os.Stdin)
	repaired := 0
	type unresolvedT struct{ id, binder, title, noteFile string }
	var notFound []unresolvedT
	offline := map[string]int{}

	for _, rec := range broken {
		id := index.AsString(rec["id"])
		binders := nonEmptyBinders(rec)
		label := repairLabel(rec)
		binderLabel := strings.Join(binders, ", ")

		fmt.Printf("Repairing: [%s] %s\n", binderLabel, label)

		// A file on a volume that is not mounted is not missing. Searching
		// would find a copy elsewhere and bind the record to it for good.
		lastPath := resolved[id].LastPath
		if vol := unmountedVolume(lastPath); vol != "" {
			fmt.Printf("  On volume '%s', which is not mounted — skipped\n\n", vol)
			offline[vol]++
			continue
		}

		firstBinder := ""
		if len(binders) > 0 {
			firstBinder = binders[0]
		}
		found, byID := locateCandidates(id, firstBinder, index.AsString(rec["filename"]), index.XattrBackend(rec))
		var candidates []string
		for _, c := range found {
			if why := candidateProblem(c, id, db, indexIDs); why != "" {
				fmt.Printf("  Ignored %s: %s\n", c, why)
				continue
			}
			candidates = append(candidates, c)
		}

		// Only a single hit by id is taken without asking, and only on the
		// volume the file was on. A hit by name is some file with that name.
		newPath := ""
		sure := byID && len(candidates) == 1 &&
			(lastPath == "" || volumeOf(lastPath) == volumeOf(candidates[0]))
		if sure {
			newPath = candidates[0]
			fmt.Printf("  Found: %s\n", newPath)
		} else if len(candidates) > 0 {
			if interactive {
				fmt.Fprintln(os.Stderr, "  Candidates found — please confirm:")
				for i, c := range candidates {
					fmt.Fprintf(os.Stderr, "    %d. %s\n", i+1, c)
				}
				fmt.Fprintf(os.Stderr, "  Choose [1-%d] or blank to skip: ", len(candidates))
				choice, _ := stdin.ReadString('\n')
				choice = strings.TrimSpace(choice)
				if digitsRe.MatchString(choice) {
					if idx, _ := strconv.Atoi(choice); idx-1 >= 0 && idx-1 < len(candidates) {
						newPath = candidates[idx-1]
					}
				}
			} else {
				fmt.Printf("  %d candidate(s) to confirm — rerun with --interactive to choose:\n", len(candidates))
				for _, c := range candidates {
					fmt.Printf("    %s\n", c)
				}
			}
		}

		if newPath == "" && interactive {
			newPath = promptForPath(rec, stdin)
		}

		if newPath == "" {
			notFound = append(notFound, unresolvedT{id, binderLabel, label, index.AsString(rec["_note_file"])})
			fmt.Println("  Not found — skipped")
			fmt.Println("")
			continue
		}

		if repairRecord(rec, newPath) != "" {
			fmt.Printf("  Repaired: re-bound id %s (unchanged)\n", id)
			repaired++
		} else {
			fmt.Printf("  Failed to create new bookmark for %s\n", newPath)
			notFound = append(notFound, unresolvedT{id, binderLabel, label, index.AsString(rec["_note_file"])})
		}
		fmt.Println("")
	}

	fmt.Printf("Repair complete: %d repaired, %d unresolved.\n", repaired, len(notFound))
	for vol, n := range offline {
		fmt.Printf("%d record(s) on volume '%s', which is not mounted — they resolve again once it is connected.\n", n, vol)
	}

	if len(notFound) > 0 {
		fmt.Println("")
		fmt.Printf("Unresolved (%d):\n", len(notFound))
		for _, r := range notFound {
			fmt.Printf("  [%s] %s (id: %s)\n", r.binder, r.title, r.id)
			fmt.Printf("    in: %s\n", r.noteFile)
		}
		fmt.Println("")
		fmt.Println("Hint: run with --interactive to provide paths manually.")
	}
	return 0
}

// promptForPath asks the user for a path when a file cannot be located.
func promptForPath(rec map[string]any, stdin *bufio.Reader) string {
	label := index.AsString(rec["title"])
	if label == "" {
		label = index.AsString(rec["id"])
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "Cannot locate file for: [%s] %s (id: %s)\n", strings.Join(index.AsStrings(rec["binder"]), ", "), label, index.AsString(rec["id"]))
	fmt.Fprint(os.Stderr, "  Enter path (or blank to skip): ")
	path, _ := stdin.ReadString('\n')
	path = strings.TrimSpace(path)
	if path == "" || !index.FileExists(path) {
		return ""
	}
	return path
}
