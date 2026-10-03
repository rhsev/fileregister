package main

// cmd_write — read JSONL from stdin, write ref records to Markdown/JSONL files.
// Emits one status line per input record (in input order, after EOF); exits 1
// if any record failed. Records are batched per target file, so streaming N
// records into one binder costs one read + one write instead of N rewrite
// cycles of a growing file.

import (
	"github.com/rhsev/fileregister/internal/index"

	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// writerExtSupported reports whether the target is a writable format.
func writerExtSupported(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".jsonl":
		return true
	}
	return false
}

func writeStatusOK(noteFile, action, xattr string) string {
	return `{"ok":true,"_note_file":` + index.JSONVal(noteFile) +
		`,"action":` + index.JSONVal(action) + `,"xattr":` + index.JSONVal(xattr) + `}`
}

func writeStatusErr(noteFile any, msg string) string {
	return `{"ok":false,"_note_file":` + index.JSONVal(noteFile) + `,"error":` + index.JSONVal(msg) + `}`
}

// writeItem is one parsed input line: either an immediate error status, or a
// record queued for its target's batch.
type writeItem struct {
	status  string
	ok      bool
	queued  bool
	target  string
	isJSONL bool
	rec     index.RefRecord
	refPath string
}

// writeParse validates one input line into a writeItem.
func writeParse(line string) writeItem {
	data := index.ParseJSONObject(line)
	if data == nil {
		return writeItem{status: writeStatusErr(nil, "invalid JSON")}
	}
	target, hasTarget := data["_note_file"]
	if !hasTarget || target == nil {
		return writeItem{status: writeStatusErr(nil, "missing _note_file")}
	}
	targetStr := index.AsString(target)
	if !writerExtSupported(targetStr) {
		return writeItem{status: writeStatusErr(targetStr, "unsupported target format: "+strings.ToLower(filepath.Ext(targetStr)))}
	}
	if t, _ := data["type"].(string); t != "ref" {
		return writeItem{status: writeStatusErr(targetStr, "v1 supports type:ref records only")}
	}
	// The stream is one membership per line: binder is a single string. An
	// array used to be stored as its own text, `["a"]`, as a binder name.
	if b, ok := data["binder"]; ok && b != nil {
		if _, isString := b.(string); !isString {
			return writeItem{status: writeStatusErr(targetStr, "binder must be one string per line (one membership each)")}
		}
	}
	// aka and tags are lists in a record; a single string is one element.
	for _, k := range []string{"aka", "tags"} {
		if s, ok := data[k].(string); ok {
			data[k] = []any{s}
		}
	}
	rec := index.NewRefRecord(data)
	if !rec.Valid() {
		return writeItem{status: writeStatusErr(targetStr, "missing required field: id")}
	}
	if rec.Binder != "" {
		if p := index.BinderNameProblem(rec.Binder); p != "" {
			return writeItem{status: writeStatusErr(targetStr, "binder name '"+rec.Binder+"' "+p)}
		}
	}
	return writeItem{
		queued:  true,
		target:  targetStr,
		isJSONL: strings.HasSuffix(strings.ToLower(targetStr), ".jsonl"),
		rec:     rec,
		refPath: index.AsString(data["_ref_path"]),
	}
}

// writeIndexProblem says why rec must not be written to target in the index
// refs, or "": its id is recorded in another file of the index (add updates a
// record in its own file; a second record would fork the identity), or one of
// its aka handles belongs to another record.
func writeIndexProblem(refs []map[string]any, rec index.RefRecord, target string) string {
	for _, r := range refs {
		if index.AsString(r["id"]) == rec.ID && !index.PathsEqual(index.AsString(r["_note_file"]), target) {
			return "id " + rec.ID + " is recorded in " + filepath.Base(index.AsString(r["_note_file"])) + " — write it there"
		}
	}
	for _, h := range index.AsStrings(rec.Aka) {
		if other := index.ResolveKey(refs, h); other != nil && index.AsString(other["id"]) != rec.ID {
			return "aka '" + h + "' already resolves to record " + index.AsString(other["id"])
		}
	}
	return ""
}

