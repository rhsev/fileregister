package main

// cmd_album — a binder as an album: its members in order, with their fields
// and what the files themselves carry. The output is data, one JSON line per
// member, and nothing else; whoever wants a gallery, a report or a page builds
// it from that.
//
// An album line is a `list <binder> --json` line plus three keys: position
// (the order `order show` resolves), fields (the member's block, read through
// grubber, frontmatter inherited) and file (Spotlight and the file system).
// list stays fast because it touches neither grubber nor Spotlight; album is
// the one command that joins the three layers.

import (
	"github.com/rhsev/fileregister/v2/internal/index"

	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	unorm "golang.org/x/text/unicode/norm"
)

const albumUsage = "Usage: register album <binder> [--title TEXT]"

// albumRemovedFlags were the HTML renderer's, gone since the album became data.
// They get a sentence saying so instead of a bare "unknown option".
var albumRemovedFlags = map[string]bool{"--out": true, "--css": true, "--milan": true, "--open": true}

func cmdAlbum(args []string) int {
	vals, bools, pos, unk := parseFlags(args, map[string]bool{"--title": true}, nil)
	if name := strings.TrimSuffix(unk, missingValue); albumRemovedFlags[name] {
		fmt.Fprintf(os.Stderr, "register album: %s is gone — album prints the album as JSON lines, "+
			"and rendering it is up to the tool that reads them (ALBUM.md)\n", name)
		return 1
	}
	if unk != "" {
		return unknownOption("album", unk)
	}
	if bools["--help"] {
		fmt.Println(albumUsage)
		return 0
	}
	if bools["--version"] {
		fmt.Println("register album " + registerVersion)
		return 0
	}
	if len(pos) != 1 {
		fmt.Fprintln(os.Stderr, albumUsage)
		return 1
	}
	binder := pos[0]

	nd, err := notesDir()
	if err != nil {
		return 1
	}
	// Naming the album is the one write album does, and it happens before the
	// read, so the lines already carry the new name. Messages go to stderr:
	// stdout is the data.
	title, titleSet := vals["--title"]
	clearTitle := titleSet && strings.TrimSpace(title) == ""
	if titleSet && !clearTitle {
		// NFC, as binder names are: a name pasted from the Finder arrives
		// decomposed, looks the same, and a search typed on the keyboard would
		// not find it. A heading is one line, so no line breaks or tabs.
		title = unorm.NFC.String(title)
		if strings.IndexFunc(title, unicode.IsControl) >= 0 {
			fmt.Fprintf(os.Stderr, "register album: %q contains a line break or another control character — an album name is one line\n", title)
			return 1
		}
	}
	cleared := ""
	if clearTitle {
		var werr error
		if cleared, werr = removeFrontmatterField(defaultPromoteTarget(nd, binder), "album"); werr != nil {
			fmt.Fprintf(os.Stderr, "Error: removing the album field failed: %v\n", werr)
			return 1
		}
	} else if titleSet {
		what, werr := upsertFrontmatterField(defaultPromoteTarget(nd, binder), "album", title)
		if werr != nil {
			fmt.Fprintf(os.Stderr, "Error: writing the album field failed: %v\n", werr)
			return 1
		}
		fmt.Fprintf(os.Stderr, "Album field %s: %q — every member of '%s' inherits it\n", what, title, binder)
	}

	// Members and their order exactly as `order show` has them: one source of
	// order, so the album cannot disagree with the ordering it is made of.
	ctx, ok := buildOrderContext(binder, "")
	if !ok {
		return 1
	}
	byID := recordsByID(ctx.records)

	// The Markdown layer is grubber's, not ours.
	blocks, gerr := grubberRecordsFor(nd, binder)
	if gerr != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", gerr)
		return 1
	}

	resolved, rerr := resolveRecordPaths(ctx.records)
	if rerr != nil {
		fmt.Fprintf(os.Stderr, "Warning: fileanchor batch resolve failed: %v — paths shown as broken\n", rerr)
	}
	locator := func(r map[string]any) string {
		if u := index.RefURL(r); u != "" {
			return u
		}
		return resolved[index.AsString(r["id"])]
	}

	var paths []string
	for _, r := range ctx.records {
		if !index.URLRef(r) && locator(r) != "" {
			paths = append(paths, locator(r))
		}
	}
	files := albumFileMetaAll(paths)

	for i, id := range ctx.displayed {
		rec := byID[id]
		loc := locator(rec)
		parts := append(memberJSONParts(rec, loc), jsonPair("position", i+1))
		if f := albumFields(blocks[id]); len(f) > 0 {
			parts = append(parts, jsonPair("fields", f))
		}
		if !index.URLRef(rec) && loc != "" {
			if f := files[loc]; len(f) > 0 {
				parts = append(parts, jsonPair("file", f))
			}
		}
		fmt.Println("{" + strings.Join(parts, ",") + "}")
	}

	if clearTitle {
		// Say what the name is now, not what was attempted: a note put
		// together by hand can still name the album after ours stopped.
		if name, from := albumFieldOf(blocks, ctx.displayed); name != "" {
			fmt.Fprintf(os.Stderr, "note: the album is still named %q by the frontmatter of %s\n", name, from)
		} else if cleared == "removed" {
			fmt.Fprintln(os.Stderr, "Album field removed — the album is called by its binder name again")
		} else {
			fmt.Fprintln(os.Stderr, "No album field to remove — the album is called by its binder name")
		}
	}
	return 0
}

