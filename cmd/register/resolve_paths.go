package main

// resolve_paths — the one shared "collect unique ids → BatchGet" prelude.

import (
	"github.com/rhsev/fileregister/v2/internal/index"

	"fmt"
)

// resolveRecordPaths resolves every non-URL record's bookmark in one engine
// batch. Returns id → path ("" for a missing or unresolvable blob) plus the
// engine-level error, if any — partial results are kept, so callers can tell
// "engine broken" apart from "file gone".
func resolveRecordPaths(records []map[string]any) (map[string]string, error) {
	return index.BatchGet(recordFileIDs(records))
}

// resolveRecords is resolveRecordPaths with the recorded path of each blob that
// does not resolve (repair tells an unmounted volume from a gone file by it).
func resolveRecords(records []map[string]any) (map[string]index.Resolution, error) {
	return index.BatchResolve(recordFileIDs(records))
}

// recordFileIDs collects the unique ids of the non-URL records.
func recordFileIDs(records []map[string]any) []string {
	var ids []string
	seen := map[string]bool{}
	for _, r := range records {
		if index.URLRef(r) {
			continue
		}
		id := index.AsString(r["id"])
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

// displacedEntry is a record whose file was found elsewhere while another
// file lies where it was bookmarked (index.Resolution.AtOldPath).
type displacedEntry struct {
	binders            []string
	label, path, other string
}

func printDisplaced(d displacedEntry) {
	fmt.Printf("  [%s] %s\n", binderTag(d.binders), d.label)
	fmt.Printf("    file:      %s\n", d.path)
	fmt.Printf("    old place: %s\n", d.other)
}

// carriesID reports whether the file has id in either of its carriers.
func carriesID(path, id string) bool {
	return index.HasDescriptionID(path, id) || index.SyncID(path) == id
}
