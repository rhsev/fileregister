package main

// cmd_resolve — resolve a key (id or aka handle) to the file's path.
// Composable: open "$(register resolve <key>)".

import (
	"github.com/rhsev/fileregister/internal/index"

	"fmt"
	"os"
	"strings"
)

const resolveUsage = `Usage: register resolve <key>

  Resolve a key — a record id or an aka handle — to the file's path.
  Composable: open "$(register resolve vertrag-mueller)"

Options:
        --record                 Print the record (id, binder, filename, aka, path) instead of just the path
    -h, --help
    -v, --version`

// cmdResolve runs `register resolve` and returns the process exit code.
func cmdResolve(args []string) int {
	record := false
	key := ""
	for _, a := range args {
		switch a {
		case "--record":
			record = true
		case "-h", "--help":
			fmt.Println(resolveUsage)
			return 0
		case "-v", "--version":
			fmt.Println("register resolve " + registerVersion)
			return 0
		default:
			if strings.HasPrefix(a, "-") {
				return unknownOption("resolve", a)
			}
			if key == "" {
				key = a
			}
		}
	}

	if key == "" {
		fmt.Fprintln(os.Stderr, "Usage: register resolve <key>")
		return 1
	}

	dir, err := notesDir()
	if err != nil {
		return 1
	}
	recs, err := index.ReadAllRefs(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "register:", err)
		return 1
	}

	rec := index.ResolveKey(recs, key)
	if rec == nil {
		fmt.Fprintf(os.Stderr, "No record resolves for key '%s'\n", key)
		return 1
	}

	// Locator dispatch: a url ref resolves to its URL, a file ref via bookmark.
	var path string
	if u := index.RefURL(rec); u != "" {
		path = u
	} else {
		var err error
		if path, err = index.BookmarkGet(index.AsString(rec["id"])); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			return 1
		}
	}

	if record {
		fmt.Printf("id:       %s\n", index.AsString(rec["id"]))
		fmt.Printf("binder:   %s\n", strings.Join(index.AsStrings(rec["binder"]), ", "))
		if index.Truthy(rec["filename"]) {
			fmt.Printf("filename: %s\n", index.AsString(rec["filename"]))
		}
		if aka := index.AkaList(rec); len(aka) > 0 {
			fmt.Printf("aka:      %s\n", strings.Join(aka, ", "))
		}
		if index.Truthy(rec["kind"]) {
			fmt.Printf("kind:     %s\n", index.AsString(rec["kind"]))
		}
		if index.URLRef(rec) {
			fmt.Printf("url:      %s\n", path)
		} else {
			shown := path
			if shown == "" {
				shown = "(broken bookmark)"
			}
			fmt.Printf("path:     %s\n", shown)
		}
		if path == "" {
			return 1
		}
		return 0
	}

	if path == "" {
		fmt.Fprintf(os.Stderr, "Bookmark for id %s does not resolve (broken — try 'register repair')\n", index.AsString(rec["id"]))
		return 1
	}
	fmt.Println(path)
	return 0
}
