package main

// cmd_add — add a file (or URL) to a binder: index + bookmark + xattr + ★,
// optionally with a Markdown annotation (--md / .md --target).

import (
	"github.com/rhsev/fileregister/internal/index"

	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// kindByExt maps a file extension to a coarse kind label (default for --kind).
var kindByExt = map[string]string{
	"jpg": "image", "jpeg": "image", "png": "image", "heic": "image",
	"gif": "image", "webp": "image", "tiff": "image", "bmp": "image",
	"raw": "image", "dng": "image", "cr2": "image", "cr3": "image",
	"mp4": "video", "mov": "video", "m4v": "video", "mkv": "video",
	"avi": "video", "webm": "video",
	"mp3": "audio", "m4a": "audio", "wav": "audio", "flac": "audio",
	"aac": "audio", "ogg": "audio",
	"eml": "mail", "emlx": "mail", "mbox": "mail",
	"md": "md", "markdown": "md", "mmd": "md",
	"typ": "typst",
}

func detectKind(filePath string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filePath), "."))
	if ext == "" {
		return "file"
	}
	if k, ok := kindByExt[ext]; ok {
		return k
	}
	return ext
}

var urlKindByScheme = map[string]string{
	"http": "web", "https": "web",
	"x-devonthink-item": "devonthink",
	"message":           "mail",
}

var urlRe = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*://`)
var urlSchemeRe = regexp.MustCompile(`(?i)^([a-z][a-z0-9+.-]*):`)

func detectURLKind(url string) string {
	m := urlSchemeRe.FindStringSubmatch(url)
	if m == nil {
		return "url"
	}
	scheme := strings.ToLower(m[1])
	if k, ok := urlKindByScheme[scheme]; ok {
		return k
	}
	return scheme
}

// binderSanitizeRe: slashes and ALL control characters become hyphens (SPEC
// §Record Placement); unicode and colon-hierarchy names pass through. The one
// binder → filesystem-name mapping — note names, container names and album
// slugs must agree.
var binderSanitizeRe = regexp.MustCompile("[/\\\\\x00-\x1f\x7f]")

func sanitizeBinderName(binder string) string {
	return binderSanitizeRe.ReplaceAllString(strings.TrimSpace(binder), "-")
}

type addOptions struct {
	binder                        string
	hasBinder                     bool
	url, label, kind, aka, target string
	hasKind                       bool
	xattr                         string
	hasXattr                      bool
	md                            bool
}

// parseAddArgs parses add's flags via the shared table parser; non-flag args
// are files.
func parseAddArgs(args []string) (opts addOptions, files []string, help, version bool, unknown string) {
	vals, bools, pos, unk := parseFlags(args,
		map[string]bool{"--binder": true, "--url": true, "--label": true, "--kind": true,
			"--aka": true, "--target": true, "--xattr": true},
		map[string]bool{"--md": true})
	opts.binder, opts.hasBinder = vals["--binder"]
	opts.url = vals["--url"]
	opts.label = vals["--label"]
	opts.kind, opts.hasKind = vals["--kind"]
	opts.aka = vals["--aka"]
	opts.target = vals["--target"]
	opts.xattr, opts.hasXattr = vals["--xattr"]
	opts.md = bools["--md"]
	return opts, pos, bools["--help"], bools["--version"], unk
}

