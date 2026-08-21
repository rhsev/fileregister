package main

// resolve_paths — the one shared "collect unique ids → BatchGet" prelude.

import (
	"github.com/rhsev/fileregister/internal/index"
)

// resolveRecordPaths resolves every non-URL record's bookmark in one engine
// batch. Returns id → path ("" for a missing or unresolvable blob) plus the
// engine-level error, if any — partial results are kept, so callers can tell
// "engine broken" apart from "file gone".
func resolveRecordPaths(records []map[string]any) (map[string]string, error) {
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
	return index.BatchGet(ids)
}
