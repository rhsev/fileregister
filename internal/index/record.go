package index

// record — the index ref-record layer: reading the index and resolving a key.
//
// Unlike listAll's reader (which drops binderless records — bookmarks with no
// binder to list), this keeps every `type:"ref"` record, because key resolution
// matches by id/aka across the whole index including binderless bookmarks.
//
// Records are kept as untyped maps so tolerant field access (id may be a
// number or string, binder/aka a scalar or an array) ports faithfully.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Record is the slim per-membership view used by the pure-register list:
// type/id/binder only, decoded tolerantly (id may be a number, binder a legacy
// scalar) stays possible.
type Record struct {
	Type    string
	ID      string   // decoded manually with UseNumber
	Binders []string // binder is a set (array)
}

// ReadIndex returns the active memberships from collections/*.jsonl — every
// type:ref record with a non-empty binder set (bookmarks are skipped).
func ReadIndex(notesDir string) ([]Record, error) {
	dir := filepath.Join(notesDir, "collections")
	entries, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	sort.Strings(entries)

	var recs []Record
	for _, f := range entries {
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(fh)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // same limit as ReadAllRefs
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			dec := json.NewDecoder(strings.NewReader(line))
			dec.UseNumber()
			var raw map[string]json.RawMessage
			if err := dec.Decode(&raw); err != nil {
				continue
			}
			// type must be "ref"
			var typ string
			if err := json.Unmarshal(raw["type"], &typ); err != nil || typ != "ref" {
				continue
			}
			// binder is a set (array) — schema 3. A legacy scalar string is
			// still read, but loudly: the write path never emits it, so any
			// occurrence is a straggler to fix by hand (NOTES-schema.md).
			// Records with an empty set are skipped — bookmarks have no
			// binder to list.
			var binders []string
			if b, ok := raw["binder"]; ok {
				if json.Unmarshal(b, &binders) != nil {
					var single string
					if json.Unmarshal(b, &single) == nil && single != "" {
						binders = []string{single}
						fmt.Fprintf(os.Stderr, "Warning: %s: legacy scalar binder %q (schema 3 expects an array) — fix the line by hand\n", filepath.Base(f), single)
					}
				}
			}
			clean := binders[:0]
			for _, b := range binders {
				if strings.TrimSpace(b) != "" {
					clean = append(clean, b)
				}
			}
			binders = clean
			if len(binders) == 0 {
				continue
			}
			// id: stringify (numbers → decimal string, strings → unchanged)
			var id string
			if rawID, ok := raw["id"]; ok {
				// try as json.Number first
				var num json.Number
				if err := json.Unmarshal(rawID, &num); err == nil {
					id = num.String()
				} else {
					json.Unmarshal(rawID, &id)
				}
			}
			recs = append(recs, Record{Type: typ, ID: id, Binders: binders})
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: reading %s aborted: %v\n", f, err)
		}
		fh.Close()
	}
	return recs, nil
}

// ReadAllRefs returns every ref record from collections/*.jsonl, sorted by file
// then line — the Go equivalent of read_index(notes_dir) with no binder filter.
func ReadAllRefs(notesDir string) ([]map[string]any, error) {
	dir := filepath.Join(notesDir, "collections")
	entries, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	sort.Strings(entries)

	var recs []map[string]any
	for _, f := range entries {
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // records can carry long fields
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			dec := json.NewDecoder(strings.NewReader(line))
			dec.UseNumber()
			var rec map[string]any
			if dec.Decode(&rec) != nil {
				continue
			}
			if t, _ := rec["type"].(string); t != "ref" {
				continue
			}
			rec["_note_file"] = f // provenance; dropped on any re-serialize
			recs = append(recs, rec)
		}
		if err := sc.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: reading %s aborted: %v\n", f, err)
		}
		fh.Close()
	}
	return recs, nil
}

// ParseJSONObject decodes one JSON object line into a map, numbers preserved as
// json.Number (so id 100000002 stringifies exactly, not as 1.0000e8). Returns nil
// on any error or a non-object.
func ParseJSONObject(line string) map[string]any {
	dec := json.NewDecoder(strings.NewReader(line))
	dec.UseNumber()
	var m map[string]any
	if dec.Decode(&m) != nil {
		return nil
	}
	return m
}

// AsString renders a decoded JSON value as its display string: nil→"", numbers
// to their decimal literal, arrays to a bracketed, quoted list (["a", "b"]).
// This form is part of the on-screen and golden-file contract.
func AsString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = inspectElem(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return fmt.Sprintf("%v", x)
	}
}

// inspectElem renders one array element: strings get
// double-quoted, nil is "nil", everything else uses to_s.
func inspectElem(v any) string {
	switch x := v.(type) {
	case string:
		return strconv.Quote(x)
	case nil:
		return "nil"
	default:
		return AsString(v)
	}
}

// Truthy reports presence: only nil and false are falsey (an empty string
// is Truthy — so `puts "…" if rec["filename"]` prints even for "").
func Truthy(v any) bool {
	return v != nil && v != false
}

// AsStrings coerces a field to a string slice: nil→[], an array→each element,
// any other scalar→a one-element slice.
func AsStrings(v any) []string {
	if v == nil {
		return nil
	}
	if arr, ok := v.([]any); ok {
		out := make([]string, 0, len(arr))
		for _, e := range arr {
			out = append(out, AsString(e))
		}
		return out
	}
	return []string{AsString(v)}
}

// AkaList returns a record's aka handles, mirroring Array(rec["aka"]).
func AkaList(rec map[string]any) []string {
	return AsStrings(rec["aka"])
}

// ResolveKey finds the index record whose id or one of whose aka handles equals
// key (exact match; aka are unique across the index). Returns nil if none.
func ResolveKey(recs []map[string]any, key string) map[string]any {
	for _, r := range recs {
		if AsString(r["id"]) == key {
			return r
		}
		for _, a := range AkaList(r) {
			if a == key {
				return r
			}
		}
	}
	return nil
}

// URLRef reports whether a record's locator is a URL instead of a bookmark.
func URLRef(rec map[string]any) bool {
	return AsString(rec["url"]) != ""
}

// RefURL returns the record's URL locator, or "" for a file (bookmark) ref.
func RefURL(rec map[string]any) string {
	if URLRef(rec) {
		return AsString(rec["url"])
	}
	return ""
}