// albumFields is a member's block as the album passes it on: everything the
// note says about it, including what the note's frontmatter hands down to every
// block (that is how each member knows its album). Left out are the keys that
// are the index's or the ordering's business, already in the line as id,
// binder and position, and grubber's own _-prefixed bookkeeping.
func albumFields(block map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range block {
		switch {
		case v == nil, strings.HasPrefix(k, "_"):
		case k == "type", k == "id", k == "binder", k == "sort":
		default:
			out[k] = v
		}
	}
	return out
}

// albumMdlsKeys are the Spotlight attributes an album line reports under file.
// Where one means what a curated field means, it takes that field's name, so
// the value a reader shows is simply file overlaid with fields.
var albumMdlsKeys = []string{
	"kMDItemHeadline", "kMDItemTitle", "kMDItemDescription",
	"kMDItemCity", "kMDItemCountry", "kMDItemContentCreationDate",
	"kMDItemLatitude", "kMDItemLongitude",
	"kMDItemAcquisitionMake", "kMDItemAcquisitionModel",
	"kMDItemPixelWidth", "kMDItemPixelHeight",
	"kMDItemNumberOfPages", "kMDItemDurationSeconds", "kMDItemContentType",
}

var mdlsLineRe = regexp.MustCompile(`(?m)^(kMDItem\w+)\s*=\s*(.+)$`)

// albumFileMeta is what a file says about itself: size and modification time
// from the file system, the rest from Spotlight (IPTC/EXIF for images, page
// count for PDFs, duration for media). Keys without a value are left out.
func albumFileMeta(path string) map[string]any {
	out := map[string]any{}
	if fi, err := os.Stat(path); err == nil {
		if !fi.IsDir() {
			out["size"] = fi.Size()
		}
		out["modified"] = fi.ModTime().UTC().Format(time.RFC3339)
	}
	args := make([]string, 0, len(albumMdlsKeys)*2+1)
	for _, k := range albumMdlsKeys {
		args = append(args, "-name", k)
	}
	if raw, err := exec.Command("mdls", append(args, path)...).Output(); err == nil {
		for k, v := range albumSpotlightFields(string(raw)) {
			out[k] = v
		}
	}
	// Spotlight hands images their file name as kMDItemTitle. That is not a
	// title the file carries, and the line has the file name already.
	if t, _ := out["title"].(string); t != "" {
		base := filepath.Base(path)
		if t == base || t == strings.TrimSuffix(base, filepath.Ext(base)) {
			delete(out, "title")
		}
	}
	return out
}

// albumSpotlightFields maps mdls output onto the file keys.
func albumSpotlightFields(raw string) map[string]any {
	md := map[string]string{}
	for _, m := range mdlsLineRe.FindAllStringSubmatch(raw, -1) {
		v := strings.TrimSpace(m[2])
		if v == "(null)" || v == "(" {
			continue // absent, or an array mdls spreads over several lines
		}
		md[m[1]] = strings.TrimSuffix(strings.TrimPrefix(v, `"`), `"`)
	}

	out := map[string]any{}
	setStr := func(key string, vals ...string) {
		if v := firstNonEmptyOf(vals...); v != "" {
			out[key] = v
		}
	}
	setStr("title", md["kMDItemHeadline"], md["kMDItemTitle"])
	setStr("comment", md["kMDItemDescription"])
	setStr("place", joinNonEmpty(", ", md["kMDItemCity"], md["kMDItemCountry"]))
	setStr("camera", joinNonEmpty(" ", uniqStrings([]string{md["kMDItemAcquisitionMake"], md["kMDItemAcquisitionModel"]}, true)...))
	setStr("type", md["kMDItemContentType"])
	if t, err := time.Parse("2006-01-02 15:04:05 -0700", md["kMDItemContentCreationDate"]); err == nil {
		out["date"] = t.UTC().Format(time.RFC3339)
	}
	for key, attr := range map[string]string{"lat": "kMDItemLatitude", "lon": "kMDItemLongitude", "duration": "kMDItemDurationSeconds"} {
		if f, err := strconv.ParseFloat(md[attr], 64); err == nil {
			out[key] = f
		}
	}
	for key, attr := range map[string]string{"width": "kMDItemPixelWidth", "height": "kMDItemPixelHeight", "pages": "kMDItemNumberOfPages"} {
		if n, err := strconv.ParseInt(md[attr], 10, 64); err == nil {
			out[key] = n
		}
	}
	return out
}

// albumFileMetaAll runs albumFileMeta for every path on a small worker pool:
// one mdls fork per file is the dominant cost of an album when run serially.
func albumFileMetaAll(paths []string) map[string]map[string]any {
	out := make(map[string]map[string]any, len(paths))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for _, p := range paths {
		wg.Add(1)
		sem <- struct{}{}
		go func(p string) {
			defer wg.Done()
			m := albumFileMeta(p)
			mu.Lock()
			out[p] = m
			mu.Unlock()
			<-sem
		}(p)
	}
	wg.Wait()
	return out
}

func firstNonEmptyOf(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func joinNonEmpty(sep string, vals ...string) string {
	var keep []string
	for _, v := range vals {
		if v != "" {
			keep = append(keep, v)
		}
	}
	return strings.Join(keep, sep)
}
