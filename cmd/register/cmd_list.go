package main

// cmd_list — the `list` command: all binders with counts, or the files in one.
// Listing reads the index directly; the Markdown layer is consulted only for the
// --inbox/--curated filter.

import (
	"github.com/rhsev/fileregister/internal/index"

	"fmt"
	"os"
	"sort"
	"strings"
	"unicode/utf8"
)

// cmdList parses flags and dispatches to the all-binders or single-binder view.
func cmdList(args []string) int {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			switch a {
			case "--inbox", "--curated", "--paths", "--json", "-h", "--help", "-v", "--version":
			default:
				return unknownOption("list", a)
			}
		}
	}

	filter := ""
	if containsArg(args, "--inbox") {
		filter = "inbox"
	} else if containsArg(args, "--curated") {
		filter = "curated"
	}
	pathsOnly := containsArg(args, "--paths")
	jsonOnly := containsArg(args, "--json")

	binder := ""
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			binder = a
			break
		}
	}

	dir, err := notesDir()
	if err != nil {
		return 1
	}

	if binder != "" {
		if err := listBinder(dir, binder, pathsOnly, jsonOnly); err != nil {
			fmt.Fprintln(os.Stderr, "register:", err)
			return 1
		}
		return 0
	}
	if err := listAllFiltered(dir, filter); err != nil {
		fmt.Fprintln(os.Stderr, "register:", err)
		return 1
	}
	return 0
}

func containsArg(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// listAllFiltered lists all binders with counts. filter "" is the pure-register
// view; "inbox"/"curated" restrict to records without / with a Markdown annotation.
func listAllFiltered(notesDir, filter string) error {
	active, err := index.ReadIndex(notesDir)
	if err != nil {
		return err
	}

	label := ""
	if filter != "" {
		annotated := annotatedIDs(notesDir)
		kept := active[:0]
		for _, r := range active {
			a := annotated[r.ID]
			if filter == "inbox" && a {
				continue
			}
			if filter == "curated" && !a {
				continue
			}
			kept = append(kept, r)
		}
		active = kept
		if filter == "inbox" {
			label = " (inbox only)"
		} else {
			label = " (annotated only)"
		}
	}

	if len(active) == 0 {
		fmt.Printf("No active binders found%s.\n", label)
		return nil
	}

	// Group by binder — count each membership in a record's set.
	byBinder := make(map[string]int)
	for _, r := range active {
		for _, b := range r.Binders {
			byBinder[b]++
		}
	}

	type binderEntry struct {
		name  string
		count int
	}
	sorted := make([]binderEntry, 0, len(byBinder))
	for name, count := range byBinder {
		sorted = append(sorted, binderEntry{name, count})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return strings.ToLower(sorted[i].name) < strings.ToLower(sorted[j].name)
	})

	// Column width is measured in runes, not bytes, so non-ASCII names align.
	maxLen := 0
	for _, e := range sorted {
		if l := utf8.RuneCountInString(e.name); l > maxLen {
			maxLen = l
		}
	}

	for _, e := range sorted {
		fileWord := "files"
		if e.count == 1 {
			fileWord = "file"
		}
		fmt.Printf("%s  (%d %s)\n", ljust(e.name, maxLen), e.count, fileWord)
	}
	return nil
}

// listBinder lists the members of one binder: a table by default, or bare
// locators (--paths) / neutral JSONL (--json).
func listBinder(notesDir, binder string, pathsOnly, jsonOnly bool) error {
	all, err := index.ReadAllRefs(notesDir)
	if err != nil {
		return err
	}
	var records []map[string]any
	for _, r := range all {
		for _, b := range index.AsStrings(r["binder"]) {
			if b == binder {
				records = append(records, r)
				break
			}
		}
	}
	if len(records) == 0 {
		fmt.Fprintf(os.Stderr, "No refs found for binder '%s'\n", binder)
		return nil // warn, but exit 0
	}

	// Resolve bookmark paths for the file (non-url) refs in one batch.
	resolved, rerr := resolveRecordPaths(records)
	if rerr != nil {
		fmt.Fprintf(os.Stderr, "Warning: fileanchor batch resolve failed: %v — paths shown as broken\n", rerr)
	}

	// The record's openable locator: URL for url refs, bookmark path for files.
	locator := func(r map[string]any) string {
		if u := index.RefURL(r); u != "" {
			return u
		}
		return resolved[index.AsString(r["id"])]
	}

	// sort by filename, id as the deterministic tiebreak. (A `date` field was
	// consulted here once — dead since add never writes one.)
	sort.SliceStable(records, func(i, j int) bool {
		fi, fj := index.AsString(records[i]["filename"]), index.AsString(records[j]["filename"])
		if fi != fj {
			return fi < fj
		}
		return index.AsString(records[i]["id"]) < index.AsString(records[j]["id"])
	})

	if jsonOnly {
		for _, rec := range records {
			loc := locator(rec)
			parts := []string{
				jsonPair("id", index.AsString(rec["id"])),
				jsonPair("binder", index.AsStrings(rec["binder"])),
				jsonPair("filename", index.AsString(rec["filename"])),
				jsonPair("kind", index.AsString(rec["kind"])),
			}
			if akas := uniqStrings(index.AkaList(rec), true); len(akas) > 0 {
				parts = append(parts, jsonPair("aka", akas))
			}
			// A broken bookmark stays visible to machine consumers — dropping
			// the line would be indistinguishable from "not a member".
			if loc == "" {
				parts = append(parts, `"broken":true`)
			} else {
				locKey := "path"
				if index.URLRef(rec) {
					locKey = "url"
				}
				parts = append(parts, jsonPair(locKey, loc))
			}
			fmt.Println("{" + strings.Join(parts, ",") + "}")
		}
		return nil
	}

	if pathsOnly {
		broken := 0
		for _, rec := range records {
			if loc := locator(rec); loc != "" {
				fmt.Println(loc)
			} else {
				broken++
			}
		}
		if broken > 0 {
			fmt.Fprintf(os.Stderr, "Warning: %d member(s) with broken bookmarks omitted — run register repair\n", broken)
		}
		return nil
	}

	// Default table: filename (padded), locator, optional [kind].
	maxTitle := 0
	for _, r := range records {
		if l := utf8.RuneCountInString(index.AsString(r["filename"])); l > maxTitle {
			maxTitle = l
		}
	}
	maxTitle = clampInt(maxTitle, 8, 40)

	for _, rec := range records {
		loc := locator(rec)
		if loc == "" {
			loc = "(broken bookmark)"
		}
		kindTag := ""
		if kind := index.AsString(rec["kind"]); kind != "" {
			kindTag = "  [" + kind + "]"
		}
		fmt.Printf("%s  %s%s\n", ljust(index.AsString(rec["filename"]), maxTitle), loc, kindTag)
	}
	return nil
}

// jsonPair renders "key":value with HTML escaping off, so the byte form matches
// the wire form consumers expect (<, >, & and non-ASCII left intact).
func jsonPair(key string, val any) string {
	return index.JSONVal(key) + ":" + index.JSONVal(val)
}

// ljust pads s with spaces to n runes (measured in characters, not bytes).
func ljust(s string, n int) string {
	if pad := n - utf8.RuneCountInString(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
