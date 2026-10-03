package main

// cmd_unmarshal — unpack a container and recreate the local collection.
//
// Mirror placement: each file goes to `origin` resolved against the LOCAL jsonl
// dir; the id is preserved (re-bookmarked under the same id) so the Markdown
// back-reference re-links. Safe by default: a file is never overwritten and an
// already-managed id is never touched — conflicts are parked in the import folder.

import (
	"github.com/rhsev/fileregister/internal/index"

	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var containerExtRe = regexp.MustCompile(`(?i)\.(tar\.gz|tgz)$`)

// ensureSchema stamps the current schema version (3: id string, binder array —
// NOTES-schema.md §JSONL contract) as collections/SCHEMA.
func ensureSchema(notesDir string) {
	ensureSchemaDir(filepath.Join(notesDir, "collections"))
}

// ensureSchemaDir stamps dir/SCHEMA = 3. Every entry point that creates or
// extends schema-3 JSONL stamps its directory (add, unmarshal, reindex, write).
func ensureSchemaDir(dir string) {
	p := filepath.Join(dir, "SCHEMA")
	if data, err := os.ReadFile(p); err == nil && strings.TrimSpace(string(data)) == "3" {
		return
	}
	os.MkdirAll(dir, 0755)
	if err := index.AtomicWrite(p, []byte("3\n")); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: writing %s failed: %v\n", p, err)
	}
}

// containedJoin joins rel onto base and reports whether the result stays at or
// below base AFTER cleaning — so interior `..` segments (sub/../../x) that
// filepath.Join collapses can't escape. A string prefix check on the raw rel
// would miss exactly those. Returns the cleaned absolute path and ok.
func containedJoin(base, rel string) (string, bool) {
	joined := filepath.Join(base, rel)
	r, err := filepath.Rel(base, joined)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) || filepath.IsAbs(r) {
		return joined, false
	}
	return joined, true
}

// stagedFile returns the staged source for a manifest path, or why it cannot
// be used. It must be a regular file that really lies inside the extracted
// container: the containment check above is lexical, and a symlink in the
// archive (to a host file, or a directory link leading out) would otherwise
// be followed and the host file imported.
func stagedFile(inner, rel string) (string, string) {
	src, ok := containedJoin(inner, rel)
	if !ok {
		return src, "file path escapes the container (" + rel + ")"
	}
	fi, err := os.Lstat(src)
	if err != nil {
		return src, "staged file missing"
	}
	if !fi.Mode().IsRegular() {
		return src, "not a regular file in the container (" + rel + ")"
	}
	innerReal, err1 := filepath.EvalSymlinks(inner)
	real, err2 := filepath.EvalSymlinks(src)
	if err1 != nil || err2 != nil {
		return src, "staged file missing"
	}
	if r, err := filepath.Rel(innerReal, real); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return src, "file path escapes the container (" + rel + ")"
	}
	return src, ""
}

// protectedTarget says why a container may not write target, or "". Inside
// collections/ a .jsonl would be read as index — bypassing every check add
// makes — and --force would let a container replace inbox.jsonl, SCHEMA or an
// annotation note. A symlink target is never written through, anywhere.
func protectedTarget(target string, inVault bool) string {
	fi, err := os.Lstat(target)
	if err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return "a symlink"
	}
	if !inVault {
		return ""
	}
	base := strings.ToLower(filepath.Base(target))
	switch {
	case strings.HasSuffix(base, ".jsonl"):
		return "an index file (.jsonl)"
	case base == "schema" || base == ".register.lock":
		return "fileregister's own bookkeeping"
	case err == nil && strings.HasSuffix(base, ".md"):
		return "an existing annotation note"
	}
	return ""
}

// rejectKeys returns a copy of m without the given keys.
func rejectKeys(m map[string]any, keys ...string) map[string]any {
	drop := map[string]bool{}
	for _, k := range keys {
		drop[k] = true
	}
	out := map[string]any{}
	for k, v := range m {
		if !drop[k] {
			out[k] = v
		}
	}
	return out
}

