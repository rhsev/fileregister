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
	"errors"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/text/unicode/norm"
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

// LoadDB reads the id→blob map. Only a missing file is an empty database. A
// file that cannot be read or parsed is an error: every writer saves the whole
// map back, so reading a damaged file as empty would wipe every bookmark on
// the next save.
func LoadDB() (map[string]string, error) {
	path := bookmarkFile()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read the bookmark database %s: %w", path, err)
	}
	var db map[string]string
	if err := json.Unmarshal(data, &db); err != nil {
		return nil, fmt.Errorf("the bookmark database %s is damaged (%v); restore it from a backup — register will not overwrite it", path, err)
	}
	if db == nil {
		db = map[string]string{}
	}
	return db, nil
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

// BookmarkGet resolves a single id to a file path, or "" if unresolvable. The
// error is for a bookmark database that cannot be read, or a broken engine.
func BookmarkGet(id string) (string, error) {
	db, err := LoadDB()
	if err != nil {
		return "", err
	}
	blob, ok := db[id]
	if !ok {
		return "", nil
	}
	resp, err := fileAnchor().request(map[string]any{"op": "resolve", "blob": blob})
	if err != nil {
		return "", err // the engine is broken — not the same as a gone file
	}
	if ok, _ := resp["ok"].(bool); ok {
		path, _ := resp["path"].(string)
		return path, nil
	}
	return "", nil
}

// Resolution is one id's bookmark outcome: Path when it resolves, else
// LastPath — where the file was when it was bookmarked ("" if unknown).
type Resolution struct {
	Path, LastPath string
}

// BatchGet resolves many ids in one shot: id → path, "" for a missing blob or
// an unresolvable one. See BatchResolve.
func BatchGet(ids []string) (map[string]string, error) {
	res, err := BatchResolve(ids)
	paths := make(map[string]string, len(res))
	for id, r := range res {
		paths[id] = r.Path
	}
	return paths, err
}

// BatchResolve resolves many ids through the single persistent engine process.
// A resolve request is issued even for a missing blob (empty blob string) so
// the response stream stays index-aligned with the input. An engine-level
// failure is returned as the error — responses received before the failure are
// kept, so callers can tell "engine broken" apart from "file gone".
func BatchResolve(ids []string) (map[string]Resolution, error) {
	results := map[string]Resolution{}
	if len(ids) == 0 {
		return results, nil
	}
	db, err := LoadDB()
	if err != nil {
		return results, err
	}

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
		var r Resolution
		if p.has && i < len(resps) {
			if ok, _ := resps[i]["ok"].(bool); ok {
				r.Path, _ = resps[i]["path"].(string)
			} else {
				r.LastPath, _ = resps[i]["last_path"].(string)
			}
		}
		results[p.id] = r
	}
	return results, err
}

// OwnIDs returns the ids stamped on the file that are really its own: the
// kMDItemInformation ids, else the #S copy (iCloud Drive strips the former),
// minus any whose bookmark resolves to a different existing file. cp and
// Finder's Duplicate copy the id xattrs, so a copy carries the original's id,
// and only the bookmark tells the two apart. An id with no local bookmark, or
// one that no longer resolves, still counts: that is how a moved file, or one
// synced from another Mac, is recognized.
func OwnIDs(db map[string]string, path string) []string {
	var own []string
	for _, id := range OfFileIDs(path) {
		if blob, ok := db[id]; ok && isCopyOf(blob, path) {
			continue
		}
		own = append(own, id)
	}
	return own
}

// isCopyOf reports whether blob resolves to an existing file other than path.
func isCopyOf(blob, path string) bool {
	return copyOriginal(blob, path) != ""
}

// copyOriginal returns the existing file other than path that blob resolves
// to, or "".
func copyOriginal(blob, path string) string {
	resp, err := fileAnchor().request(map[string]any{"op": "resolve", "blob": blob})
	if err != nil {
		return ""
	}
	resolved, _ := resp["path"].(string)
	if ok, _ := resp["ok"].(bool); !ok || resolved == "" {
		return ""
	}
	if PathsEqual(resolved, path) || !FileExists(resolved) {
		return ""
	}
	return resolved
}

// CopiedFrom tells whether path is a copy of a registered file: it carries
// that file's id, whose bookmark resolves to the other file. Returns the id
// and the original's path, or "", "".
func CopiedFrom(db map[string]string, path string) (string, string) {
	for _, id := range OfFileIDs(path) {
		if blob, ok := db[id]; ok {
			if orig := copyOriginal(blob, path); orig != "" {
				return id, orig
			}
		}
	}
	return "", ""
}

