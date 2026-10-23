package main

// transfer — what marshal/unmarshal and snapshot/restore share: where a file
// goes when it is placed again (bases), how a file is recognized (size and
// sha256), and the Finder tags and comment that travel with a file.
//
// A file is recorded as a base and a path relative to it:
//
//	notes          the notes folder
//	home           the home folder
//	volume:<name>  a mounted volume (/Volumes/<name>; on Linux a mount under
//	               /media, /run/media or /mnt)
//	system         anything else, absolute
//
// The narrowest base wins in that order. The container's head records what
// each base was on the system that made it.

import (
	"github.com/rhsev/fileregister/v2/internal/index"

	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	baseNotes    = "notes"
	baseHome     = "home"
	baseSystem   = "system"
	volumePrefix = "volume:"
)

// realOrSelf resolves symlinks in p, or in its nearest existing ancestor when
// p itself is gone (a file that did not resolve still names where it was).
func realOrSelf(p string) string {
	if p == "" {
		return ""
	}
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	dir, rest := filepath.Dir(p), filepath.Base(p)
	for dir != p {
		if r, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(r, rest)
		}
		p = dir
		dir, rest = filepath.Dir(dir), filepath.Join(filepath.Base(dir), rest)
	}
	return p
}

// relUnder returns p relative to root when p lies at or below it, comparing
// the two in NFC: a notes folder named in the environment and a path the
// engine returns in disk bytes may spell the same name in two forms. The
// returned part keeps p's own bytes.
func relUnder(p, root string) (string, bool) {
	if root == "" {
		return "", false
	}
	if root == "/" {
		return strings.TrimPrefix(p, "/"), true
	}
	pc := strings.Split(strings.Trim(p, "/"), "/")
	rc := strings.Split(strings.Trim(root, "/"), "/")
	if len(pc) < len(rc) {
		return "", false
	}
	for i := range rc {
		if !index.SameName(pc[i], rc[i]) {
			return "", false
		}
	}
	return strings.Join(pc[len(rc):], "/"), true
}

// baseClassifier assigns paths on this system to bases and records each
// base's root as it is used.
type baseClassifier struct {
	notes, home string
	roots       map[string]string // base → root on this system
}

func newBaseClassifier(notesDir string) *baseClassifier {
	home, _ := os.UserHomeDir()
	c := &baseClassifier{notes: realOrSelf(notesDir), roots: map[string]string{}}
	if home != "" {
		c.home = realOrSelf(home)
	}
	c.roots[baseNotes] = c.notes
	return c
}

// classify returns the base and the path below it ("/"-separated). An empty
// path gives "", "".
func (c *baseClassifier) classify(p string) (string, string) {
	if p == "" {
		return "", ""
	}
	real := realOrSelf(p)
	if rel, ok := relUnder(real, c.notes); ok {
		return baseNotes, rel
	}
	if rel, ok := relUnder(real, c.home); ok && c.home != "/" {
		c.roots[baseHome] = c.home
		return baseHome, rel
	}
	if name, mount := volumeOfPath(real); name != "" {
		if rel, ok := relUnder(real, mount); ok {
			c.roots[volumePrefix+name] = mount
			return volumePrefix + name, rel
		}
	}
	c.roots[baseSystem] = "/"
	return baseSystem, strings.TrimPrefix(real, "/")
}

// placement decides where a base lands on this system.
type placement struct {
	notesDir string
	home     string
	into     string            // --into: every base but notes below one folder
	maps     map[string]string // --map base=dir, wins over into
	// defaults places a base that has no mapping where it was: home in this
	// system's home, a volume at its mount point if that is here, system at its
	// absolute path if the container comes from the same kind of system.
	defaults  bool
	srcSystem string
	srcRoots  map[string]string
}