func cmdUnmarshal(args []string) int {
	force := false
	scatter := false
	container := ""
	for _, a := range args {
		switch {
		case a == "--force":
			force = true
		case a == "--scatter":
			scatter = true
		case a == "-h" || a == "--help":
			fmt.Println("Usage: register unmarshal <container>.tar.gz [--force] [--scatter]")
			return 0
		case a == "-v" || a == "--version":
			fmt.Println("register unmarshal " + registerVersion)
			return 0
		default:
			if strings.HasPrefix(a, "-") {
				return unknownOption("unmarshal", a)
			}
			if container == "" {
				container = a
			}
		}
	}

	if container == "" || !index.FileExists(container) {
		fmt.Fprintln(os.Stderr, "Error: container file required: register unmarshal <file>.tar.gz")
		return 1
	}

	notesDir, err := notesDir()
	if err != nil {
		return 1
	}
	collections := filepath.Join(notesDir, "collections")
	os.MkdirAll(collections, 0755)
	jsonlRoot, err := filepath.EvalSymlinks(collections)
	if err != nil {
		jsonlRoot = collections
	}
	inbox := filepath.Join(collections, "inbox.jsonl")

	// Extract into a recognizable import staging folder; conflicts stay here.
	importDir := filepath.Join(collections, "import")
	name := containerExtRe.ReplaceAllString(filepath.Base(container), "")
	// "...tar.gz" leaves "..", ".tar.gz" leaves "": either would make staging
	// collections/ or import/ itself, which is removed just below.
	if name == "" || name == "." || name == ".." {
		name = "container"
	}
	staging := filepath.Join(importDir, name)
	os.RemoveAll(staging)
	os.MkdirAll(staging, 0755)
	if out, terr := exec.Command("tar", "-xzf", container, "-C", staging).CombinedOutput(); terr != nil {
		fmt.Fprintf(os.Stderr, "Error: tar failed to extract %s\n%s", container, out)
		return 1
	}

	manifestPath := findManifest(staging)
	if manifestPath == "" {
		fmt.Fprintf(os.Stderr, "Error: no manifest.jsonl found in %s\n", container)
		return 1
	}
	inner := filepath.Dir(manifestPath)

	var refLines, noteLines []map[string]any
	if data, rerr := os.ReadFile(manifestPath); rerr == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			e := index.ParseJSONObject(line)
			if e == nil {
				continue
			}
			switch index.AsString(e["type"]) {
			case "ref":
				refLines = append(refLines, e)
			case "note":
				noteLines = append(noteLines, e)
			}
		}
	}

	// A container made elsewhere may carry binders this side refuses. Stop
	// before anything is written rather than fail at the xattr write.
	var badBinders []string
	for _, e := range refLines {
		for _, b := range index.NormalizeBinders(e["binder"]) {
			name := index.AsString(b)
			if p := index.BinderNameProblem(name); p != "" && !containsStr(badBinders, name) {
				badBinders = append(badBinders, name)
				fmt.Fprintf(os.Stderr, "Error: binder name '%s' %s\n", name, p)
			}
		}
	}
	if len(badBinders) > 0 {
		fmt.Fprintln(os.Stderr, "Nothing imported. Rename the binder where the container was made, then marshal it again.")
		return 1
	}

	if lerr := index.LockBookmarks(); lerr != nil {
		fmt.Fprintln(os.Stderr, "register:", lerr)
		return 1
	}
	db, err := index.LoadDB() // one load: the snapshot AND the working DB for this run
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}
	dbDirty := false
	imported := 0
	parked := 0
	bookmarkFailed := 0
	var reasons []string
	var indexRefs []index.RefRecord

	// Local URL refs never enter the bookmark db — an imported file ref with
	// the same id would fuse two identities, so their ids park the entry.
	localURLByID := map[string]string{}
	for _, r := range mustRefs(notesDir) {
		if index.URLRef(r) {
			localURLByID[index.AsString(r["id"])] = index.AsString(r["url"])
		}
	}

	appendRefs := func(entry map[string]any) {
		rec := rejectKeys(entry, "file", "origin", "markdown", "binder")
		binders := index.NormalizeBinders(entry["binder"])
		if len(binders) == 0 {
			if r := index.NewRefRecord(rec); r.Valid() {
				indexRefs = append(indexRefs, r)
			}
			return
		}
		for _, b := range binders {
			m := rejectKeys(rec) // copy
			m["binder"] = b
			if r := index.NewRefRecord(m); r.Valid() {
				indexRefs = append(indexRefs, r)
			}
		}
	}

	for _, entry := range refLines {
		id := index.AsString(entry["id"])
		label := id
		if fn, ok := entry["filename"]; ok && fn != nil {
			label = index.AsString(fn)
		}

		// URL refs are pure data — no file, no bookmark, no xattr. But an id
		// held by a local FILE record must not be grafted with a url (the
		// backfill would flip that record into a URL ref).
		if index.AsString(entry["url"]) != "" {
			if _, isLocalFile := db[id]; isLocalFile {
				parked++
				reasons = append(reasons, fmt.Sprintf("%s — id %s belongs to a local file record", label, id))
				continue
			}
			appendRefs(entry)
			imported++
			continue
		}

		// idempotent: this id is already managed locally
		if _, ok := db[id]; ok {
			parked++
			reasons = append(reasons, fmt.Sprintf("%s — id %s already present", label, id))
			continue
		}
		if u, taken := localURLByID[id]; taken {
			parked++
			reasons = append(reasons, fmt.Sprintf("%s — id %s belongs to a local URL ref (%s)", label, id, u))
			continue
		}
		origin := entry["origin"]
		fRel := entry["file"]
		if origin == nil || fRel == nil {
			parked++
			reasons = append(reasons, fmt.Sprintf("%s — no file in container", label))
			continue
		}

		// The staged source must stay inside the extracted container — a
		// manifest `file` of ../../etc/passwd would otherwise read (and later
		// os.Remove) an arbitrary host file. Checked after cleaning, so
		// interior `..` can't slip past, and after resolving symlinks.
		src, why := stagedFile(inner, index.AsString(fRel))
		if why != "" {
			parked++
			reasons = append(reasons, fmt.Sprintf("%s — %s", label, why))
			continue
		}

		// Mirroring outside collections/ places files at manifest-controlled
		// paths anywhere the user can write — the intended mechanism for one's
		// OWN archives, but an arbitrary-file-write for a container from
		// elsewhere. Explicit opt-in via --scatter; and even then, the target
		// is decided after cleaning so `origin` can't sneak past the check.
		o := index.AsString(origin)
		target, inVault := containedJoin(jsonlRoot, o)
		if !scatter && !inVault {
			parked++
			reasons = append(reasons, fmt.Sprintf("%s — origin outside collections/ (%s); rerun with --scatter to place it", label, o))
			continue
		}

		if why := protectedTarget(target, inVault); why != "" {
			parked++
			reasons = append(reasons, fmt.Sprintf("%s — target is %s (%s)", label, why, target))
			continue
		}
		if index.FileExists(target) && !force {
			parked++
			reasons = append(reasons, fmt.Sprintf("%s — target exists (%s)", filepath.Base(target), target))
			continue
		}

		// Place the file, re-bookmark under the SAME id, regenerate binder xattr
		// AND the ★ managed marker — a self-complete restore.
		os.MkdirAll(filepath.Dir(target), 0755)
		if err := copyFile(src, target); err != nil {
			fmt.Fprintln(os.Stderr, "register:", err)
			return 1
		}
		// Re-bookmark under the same id using the shared DB (saved once, after the
		// loop) instead of a load+save per file. A failed save leaves the record
		// importable but its bookmark broken — say so instead of counting it done.
		if newID, berr := index.RegisterIn(db, target, id); berr != nil || newID == "" {
			bookmarkFailed++
			fmt.Fprintf(os.Stderr, "  Warning: bookmark save failed for %s (id %s)\n", filepath.Base(target), id)
		} else {
			dbDirty = true
		}
		binders := index.NormalizeBinders(entry["binder"])
		backend := index.XattrBackend(entry)
		for _, b := range binders {
			index.XattrBackendAdd(target, index.AsString(b), backend)
		}
		if len(binders) > 0 {
			index.ManagedMark(target) // ★ marks membership only
		}

		appendRefs(entry)
		os.Remove(src) // imported → drop from staging
		imported++
	}

	if dbDirty {
		if err := index.SaveDB(db); err != nil {
			fmt.Fprintln(os.Stderr, "register:", err)
			return 1
		}
	}

	if len(indexRefs) > 0 {
		if _, err := index.JSONLWriteMany(indexRefs, inbox); err != nil {
			fmt.Fprintln(os.Stderr, "register:", err)
			return 1
		}
	}
	ensureSchema(notesDir) // stamp schema on the foreign machine too

	// Annotation notes. Binder notes (origin inside collections/) mirror into the
	// local collections/; a local note of the same name wins (kept staged).
	// Scattered notes stay staged.
	placedNotes := 0
	var keptNotes, scatteredNotes []string
	for _, n := range noteLines {
		src, why := stagedFile(inner, index.AsString(n["file"]))
		if why != "" {
			continue
		}
		// A note is Markdown; anything else placed in collections/ could be
		// read as index (.jsonl) or bookkeeping.
		if !strings.EqualFold(filepath.Ext(src), ".md") {
			keptNotes = append(keptNotes, fmt.Sprintf("%s — not a Markdown note (kept staged)", filepath.Base(src)))
			continue
		}
		if _, inVault := containedJoin(jsonlRoot, index.AsString(n["origin"])); !inVault {
			scatteredNotes = append(scatteredNotes, index.AsString(n["file"]))
			continue
		}
		// Notes always land in collections/ by basename — origin governs only
		// the binder-vs-scattered classification above, never the write path.
		target := filepath.Join(collections, filepath.Base(src))
		if index.FileExists(target) {
			keptNotes = append(keptNotes, fmt.Sprintf("%s — note exists locally (kept staged)", filepath.Base(src)))
			continue
		}
		if err := copyFile(src, target); err != nil {
			fmt.Fprintln(os.Stderr, "register:", err)
			return 1
		}
		os.Remove(src)
		placedNotes++
	}

	fmt.Printf("Imported %d record(s) into %s\n", imported, notesDir)
	if bookmarkFailed > 0 {
		fmt.Printf("%d record(s) imported with a BROKEN bookmark — run: register repair\n", bookmarkFailed)
	}
	if placedNotes > 0 {
		fmt.Printf("Placed %d annotation note(s) into collections/\n", placedNotes)
	}
	if parked > 0 || len(keptNotes) > 0 {
		fmt.Printf("Parked %d (left in %s):\n", parked+len(keptNotes), staging)
		for _, r := range append(append([]string{}, reasons...), keptNotes...) {
			fmt.Printf("  - %s\n", r)
		}
	}
	if len(scatteredNotes) > 0 {
		fmt.Printf("Scattered note(s) left in %s — file them manually:\n", staging)
		for _, f := range scatteredNotes {
			fmt.Printf("  - %s\n", f)
		}
	}
	if parked == 0 && len(keptNotes) == 0 && len(scatteredNotes) == 0 {
		os.RemoveAll(staging) // nothing left behind → clean up
	}
	if bookmarkFailed > 0 {
		return 1
	}
	return 0
}

// findManifest returns the first manifest.jsonl under root (any depth).
func findManifest(root string) string {
	var found string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if !d.IsDir() && d.Name() == "manifest.jsonl" {
			found = p
		}
		return nil
	})
	return found
}
