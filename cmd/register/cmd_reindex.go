package main

// cmd_reindex — rebuild the central index (collections/inbox.jsonl) from Markdown
// ref blocks, so every annotated record has an authoritative index entry. Idempotent: ids already in the index are skipped.

import (
	"github.com/rhsev/fileregister/internal/index"

	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func cmdReindex(args []string) int {
	dry := false
	for _, a := range args {
		switch a {
		case "--dry-run":
			dry = true
		case "-h", "--help":
			fmt.Println("Usage: register reindex [--dry-run]")
			return 0
		case "-v", "--version":
			fmt.Println("register reindex " + registerVersion)
			return 0
		default:
			if strings.HasPrefix(a, "-") {
				return unknownOption("reindex", a)
			}
		}
	}

	nd, err := notesDir()
	if err != nil {
		return 1
	}
	indexPath := filepath.Join(nd, "collections", "inbox.jsonl")

	allIndex, refsOK := loadRefs(nd)
	if !refsOK {
		return 1
	}
	indexed := map[string]bool{}
	for _, r := range allIndex {
		indexed[index.AsString(r["id"])] = true
	}
	mdRecs := readAnnotations(nd)

	// Backfill id for hand-written aka-only blocks via the index's aka → id map.
	akaToID := map[string]string{}
	for _, r := range allIndex {
		for _, a := range index.AsStrings(r["aka"]) {
			if a != "" {
				akaToID[a] = index.AsString(r["id"])
			}
		}
	}
	for i, r := range mdRecs {
		if strings.TrimSpace(index.AsString(r["id"])) != "" {
			continue
		}
		foundID := ""
		for _, a := range index.AsStrings(r["aka"]) {
			if a == "" {
				continue
			}
			if id, ok := akaToID[a]; ok {
				foundID = id
				break
			}
		}
		if foundID != "" {
			cp := map[string]any{}
			for k, v := range r {
				cp[k] = v
			}
			cp["id"] = foundID
			mdRecs[i] = cp
		}
	}

	// Records present in Markdown but missing from the index. Every block of an
	// id is kept — one per binder — so JSONLWriteMany folds them into one record
	// carrying every membership; only exact (id, binder) duplicates drop.
	seen := map[string]bool{}
	var missing []map[string]any
	for _, r := range mdRecs {
		id := index.AsString(r["id"])
		key := id + "\x00" + index.AsString(r["binder"])
		if id == "" || indexed[id] || seen[key] {
			continue
		}
		seen[key] = true
		missing = append(missing, r)
	}

	if len(missing) == 0 {
		fmt.Printf("Index is complete: %d record(s), nothing to add.\n", len(indexed))
		return 0
	}

	fmt.Printf("%d record(s) in Markdown have no index entry.\n", len(missing))
	if dry {
		for _, r := range missing {
			fmt.Printf("  + %s  %s  %s\n", index.AsString(r["id"]), index.AsString(r["binder"]), index.AsString(r["filename"]))
		}
		fmt.Println("(dry run — nothing written)")
		return 0
	}

	var refs []index.RefRecord
	for _, r := range missing {
		ref := index.NewRefRecord(r) // index.NewRefRecord ignores _note_file
		if !ref.Valid() {
			fmt.Fprintf(os.Stderr, "  Skipping %s: missing required field: id\n", index.AsString(r["id"]))
			continue
		}
		refs = append(refs, ref)
	}

	actions, werr := index.JSONLWriteMany(refs, indexPath)
	if werr != nil {
		fmt.Fprintln(os.Stderr, "register:", werr)
		return 1
	}
	ensureSchema(nd) // a vault bootstrapped purely via reindex is stamped too
	added, merged := 0, 0
	for _, a := range actions {
		switch a {
		case "appended":
			added++
		case "updated":
			merged++ // additional membership folded into a record added above
		}
	}
	if merged > 0 {
		fmt.Printf("Reindex complete: %d added (%d extra membership(s) folded) → %s\n", added, merged, filepath.Base(indexPath))
	} else {
		fmt.Printf("Reindex complete: %d added → %s\n", added, filepath.Base(indexPath))
	}
	return 0
}