// validBase reports whether name is a base a container may carry.
func validBase(name string) bool {
	switch name {
	case baseNotes, baseHome, baseSystem:
		return true
	}
	v, ok := strings.CutPrefix(name, volumePrefix)
	return ok && v != "" && v != "." && v != ".." && !strings.ContainsAny(v, "/\x00")
}

// parseMaps reads --map base=dir values. notes always maps to the notes folder.
func parseMaps(vals []string) (map[string]string, error) {
	out := map[string]string{}
	for _, v := range vals {
		base, dir, ok := strings.Cut(v, "=")
		if !ok || dir == "" {
			return nil, fmt.Errorf("--map needs <base>=<folder>, got '%s'", v)
		}
		if !validBase(base) {
			return nil, fmt.Errorf("--map: '%s' is not a base (home, system or volume:<name>)", base)
		}
		if base == baseNotes {
			return nil, fmt.Errorf("--map: notes always goes to the notes folder")
		}
		abs, err := filepath.Abs(index.ExpandPath(dir))
		if err != nil {
			return nil, err
		}
		out[base] = abs
	}
	return out, nil
}

// root returns the folder base lands in, or why it has none.
func (pl *placement) root(base string) (string, string) {
	if !validBase(base) {
		return "", fmt.Sprintf("unknown base '%s'", base)
	}
	if base == baseNotes {
		return pl.notesDir, ""
	}
	if d, ok := pl.maps[base]; ok {
		return d, ""
	}
	if pl.into != "" {
		switch {
		case base == baseHome:
			return pl.into, ""
		case base == baseSystem:
			return filepath.Join(pl.into, "system"), ""
		default:
			return filepath.Join(pl.into, strings.TrimPrefix(base, volumePrefix)), ""
		}
	}
	hint := fmt.Sprintf("use --into <folder> or --map %s=<folder>", base)
	if !pl.defaults {
		return "", "outside the notes folder; " + hint
	}
	switch {
	case base == baseHome:
		if pl.home == "" {
			return "", "no home folder here; " + hint
		}
		return pl.home, ""
	case base == baseSystem:
		if pl.srcSystem != runtime.GOOS {
			return "", fmt.Sprintf("an absolute path from %s is not guessed on %s; %s", pl.srcSystem, runtime.GOOS, hint)
		}
		return "/", ""
	default:
		src := pl.srcRoots[base]
		if st, err := os.Stat(src); src != "" && err == nil && st.IsDir() {
			return src, ""
		}
		where := src
		if where == "" {
			where = "unknown"
		}
		return "", fmt.Sprintf("%s is not here (was %s); %s", base, where, hint)
	}
}

// target joins a base's root and a path below it. The path comes from the
// container, so it must stay below the root after cleaning.
func (pl *placement) target(base, rel string) (string, string) {
	root, why := pl.root(base)
	if why != "" {
		return "", why
	}
	if rel == "" || filepath.IsAbs(rel) {
		return "", fmt.Sprintf("no usable path below %s ('%s')", base, rel)
	}
	t, ok := containedJoin(root, filepath.FromSlash(placeName(rel)))
	if !ok || t == filepath.Clean(root) {
		return "", fmt.Sprintf("path escapes its base (%s)", rel)
	}
	return t, ""
}

// rawTarget is target without placeName, for the Linux check that the same
// name already exists decomposed.
func (pl *placement) rawTarget(base, rel string) string {
	root, why := pl.root(base)
	if why != "" {
		return ""
	}
	t, _ := containedJoin(root, filepath.FromSlash(rel))
	return t
}