// FileIDsByName maps each file record's filename (NFC) to its ids.
func FileIDsByName(refs []map[string]any) map[string][]string {
	out := map[string][]string{}
	for _, r := range refs {
		if URLRef(r) {
			continue
		}
		if name := AsString(r["filename"]); name != "" {
			key := norm.NFC.String(name)
			out[key] = append(out[key], AsString(r["id"]))
		}
	}
	return out
}

// idByBookmark finds the registered id of a file that carries none — its id
// xattr could not be written (a locked or read-only file, a volume without
// xattrs), or was stripped. Records with the file's name are candidates; the
// one whose bookmark resolves to this very file is it.
func idByBookmark(db map[string]string, path string, byName map[string][]string) string {
	for _, id := range byName[norm.NFC.String(filepath.Base(path))] {
		blob, ok := db[id]
		if !ok {
			continue
		}
		resp, err := fileAnchor().request(map[string]any{"op": "resolve", "blob": blob})
		if err != nil {
			continue
		}
		if resolved, _ := resp["path"].(string); resolved != "" && PathsEqual(resolved, path) {
			return id
		}
	}
	return ""
}

// existingIDIn returns the file's own id that has a bookmark in db, or "".
func existingIDIn(db map[string]string, path string) string {
	for _, id := range OwnIDs(db, path) {
		if _, ok := db[id]; ok {
			return id
		}
	}
	return ""
}

// ExistingIDIn is existingIDIn for callers that already hold a loaded db.
func ExistingIDIn(db map[string]string, path string) string {
	return existingIDIn(db, path)
}

// RegisterIn mints a bookmark blob for path under id, writes the id xattrs, and
// stashes the blob in the already-loaded db (caller persists). Returns id, or ""
// when the engine produced no blob (a soft failure). The id is added to the
// file's kMDItemInformation ids; registerFresh replaces them instead.
func RegisterIn(db map[string]string, path, id string) (string, error) {
	return registerIn(db, path, id, "add")
}

// registerFresh is RegisterIn for a file getting a new id. Any ids already on
// it are not its own (a copy's, or leftovers), and would make an id lookup
// find it next to the file they belong to.
func registerFresh(db map[string]string, path, id string) (string, error) {
	return registerIn(db, path, id, "set")
}

func registerIn(db map[string]string, path, id, idMode string) (string, error) {
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
	if why := setDescriptionXattr(path, id, idMode); why != "" {
		fmt.Fprintf(os.Stderr, "  Warning: could not store id %s on %s (%s) — it is found by its bookmark instead\n",
			id, filepath.Base(path), why)
	}
	SetSyncXattr(path, id)
	db[id] = blob
	return id, nil
}

// BookmarkAdd registers path with a bookmark, or returns its existing id if the
// file already carries one. Idempotent. An explicit id (repair flows) bypasses
// the existence check. Returns "" on a save failure.
func BookmarkAdd(path, id string) (string, error) {
	if err := LockBookmarks(); err != nil {
		return "", err
	}
	db, err := LoadDB()
	if err != nil {
		return "", err
	}
	if id == "" {
		if existing := existingIDIn(db, path); existing != "" {
			return existing, nil
		}
	}
	id = NextFreeID(db, id)
	newID, err := registerFresh(db, path, id)
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
// byName (FileIDsByName) finds a file whose id xattr could never be written.
func AddMany(paths []string, reserved map[string]bool, byName map[string][]string) ([]BookmarkResult, error) {
	if err := LockBookmarks(); err != nil {
		return nil, err
	}
	db, err := LoadDB()
	if err != nil {
		return nil, err
	}
	results := make([]BookmarkResult, 0, len(paths))
	dirty := false
	for _, path := range paths {
		if existing := existingIDIn(db, path); existing != "" {
			results = append(results, BookmarkResult{Path: path, ID: existing})
			continue
		}
		if known := idByBookmark(db, path, byName); known != "" {
			results = append(results, BookmarkResult{Path: path, ID: known})
			continue
		}
		id := NextFreeID(db, "")
		for reserved[id] {
			id = NextFreeID(db, GenerateID())
		}
		if newID, err := registerFresh(db, path, id); err == nil && newID != "" {
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
	db, err := LoadDB()
	if err != nil {
		return "", err
	}
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

// setDescriptionXattr writes id to the kMDItemInformation xattr (space-separated,
// multi-valued): mode "add" appends it idempotently, "set" makes it the only one.
// Returns why it failed, or "".
func setDescriptionXattr(path, id, mode string) string {
	resp, err := fileAnchor().request(map[string]any{"op": "set_meta", "path": path, "key": "id", "value": id, "mode": mode})
	if err != nil {
		return err.Error()
	}
	if ok, _ := resp["ok"].(bool); !ok {
		why, _ := resp["error"].(string)
		return why
	}
	return ""
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
