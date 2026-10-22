package main

// cmd_restore — rebuild a register from a snapshot, into an empty register.
// The index files come back under their names, the notes at their paths below
// the notes folder, and every file record finds its file again: placed from
// the snapshot, taken over where it already lies (same sha256), or looked for
// by size and sha256 when the snapshot carries no content. Nothing that exists
// is overwritten; what cannot be placed stays parked in collections/import/.
// Merging into a register that has records is unmarshal's job.

import (
	"github.com/rhsev/fileregister/v2/internal/index"

	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const restoreUsage = "Usage: register restore <snapshot>.tar.gz [--into <folder>] [--map <base>=<folder>]... [--dry-run]"

// restoreItem is one file record on its way back.
type restoreItem struct {
	id, base, path, label string
	size                  int64 // -1 when the snapshot has none
	sum                   string
	tags                  []string
	comment               string
	finder                bool
	content               string // staged file, "" when the snapshot carries none
	mtime                 extractedEntry

	target  string
	action  string // create, adopt, changed, unverified, moved, nofile, park
	reason  string
	movedTo string
}

func cmdRestore(args []string) int {
	multi, bools, pos, unknown := parseFlagsMulti(args,
		map[string]bool{"--into": true, "--map": true},
		map[string]bool{"--dry-run": true})
	if bools["--help"] {
		fmt.Println(restoreUsage)
		return 0
	}
	if bools["--version"] {
		fmt.Println("register restore " + registerVersion)
		return 0
	}
	if unknown != "" {
		return unknownOption("restore", unknown)
	}
	if len(pos) != 1 {
		fmt.Fprintln(os.Stderr, restoreUsage)
		return 1
	}
	container := pos[0]
	if !index.FileExists(container) {
		fmt.Fprintf(os.Stderr, "Error: no such snapshot: %s\n", container)
		return 1
	}
	dry := bools["--dry-run"]
	maps, merr := parseMaps(multi["--map"])
	if merr != nil {
		fmt.Fprintln(os.Stderr, "Error:", merr)
		return 1
	}
	into := ""
	if v := multi["--into"]; len(v) > 0 {
		into, _ = filepath.Abs(index.ExpandPath(v[len(v)-1]))
	}

	notesDir, err := notesDir()
	if err != nil {
		return 1
	}
	collections := filepath.Join(notesDir, "collections")
	if !dry {
		if err := index.LockIndexDir(collections); err != nil {
			fmt.Fprintln(os.Stderr, "register:", err)
			return 1
		}
	}
	// A register with records is refused, unless its index is the snapshot's
	// own: then an earlier restore wrote it, and this run goes on with what
	// that one parked or did not find.
	again := false
	if why := registerNotEmpty(collections); why != "" {
		same, cerr := indexMatchesSnapshot(container, collections)
		if cerr != nil {
			fmt.Fprintln(os.Stderr, "Error:", cerr)
			return 1
		}
		if !same {
			fmt.Fprintf(os.Stderr, "Error: %s that differ from the snapshot — restore only fills an empty register, or one it filled from this snapshot. To add a binder to this one, use marshal and unmarshal.\n", why)
			return 1
		}
		again = true
		fmt.Fprintln(os.Stderr, "The index is this snapshot's; going on with the files an earlier restore left.")
	}

	// Unpack. A real run stages below collections/import/, where whatever is
	// parked waits; a dry run reads only the small parts into a temp folder.
	name := containerExtRe.ReplaceAllString(filepath.Base(container), "")
	if name == "" || name == "." || name == ".." {
		name = "snapshot"
	}
	var staging string
	var keep func(string) bool
	if dry {
		staging, err = os.MkdirTemp("", "register-restore")
		if err != nil {
			fmt.Fprintln(os.Stderr, "register:", err)
			return 1
		}
		defer os.RemoveAll(staging)
		keep = func(n string) bool { return !strings.HasPrefix(n, "files/") }
	} else {
		// What an earlier run parked here came from the container and is
		// unpacked again.
		staging = filepath.Join(importDir(notesDir), name)
		os.RemoveAll(staging)
		if err := os.MkdirAll(staging, 0755); err != nil {
			fmt.Fprintln(os.Stderr, "register:", err)
			return 1
		}
	}
	_, entries, xerr := extractContainer(container, staging, keep)
	if xerr != nil {
		fmt.Fprintln(os.Stderr, "Error:", xerr)
		if !dry {
			os.RemoveAll(staging)
		}
		return 1
	}

	headEntry, ok := entries["snapshot.json"]
	if !ok {
		fmt.Fprintf(os.Stderr, "Error: %s is not a register snapshot (no snapshot.json)\n", container)
		cleanupStaging(dry, staging, notesDir)
		return 1
	}
	var head struct {
		Format  string            `json:"format"`
		Version int               `json:"version"`
		System  string            `json:"system"`
		Bases   map[string]string `json:"bases"`
		Files   bool              `json:"files"`
		Created string            `json:"created"`
	}
	hdata, _ := os.ReadFile(headEntry.path)
	if json.Unmarshal(hdata, &head) != nil || head.Format != snapshotFormat {
		fmt.Fprintf(os.Stderr, "Error: %s is not a register snapshot\n", container)
		cleanupStaging(dry, staging, notesDir)
		return 1
	}
	if head.Version > snapshotVersion {
		fmt.Fprintf(os.Stderr, "Error: snapshot format %d is newer than this register understands (%d) — update register\n", head.Version, snapshotVersion)
		cleanupStaging(dry, staging, notesDir)
		return 1
	}

	home, _ := os.UserHomeDir()
	pl := &placement{notesDir: notesDir, home: home, into: into, maps: maps,
		defaults: true, srcSystem: head.System, srcRoots: head.Bases}

	// The records, from the snapshot's own index files: binders, backend and
	// kept tags decide what is written onto each file.
	recByID := map[string]map[string]any{}
	var indexNames []string
	for n, e := range entries {
		in, ok := strings.CutPrefix(n, "index/")
		if !ok {
			continue
		}
		if !isIndexName(in) {
			fmt.Fprintf(os.Stderr, "  Warning: index/%s is not an index file — skipped\n", in)
			continue
		}
		indexNames = append(indexNames, in)
		if !strings.HasSuffix(in, ".jsonl") {
			continue
		}
		data, _ := os.ReadFile(e.path)
		for _, line := range strings.Split(string(data), "\n") {
			rec := index.ParseJSONObject(strings.TrimSpace(line))
			if rec == nil || index.AsString(rec["type"]) != "ref" || index.URLRef(rec) {
				continue
			}
			id := index.AsString(rec["id"])
			if prev, ok := recByID[id]; ok {
				merged := index.NormalizeBinders(prev["binder"])
				for _, b := range index.NormalizeBinders(rec["binder"]) {
					if !containsStr(index.AsStrings(merged), index.AsString(b)) {
						merged = append(merged, b)
					}
				}
				prev["binder"] = merged
				continue
			}
			recByID[id] = rec
		}
	}
	sort.Strings(indexNames)

	// One item per file record.
	var items []*restoreItem
	if fe, ok := entries["files.jsonl"]; ok {
		data, _ := os.ReadFile(fe.path)
		for _, line := range strings.Split(string(data), "\n") {
			m := index.ParseJSONObject(strings.TrimSpace(line))
			if m == nil {
				continue
			}
			it := &restoreItem{
				id:      index.AsString(m["id"]),
				base:    index.AsString(m["base"]),
				path:    index.AsString(m["path"]),
				size:    manifestInt(m["size"]),
				sum:     index.AsString(m["sha256"]),
				comment: index.AsString(m["finder_comment"]),
				tags:    index.AsStrings(m["finder_tags"]),
			}
			_, it.finder = m["finder_tags"]
			it.label = index.AsString(m["filename"])
			if it.label == "" {
				it.label = filepath.Base(it.path)
			}
			if f := index.AsString(m["file"]); f != "" {
				if e, ok := entries[f]; ok {
					it.content, it.mtime = e.path, e
				} else if !dry {
					it.action, it.reason = "park", "its file is named in the snapshot but missing from it ("+f+")"
				} else {
					it.content = f // dry run: the content is there, just not unpacked
				}
			}
			items = append(items, it)
		}
	}

	if !dry {
		if lerr := index.LockBookmarks(); lerr != nil {
			fmt.Fprintln(os.Stderr, "register:", lerr)
			return 1
		}
	}
	db, err := index.LoadDB()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}

	// On a second run, a record whose bookmark finds its file is done.
	bound := map[string]string{}
	if again {
		var ids []string
		for _, it := range items {
			if _, ok := db[it.id]; ok {
				ids = append(ids, it.id)
			}
		}
		res, rerr := index.BatchResolve(ids)
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "Error: fileanchor batch resolve failed: %v\n", rerr)
			return 1
		}
		for id, r := range res {
			if r.Path != "" && index.FileExists(r.Path) {
				bound[id] = r.Path
			}
		}
	}

	// Decide where each file goes and what is there.
	claimed := map[string]bool{}
	for _, p := range bound {
		claimed[p] = true
	}
	var searching []*restoreItem
	for _, it := range items {
		if p, ok := bound[it.id]; ok && it.action == "" {
			it.action, it.target = "bound", p
		}
		if it.action != "" {
			continue
		}
		if it.id == "" {
			it.action, it.reason = "park", "line without id"
			continue
		}
		if it.base == "" {
			it.action, it.reason = "nofile", "the snapshot does not say where the file was"
			continue
		}
		target, why := pl.target(it.base, it.path)
		if why != "" {
			it.action, it.reason = "park", why
			continue
		}
		it.target = target
		if decomposedTwin(pl.rawTarget(it.base, it.path), target) {
			it.action, it.reason = "park", "the file exists with a decomposed (NFD) name; rename it to NFC, then run the same restore again"
			continue
		}
		if why := protectedTarget(target, underDir(target, collections)); why != "" && why != "an existing annotation note" {
			it.action, it.reason = "park", "target is "+why
			continue
		}
		fi, lerr := os.Lstat(target)
		switch {
		case lerr != nil && it.content != "":
			it.action = "create"
		case lerr != nil && it.sum != "" && it.size >= 0:
			it.action = "search"
			searching = append(searching, it)
		case lerr != nil:
			it.action, it.reason = "nofile", "no content and no checksum in the snapshot"
		case fi.Mode()&os.ModeSymlink != 0:
			it.action, it.reason = "park", "target is a symlink ("+target+")"
		case it.sum == "":
			it.action, it.reason = "unverified", "no checksum in the snapshot to compare"
		case !fi.Mode().IsRegular():
			it.action, it.reason = "park", "target is not a regular file ("+target+")"
		default:
			if sameContent(target, fi.Size(), it.size, it.sum) {
				it.action = "adopt"
			} else if it.content != "" {
				it.action, it.reason = "park", "a different file lies at "+target
			} else {
				it.action, it.reason = "changed", "content differs from the snapshot"
			}
		}
		if it.action != "park" && it.action != "nofile" && it.action != "search" {
			claimed[target] = true
		}
	}

	// Files the snapshot does not carry and that are not where they were:
	// look below the bases they belong to, by size first.
	if len(searching) > 0 {
		sizes := map[int64]bool{}
		rootSet := map[string]bool{}
		for _, it := range searching {
			sizes[it.size] = true
			if r, why := pl.root(it.base); why == "" {
				rootSet[r] = true
			}
		}
		var all []string
		for r := range rootSet {
			all = append(all, r)
		}
		roots := topRoots(all)
		fmt.Fprintf(os.Stderr, "Looking for %d file(s) below %s…\n", len(searching), strings.Join(roots, ", "))
		finder := newContentFinder(roots, sizes, []string{importDir(notesDir)})
		for _, it := range searching {
			found := finder.find(it.size, it.sum, claimed)
			switch len(found) {
			case 0:
				it.action, it.reason = "nofile", "not at "+it.target+" and not found below "+strings.Join(roots, ", ")
			case 1:
				it.action, it.movedTo = "moved", found[0]
				it.reason = "was " + it.target
				it.target = found[0]
				claimed[found[0]] = true
			default:
				it.action, it.reason = "park", fmt.Sprintf("%d files match its size and checksum: %s", len(found), strings.Join(found, ", "))
			}
		}
	}

	// Engine checks (Linux: roots, NFC, extended attributes) and an id this
	// system already has a bookmark for elsewhere.
	for _, it := range items {
		switch it.action {
		case "create", "adopt", "changed", "unverified", "moved":
		default:
			continue
		}
		if why := placeRefusal(it.target); why != "" {
			it.action, it.reason = "park", why
			continue
		}
		if it.action != "create" {
			if other := otherIdentity(db, it.target, it.id); other != "" {
				it.action, it.reason = "park", fmt.Sprintf("%s is already registered here as id %s", it.target, other)
				continue
			}
		}
		if p := bookmarkElsewhere(db, it.id, it.target); p != "" {
			it.action, it.reason = "park", "id "+it.id+" is already bookmarked on this system for "+p
		}
	}

	// Notes: back to their paths; one that exists is not overwritten.
	type noteItem struct {
		rel, src, target, why string
		there                 bool // the same note already lies at target
	}
	var notes []noteItem
	for n, e := range entries {
		rel, ok := strings.CutPrefix(n, "notes/")
		if !ok {
			continue
		}
		ni := noteItem{rel: rel, src: e.path}
		target, inside := containedJoin(notesDir, filepath.FromSlash(placeName(rel)))
		switch {
		case !inside:
			ni.why = "path escapes the notes folder"
		case !strings.EqualFold(filepath.Ext(rel), ".md"):
			ni.why = "not a Markdown note"
		case underDir(target, importDir(notesDir)):
			ni.why = "lies in the import folder"
		case index.FileExists(target):
			if sameBytes(target, e.path) {
				ni.there = true
			} else {
				ni.why = "a different note of that name exists (" + target + ")"
			}
		}
		ni.target = target
		notes = append(notes, ni)
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].rel < notes[j].rel })

	// Act.
	bookmarkFailed := 0
	var remarks []string
	if !dry {
		dbDirty := false
		for _, it := range items {
			switch it.action {
			case "create":
				if err := moveOrCopy(it.content, it.target, it.mtime.mtime); err != nil {
					it.action, it.reason = "park", "could not place: "+err.Error()
					continue
				}
				if why := placeRefusal(it.target); why != "" {
					os.Rename(it.target, it.content)
					it.action, it.reason = "park", why
					continue
				}
			case "adopt", "changed", "unverified", "moved":
			default:
				continue
			}
			rec := recByID[it.id]
			if rec == nil {
				rec = map[string]any{}
			}
			ok, rm := bindFile(db, it.target, it.id, rec, it.tags, it.comment, it.finder)
			dbDirty = true
			if !ok {
				bookmarkFailed++
				rm = append([]string{"bookmark could not be made — run: register repair"}, rm...)
			}
			for _, r := range rm {
				remarks = append(remarks, fmt.Sprintf("%s — %s", it.label, r))
			}
		}
		if dbDirty {
			if err := index.SaveDB(db); err != nil {
				fmt.Fprintln(os.Stderr, "register:", err)
				return 1
			}
		}
		if err := os.MkdirAll(collections, 0755); err != nil {
			fmt.Fprintln(os.Stderr, "register:", err)
			return 1
		}
		for _, n := range indexNames {
			if again {
				break // the index is the snapshot's already
			}
			data, _ := os.ReadFile(entries["index/"+n].path)
			if err := index.AtomicWrite(filepath.Join(collections, n), data); err != nil {
				fmt.Fprintf(os.Stderr, "Error: writing %s failed: %v\n", n, err)
				return 1
			}
			os.Remove(entries["index/"+n].path)
		}
		for i := range notes {
			ni := &notes[i]
			if ni.why != "" || ni.there {
				continue
			}
			if err := moveOrCopy(ni.src, ni.target, entries["notes/"+ni.rel].mtime); err != nil {
				ni.why = "could not place: " + err.Error()
			}
		}
	}

	// Report.
	count := map[string]int{}
	for _, it := range items {
		count[it.action]++
	}
	notesPlaced, notesThere, notesParked := 0, 0, 0
	for _, ni := range notes {
		switch {
		case ni.why != "":
			notesParked++
		case ni.there:
			notesThere++
		default:
			notesPlaced++
		}
	}
	verb := "Restored"
	if dry {
		verb = "Would restore (dry run)"
	}
	fmt.Printf("%s %s into %s\n", verb, filepath.Base(container), notesDir)
	fmt.Printf("  index files      : %d\n", len(indexNames))
	if notesThere > 0 {
		fmt.Printf("  notes            : %d placed, %d already there, %d parked\n", notesPlaced, notesThere, notesParked)
	} else {
		fmt.Printf("  notes            : %d placed, %d parked\n", notesPlaced, notesParked)
	}
	section := func(action, title string) {
		if count[action] == 0 {
			return
		}
		fmt.Printf("  %-17s: %d\n", title, count[action])
		for _, it := range items {
			if it.action != action {
				continue
			}
			switch {
			case action == "moved" || action == "changed" || action == "unverified":
				fmt.Printf("    - %s → %s (%s)\n", it.label, it.target, it.reason)
			case it.reason != "":
				fmt.Printf("    - %s — %s\n", it.label, it.reason)
			case it.target != "":
				fmt.Printf("    - %s → %s\n", it.label, it.target)
			}
		}
	}
	if count["bound"] > 0 {
		fmt.Printf("  %-17s: %d\n", "already restored", count["bound"])
	}
	section("create", "created")
	section("adopt", "taken over")
	section("moved", "found moved")
	section("changed", "changed")
	section("unverified", "taken over, unchecked")
	section("nofile", "without file")
	section("park", "parked")
	for _, ni := range notes {
		if ni.why != "" {
			fmt.Printf("    - note %s — %s\n", ni.rel, ni.why)
		}
	}
	if len(remarks) > 0 {
		fmt.Println("  remarks:")
		for _, r := range remarks {
			fmt.Printf("    - %s\n", r)
		}
	}

	parked := count["park"] + notesParked
	if !dry {
		if parked > 0 {
			fmt.Printf("Parked items are in %s\n", staging)
			if count["park"] > 0 {
				fmt.Println("Their records are in the index: clear the conflict or put the file in place, then run the same restore again.")
			}
		} else {
			cleanupStaging(dry, staging, notesDir)
		}
	}
	// A file found under another name keeps its old name in the index until
	// refresh writes the new one. restore leaves the index as the snapshot
	// has it; a second run accepts it with refreshed names (sameIndexFile).
	renamedFound := 0
	for _, it := range items {
		if it.action == "moved" && !index.SameName(it.label, filepath.Base(it.target)) {
			renamedFound++
		}
	}
	if renamedFound > 0 && !dry {
		fmt.Printf("%d file(s) were found under another name; run register refresh to update the names in the index.\n", renamedFound)
	}
	if count["nofile"] > 0 {
		fmt.Println("Records without file are in the index; register audit lists them. Once a file is back, run the same restore again, or register repair --interactive.")
	}
	if parked > 0 || count["nofile"] > 0 || bookmarkFailed > 0 {
		return 1
	}
	return 0
}