func cmdWrite(args []string) int {
	for _, a := range args {
		switch a {
		case "-v", "--version":
			fmt.Println("register write " + registerVersion)
			return 0
		case "-h", "--help":
			fmt.Println("Usage: register write [options] < records.jsonl")
			return 0
		default:
			if strings.HasPrefix(a, "-") {
				return unknownOption("write", a)
			}
		}
	}

	var lines []string
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	if serr := sc.Err(); serr != nil {
		// A line over the 4 MB cap (or a read error) would otherwise truncate
		// the batch silently with exit 0. Fail before any write is applied.
		fmt.Fprintf(os.Stderr, "register write: reading stdin: %v\n", serr)
		return 1
	}

	items := make([]writeItem, len(lines))
	for i, line := range lines {
		items[i] = writeParse(line)
	}

	// A .jsonl target inside a collections/ folder is part of an index: the
	// record must not fork an id recorded in another of its files, nor take
	// an aka another record has. Each index is read once, in full.
	indexes := map[string][]map[string]any{}
	for i := range items {
		it := &items[i]
		if !it.queued || !it.isJSONL {
			continue
		}
		col := filepath.Dir(it.target)
		if filepath.Base(col) != "collections" {
			continue
		}
		refs, seen := indexes[col]
		if !seen {
			var err error
			if refs, err = index.ReadAllRefs(filepath.Dir(col)); err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
				return 1
			}
			indexes[col] = refs
		}
		if why := writeIndexProblem(refs, it.rec, it.target); why != "" {
			it.queued = false
			it.status = writeStatusErr(it.target, why)
		}
	}

	// Group queued records by (format, target); within-target input order is
	// preserved, so per-record actions stay aligned.
	groups := map[string][]int{}
	var groupOrder []string
	for i := range items {
		if !items[i].queued {
			continue
		}
		key := "m\x00" + items[i].target
		if items[i].isJSONL {
			key = "j\x00" + items[i].target
		}
		if _, ok := groups[key]; !ok {
			groupOrder = append(groupOrder, key)
		}
		groups[key] = append(groups[key], i)
	}

	for _, key := range groupOrder {
		idxs := groups[key]
		target := items[idxs[0]].target
		recs := make([]index.RefRecord, len(idxs))
		for j, i := range idxs {
			recs[j] = items[i].rec
		}
		var actions []string
		if items[idxs[0]].isJSONL {
			acts, werr := index.JSONLWriteMany(recs, target)
			if werr != nil || len(acts) != len(idxs) {
				for _, i := range idxs {
					items[i].status = writeStatusErr(target, "write failed")
				}
				continue
			}
			if dir := filepath.Dir(target); filepath.Base(dir) == "collections" {
				ensureSchemaDir(dir) // write extends schema-3 JSONL too — only an index's
			}
			actions = acts
		} else {
			actions = mdFileWriteMany(recs, target)
		}
		for j, i := range idxs {
			if actions[j] == "failed" {
				items[i].status = writeStatusErr(target, "write failed")
				continue
			}
			// Membership on the file for _ref_path (idempotent), as add does it:
			// through the record's backend, with ★. Only an index write is a
			// membership — a Markdown block is annotation — and a bookmark
			// (no binder) has none.
			xattrStatus := "skipped"
			rec := items[i].rec
			if items[i].refPath != "" && items[i].isJSONL && rec.Binder != "" {
				if !index.FileExists(items[i].refPath) {
					xattrStatus = "missing"
				} else {
					backend := index.XattrBackend(rec.ToH())
					for _, r := range indexes[filepath.Dir(target)] {
						if index.AsString(r["id"]) == rec.ID {
							backend = index.XattrBackend(r) // the stored backend wins, as in add
							break
						}
					}
					xattrStatus = index.XattrBackendAdd(items[i].refPath, rec.Binder, backend)
					index.ManagedMark(items[i].refPath)
				}
			}
			items[i].status = writeStatusOK(target, actions[j], xattrStatus)
			items[i].ok = true
		}
	}

	hadError := false
	for i := range items {
		fmt.Println(items[i].status)
		if !items[i].ok {
			hadError = true
		}
	}
	if hadError {
		return 1
	}
	return 0
}
