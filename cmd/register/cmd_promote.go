package main

// cmd_promote — create a curated Markdown annotation for a binder's records.
// The record stays in the index; promote adds a lean
// annotation block per record. Idempotent.

import (
	"github.com/rhsev/fileregister/internal/index"

	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func cmdPromote(args []string) int {
	vals, bools, _, unk := parseFlags(args,
		map[string]bool{"--binder": true, "--target": true, "--id": true},
		map[string]bool{"--edit": true})
	if unk != "" {
		return unknownOption("promote", unk)
	}
	if bools["--help"] {
		fmt.Println("Usage: register promote --binder <name> [--target <file>.md] [--id <id>] [--edit]")
		return 0
	}
	if bools["--version"] {
		fmt.Println("register promote " + registerVersion)
		return 0
	}
	binder, hasBinder := vals["--binder"]
	target := vals["--target"]
	id := vals["--id"]
	edit := bools["--edit"]

	if !hasBinder {
		binder = os.Getenv("REGISTER_BINDER")
		hasBinder = binder != ""
	}
	if !hasBinder {
		fmt.Fprintln(os.Stderr, "Error: --binder is required (or set REGISTER_BINDER env var)")
		fmt.Fprintln(os.Stderr, "Usage: register promote --binder <name> [--target <file>.md] [--id <id>] [--edit]")
		return 1
	}

	nd, err := notesDir()
	if err != nil {
		return 1
	}

	// One index read serves the key resolution and the binder filter.
	refs := mustRefs(nd)
	if id != "" {
		raw := id
		if rec := index.ResolveKey(refs, raw); rec != nil {
			id = index.AsString(rec["id"])
		} else {
			fmt.Fprintf(os.Stderr, "Error: '%s' does not match any record id or aka\n", raw)
			return 1
		}
	}

	tgt := defaultPromoteTarget(nd, binder)
	if target != "" {
		tgt = index.ExpandPath(target)
	}
	if !strings.HasSuffix(strings.ToLower(tgt), ".md") {
		fmt.Fprintln(os.Stderr, "Error: promote target must be a .md file")
		return 1
	}

	records := recordsForBinder(refs, binder)
	if id != "" {
		var filtered []map[string]any
		for _, r := range records {
			if index.AsString(r["id"]) == id {
				filtered = append(filtered, r)
			}
		}
		records = filtered
		if len(records) == 0 {
			fmt.Fprintf(os.Stderr, "No record found for binder '%s' with id '%s'\n", binder, id)
			return 1
		}
	}
	if len(records) == 0 {
		fmt.Printf("No records for binder '%s'. Nothing to annotate.\n", binder)
		return 0
	}

	fmt.Printf("Annotating %d record(s) → %s\n", len(records), filepath.Base(tgt))
	promoted, noop, failed := promoteRecords(records, tgt, binder)
	fmt.Println("Promote complete:")
	fmt.Printf("  Annotated : %d\n", promoted)
	if noop > 0 {
		fmt.Printf("  Already   : %d\n", noop)
	}
	if failed > 0 {
		fmt.Printf("  Failed    : %d\n", failed)
	}

	if edit && promoted > 0 {
		editor := os.Getenv("VISUAL")
		if editor == "" {
			editor = os.Getenv("EDITOR")
		}
		if editor == "" {
			editor = "vi"
		}
		// $EDITOR may carry arguments ("code -w") — split before LookPath.
		if argv := strings.Fields(editor); len(argv) > 0 {
			if p, lerr := exec.LookPath(argv[0]); lerr == nil {
				syscall.Exec(p, append(argv, tgt), os.Environ())
			} else {
				fmt.Fprintf(os.Stderr, "Warning: editor '%s' not found — note written, not opened\n", argv[0])
			}
		}
	}
	return 0
}