// topRoots drops the roots that lie below another one and sorts the rest.
func topRoots(roots []string) []string {
	sort.Strings(roots)
	var out []string
	for _, r := range roots {
		nested := false
		for _, o := range out {
			if _, ok := relUnder(r, o); ok {
				nested = true
			}
		}
		if !nested {
			out = append(out, r)
		}
	}
	return out
}

// indexMatchesSnapshot reports whether the index files in collections are
// the snapshot's (sameIndexFile): every one that holds records is in the
// snapshot, and every one in the snapshot is here.
func indexMatchesSnapshot(container, collections string) (bool, error) {
	tmp, err := os.MkdirTemp("", "register-restore-index")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(tmp)
	_, entries, err := extractContainer(container, tmp, func(n string) bool {
		return strings.HasPrefix(n, "index/") && strings.HasSuffix(n, ".jsonl")
	})
	if err != nil {
		return false, err
	}
	snap := map[string]string{}
	for n, e := range entries {
		snap[strings.TrimPrefix(n, "index/")] = e.path
	}
	local, _ := filepath.Glob(filepath.Join(collections, "*.jsonl"))
	for _, f := range local {
		data, err := os.ReadFile(f)
		if err != nil {
			return false, err
		}
		if _, ok := snap[filepath.Base(f)]; !ok && strings.TrimSpace(string(data)) != "" {
			return false, nil
		}
	}
	for n, p := range snap {
		if !sameIndexFile(p, filepath.Join(collections, n)) {
			return false, nil
		}
	}
	return len(snap) > 0, nil
}

