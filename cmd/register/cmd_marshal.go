package main

// cmd_marshal — pack a binder into a portable tar.gz container.
//
// Container layout:
//   <name>/
//     manifest.jsonl   index records; file/markdown are paths relative to <name>/,
//                      plus one {"type":"note",…} line per packed annotation note
//     files/           the target files
//     notes/           the associated Markdown annotation notes
//
// The archive carries no xattrs — unmarshal rebuilds metadata from the manifest,
// keeping the format OS-neutral.
//
// Note on manifest key order: the manifest preserves the record's input
// key order; this Go port emits map order. The manifest is machine-read by
// unmarshal (order-insensitive), so this is a deliberate, harmless deviation.

import (
	"github.com/rhsev/fileregister/internal/index"

	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	unorm "golang.org/x/text/unicode/norm"
)

// marshalSanitize makes a filesystem-safe container/file base name — the same
// mapping the annotation-note names use, so a binder's artifacts share one slug.
func marshalSanitize(name string) string {
	return sanitizeBinderName(name)
}

// marshalDirName is the container's folder and default file name for binder.
// A binder named ".." (or ".") would make the staging folder the temp dir's
// parent and the container "...tar.gz"; such names become "binder".
func marshalDirName(binder string) string {
	name := marshalSanitize(binder)
	if name == "" || name == "." || name == ".." {
		return "binder"
	}
	return name
}

// marshalNameKey folds a name for uniqueness decisions: staging dirs, albums
// and unpacked containers must stay collision-free on case- and
// normalization-insensitive filesystems too, so uniqueness is decided on the
// least discriminating form regardless of where the container was built.
func marshalNameKey(s string) string {
	return strings.ToLower(unorm.NFC.String(s))
}

// marshalUniqueName returns a name not already in taken and claims it,
// appending -1, -2, … before the extension. taken holds marshalNameKey forms.
func marshalUniqueName(base string, taken map[string]bool) string {
	if k := marshalNameKey(base); !taken[k] {
		taken[k] = true
		return base
	}
	ext := filepath.Ext(base)
	stem := base[:len(base)-len(ext)]
	for i := 1; ; i++ {
		cand := fmt.Sprintf("%s-%d%s", stem, i, ext)
		if k := marshalNameKey(cand); !taken[k] {
			taken[k] = true
			return cand
		}
	}
}

