package index

// bookmarks — id↔file identity over the fileanchor engine.
//
// Manages
// ~/.local/share/bookmarks.json (id → bookmark-blob) directly; the engine is
// stateless about that map. It does save(path)→blob, resolve(blob)→path, and the
// id-cache xattrs (description, sync). Blobs are the same Foundation format the
// engine emits, so an existing bookmarks.json keeps resolving whether the
// Go writes it during the transition.

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	allDigitsRe       = regexp.MustCompile(`^\d+$`)
	trailingDashNumRe = regexp.MustCompile(`-\d+$`)
	trailingNumRe     = regexp.MustCompile(`\d+$`)
)

// bookmarkFile is ~/.local/share/bookmarks.json. Resolved via $HOME (os.UserHomeDir)
// so tests can isolate it.
func bookmarkFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "bookmarks.json")
}

// LoadDB reads the id→blob map. A missing or corrupt file yields an empty map,
// a corrupt file reads as an empty database rather than a fatal error.
func LoadDB() map[string]string {
	data, err := os.ReadFile(bookmarkFile())
	if err != nil {
		return map[string]string{}
	}
	var db map[string]string
	if err := json.Unmarshal(data, &db); err != nil || db == nil {
		return map[string]string{}
	}
	return db
}

// SaveDB writes the map as pretty JSON. Keys are sorted, which also gives
// insertion order — both are valid and mutually readable; the file is a machine
// store, so the ordering difference only shows as harmless churn in a diff.
// Callers that load-modify-save must take LockBookmarks BEFORE their LoadDB;
// the lock here only backstops direct saves.
func SaveDB(db map[string]string) error {
	if err := LockBookmarks(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(bookmarkFile()), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWrite(bookmarkFile(), data)
}

// GenerateID mints a fresh 9-digit id, uniformly in [100000000, 999999999].
func GenerateID() string {
	return strconv.Itoa(rand.Intn(900_000_000) + 100_000_000)
}

// succTrailingNum increments the trailing digit run of s, preserving zero-padding
// width ("foo-9" → "foo-10", "a-099" → "a-100").
// Only reached for strings matching /-\d+$/.
func succTrailingNum(s string) string {
	loc := trailingNumRe.FindStringIndex(s)
	digits := s[loc[0]:loc[1]]
	n, _ := strconv.Atoi(digits)
	inc := strconv.Itoa(n + 1)
	if len(inc) < len(digits) {
		inc = strings.Repeat("0", len(digits)-len(inc)) + inc
	}
	return s[:loc[0]] + inc
}

// NextFreeID picks an id not already a key in db. An empty seed generates a fresh
// one; a pure-numeric collision regenerates, an aka-style handle grows a -N suffix
// (incrementing the trailing number until the id is free).
func NextFreeID(db map[string]string, id string) string {
	if id == "" {
		id = GenerateID()
	}
	for {
		if _, exists := db[id]; !exists {
			return id
		}
		switch {
		case allDigitsRe.MatchString(id):
			id = GenerateID()
		case trailingDashNumRe.MatchString(id):
			id = succTrailingNum(id)
		default:
			id = id + "-2"
		}
	}
}

// BookmarkGet resolves a single id to a file path, or "" if unresolvable.
func BookmarkGet(id string) string {
	db := LoadDB()
	blob, ok := db[id]
	if !ok {
		return ""
	}
	resp, err := fileAnchor().request(map[string]any{"op": "resolve", "blob": blob})
	if err != nil {
		return ""
	}
	if ok, _ := resp["ok"].(bool); ok {
		path, _ := resp["path"].(string)
		return path
	}
	return ""
}

// BatchGet resolves many ids in one shot through the single persistent engine
// process. Returns id → path, with "" for a missing blob or an unresolvable one.
// A resolve request is issued even for a missing blob (empty blob string) so the
// response stream stays index-aligned with the input. An engine-level failure
// is returned as the error — responses received before the failure are kept,
// so callers can tell "engine broken" apart from "file gone".
func BatchGet(ids []string) (map[string]string, error) {
	results := map[string]string{}
	if len(ids) == 0 {
		return results, nil
	}
	db := LoadDB()

	type pair struct {
		id   string
		blob string
		has  bool
	}
	pairs := make([]pair, len(ids))
	reqs := make([]map[string]any, len(ids))
	for i, id := range ids {
		blob, has := db[id]
		pairs[i] = pair{id: id, blob: blob, has: has}
		reqs[i] = map[string]any{"op": "resolve", "blob": blob} // "" when missing, like blob.to_s
	}

	resps, err := fileAnchor().batch(reqs)
	for i, p := range pairs {
		if !p.has {
			results[p.id] = ""
			continue
		}
		if i < len(resps) {
			if ok, _ := resps[i]["ok"].(bool); ok {
				results[p.id], _ = resps[i]["path"].(string)
				continue
			}
		}
		results[p.id] = ""
	}
	return results, err
}

// existingIDIn returns the first id in the file's kMDItemInformation xattr that is
// a key in db, or "" if none.
func existingIDIn(db map[string]string, path string) string {
	resp, err := fileAnchor().request(map[string]any{"op": "get_meta", "path": path, "key": "id"})
	if err != nil {
		return ""
	}
	if ok, _ := resp["ok"].(bool); !ok {
		return ""
	}
	values, _ := resp["values"].([]any)
	for _, v := range values {
		if s, ok := v.(string); ok {
			if _, exists := db[s]; exists {
				return s
			}
		}
	}
	return ""
}

// FindExistingID loads the db and looks up an id already carried by the file.
func FindExistingID(path string) string {
	return existingIDIn(LoadDB(), path)
}

// ExistingIDIn is existingIDIn for callers that already hold a loaded db.
func ExistingIDIn(db map[string]string, path string) string {
	return existingIDIn(db, path)
}

// RegisterIn mints a bookmark blob for path under id, writes the id xattrs, and
// stashes the blob in the already-loaded db (caller persists). Returns id, or ""
// when the engine produced no blob (a soft failure).
func RegisterIn(db map[string]string, path, id string) (string, error) {
	resp, err := fileAnchor().request(map[string]any{"op": "save", "path": path})
	if err != nil {
		return "", err
	}
	blob := ""
	if ok, _ := resp["ok"].(bool); ok {
		blob, _ = resp["blob"].(string)
	}
	if blob == "" {
		return "", nil
	}
	setDescriptionXattr(path, id)
	SetSyncXattr(path, id)
	db[id] = blob
	return id, nil
}

// BookmarkAdd registers path with a bookmark, or returns its existing id if the
// file already carries one. Idempotent. An explicit id (repair flows) bypasses
// the existence check. Returns "" on a save failure.
func BookmarkAdd(path, id string) (string, error) {
	if id == "" {
		if existing := FindExistingID(path); existing != "" {
			return existing, nil
		}
	}
	if err := LockBookmarks(); err != nil {
		return "", err
	}
	db := LoadDB()
	id = NextFreeID(db, id)
	newID, err := RegisterIn(db, path, id)
	if err != nil {
		return "", err
	}
	if newID == "" {
		fmt.Fprintf(os.Stderr, "Error: bookmark save failed for %s\n", path)
		return "", nil
	}
	if err := SaveDB(db); err != nil {
		return "", err
	}
	return id, nil
}

// BookmarkResult is one outcome from AddMany, in input order.
type BookmarkResult struct {
	Path  string
	ID    string
	Error string
}

// AddMany registers many paths in a single db load/save cycle. Idempotent per
// file (reuses an existing id). The engine's save runs once per file. A failed
// db save is returned as an error — the caller must NOT record the new ids
// anywhere (their blobs were never persisted). reserved holds ids taken
// outside the db (URL refs live only in the index) that fresh ids must avoid.
func AddMany(paths []string, reserved map[string]bool) ([]BookmarkResult, error) {
	if err := LockBookmarks(); err != nil {
		return nil, err
	}
	db := LoadDB()
	results := make([]BookmarkResult, 0, len(paths))
	dirty := false
	for _, path := range paths {
		if existing := existingIDIn(db, path); existing != "" {
			results = append(results, BookmarkResult{Path: path, ID: existing})
			continue
		}
		id := NextFreeID(db, "")
		for reserved[id] {
			id = NextFreeID(db, GenerateID())
		}
		if newID, err := RegisterIn(db, path, id); err == nil && newID != "" {
			results = append(results, BookmarkResult{Path: path, ID: id})
			dirty = true
		} else {
			results = append(results, BookmarkResult{Path: path, Error: "bookmark save failed"})
		}
	}
	if dirty {
		if err := SaveDB(db); err != nil {
			return results, err
		}
	}
	return results, nil
}

// Rebind re-mints a blob for a relocated file and stores it under the SAME id,
// refreshing the id xattrs. The id is the file's permanent identity. Returns id,
// or "" on save failure.
func Rebind(id, path string) (string, error) {
	if err := LockBookmarks(); err != nil {
		return "", err
	}
	db := LoadDB()
	newID, err := RegisterIn(db, path, id)
	if err != nil {
		return "", err
	}
	if newID == "" {
		return "", nil
	}
	if err := SaveDB(db); err != nil {
		return "", err
	}
	return id, nil
}

// setDescriptionXattr appends id to the kMDItemInformation xattr (space-separated,
// idempotent, multi-valued).
func setDescriptionXattr(path, id string) {
	fileAnchor().request(map[string]any{"op": "set_meta", "path": path, "key": "id", "value": id, "mode": "add"})
}

// SetSyncXattr writes id to the syncable cross-device alias (com.fileregister.id#S),
// single-valued. Returns true if the value changed, false if already current or on
// failure.
func SetSyncXattr(path, id string) bool {
	resp, err := fileAnchor().request(map[string]any{"op": "set_meta", "path": path, "key": "sync", "value": id, "mode": "set"})
	if err != nil {
		return false
	}
	if ok, _ := resp["ok"].(bool); !ok {
		return false
	}
	return resp["action"] != "noop"
}

// syncID reads the bookmark id from com.fileregister.id#S, or "" if absent.
func syncID(path string) string {
	resp, err := fileAnchor().request(map[string]any{"op": "get_meta", "path": path, "key": "sync"})
	if err != nil {
		return ""
	}
	if ok, _ := resp["ok"].(bool); !ok {
		return ""
	}
	v, _ := resp["value"].(string)
	return v
}