// sameIndexFile reports whether two index files hold the same lines, where a
// ref record may differ in its filename alone: refresh writes the name a file
// has now, and the register is still the one this snapshot filled.
func sameIndexFile(a, b string) bool {
	da, err1 := os.ReadFile(a)
	db, err2 := os.ReadFile(b)
	if err1 != nil || err2 != nil {
		return false
	}
	la := strings.Split(strings.TrimRight(string(da), "\n"), "\n")
	lb := strings.Split(strings.TrimRight(string(db), "\n"), "\n")
	if len(la) != len(lb) {
		return false
	}
	for i := range la {
		if la[i] == lb[i] {
			continue
		}
		ra, rb := index.ParseJSONObject(la[i]), index.ParseJSONObject(lb[i])
		if ra == nil || rb == nil || index.AsString(ra["type"]) != "ref" {
			return false
		}
		delete(ra, "filename")
		delete(rb, "filename")
		if index.JSONVal(ra) != index.JSONVal(rb) {
			return false
		}
	}
	return true
}

// registerNotEmpty says why the register at collections already has records,
// or "".
func registerNotEmpty(collections string) string {
	files, _ := filepath.Glob(filepath.Join(collections, "*.jsonl"))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return fmt.Sprintf("cannot read %s", f)
		}
		if strings.TrimSpace(string(data)) != "" {
			return fmt.Sprintf("%s has records", f)
		}
	}
	return ""
}

// sameBytes reports whether the files a and b hold the same bytes.
func sameBytes(a, b string) bool {
	sa, ha, ea := fileDigest(a)
	sb, hb, eb := fileDigest(b)
	return ea == nil && eb == nil && sa == sb && ha == hb
}

// sameContent reports whether the file at p (of size have) has the size and
// sha256 the snapshot recorded; the size is compared first.
func sameContent(p string, have, size int64, sum string) bool {
	if size >= 0 && have != size {
		return false
	}
	_, s, err := fileDigest(p)
	return err == nil && s == sum
}

// underDir reports whether p lies below dir.
func underDir(p, dir string) bool {
	r, err := filepath.Rel(dir, p)
	return err == nil && r != "." && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}

// cleanupStaging removes a real run's staging folder, and import/ once empty.
func cleanupStaging(dry bool, staging, notesDir string) {
	if dry {
		return
	}
	os.RemoveAll(staging)
	os.Remove(importDir(notesDir))
}