// fileDigest returns size and sha256 of a file's content.
func fileDigest(path string) (int64, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return 0, "", err
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

// copyFileHashed copies src to dst and returns the size and sha256 of what it
// copied, keeping the modification time.
func copyFileHashed(src, dst string) (int64, string, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, "", err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return 0, "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return 0, "", err
	}
	if fi, serr := in.Stat(); serr == nil {
		os.Chtimes(dst, fi.ModTime(), fi.ModTime())
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

// moveOrCopy puts src at dst: a rename when both lie on one file system,
// else a copy. dst must not exist; the caller checked.
func moveOrCopy(src, dst string, mtime time.Time) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if _, _, err := copyFileHashed(src, dst); err != nil {
		os.Remove(dst)
		return err
	}
	if !mtime.IsZero() {
		os.Chtimes(dst, mtime, mtime)
	}
	os.Remove(src)
	return nil
}

// finderFacts reads the free Finder tags and the Finder comment of a
// record's file. A read the engine refuses leaves the field out.
func finderFacts(rec map[string]any, path string) (tags []string, comment string, ok bool) {
	t, terr := index.FileTags(path)
	c, cerr := index.FileComment(path)
	if terr != nil && cerr != nil {
		return nil, "", false
	}
	return index.FreeTags(rec, t), c, true
}

// addFinderFields puts a file's free tags and comment into a manifest line.
func addFinderFields(line, rec map[string]any, path string) {
	tags, comment, ok := finderFacts(rec, path)
	if !ok {
		return
	}
	if tags == nil {
		tags = []string{}
	}
	line["finder_tags"] = tags
	line["finder_comment"] = comment
}

// setFileComment writes a Finder comment. A variable so the tests can watch
// the write: on macOS the engine also tells Finder, by Apple Event.
var setFileComment = index.SetFileComment

// bindFile makes target the file of record id on this system: bookmark under
// the same id, id attributes, binder attribute per backend, ★ for a member or
// ☆ for a record in no binder, and with finder the Finder part (writeFinder).
// Returns false when the bookmark could not be made, and remarks for the
// report (a comment the file keeps, a tag the engine refused).
func bindFile(db map[string]string, target, id string, rec map[string]any, tags []string, comment string, finder bool) (bool, []string) {
	var remarks []string
	ok := true
	if newID, err := index.RegisterIn(db, target, id); err != nil || newID == "" {
		ok = false
	}
	binders := index.NormalizeBinders(rec["binder"])
	backend := index.XattrBackend(rec)
	for _, b := range binders {
		index.XattrBackendAdd(target, index.AsString(b), backend)
	}
	index.StatusMark(target, len(binders) > 0)
	if !finder {
		return ok, remarks
	}
	_, remarks = writeFinder(target, tags, comment)
	return ok, remarks
}

// writeFinder adds the free Finder tags the file lacks and sets the comment
// when the file has none; a comment of its own stays. It reports whether
// anything was written, and remarks for the report (a comment the file keeps,
// a tag the engine refused).
func writeFinder(target string, tags []string, comment string) (bool, []string) {
	var remarks []string
	wrote := false
	for _, t := range tags {
		switch r := index.AddTag(target, t); r {
		case "added":
			wrote = true
		case "noop":
		default:
			remarks = append(remarks, fmt.Sprintf("tag '%s' not set (%s)", t, r))
		}
	}
	if comment == "" {
		return wrote, remarks
	}
	have, err := index.FileComment(target)
	switch {
	case err != nil:
		remarks = append(remarks, fmt.Sprintf("comment not read (%v)", err))
	case have == "":
		if err := setFileComment(target, comment); err != nil {
			remarks = append(remarks, fmt.Sprintf("comment not set (%v)", err))
		} else {
			wrote = true
		}
	case have != comment:
		remarks = append(remarks, fmt.Sprintf("keeps its own comment %q; the container has %q", have, comment))
	}
	return wrote, remarks
}

// finderEntry is what a snapshot recorded of a file's Finder side.
type finderEntry struct {
	tags    []string
	comment string
}

// snapshotFileLines reads a snapshot's files.jsonl, one line per record id.
func snapshotFileLines(container string) (map[string]map[string]any, error) {
	tmp, err := os.MkdirTemp("", "register-snapshot-lines")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	_, entries, err := extractContainer(container, tmp, func(n string) bool { return n == "files.jsonl" })
	if err != nil {
		return nil, err
	}
	fe, ok := entries["files.jsonl"]
	if !ok {
		return nil, fmt.Errorf("%s is not a register snapshot (no files.jsonl)", container)
	}
	data, err := os.ReadFile(fe.path)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]any{}
	for _, line := range strings.Split(string(data), "\n") {
		if m := index.ParseJSONObject(strings.TrimSpace(line)); m != nil {
			out[index.AsString(m["id"])] = m
		}
	}
	return out, nil
}