func cmdAdd(args []string) int {
	opts, files, help, version, unknown := parseAddArgs(args)
	if unknown != "" {
		return unknownOption("add", unknown)
	}
	if version {
		fmt.Println("register add " + registerVersion)
		return 0
	}
	if help {
		fmt.Println("Usage: register add <file>... [--binder <name>] [options]")
		return 0
	}

	if !opts.hasBinder {
		opts.binder = os.Getenv("REGISTER_BINDER")
		opts.hasBinder = opts.binder != ""
	}
	binderSet := opts.binder != ""
	if binderSet {
		if p := index.BinderNameProblem(opts.binder); p != "" {
			fmt.Fprintf(os.Stderr, "Error: binder name '%s' %s\n", opts.binder, p)
			return 1
		}
	}
	targetIsMd := strings.HasSuffix(strings.ToLower(opts.target), ".md")

	if opts.md && opts.target != "" {
		fmt.Fprintln(os.Stderr, "Error: --md and --target are mutually exclusive (both choose where to write)")
		return 1
	}
	if !binderSet && (opts.md || targetIsMd) {
		fmt.Fprintln(os.Stderr, "Error: annotations are per-binder; --md or a .md --target needs a --binder")
		return 1
	}

	if opts.url != "" {
		if len(files) > 0 {
			fmt.Fprintln(os.Stderr, "Error: --url and file arguments are mutually exclusive")
			return 1
		}
		if opts.hasXattr {
			fmt.Fprintln(os.Stderr, "Error: --xattr does not apply to --url records (no file to cache on)")
			return 1
		}
		return addURLRecord(opts, binderSet)
	}

	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "Error: at least one file argument required")
		fmt.Fprintln(os.Stderr, "Usage: register add <file>... [--binder <name>] [options]")
		return 1
	}

	if opts.hasXattr && !validXattr(opts.xattr) {
		fmt.Fprintln(os.Stderr, "Error: --xattr must be one of: itemprojects, tags, none")
		return 1
	}

	if len(files) > 1 && opts.aka != "" {
		fmt.Fprintln(os.Stderr, "Error: --aka applies to a single file; omit it when adding multiple files")
		return 1
	}

	// Expand paths.
	expanded := make([]string, len(files))
	for i, f := range files {
		expanded[i] = index.ExpandPath(f)
	}

	backend := "itemprojects"
	if raw := strings.ToLower(strings.TrimSpace(opts.xattr)); validXattr(raw) {
		backend = raw
	}

	// Partition missing vs present.
	var missing, present []string
	for _, f := range expanded {
		if index.FileExists(f) {
			present = append(present, f)
		} else {
			missing = append(missing, f)
		}
	}
	for _, f := range missing {
		fmt.Fprintf(os.Stderr, "Skipping (not found): %s\n", f)
	}
	if len(present) == 0 {
		fmt.Fprintln(os.Stderr, "Error: no existing files to add")
		return 1
	}

	nd, err := notesDir()
	if err != nil {
		return 1
	}

	// One index read and one bookmark-db read serve the aka check and the
	// guard sweep below (add is the hottest daily command).
	refs, refsOK := loadRefs(nd)
	if !refsOK {
		return 1
	}
	db, err := index.LoadDB()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}

	// aka uniqueness across the index (a clash with the file's OWN record is fine).
	if opts.aka != "" {
		if clash := index.ResolveKey(refs, opts.aka); clash != nil {
			ownID := ""
			if len(present) == 1 {
				ownID = index.ExistingIDIn(db, present[0])
			}
			if ownID == "" || ownID != index.AsString(clash["id"]) {
				fmt.Fprintf(os.Stderr, "Error: aka '%s' already resolves to record %s (binder '%s')\n",
					opts.aka, index.AsString(clash["id"]), strings.Join(index.AsStrings(clash["binder"]), ", "))
				return 1
			}
		}
	}

	// Target: the index (default inbox.jsonl, or an explicit .jsonl). An explicit
	// .md target or --md becomes an *additional* annotation destination.
	explicit := ""
	if opts.target != "" {
		explicit = index.ExpandPath(opts.target)
	}
	explicitJSONL := ""
	target := filepath.Join(nd, "collections", "inbox.jsonl")
	annotateTarget := ""
	switch {
	case explicit != "" && strings.HasSuffix(strings.ToLower(explicit), ".jsonl"):
		explicitJSONL = explicit
		target = explicit
	case explicit != "":
		annotateTarget = explicit
	case opts.md:
		annotateTarget = defaultPromoteTarget(nd, opts.binder)
	}
	if annotateTarget != "" && !strings.HasSuffix(strings.ToLower(annotateTarget), ".md") {
		fmt.Fprintln(os.Stderr, "Error: annotation target must be a .md file")
		return 1
	}

	// Guard against a split identity: a file that carries an id the INDEX
	// knows, while the local bookmark db has no blob for it (fresh machine,
	// notes synced but bookmarks.json not). Minting a new id here would fork
	// the identity — the right tool is repair, which re-binds under the old id.
	// The same sweep collects each record's stored xattr backend and home file,
	// used below to keep this add consistent with the existing record.
	indexIDs := map[string]bool{}
	backendByID := map[string]string{}
	noteFileByID := map[string]string{}
	recByID := map[string]map[string]any{}
	for _, r := range refs {
		id := index.AsString(r["id"])
		indexIDs[id] = true
		recByID[id] = r
		backendByID[id] = index.XattrBackend(r)
		if nf := index.AsString(r["_note_file"]); strings.HasSuffix(strings.ToLower(nf), ".jsonl") {
			noteFileByID[id] = nf
		}
	}
	kept := present[:0]
	needRepair := 0
	for _, f := range present {
		orphaned := ""
		for _, id := range index.OfFileIDs(f) {
			if _, inDB := db[id]; indexIDs[id] && !inDB {
				orphaned = id
				break
			}
		}
		if orphaned != "" {
			fmt.Fprintf(os.Stderr, "Skipping %s: carries id %s known to the index but missing from the local bookmark db — run 'register repair' to re-bind it\n",
				filepath.Base(f), orphaned)
			needRepair++
			continue
		}
		kept = append(kept, f)
	}
	present = kept
	if len(present) == 0 {
		fmt.Fprintln(os.Stderr, "Error: nothing to add (see repair hints above)")
		return 1
	}

	// 1. Bookmark every file in one DB load/save cycle. A failed db save aborts
	// BEFORE any index write — no records may reference unpersisted blobs.
	// indexIDs is passed as reserved: URL-ref ids never enter the bookmark db,
	// so without it a fresh id could collide with one and fuse two identities.
	fmt.Fprintf(os.Stderr, "Bookmarking %d file(s)…\n", len(present))
	bookmarked, dbErr := index.AddMany(present, indexIDs, index.FileIDsByName(refs))
	if dbErr != nil {
		fmt.Fprintf(os.Stderr, "Error: bookmark db save failed: %v\n", dbErr)
		return 1
	}

	// 2. Build ref records for the successfully bookmarked files.
	failed := 0
	var records []index.RefRecord
	pathByIdx := []string{}
	for _, res := range bookmarked {
		if res.Error != "" {
			fmt.Fprintf(os.Stderr, "  %s: %s\n", filepath.Base(res.Path), res.Error)
			failed++
			continue
		}
		rec := buildRefRecord(res.Path, res.ID, opts, backend)
		ref := index.NewRefRecord(rec)
		if !ref.Valid() {
			fmt.Fprintf(os.Stderr, "  %s: missing required field: id\n", filepath.Base(res.Path))
			failed++
			continue
		}
		records = append(records, ref)
		pathByIdx = append(pathByIdx, res.Path)
	}

	// 3. Write the records to the index — grouped by destination: an id that
	// already has an index record is updated in ITS file, so neither an
	// explicit --target nor the default inbox forks a second record for an id
	// that lives elsewhere.
	actions := make([]string, len(records))
	destOf := make([]string, len(records))
	if len(records) > 0 {
		type destGroup struct {
			dest string
			idxs []int
			recs []index.RefRecord
		}
		var groups []destGroup
		byDest := map[string]int{}
		for i, rec := range records {
			dest := target
			if nf := noteFileByID[rec.ID]; nf != "" && !index.PathsEqual(nf, target) {
				fmt.Fprintf(os.Stderr, "  Note: id %s already recorded in %s — updating there\n", rec.ID, filepath.Base(nf))
				dest = nf
			}
			gi, ok := byDest[dest]
			if !ok {
				gi = len(groups)
				byDest[dest] = gi
				groups = append(groups, destGroup{dest: dest})
			}
			groups[gi].idxs = append(groups[gi].idxs, i)
			groups[gi].recs = append(groups[gi].recs, rec)
			destOf[i] = dest
		}
		for _, g := range groups {
			acts, werr := index.JSONLWriteMany(g.recs, g.dest)
			if werr != nil {
				fmt.Fprintln(os.Stderr, "register:", werr)
				return 1
			}
			for j, a := range acts {
				actions[g.idxs[j]] = a
			}
		}
	}
	ensureSchema(nd)

	// 4. Membership layer per file (binder xattr + ★). Both mark membership, so a
	//    bookmark add (no binder) writes neither.
	appended, noop := 0, 0
	keptChanges := map[string]map[string]map[string]bool{} // index file → id → tag → keep
	noteKept := func(i int, keep bool) {
		file, id := destOf[i], records[i].ID
		if keptChanges[file] == nil {
			keptChanges[file] = map[string]map[string]bool{}
		}
		if keptChanges[file][id] == nil {
			keptChanges[file][id] = map[string]bool{}
		}
		keptChanges[file][id][opts.binder] = keep
	}
	for i := range records {
		if binderSet {
			path := pathByIdx[i]
			// An existing record's stored backend wins over this invocation's
			// flag: remove/audit derive their layer from the record, and
			// backfill never overwrites the stored field.
			effBackend := backend
			if stored, ok := backendByID[records[i].ID]; ok && stored != backend {
				if opts.hasXattr {
					fmt.Fprintf(os.Stderr, "  Note: %s: record already uses xattr backend '%s' — --xattr %s ignored\n",
						filepath.Base(path), stored, backend)
				}
				effBackend = stored
			}
			result := index.XattrBackendAdd(path, opts.binder, effBackend)
			if result == "failed" {
				layer := "kMDItemProjects"
				if effBackend == "tags" {
					layer = "kMDItemUserTags"
				}
				fmt.Fprintf(os.Stderr, "  Warning: failed to update %s on %s\n", layer, filepath.Base(path))
			}
			// The tags backend shares Finder tags with the user. A tag that
			// was already there when the file joined the binder is the user's:
			// remember it, so remove never takes it (SPEC: kept_tags).
			if effBackend == "tags" {
				isNew := i < len(actions) && actions[i] != "noop"
				switch {
				case result == "noop" && isNew:
					noteKept(i, true)
				case result == "added" && index.IsKeptTag(recByID[records[i].ID], opts.binder):
					noteKept(i, false)
				}
			}
			if index.ManagedMark(path) == "failed" {
				fmt.Fprintf(os.Stderr, "  Warning: failed to set ★ on %s\n", filepath.Base(path))
			}
		}
		if i < len(actions) && actions[i] == "noop" {
			noop++
		} else {
			appended++
		}
	}

	for file, changes := range keptChanges {
		if _, err := index.JSONLSetKeptTags(file, changes); err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: recording kept tags in %s failed: %v\n", filepath.Base(file), err)
		}
	}

	// 4b. Optional Markdown annotation destination (idempotent, index stays truth).
	annotated := 0
	if annotateTarget != "" && len(records) > 0 {
		recMaps := make([]map[string]any, len(records))
		for i, r := range records {
			recMaps[i] = r.ToH()
		}
		annotated, _, _ = promoteRecords(recMaps, annotateTarget, opts.binder)
	}

	if opts.aka != "" && len(records) == 1 {
		warnAkaNotApplied(nd, opts.aka, records[0].ID)
	}

	// 5. Report.
	targetLabel := filepath.Base(target)
	if explicitJSONL != "" && target == explicitJSONL {
		targetLabel = target
	}
	if binderSet {
		fmt.Printf("Added to '%s' → %s [%s]\n", opts.binder, targetLabel, backend)
	} else {
		s := ""
		if len(records) != 1 {
			s = "s"
		}
		fmt.Printf("Registered bookmark%s → %s\n", s, targetLabel)
	}
	fmt.Printf("  appended : %d\n", appended)
	if noop > 0 {
		fmt.Printf("  noop     : %d\n", noop)
	}
	if annotateTarget != "" {
		fmt.Printf("  annotated: %d → %s\n", annotated, filepath.Base(annotateTarget))
	}
	if len(missing)+needRepair > 0 {
		fmt.Printf("  skipped  : %d\n", len(missing)+needRepair)
	}
	if failed > 0 {
		fmt.Printf("  failed   : %d\n", failed)
	}
	if appended == 0 && noop == 0 {
		return 1
	}
	return 0
}

