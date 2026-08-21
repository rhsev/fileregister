package index

// write — the JSONL index writer. Folds a stream of
// (id, binder) ref records into the index, keeping ONE record per id with
// `binder` as a set (array).

import (
	"os"
	"path/filepath"
	"strings"
)

// RefRecord holds a ref record's canonical fields.
type RefRecord struct {
	ID, Binder, URL, Filename, Kind, Xattr string
	Aka, Tags                              any
}

func NewRefRecord(data map[string]any) RefRecord {
	return RefRecord{
		ID:       AsString(data["id"]),
		Binder:   AsString(data["binder"]),
		URL:      AsString(data["url"]),
		Filename: AsString(data["filename"]),
		Kind:     AsString(data["kind"]),
		Xattr:    AsString(data["xattr"]),
		Aka:      data["aka"],
		Tags:     data["tags"],
	}
}

func (r RefRecord) Valid() bool { return r.ID != "" }

// NotEmptyVal reports whether an aka/tags value is present and non-empty
// (array or scalar).
func NotEmptyVal(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case string:
		return x != ""
	case []any:
		return len(x) > 0
	default:
		return true
	}
}

// ToH builds the record's field map (binder scalar), matching RefRecord#to_h.
func (r RefRecord) ToH() map[string]any {
	h := map[string]any{"type": "ref", "id": r.ID, "binder": r.Binder}
	if r.URL != "" {
		h["url"] = r.URL
	}
	if r.Filename != "" {
		h["filename"] = r.Filename
	}
	if r.Kind != "" {
		h["kind"] = r.Kind
	}
	if NotEmptyVal(r.Aka) {
		h["aka"] = r.Aka
	}
	if NotEmptyVal(r.Tags) {
		h["tags"] = r.Tags
	}
	if r.Xattr != "" && r.Xattr != "itemprojects" {
		h["xattr"] = r.Xattr
	}
	return h
}

// IndexRecord is the dict for a brand-new id, with binder as a set.
func (r RefRecord) IndexRecord() map[string]any {
	h := r.ToH()
	if r.Binder == "" {
		h["binder"] = []any{}
	} else {
		h["binder"] = []any{r.Binder}
	}
	return h
}

// NormalizeBinders mirrors Array(v).map(&:to_s).reject(&:empty?).uniq as []any.
// Always returns a non-nil slice — nil would serialize as JSON null, and the
// schema promises `binder` is always an array.
func NormalizeBinders(v any) []any {
	seen := map[string]bool{}
	out := []any{}
	for _, s := range AsStrings(v) {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func containsAnyStr(arr []any, s string) bool {
	for _, e := range arr {
		if AsString(e) == s {
			return true
		}
	}
	return false
}

// backfillIndexFields adds scalar fields the existing record lacks (never overwrites).
func backfillIndexFields(existing map[string]any, record RefRecord) {
	src := record.ToH()
	for _, k := range []string{"url", "filename", "kind", "aka", "tags", "xattr"} {
		if v, ok := src[k]; ok {
			if ev, present := existing[k]; !present || ev == nil || ev == "" {
				existing[k] = v
			}
		}
	}
}

// JSONLWriteMany folds records into the JSONL index at targetPath. New ids are
// appended; a new binder on an existing id set-inserts and triggers a full
// rewrite. Lines the parser does not understand — foreign records, id-less or
// malformed lines, blank separators — pass through a rewrite verbatim, per the
// wire contract. Returns "appended"/"updated"/"noop" per record in input order.
func JSONLWriteMany(records []RefRecord, targetPath string) ([]string, error) {
	if err := LockIndexDir(filepath.Dir(targetPath)); err != nil {
		return nil, err
	}

	type idxLine struct {
		raw   string         // original text, emitted verbatim while dirty is false
		rec   map[string]any // parsed record carrying an id, else nil (passthrough)
		dirty bool
	}
	var lines []idxLine
	byID := map[string]int{} // id → index of its (last) line
	needsNL := false         // existing file lacks a trailing newline

	if data, err := os.ReadFile(targetPath); err == nil {
		needsNL = len(data) > 0 && data[len(data)-1] != '\n'
		raw := strings.Split(string(data), "\n")
		if n := len(raw); n > 0 && raw[n-1] == "" {
			raw = raw[:n-1] // artifact of the trailing newline
		}
		for _, line := range raw {
			l := idxLine{raw: line}
			if strings.TrimSpace(line) != "" {
				if rec := ParseJSONObject(line); rec != nil && rec["id"] != nil {
					l.rec = rec
					byID[AsString(rec["id"])] = len(lines)
				}
			}
			lines = append(lines, l)
		}
	} else if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return nil, err
	}

	dirty := false
	var appends []map[string]any
	actions := make([]string, len(records))
	for i, record := range records {
		id := record.ID
		b := record.Binder
		if at, ok := byID[id]; ok {
			existing := lines[at].rec
			binders := NormalizeBinders(existing["binder"])
			if b == "" || containsAnyStr(binders, b) {
				actions[i] = "noop"
				continue
			}
			existing["binder"] = append(binders, b)
			backfillIndexFields(existing, record)
			lines[at].dirty = true
			dirty = true
			actions[i] = "updated"
		} else {
			rec := record.IndexRecord()
			byID[id] = len(lines)
			lines = append(lines, idxLine{rec: rec, dirty: true})
			appends = append(appends, rec)
			actions[i] = "appended"
		}
	}

	if dirty {
		out := make([]string, 0, len(lines))
		for _, l := range lines {
			if l.dirty {
				out = append(out, JSONVal(l.rec))
			} else {
				out = append(out, l.raw)
			}
		}
		content := ""
		if len(out) > 0 {
			content = strings.Join(out, "\n") + "\n"
		}
		return actions, AtomicWrite(targetPath, []byte(content))
	}
	if len(appends) > 0 {
		f, err := os.OpenFile(targetPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return actions, err
		}
		if needsNL {
			// A torn earlier append left no trailing newline; don't glue onto it.
			if _, err := f.WriteString("\n"); err != nil {
				f.Close()
				return actions, err
			}
		}
		for _, rec := range appends {
			if _, err := f.WriteString(JSONVal(rec) + "\n"); err != nil {
				f.Close()
				return actions, err
			}
		}
		return actions, f.Close()
	}
	return actions, nil
}
