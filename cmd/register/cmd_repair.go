package main

// cmd_repair — restore broken bookmarks via Spotlight (register repair).

import (
	"github.com/rhsev/fileregister/internal/index"

	"bufio"
	"fmt"
	"os"
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
// nothing has been found. Returns unique candidates.
func locateCandidates(id, binder, filename, backend string) []string {
	candidates := index.ByDescriptionID(id)
	if len(candidates) == 0 && filename != "" {
		candidates = index.ByFilename(filename)
	}
	if len(candidates) == 0 && filename != "" {
		if backend == "tags" {
			candidates = index.ByXattrTags(binder, filename)
		} else {
			candidates = index.ByXattrItemProjects(binder, filename)
		}
	}
	return uniqStrings(candidates, false)
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
	active := mustRefs(nd) // repair the bookmark blob for every record, bookmarks included
	if len(active) == 0 {
		fmt.Println("No ref records found.")
		return 0
	}

	fmt.Fprintf(os.Stderr, "Resolving bookmarks for %d record(s)…\n", len(active))
	resolved, rerr := resolveRecordPaths(active)
	if rerr != nil {
		// With the engine failing, every record looks broken — repairing now
		// would re-bind healthy bookmarks to whatever Spotlight finds.
		fmt.Fprintf(os.Stderr, "Error: fileanchor batch resolve failed: %v — refusing to repair\n", rerr)
		return 1
	}

	var broken []map[string]any
	for _, r := range active {
		if index.URLRef(r) || resolved[index.AsString(r["id"])] != "" {
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

	for _, rec := range broken {
		id := index.AsString(rec["id"])
		binders := nonEmptyBinders(rec)
		label := repairLabel(rec)
		binderLabel := strings.Join(binders, ", ")

		fmt.Printf("Repairing: [%s] %s\n", binderLabel, label)

		firstBinder := ""
		if len(binders) > 0 {
			firstBinder = binders[0]
		}
		candidates := locateCandidates(id, firstBinder, index.AsString(rec["filename"]), index.XattrBackend(rec))

		newPath := ""
		if len(candidates) == 1 {
			newPath = candidates[0]
			fmt.Printf("  Found: %s\n", newPath)
		} else if len(candidates) > 1 {
			if interactive {
				fmt.Fprintln(os.Stderr, "  Multiple candidates found:")
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
				fmt.Printf("  %d candidates — rerun with --interactive to choose:\n", len(candidates))
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