// finderFromSnapshot reads the free Finder tags and the comment per record id
// from a snapshot's files.jsonl. Records the snapshot holds without them
// (taken with --no-finder) are left out.
func finderFromSnapshot(container string) (map[string]finderEntry, error) {
	lines, err := snapshotFileLines(container)
	if err != nil {
		return nil, err
	}
	out := map[string]finderEntry{}
	for id, m := range lines {
		if _, has := m["finder_tags"]; !has {
			continue
		}
		out[id] = finderEntry{index.AsStrings(m["finder_tags"]), index.AsString(m["finder_comment"])}
	}
	return out, nil
}

// bookmarkElsewhere returns the path an existing bookmark for id leads to
// when that is another existing file than target, or "". Such an id belongs
// to a file this system already knows (another notes set, an older copy).
func bookmarkElsewhere(db map[string]string, id, target string) string {
	if _, ok := db[id]; !ok {
		return ""
	}
	res, err := index.BatchResolve([]string{id})
	if err != nil {
		return ""
	}
	p := res[id].Path
	if p == "" || index.PathsEqual(p, target) || !index.FileExists(p) {
		return ""
	}
	return p
}

// otherIdentity returns the id under which target is already registered on
// this system when that is not id, or "". Taking such a file over would give
// it two identities.
func otherIdentity(db map[string]string, target, id string) string {
	if own := index.ExistingIDIn(db, target); own != "" && own != id {
		return own
	}
	return ""
}

// contentFinder looks for files by size and sha256 below some roots: each
// tree is walked once, a file is read only when its size matches one wanted.
type contentFinder struct {
	bySize map[int64][]string
	sums   map[string]string
}

// newContentFinder walks roots (hidden folders and skip excluded, links not
// followed) and keeps the files whose size is in sizes.
func newContentFinder(roots []string, sizes map[int64]bool, skip []string) *contentFinder {
	f := &contentFinder{bySize: map[int64][]string{}, sums: map[string]string{}}
	seen := map[string]bool{}
	for _, root := range topRoots(roots) {
		if root == "" || root == "/" {
			continue
		}
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if d != nil && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				if p != root && strings.HasPrefix(d.Name(), ".") {
					return fs.SkipDir
				}
				for _, s := range skip {
					if p == s {
						return fs.SkipDir
					}
				}
				return nil
			}
			if !d.Type().IsRegular() || seen[p] {
				return nil
			}
			info, ierr := d.Info()
			if ierr != nil || !sizes[info.Size()] {
				return nil
			}
			seen[p] = true
			f.bySize[info.Size()] = append(f.bySize[info.Size()], p)
			return nil
		})
	}
	return f
}

// find returns the files with this size and sum that are not claimed.
func (f *contentFinder) find(size int64, sum string, claimed map[string]bool) []string {
	var out []string
	for _, p := range f.bySize[size] {
		if claimed[p] {
			continue
		}
		s, ok := f.sums[p]
		if !ok {
			_, s, _ = fileDigest(p)
			f.sums[p] = s
		}
		if s == sum {
			out = append(out, p)
		}
	}
	return out
}

// manifestInt reads a JSON number field as int64 (-1 when absent).
func manifestInt(v any) int64 {
	var n int64 = -1
	if s := index.AsString(v); s != "" {
		fmt.Sscan(s, &n)
	}
	return n
}
