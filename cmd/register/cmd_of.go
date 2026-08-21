package main

// cmd_of — reverse lookup: given a file, report the record(s) it belongs to —
// its id, aka handle(s), and which collection(s) it is in. Inverse of resolve.
//

import (
	"github.com/rhsev/fileregister/internal/index"

	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const ofUsage = `Usage: register of <file>

  Reverse lookup: report the record(s) a file belongs to —
  its id, aka handle(s), and which collection(s) it is in.
    -h, --help
    -v, --version`

// cmdOf runs `register of` and returns the process exit code.
func cmdOf(args []string) int {
	arg := ""
	for _, a := range args {
		switch a {
		case "-h", "--help":
			fmt.Println(ofUsage)
			return 0
		case "-v", "--version":
			fmt.Println("register of " + registerVersion)
			return 0
		default:
			if strings.HasPrefix(a, "-") {
				return unknownOption("of", a)
			}
			if arg == "" {
				arg = a
			}
		}
	}
	if arg == "" {
		fmt.Fprintln(os.Stderr, "Usage: register of <file>")
		return 1
	}

	path := index.ExpandPath(arg)
	if _, err := os.Stat(path); err != nil {
		fmt.Fprintf(os.Stderr, "No such file: %s\n", path)
		return 1
	}

	dir, err := notesDir()
	if err != nil {
		return 1
	}
	refs, err := index.ReadAllRefs(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "register:", err)
		return 1
	}

	ids := index.OfFileIDs(path)
	var recs []map[string]any
	if len(ids) > 0 {
		idSet := map[string]bool{}
		for _, id := range ids {
			idSet[id] = true
		}
		for _, r := range refs {
			if idSet[index.AsString(r["id"])] {
				recs = append(recs, r)
			}
		}
	}

	// Fallback: no id on the file (never added / xattr stripped) → match by basename.
	matchedByFilename := false
	if len(recs) == 0 {
		base := filepath.Base(path)
		for _, r := range refs {
			if index.AsString(r["filename"]) == base {
				recs = append(recs, r)
			}
		}
		matchedByFilename = len(recs) > 0
	}

	if len(recs) == 0 {
		if len(ids) == 0 {
			fmt.Fprintln(os.Stderr, "Not managed by fileregister: no id on the file, and no index record matches its name.")
		} else {
			fmt.Fprintf(os.Stderr, "File carries id %s but no index record references it (try 'register reindex').\n", strings.Join(ids, ", "))
		}
		return 1
	}

	// A file can belong to several binders — aggregate across the matching records.
	idList := uniqStrings(mapStr(recs, "id"), false)
	akas := uniqStrings(flatArr(recs, "aka"), true)
	binders := uniqStrings(flatArr(recs, "binder"), true)
	filename := firstNonEmpty(mapStr(recs, "filename"))

	fmt.Printf("file:     %s\n", path)
	fmt.Printf("id:       %s\n", strings.Join(idList, ", "))
	if len(akas) > 0 {
		fmt.Printf("aka:      %s\n", strings.Join(akas, ", "))
	}
	if len(binders) == 0 {
		fmt.Println("binder:   (orphan — no active collection)")
	} else {
		fmt.Printf("binder:   %s\n", strings.Join(binders, ", "))
	}
	if filename != "" {
		fmt.Printf("filename: %s\n", filename)
	}
	if index.ManagedMarked(path) {
		fmt.Println("managed:  ★ yes")
	} else {
		fmt.Println("managed:  no")
	}
	if matchedByFilename {
		fmt.Fprintln(os.Stderr, "(matched by filename — the file carries no id xattr, so this is best-effort)")
	}
	return 0
}

// mapStr returns index.AsString(rec[key]) for each record.
func mapStr(recs []map[string]any, key string) []string {
	out := make([]string, len(recs))
	for i, r := range recs {
		out[i] = index.AsString(r[key])
	}
	return out
}

// flatArr concatenates Array(rec[key]) across records.
func flatArr(recs []map[string]any, key string) []string {
	var out []string
	for _, r := range recs {
		out = append(out, index.AsStrings(r[key])...)
	}
	return out
}

// uniqStrings dedupes in order; when rejectEmpty, drops empty strings.
func uniqStrings(in []string, rejectEmpty bool) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if rejectEmpty && s == "" {
			continue
		}
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// firstNonEmpty returns the first non-empty string, or "".
func firstNonEmpty(in []string) string {
	for _, s := range in {
		if s != "" {
			return s
		}
	}
	return ""
}
