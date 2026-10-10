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

const listUsage = "Usage: register list [<binder>] [--inbox|--curated] [--paths|--json|--print0]"

// cmdList parses flags and dispatches to the all-binders or single-binder view.
func cmdList(args []string) int {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			switch a {
			case "--inbox", "--curated", "--paths", "--print0", "--json":
			// -h and --version were listed as known and then ignored, so
			// `register list --help` printed the binder list: a request for
			// help silently ran the command instead of answering.
			case "-h", "--help":
				fmt.Println(listUsage)
				return 0
			case "-v", "--version":
				fmt.Println("register list " + registerVersion)
				return 0
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
	print0 := containsArg(args, "--print0")

	var positional []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
		}
	}
	// Other tools read --paths and --json. Without a binder they used to get
	// the human table, with exit 0; a second name was ignored.
	if len(positional) > 1 {
		fmt.Fprintf(os.Stderr, "register list: one binder at a time (got %s)\n", strings.Join(positional, ", "))
		return 1
	}
	binder := ""
	if len(positional) == 1 {
		binder = positional[0]
	}
	// Without a binder, --json lists the binders themselves; paths only
	// exist per binder.
	if binder == "" && (pathsOnly || print0) {
		fmt.Fprintln(os.Stderr, "register list: --paths and --print0 list one binder: register list <binder> --paths")
		return 1
	}
	if print0 && !pathsOnly {
		fmt.Fprintln(os.Stderr, "register list: --print0 goes with --paths")
		return 1
	}

	dir, err := notesDir()
	if err != nil {
		return 1
	}

	if binder != "" {
		if err := listBinder(dir, binder, pathsOnly, jsonOnly, print0); err != nil {
			fmt.Fprintln(os.Stderr, "register:", err)
			return 1
		}
		return 0
	}
	if err := listAllFiltered(dir, filter, jsonOnly); err != nil {
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
// jsonOnly prints one {"name","count"} line per binder instead of the table —
// and nothing at all for an empty set, where the table prints a sentence a
// consumer would otherwise have to recognize. The key is "name", not "binder":
// everywhere else "binder" is a record's membership, a list, and this line
// describes the binder itself.
func listAllFiltered(notesDir, filter string, jsonOnly bool) error {
	active, err := index.ReadIndex(notesDir)
	if err != nil {
		return err
	}

	label := ""
	if filter != "" {
		annotated, aerr := annotatedIDs(notesDir)
		if aerr != nil {
			return aerr
		}
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
			label = "inbox"
		} else {
			label = "curated"
		}
	}

	if len(active) == 0 {
		if !jsonOnly {
			switch label {
			case "inbox":
				fmt.Println("No binder has records without a note block.")
			case "curated":
				fmt.Println("No binder has records with a note block.")
			default:
				fmt.Println("No active binders found.")
			}
		}
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

	if jsonOnly {
		for _, e := range sorted {
			fmt.Println("{" + jsonPair("name", e.name) + "," + jsonPair("count", e.count) + "}")
		}
		return nil
	}

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
func listBinder(notesDir, binder string, pathsOnly, jsonOnly, print0 bool) error {
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

	// The record's openable locator: URL for URL records, bookmark path for files.
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
			fmt.Println("{" + strings.Join(memberJSONParts(rec, locator(rec)), ",") + "}")
		}
		return nil
	}

	if pathsOnly {
		// One path per line; a path with a newline in it would read as two,
		// so line mode leaves it out and --print0 (NUL-separated) has it.
		broken, multiline := 0, 0
		for _, rec := range records {
			loc := locator(rec)
			switch {
			case loc == "":
				broken++
			case print0:
				fmt.Print(loc + "\x00")
			case strings.ContainsAny(loc, "\n\r"):
				multiline++
			default:
				fmt.Println(loc)
			}
		}
		if broken > 0 {
			fmt.Fprintf(os.Stderr, "Warning: %d member(s) with broken bookmarks omitted — run register repair\n", broken)
		}
		if multiline > 0 {
			fmt.Fprintf(os.Stderr, "Warning: %d path(s) with a line break omitted — use --paths --print0\n", multiline)
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
// memberJSONParts is one member as `list <binder> --json` prints it, as the
// key:value pairs in order: id, binder, filename, kind, aka, then path or url,
// or broken. `register album` prints the same pairs and appends its own, so
// the two lines cannot drift apart. loc is the resolved path or the URL, ""
// when the bookmark does not resolve.
func memberJSONParts(rec map[string]any, loc string) []string {
	parts := []string{
		jsonPair("id", index.AsString(rec["id"])),
		jsonPair("binder", index.AsStrings(rec["binder"])),
		jsonPair("filename", index.AsString(rec["filename"])),
		jsonPair("kind", index.AsString(rec["kind"])),
	}
	if akas := uniqStrings(index.AkaList(rec), true); len(akas) > 0 {
		parts = append(parts, jsonPair("aka", akas))
	}
	// A broken bookmark stays visible to machine consumers — dropping the
	// line would be indistinguishable from "not a member".
	if loc == "" {
		return append(parts, `"broken":true`)
	}
	locKey := "path"
	if index.URLRef(rec) {
		locKey = "url"
	}
	return append(parts, jsonPair(locKey, loc))
}

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