// buildRefRecord builds the ref-record map for one file.
func buildRefRecord(filePath, id string, opts addOptions, backend string) map[string]any {
	kind := opts.kind
	if !opts.hasKind {
		kind = detectKind(filePath)
	}
	rec := map[string]any{
		"type":     "ref",
		"id":       id,
		"binder":   opts.binder,
		"filename": filepath.Base(filePath),
	}
	if kind != "" {
		rec["kind"] = kind
	}
	if opts.aka != "" {
		rec["aka"] = []any{opts.aka}
	}
	if backend != "itemprojects" {
		rec["xattr"] = backend
	}
	return rec
}

// addURLRecord adds a URL reference (no bookmark, no xattr, no ★).
func addURLRecord(opts addOptions, binderSet bool) int {
	url := opts.url
	if !urlRe.MatchString(url) {
		fmt.Fprintln(os.Stderr, "Error: --url must be a scheme://… URL")
		return 1
	}
	nd, err := notesDir()
	if err != nil {
		return 1
	}
	refs, refsOK := loadRefs(nd)
	if !refsOK {
		return 1
	}

	if opts.aka != "" {
		if clash := index.ResolveKey(refs, opts.aka); clash != nil && index.AsString(clash["url"]) != url {
			fmt.Fprintf(os.Stderr, "Error: aka '%s' already resolves to record %s (binder '%s')\n",
				opts.aka, index.AsString(clash["id"]), strings.Join(index.AsStrings(clash["binder"]), ", "))
			return 1
		}
	}

	var existingURL map[string]any
	for _, r := range refs {
		if index.AsString(r["url"]) == url {
			existingURL = r
			break
		}
	}

	// Index target (default inbox.jsonl or explicit .jsonl); an explicit .md or
	// --md is an additional annotation destination.
	explicit := ""
	if opts.target != "" {
		explicit = index.ExpandPath(opts.target)
	}
	target := filepath.Join(nd, "collections", "inbox.jsonl")
	annotateTarget := ""
	switch {
	case explicit != "" && strings.HasSuffix(strings.ToLower(explicit), ".jsonl"):
		target = explicit
	case explicit != "":
		annotateTarget = explicit
	case opts.md:
		annotateTarget = defaultPromoteTarget(nd, opts.binder)
	}
	if annotateTarget != "" && !strings.HasSuffix(strings.ToLower(annotateTarget), ".md") {
		fmt.Fprintln(os.Stderr, "Error: annotation target must be a .md file")
		return 1
	}

	var id string
	if existingURL != nil {
		id = index.AsString(existingURL["id"])
	} else {
		db, err := index.LoadDB()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			return 1
		}
		ids := map[string]bool{}
		for _, r := range refs {
			ids[index.AsString(r["id"])] = true
		}
		id = index.GenerateID()
		for {
			_, inDB := db[id]
			if !inDB && !ids[id] {
				break
			}
			id = index.GenerateID()
		}
	}

	kind := opts.kind
	if !opts.hasKind {
		kind = detectURLKind(url)
	}
	rec := map[string]any{"type": "ref", "id": id, "binder": opts.binder, "url": url}
	if opts.label != "" {
		rec["filename"] = opts.label
	}
	if kind != "" {
		rec["kind"] = kind
	}
	if opts.aka != "" {
		rec["aka"] = []any{opts.aka}
	}

	// An existing record is updated in ITS file — an explicit --target must not
	// fork a second record for the same id.
	dest := target
	if existingURL != nil {
		if nf := index.AsString(existingURL["_note_file"]); strings.HasSuffix(strings.ToLower(nf), ".jsonl") && !index.PathsEqual(nf, dest) {
			fmt.Fprintf(os.Stderr, "Note: id %s already recorded in %s — updating there\n", id, filepath.Base(nf))
			dest = nf
		}
	}
	ref := index.NewRefRecord(rec)
	actions, werr := index.JSONLWriteMany([]index.RefRecord{ref}, dest)
	if werr != nil {
		fmt.Fprintln(os.Stderr, "register:", werr)
		return 1
	}
	ensureSchema(nd)

	annotated := 0
	if annotateTarget != "" {
		annotated, _, _ = promoteRecords([]map[string]any{ref.ToH()}, annotateTarget, opts.binder)
	}

	action := "appended"
	if len(actions) > 0 {
		action = actions[0]
	}
	label := "as bookmark"
	if binderSet {
		label = "to '" + opts.binder + "'"
	}
	fmt.Printf("Added URL %s → %s [%s]\n", label, filepath.Base(dest), action)
	fmt.Printf("  url      : %s\n", url)
	fmt.Printf("  id       : %s\n", id)
	if opts.aka != "" {
		warnAkaNotApplied(nd, opts.aka, id)
	}
	if annotateTarget != "" {
		fmt.Printf("  annotated: %d → %s\n", annotated, filepath.Base(annotateTarget))
	}
	return 0
}

func validXattr(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "itemprojects", "tags", "none":
		return true
	}
	return false
}

// loadRefs reads the whole index. On a read error it says so and returns
// false: the caller stops, because every command that goes on with a partial
// index can mint duplicates or report records as missing.
func loadRefs(notesDir string) ([]map[string]any, bool) {
	recs, err := index.ReadAllRefs(notesDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return nil, false
	}
	return recs, true
}