func cmdMarshal(args []string) int {
	binder := ""
	out := ""
	allNotes := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--binder" && i+1 < len(args):
			i++
			binder = args[i]
		case strings.HasPrefix(a, "--binder="):
			binder = strings.TrimPrefix(a, "--binder=")
		case a == "--out" && i+1 < len(args):
			i++
			out = args[i]
		case strings.HasPrefix(a, "--out="):
			out = strings.TrimPrefix(a, "--out=")
		case a == "--all-notes":
			allNotes = true
		case a == "-h" || a == "--help":
			fmt.Println("Usage: register marshal --binder <name> [--out <file>.tar.gz]")
			return 0
		case a == "-v" || a == "--version":
			fmt.Println("register marshal " + registerVersion)
			return 0
		default:
			if strings.HasPrefix(a, "-") {
				return unknownOption("marshal", a)
			}
		}
	}

	if binder == "" {
		binder = os.Getenv("REGISTER_BINDER")
	}
	if binder == "" {
		fmt.Fprintln(os.Stderr, "Error: --binder is required (or set REGISTER_BINDER env var)")
		fmt.Fprintln(os.Stderr, "Usage: register marshal --binder <name> [--out <file>.tar.gz]")
		return 1
	}

	notesDir, err := notesDir()
	if err != nil {
		return 1
	}

	if out == "" {
		out = marshalDirName(binder) + ".tar.gz"
	}
	if abs, aerr := filepath.Abs(out); aerr == nil {
		out = abs
	}
	if !strings.HasSuffix(out, ".tar.gz") && !strings.HasSuffix(out, ".tgz") {
		fmt.Fprintln(os.Stderr, "Error: --out must end in .tar.gz (or .tgz)")
		return 1
	}

	fmt.Fprintf(os.Stderr, "Collecting records for binder '%s'…\n", binder)

	all, err := index.ReadAllRefs(notesDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "register:", err)
		return 1
	}
	var records []map[string]any
	for _, r := range all {
		for _, b := range index.AsStrings(r["binder"]) {
			if b == binder {
				records = append(records, r)
				break
			}
		}
	}
	if len(records) == 0 {
		fmt.Fprintf(os.Stderr, "No records for binder '%s'. Nothing to marshal.\n", binder)
		return 0
	}

	// Resolve every record's bookmark (url refs carry no path and are handled
	// without a file payload below).
	resolved, rerr := resolveRecordPaths(records)
	if rerr != nil {
		fmt.Fprintf(os.Stderr, "Error: fileanchor batch resolve failed: %v — container would be incomplete\n", rerr)
		return 1
	}

	// Annotation notes carrying this binder's ids, grouped by note file.
	idsSet := map[string]bool{}
	for _, r := range records {
		idsSet[index.AsString(r["id"])] = true
	}
	notesForIDs := map[string][]map[string]any{}
	annos, aerr := readAnnotations(notesDir)
	if aerr != nil {
		fmt.Fprintf(os.Stderr, "Error: %v — container would be incomplete\n", aerr)
		return 1
	}
	for _, r := range annos {
		if idsSet[index.AsString(r["id"])] {
			nf := index.AsString(r["_note_file"])
			notesForIDs[nf] = append(notesForIDs[nf], r)
		}
	}

	// jsonl_root: paths are recorded relative to the index/jsonl directory.
	collections := filepath.Join(notesDir, "collections")
	rootDir := notesDir
	if st, serr := os.Stat(collections); serr == nil && st.IsDir() {
		rootDir = collections
	}
	jsonlRoot, err := filepath.EvalSymlinks(rootDir)
	if err != nil {
		jsonlRoot = rootDir
	}

	missing := 0
	urls := 0
	var skippedNotes []string

	tmp, err := os.MkdirTemp("", "fileregister-marshal")
	if err != nil {
		fmt.Fprintln(os.Stderr, "register:", err)
		return 1
	}
	defer os.RemoveAll(tmp)

	root := filepath.Join(tmp, marshalDirName(binder))
	filesDir := filepath.Join(root, "files")
	notesOut := filepath.Join(root, "notes")
	if err := os.MkdirAll(filesDir, 0755); err != nil {
		fmt.Fprintln(os.Stderr, "register:", err)
		return 1
	}
	if err := os.MkdirAll(notesOut, 0755); err != nil {
		fmt.Fprintln(os.Stderr, "register:", err)
		return 1
	}

	// notePack: note path → {file, origin}. Binder notes (origin inside
	// collections/) always travel; scattered notes (origin escaping via ..) only
	// with --all-notes.
	type packEntry struct{ file, origin string }
	notePack := map[string]packEntry{}
	notesTaken := map[string]bool{}
	var noteKeys []string
	for k := range notesForIDs {
		noteKeys = append(noteKeys, k)
	}
	sort.Strings(noteKeys)
	for _, note := range noteKeys {
		if _, serr := os.Stat(note); serr != nil {
			continue
		}
		realNote, rerr := filepath.EvalSymlinks(note)
		if rerr != nil {
			realNote = note
		}
		origin, oerr := filepath.Rel(jsonlRoot, realNote)
		if oerr != nil {
			origin = realNote
		}
		if strings.HasPrefix(origin, "..") && !allNotes {
			skippedNotes = append(skippedNotes, origin)
			continue
		}
		name := marshalUniqueName(filepath.Base(note), notesTaken)
		if err := copyFile(note, filepath.Join(notesOut, name)); err != nil {
			fmt.Fprintln(os.Stderr, "register:", err)
			return 1
		}
		notePack[note] = packEntry{file: "notes/" + name, origin: origin}
	}

	// Preferred annotation note per id — binder notes win over scattered ones.
	mdForID := map[string]string{}
	packedNotes := make([]string, 0, len(notePack))
	for note := range notePack {
		packedNotes = append(packedNotes, note)
	}
	sort.Slice(packedNotes, func(i, j int) bool {
		pi, pj := notePack[packedNotes[i]], notePack[packedNotes[j]]
		si, sj := scatteredRank(pi.origin), scatteredRank(pj.origin)
		if si != sj {
			return si < sj
		}
		return pi.origin < pj.origin
	})
	for _, note := range packedNotes {
		for _, r := range notesForIDs[note] {
			id := index.AsString(r["id"])
			if _, ok := mdForID[id]; !ok {
				mdForID[id] = notePack[note].file
			}
		}
	}

	fileRel := map[string]string{}
	originRel := map[string]string{}
	filesTaken := map[string]bool{}
	var manifest []map[string]any

	for _, rec := range records {
		id := index.AsString(rec["id"])
		src := resolved[id]

		if index.URLRef(rec) {
			urls++ // pure data — no file payload to stage
		} else if _, done := fileRel[id]; !done {
			if src != "" && index.FileExists(src) {
				name := marshalUniqueName(filepath.Base(src), filesTaken)
				if err := copyFile(src, filepath.Join(filesDir, name)); err != nil {
					fmt.Fprintln(os.Stderr, "register:", err)
					return 1
				}
				fileRel[id] = "files/" + name
				realSrc, rerr := filepath.EvalSymlinks(src)
				if rerr != nil {
					realSrc = src
				}
				if rel, rlerr := filepath.Rel(jsonlRoot, realSrc); rlerr == nil {
					originRel[id] = rel
				} else {
					originRel[id] = realSrc
				}
			} else {
				missing++
				fmt.Fprintf(os.Stderr, "  Warning: file for id %s not found — record marshaled without its file\n", id)
			}
		}

		entry := map[string]any{}
		for k, v := range rec {
			if !strings.HasPrefix(k, "_") { // drop injected provenance
				entry[k] = v
			}
		}
		entry["file"] = nilIfEmpty(fileRel[id])
		entry["origin"] = nilIfEmpty(originRel[id])
		entry["markdown"] = nilIfEmpty(mdForID[id])
		manifest = append(manifest, entry)
	}

	// One note line per packed annotation note.
	for _, note := range noteKeys {
		if pack, ok := notePack[note]; ok {
			manifest = append(manifest, map[string]any{
				"type": "note", "file": pack.file, "origin": pack.origin,
			})
		}
	}
	notesPacked := len(notePack)

	// Checked writes: a short write here (ENOSPC while staging) would tar a
	// silently truncated manifest that imports as a subset on the other side.
	mf, err := os.Create(filepath.Join(root, "manifest.jsonl"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "register:", err)
		return 1
	}
	for _, e := range manifest {
		if _, werr := fmt.Fprintln(mf, index.JSONVal(e)); werr != nil {
			mf.Close()
			fmt.Fprintf(os.Stderr, "Error: writing manifest failed: %v\n", werr)
			return 1
		}
	}
	if cerr := mf.Close(); cerr != nil {
		fmt.Fprintf(os.Stderr, "Error: writing manifest failed: %v\n", cerr)
		return 1
	}

	os.Remove(out)
	// Plain tar; --no-mac-metadata keeps AppleDouble cruft out so the archive
	// stays OS-neutral.
	cmd := exec.Command("tar", "--no-mac-metadata", "-czf", out, "-C", tmp, filepath.Base(root))
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: tar failed to create %s\n", out)
		return 1
	}

	fmt.Printf("Marshaled '%s' → %s\n", binder, out)
	fmt.Printf("  records      : %d\n", len(records))
	fmt.Printf("  files packed : %d\n", len(records)-missing-urls)
	if urls > 0 {
		fmt.Printf("  url refs     : %d\n", urls)
	}
	if notesPacked > 0 {
		fmt.Printf("  notes packed : %d\n", notesPacked)
	}
	if missing > 0 {
		fmt.Printf("  missing files: %d\n", missing)
	}
	if len(skippedNotes) > 0 {
		fmt.Printf("  notes not packed (outside collections/, use --all-notes): %d\n", len(skippedNotes))
		for _, n := range skippedNotes {
			fmt.Printf("    - %s\n", n)
		}
	}
	return 0
}

// scatteredRank is 1 for a scattered note (origin escaping via ..), else 0.
func scatteredRank(origin string) int {
	if strings.HasPrefix(origin, "..") {
		return 1
	}
	return 0
}

// nilIfEmpty returns nil for "" so the manifest emits JSON null (rather than
// nil), or the string otherwise.
func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// copyFile copies file contents from src to dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
