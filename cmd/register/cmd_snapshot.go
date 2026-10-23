package main

// cmd_snapshot — pack the whole register into one tar.gz: every index file
// byte for byte, every note with ref blocks, and one line per file record with
// where the file was (base and path), its size and sha256, its free Finder
// tags and comment, and by default the file itself. Each line also carries the
// file's modification time, so that a later snapshot taken --since this one
// reads only the files that changed.
//
// Container layout:
//   register-snapshot-<date>/
//     snapshot.json   format, time, register version, system, bases, counts
//     index/          collections/*.jsonl and SCHEMA, unchanged
//     notes/          notes with ref blocks, by their path below the notes folder
//     files.jsonl     one line per file record
//     files/<id>/     the file, by id so equal names do not collide
//
// The bookmark store never travels: a bookmark works only on the system that
// made it, and restore makes new ones.

import (
	"github.com/rhsev/fileregister/v2/internal/index"

	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const snapshotFormat = "register-snapshot"
const snapshotVersion = 1

const snapshotUsage = "Usage: register snapshot [--out <file>.tar.gz] [--no-files [--since <snapshot>]] [--no-finder]"

func cmdSnapshot(args []string) int {
	vals, bools, pos, unknown := parseFlags(args,
		map[string]bool{"--out": true, "--since": true},
		map[string]bool{"--no-files": true, "--no-finder": true})
	if bools["--help"] {
		fmt.Println(snapshotUsage)
		return 0
	}
	if bools["--version"] {
		fmt.Println("register snapshot " + registerVersion)
		return 0
	}
	if unknown != "" {
		return unknownOption("snapshot", unknown)
	}
	if len(pos) > 0 {
		fmt.Fprintf(os.Stderr, "register snapshot: unexpected argument '%s'\n%s\n", pos[0], snapshotUsage)
		return 1
	}
	withFiles := !bools["--no-files"]
	withFinder := !bools["--no-finder"]

	// --since: a file whose size and modification time are those the earlier
	// snapshot recorded keeps that snapshot's sha256 and is not read. The same
	// quick check rsync makes; a change that keeps both is not noticed. A
	// snapshot with the files reads them anyway, so it takes no --since.
	var since map[string]map[string]any
	if sp := vals["--since"]; sp != "" {
		if withFiles {
			fmt.Fprintf(os.Stderr, "Error: --since only goes with --no-files\n%s\n", snapshotUsage)
			return 1
		}
		var serr error
		if since, serr = snapshotFileLines(sp); serr != nil {
			fmt.Fprintln(os.Stderr, "Error:", serr)
			return 1
		}
	}

	notesDir, err := notesDir()
	if err != nil {
		return 1
	}
	now := time.Now()
	top := "register-snapshot-" + now.Format("2006-01-02")
	out := vals["--out"]
	if out == "" {
		out = top + ".tar.gz"
	}
	if abs, aerr := filepath.Abs(out); aerr == nil {
		out = abs
	}
	if !strings.HasSuffix(out, ".tar.gz") && !strings.HasSuffix(out, ".tgz") {
		fmt.Fprintln(os.Stderr, "Error: --out must end in .tar.gz (or .tgz)")
		return 1
	}

	// The index as it stands, byte for byte, and the records read from it. A
	// line that is not a record is packed all the same and named.
	collections := filepath.Join(notesDir, "collections")
	indexFiles, _ := filepath.Glob(filepath.Join(collections, "*.jsonl"))
	sort.Strings(indexFiles)
	if _, serr := os.Stat(filepath.Join(collections, "SCHEMA")); serr == nil {
		indexFiles = append(indexFiles, filepath.Join(collections, "SCHEMA"))
	}
	indexData := map[string][]byte{}
	var badLines []string
	recByID := map[string]map[string]any{}
	var ids []string
	urls := 0
	records := 0
	for _, f := range indexFiles {
		data, rerr := os.ReadFile(f)
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "Error: cannot read %s: %v\n", f, rerr)
			return 1
		}
		indexData[filepath.Base(f)] = data
		if !strings.HasSuffix(f, ".jsonl") {
			continue
		}
		for n, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			rec := index.ParseJSONObject(line)
			if rec == nil {
				badLines = append(badLines, fmt.Sprintf("%s:%d", filepath.Base(f), n+1))
				continue
			}
			if index.AsString(rec["type"]) != "ref" {
				continue
			}
			records++
			if index.URLRef(rec) {
				urls++
				continue
			}
			id := index.AsString(rec["id"])
			if id == "" {
				continue
			}
			if prev, ok := recByID[id]; ok {
				// one record per id; a second line adds its binders
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
			ids = append(ids, id)
		}
	}

	// Notes with ref blocks, by their path below the notes folder.
	annos, aerr := readAnnotations(notesDir)
	if aerr != nil {
		fmt.Fprintf(os.Stderr, "Error: %v — the snapshot would miss the notes\n", aerr)
		return 1
	}
	realNotes := realOrSelf(notesDir)
	noteRel := map[string]string{}
	var noteKeys []string
	for _, a := range annos {
		nf := index.AsString(a["_note_file"])
		if _, done := noteRel[nf]; done || nf == "" {
			continue
		}
		rel, ok := relUnder(realOrSelf(nf), realNotes)
		if !ok || rel == "" {
			fmt.Fprintf(os.Stderr, "  Warning: note %s lies outside the notes folder — not packed\n", nf)
			noteRel[nf] = ""
			continue
		}
		noteRel[nf] = rel
		noteKeys = append(noteKeys, nf)
	}
	sort.Strings(noteKeys)

	fmt.Fprintf(os.Stderr, "Resolving %d file record(s)…\n", len(ids))
	resolved, rerr := index.BatchResolve(ids)
	if rerr != nil {
		fmt.Fprintf(os.Stderr, "Error: fileanchor batch resolve failed: %v — the snapshot would be incomplete\n", rerr)
		return 1
	}

	os.Remove(out)
	tw, err := newTarWriter(out, top)
	if err != nil {
		fmt.Fprintln(os.Stderr, "register:", err)
		return 1
	}
	fail := func(err error) int {
		tw.close()
		os.Remove(out)
		fmt.Fprintf(os.Stderr, "Error: writing %s failed: %v\n", out, err)
		return 1
	}

	names := make([]string, 0, len(indexData))
	for n := range indexData {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if err := tw.bytes("index/"+n, indexData[n]); err != nil {
			return fail(err)
		}
	}
	for _, nf := range noteKeys {
		if _, _, err := tw.file("notes/"+noteRel[nf], nf); err != nil {
			return fail(err)
		}
	}

	classifier := newBaseClassifier(notesDir)
	var lines []map[string]any
	var unresolved []string
	packed, hashed, reused := 0, 0, 0
	for _, id := range ids {
		rec := recByID[id]
		res := resolved[id]
		line := map[string]any{"id": id}
		if fn := index.AsString(rec["filename"]); fn != "" {
			line["filename"] = fn
		}
		p := res.Path
		if p == "" || !index.FileExists(p) {
			where := res.LastPath
			if where == "" {
				where = p
			}
			if base, rel := classifier.classify(where); base != "" {
				line["base"], line["path"] = base, rel
			}
			why := "last at " + orUnknown(where)
			if v := unmountedVolume(where); v != "" {
				why = fmt.Sprintf("volume %s is not mounted (%s)", v, where)
			}
			unresolved = append(unresolved, fmt.Sprintf("%s (id %s) — %s", nameOr(rec, where), id, why))
			lines = append(lines, line)
			continue
		}
		base, rel := classifier.classify(p)
		line["base"], line["path"] = base, rel
		fi, serr := os.Stat(p)
		switch {
		case serr != nil:
		case !fi.Mode().IsRegular():
			fmt.Fprintf(os.Stderr, "  Note: %s is not a regular file — recorded without content\n", p)
		case withFiles:
			name := "files/" + id + "/" + filepath.Base(p)
			size, sum, ferr := tw.file(name, p)
			if ferr != nil {
				return fail(ferr)
			}
			line["size"], line["sha256"], line["file"] = size, sum, name
			line["mtime"] = fileMtime(fi)
			packed++
		default:
			mtime := fileMtime(fi)
			if old := since[id]; old != nil && manifestInt(old["size"]) == fi.Size() &&
				index.AsString(old["mtime"]) == mtime && index.AsString(old["sha256"]) != "" {
				line["size"], line["sha256"], line["mtime"] = fi.Size(), index.AsString(old["sha256"]), mtime
				reused++
				break
			}
			size, sum, ferr := fileDigest(p)
			if ferr != nil {
				fmt.Fprintf(os.Stderr, "  Warning: cannot read %s: %v — recorded without size and checksum\n", p, ferr)
			} else {
				line["size"], line["sha256"], line["mtime"] = size, sum, mtime
				hashed++
			}
		}
		if withFinder {
			addFinderFields(line, rec, p)
		}
		lines = append(lines, line)
	}

	var fl strings.Builder
	for _, l := range lines {
		fl.WriteString(index.JSONVal(l) + "\n")
	}
	if err := tw.bytes("files.jsonl", []byte(fl.String())); err != nil {
		return fail(err)
	}

	head := map[string]any{
		"format":   snapshotFormat,
		"version":  snapshotVersion,
		"created":  now.Format(time.RFC3339),
		"register": registerVersion,
		"system":   runtime.GOOS,
		"bases":    classifier.roots,
		"files":    withFiles,
		"finder":   withFinder,
		"counts": map[string]int{
			"records":      records,
			"url_records":  urls,
			"file_records": len(ids),
			"files_packed": packed,
			"unresolved":   len(unresolved),
			"notes":        len(noteKeys),
			"index_files":  len(indexData),
		},
	}
	hj, _ := json.MarshalIndent(head, "", "  ")
	if err := tw.bytes("snapshot.json", append(hj, '\n')); err != nil {
		return fail(err)
	}
	if err := tw.close(); err != nil {
		os.Remove(out)
		fmt.Fprintf(os.Stderr, "Error: writing %s failed: %v\n", out, err)
		return 1
	}

	fmt.Printf("Snapshot → %s\n", out)
	fmt.Printf("  index files  : %d\n", len(indexData))
	fmt.Printf("  records      : %d (%d file, %d url)\n", records, len(ids), urls)
	if withFiles {
		fmt.Printf("  files packed : %d\n", packed)
	} else {
		fmt.Printf("  files        : not packed (--no-files)\n")
		if since != nil {
			fmt.Printf("  checksums    : %d read, %d unchanged since the earlier snapshot\n", hashed, reused)
		}
	}
	fmt.Printf("  notes        : %d\n", len(noteKeys))
	if len(badLines) > 0 {
		fmt.Printf("  lines that are not records (packed as they are): %s\n", strings.Join(badLines, ", "))
	}
	if len(unresolved) > 0 {
		fmt.Printf("  files not reached (recorded without content): %d\n", len(unresolved))
		for _, u := range unresolved {
			fmt.Printf("    - %s\n", u)
		}
		return 1
	}
	if len(badLines) > 0 {
		return 1
	}
	return 0
}

// nameOr returns the record's filename, else the base name of p.
func nameOr(rec map[string]any, p string) string {
	if fn := index.AsString(rec["filename"]); fn != "" {
		return fn
	}
	if p != "" {
		return filepath.Base(p)
	}
	return "?"
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

// fileMtime is a file's modification time as the manifest records it: UTC,
// to the nanosecond the file system keeps.
func fileMtime(fi os.FileInfo) string {
	return fi.ModTime().UTC().Format(time.RFC3339Nano)
}
