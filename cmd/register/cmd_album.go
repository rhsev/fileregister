package main

// cmd_album — render a binder as a static, self-contained HTML album. Curated data lives in the annotation YAML (sort/title/
// comment/place/lat/lon/map); IPTC/EXIF via Spotlight fills the gaps.

import (
	"github.com/rhsev/fileregister/internal/index"
	"hash/fnv"
	"net/url"

	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
)

var albumMdlsKeys = []string{
	"kMDItemHeadline", "kMDItemDescription", "kMDItemCity", "kMDItemCountry",
	"kMDItemContentCreationDate", "kMDItemLatitude", "kMDItemLongitude",
	"kMDItemAcquisitionMake", "kMDItemAcquisitionModel",
}

// albumCSS is the built-in stylesheet, overridable via --css or
// ~/.config/fileregister/album.css.
const albumCSS = `body.album-page { margin: 0; background: Canvas; }
.album { color-scheme: light dark; font-family: -apple-system, sans-serif;
         max-width: 72rem; margin: 0 auto; padding: 2rem 1rem; color: CanvasText; }
.album h1 { font-weight: 600; letter-spacing: .02em; }
.album .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(20rem, 1fr));
        gap: 2rem; margin-top: 2rem; }
/* Offscreen-Einträge nicht rendern — macht auch sehr große Alben flüssig,
   ohne Paginierung (Browser überspringt Layout/Paint außerhalb des Viewports). */
.album figure, .album .card { content-visibility: auto; contain-intrinsic-size: auto 24rem; }
.album figure { margin: 0; }
.album figure img { width: 100%; height: auto; border-radius: 6px; display: block; }
.album figcaption h3 { margin: .6rem 0 .2rem; font-size: 1rem; font-weight: 600; }
.album figcaption p { margin: .2rem 0; font-size: .92rem; line-height: 1.45; }
.album .meta { opacity: .6; font-size: .8rem !important; }
.album .card { border: 1px solid color-mix(in srgb, CanvasText 20%, transparent);
        border-radius: 6px; padding: 1rem; }
.album .card a { font-weight: 600; }
/* Detailansicht: CSS-only Overlay via :target — Bild links, Metadaten oben
   rechts, Karte unten rechts. Schließen springt zurück zum Thumbnail. */
.album .detail { display: none; }
.album .detail:target { display: grid; position: fixed; inset: 0; z-index: 10;
  grid-template-columns: minmax(0, 2.2fr) minmax(16rem, 1fr); gap: 1.2rem;
  padding: 1.5rem; background: color-mix(in srgb, Canvas 94%, transparent);
  backdrop-filter: blur(8px); }
.album .detail-media { display: flex; align-items: center; justify-content: center; min-height: 0; }
.album .detail-media img { max-width: 100%; max-height: calc(100vh - 3rem);
  object-fit: contain; border-radius: 6px; }
.album .detail-side { display: flex; flex-direction: column; gap: 1.2rem;
  min-height: 0; overflow: auto; }
.album .detail-info h3 { margin: 0 0 .4rem; }
.album .detail-info p { margin: .2rem 0; font-size: .95rem; line-height: 1.5; }
.album .detail-map { flex: 1; min-height: 16rem; border: 0; border-radius: 6px; }
.album .detail-close { position: fixed; top: .6rem; right: 1.2rem; font-size: 1.8rem;
  line-height: 1; text-decoration: none; color: CanvasText; z-index: 11; }
@media (max-width: 50rem) {
  .album .detail:target { grid-template-columns: 1fr; overflow: auto; }
  .album .detail-media img { max-height: 60vh; }
}
`

// albumEsc escapes the five HTML metacharacters for text and attribute contexts.
var albumEscaper = strings.NewReplacer(
	"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")

func albumEsc(s string) string { return albumEscaper.Replace(s) }

var mdlsLineRe = regexp.MustCompile(`(?m)^(kMDItem\w+)\s*=\s*(.+)$`)

// albumSpotlightMeta reads IPTC/EXIF via Spotlight (mdls). Returns {} on failure.
func albumSpotlightMeta(path string) map[string]string {
	args := make([]string, 0, len(albumMdlsKeys)*2+1)
	for _, k := range albumMdlsKeys {
		args = append(args, "-name", k)
	}
	args = append(args, path)
	out, err := exec.Command("mdls", args...).Output()
	if err != nil {
		return map[string]string{}
	}
	meta := map[string]string{}
	for _, m := range mdlsLineRe.FindAllStringSubmatch(string(out), -1) {
		v := strings.TrimSpace(m[2])
		if v == "(null)" {
			continue
		}
		v = strings.TrimPrefix(v, `"`)
		v = strings.TrimSuffix(v, `"`)
		meta[m[1]] = v
	}
	return meta
}

// albumSpotlightMetaAll runs albumSpotlightMeta for every path on a small
// worker pool. Returns path → meta.
func albumSpotlightMetaAll(paths []string) map[string]map[string]string {
	out := make(map[string]map[string]string, len(paths))
	if len(paths) == 0 {
		return out
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for _, p := range paths {
		wg.Add(1)
		sem <- struct{}{}
		go func(p string) {
			defer wg.Done()
			m := albumSpotlightMeta(p)
			mu.Lock()
			out[p] = m
			mu.Unlock()
			<-sem
		}(p)
	}
	wg.Wait()
	return out
}

var albumAssetSafeRe = regexp.MustCompile(`[^\w. -]`)

func albumAssetSafe(name string) string { return albumAssetSafeRe.ReplaceAllString(name, "-") }

func albumThumb(src, dest string) bool {
	cmd := exec.Command("sips", "-s", "format", "jpeg", "-Z", "800", src, "--out", dest)
	cmd.Stdout, cmd.Stderr = nil, nil
	return cmd.Run() == nil
}

type albumEntry struct {
	rec                                map[string]any
	path                               string
	sortKey                            string
	hasSort                            bool
	title, comment, ort, datum, kamera string
	lat, lon                           float64
	hasLat, hasLon                     bool
	mapField                           any
	hasMap                             bool
	image                              bool
	url, filename, media, thumb        string
}

func firstNonEmptyOf(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func albumEntryOf(rec, ann map[string]any, path string, meta map[string]string) albumEntry {
	if meta == nil {
		meta = map[string]string{}
	}
	title := firstNonEmptyOf(index.AsString(ann["title"]), meta["kMDItemHeadline"], index.AsString(rec["filename"]))
	comment := firstNonEmptyOf(index.AsString(ann["comment"]), meta["kMDItemDescription"])
	ort := index.AsString(ann["place"])
	if ort == "" {
		var parts []string
		for _, p := range []string{meta["kMDItemCity"], meta["kMDItemCountry"]} {
			if p != "" {
				parts = append(parts, p)
			}
		}
		ort = strings.Join(parts, ", ")
	}
	datum := meta["kMDItemContentCreationDate"]
	if len(datum) > 10 {
		datum = datum[:10]
	}
	var kamParts []string
	kamSeen := map[string]bool{}
	for _, p := range []string{meta["kMDItemAcquisitionMake"], meta["kMDItemAcquisitionModel"]} {
		if p != "" && !kamSeen[p] {
			kamSeen[p] = true
			kamParts = append(kamParts, p)
		}
	}

	e := albumEntry{
		rec: rec, path: path, title: title, comment: comment, ort: ort,
		datum: datum, kamera: strings.Join(kamParts, " "),
		image:    index.AsString(rec["kind"]) == "image",
		url:      index.RefURL(rec),
		filename: index.AsString(rec["filename"]),
	}
	if v, ok := ann["sort"]; ok && v != nil {
		e.sortKey, e.hasSort = index.AsString(v), true
	}
	if lat, ok := albumFloat(ann["lat"], meta["kMDItemLatitude"]); ok {
		e.lat, e.hasLat = lat, true
	}
	if lon, ok := albumFloat(ann["lon"], meta["kMDItemLongitude"]); ok {
		e.lon, e.hasLon = lon, true
	}
	if v, ok := ann["map"]; ok {
		e.mapField, e.hasMap = v, true
	}
	return e
}

// albumFloat mirrors (ann_val || meta_val)&.to_f: the annotation wins, else the
// meta string; nil/"" both → not present.
func albumFloat(annVal any, metaVal string) (float64, bool) {
	if annVal != nil {
		s := index.AsString(annVal)
		if s == "" {
			return 0, false
		}
		f, _ := strconv.ParseFloat(s, 64) // unparseable input yields 0
		return f, true
	}
	if metaVal != "" {
		f, _ := strconv.ParseFloat(metaVal, 64)
		return f, true
	}
	return 0, false
}

func albumShowMap(e albumEntry) bool {
	if !e.hasLat || !e.hasLon {
		return false
	}
	if e.hasMap {
		switch v := e.mapField.(type) {
		case bool:
			if !v {
				return false
			}
		case string:
			switch strings.ToLower(strings.TrimSpace(v)) {
			case "false", "no", "off", "0":
				return false
			}
		}
	}
	return true
}

func albumMapSrc(lat, lon float64) string {
	bbox := fmt.Sprintf("%.6f%%2C%.6f%%2C%.6f%%2C%.6f", lon-0.008, lat-0.005, lon+0.008, lat+0.005)
	return fmt.Sprintf("https://www.openstreetmap.org/export/embed.html?bbox=%s&layer=mapnik&marker=%.6f%%2C%.6f",
		bbox, lat, lon)
}

func albumCaption(e albumEntry) string {
	cap := ""
	if e.title != "" {
		cap += "<h3>" + albumEsc(e.title) + "</h3>"
	}
	if e.comment != "" {
		cap += "<p>" + albumEsc(e.comment) + "</p>"
	}
	var mp []string
	for _, x := range []string{e.ort, e.datum} {
		if x != "" {
			mp = append(mp, x)
		}
	}
	metaLine := strings.Join(mp, " · ")
	if metaLine != "" {
		cap += "<p class=\"meta\">" + albumEsc(metaLine) + "</p>"
	}
	return cap
}

// heading names the album: the `album` field its members inherited from the
// note's frontmatter, or the binder name when the frontmatter does not name
// one.
func albumHTML(heading string, entries []albumEntry, css string) string {
	var body, details []string
	for i, e := range entries {
		cap := albumCaption(e)
		if e.image && e.media != "" {
			thumbSrc := e.thumb
			if thumbSrc == "" {
				thumbSrc = e.media
			}
			body = append(body, fmt.Sprintf(
				"<figure id=\"t%d\">\n  <a href=\"#d%d\"><img src=\"%s\" loading=\"lazy\" alt=\"%s\"></a>\n  <figcaption>%s</figcaption>\n</figure>\n",
				i, i, albumEsc(thumbSrc), albumEsc(e.title), cap))

			info := cap
			if e.kamera != "" {
				info += "<p class=\"meta\">" + albumEsc(e.kamera) + "</p>"
			}
			mapHTML := ""
			if albumShowMap(e) {
				mapHTML = "<iframe class=\"detail-map\" loading=\"lazy\" src=\"" + albumMapSrc(e.lat, e.lon) + "\"></iframe>"
			}
			details = append(details, fmt.Sprintf(
				"<div class=\"detail\" id=\"d%d\">\n  <a class=\"detail-close\" href=\"#t%d\">×</a>\n  <div class=\"detail-media\"><a href=\"%s\"><img src=\"%s\" loading=\"lazy\" alt=\"%s\"></a></div>\n  <div class=\"detail-side\">\n    <div class=\"detail-info\">%s</div>\n    %s\n  </div>\n</div>\n",
				i, i, albumEsc(e.media), albumEsc(e.media), albumEsc(e.title), info, mapHTML))
		} else {
			href := e.media
			if href == "" {
				href = albumSafeHref(e.url)
			}
			label := e.title
			if label == "" {
				label = e.filename
				if label == "" {
					label = href
				}
			}
			link := albumEsc(label)
			if href != "" {
				link = fmt.Sprintf("<a href=\"%s\">%s</a>", albumEsc(href), albumEsc(label))
			}
			body = append(body, fmt.Sprintf(
				"<div class=\"card\">\n  %s\n  %s\n</div>\n", link, cap))
		}
	}

	return "<!DOCTYPE html>\n<html lang=\"de\">\n<head>\n<meta charset=\"utf-8\">\n" +
		"<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n" +
		"<title>" + albumEsc(heading) + "</title>\n<style>" + css + "</style>\n</head>\n" +
		"<body class=\"album-page\">\n<div class=\"album\">\n<h1>" + albumEsc(heading) + "</h1>\n" +
		"<div class=\"grid\">\n" + strings.Join(body, "\n") + "\n</div>\n" +
		strings.Join(details, "\n") + "\n</div>\n</body>\n</html>\n"
}

// albumCSSResolve: --css FILE > ~/.config/fileregister/album.css > built-in.
func albumCSSResolve(explicit string) (string, bool) {
	if explicit != "" {
		data, err := os.ReadFile(explicit)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: stylesheet not found: %s\n", explicit)
			return "", false
		}
		return string(data), true
	}
	home, _ := os.UserHomeDir()
	userCSS := filepath.Join(home, ".config", "fileregister", "album.css")
	if data, err := os.ReadFile(userCSS); err == nil {
		return string(data), true
	}
	return albumCSS, true
}

const albumUsage = "Usage: register album <binder> [--out DIR] [--css FILE] [--milan DIR] [--title TEXT] [--open]"

func cmdAlbum(args []string) int {
	title := ""
	titleSet := false
	out := ""
	css := ""
	open := false
	milanMode := false
	milanDir := ""
	binder := ""
	i := 0
	for i < len(args) {
		a := args[i]
		takeVal := func() (string, bool) {
			if eq := strings.Index(a, "="); strings.HasPrefix(a, "--") && eq >= 0 {
				return a[eq+1:], true
			}
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				i++
				return args[i], true
			}
			return "", false
		}
		var ok bool
		switch {
		case a == "--out" || strings.HasPrefix(a, "--out="):
			if out, ok = takeVal(); !ok {
				return unknownOption("album", "--out"+missingValue)
			}
		case a == "--css" || strings.HasPrefix(a, "--css="):
			if css, ok = takeVal(); !ok {
				return unknownOption("album", "--css"+missingValue)
			}
		case a == "--milan" || strings.HasPrefix(a, "--milan="):
			milanMode = true
			if strings.HasPrefix(a, "--milan=") {
				milanDir = strings.TrimPrefix(a, "--milan=")
			} else if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") && binder != "" {
				i++
				milanDir = args[i]
			}
		case a == "--title" || strings.HasPrefix(a, "--title="):
			titleSet = true
			if strings.HasPrefix(a, "--title=") {
				title = strings.TrimPrefix(a, "--title=")
			} else if i+1 < len(args) {
				i++
				title = args[i]
			} else {
				return unknownOption("album", "--title"+missingValue)
			}
		case a == "--open":
			open = true
		case a == "-h" || a == "--help":
			fmt.Println(albumUsage)
			return 0
		case a == "-v" || a == "--version":
			fmt.Println("register album " + registerVersion)
			return 0
		default:
			if strings.HasPrefix(a, "-") {
				return unknownOption("album", a)
			}
			if binder == "" {
				binder = a
			}
		}
		i++
	}

	if binder == "" {
		fmt.Fprintln(os.Stderr, albumUsage)
		return 1
	}

	nd, err := notesDir()
	if err != nil {
		return 1
	}
	// An empty --title takes the name away again rather than writing album: '',
	// which would leave a field that names nothing.
	clearTitle := titleSet && strings.TrimSpace(title) == ""
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
		fmt.Printf("Album field %s: %q — every member of '%s' inherits it\n", what, title, binder)
	}
	allRefs, refsOK := loadRefs(nd)
	if !refsOK {
		return 1
	}
	records := recordsForBinder(allRefs, binder)
	if len(records) == 0 {
		fmt.Fprintf(os.Stderr, "No records for binder '%s'.\n", binder)
		return 1
	}

	// The Markdown layer is grubber's, not ours: curation and the inherited
	// `album` field arrive in one query instead of a parse of our own.
	annByID, gerr := grubberRecordsFor(nd, binder)
	if gerr != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", gerr)
		return 1
	}

	resolved, rerr := resolveRecordPaths(records)
	if rerr != nil {
		fmt.Fprintf(os.Stderr, "Error: fileanchor batch resolve failed: %v\n", rerr)
		return 1
	}

	var outDir, mediaDir, thumbDir, mediaPfx, thumbPfx, htmlPath, slug string
	if milanMode {
		outDir = milanDir
		if outDir == "" {
			outDir = filepath.Join(nd, "collections", "albums", "milan")
		}
		outDir = index.ExpandPath(outDir)
		slug = albumAssetSafe(marshalSanitize(binder))
		mediaDir = filepath.Join(outDir, "images")
		thumbDir = mediaDir
		mediaPfx, thumbPfx = "images/", "images/"
		htmlPath = filepath.Join(outDir, slug+".html")
	} else {
		outDir = out
		if outDir == "" {
			outDir = filepath.Join(nd, "collections", "albums", marshalSanitize(binder))
		}
		outDir = index.ExpandPath(outDir)
		mediaDir = filepath.Join(outDir, "media")
		thumbDir = filepath.Join(outDir, "thumbs")
		mediaPfx, thumbPfx = "media/", "thumbs/"
		htmlPath = filepath.Join(outDir, "index.html")
	}
	os.MkdirAll(mediaDir, 0755)
	os.MkdirAll(thumbDir, 0755)

	type pendingEntry struct {
		rec  map[string]any
		path string
	}
	var pending []pendingEntry
	missing := 0
	for _, rec := range records {
		path := ""
		if !index.URLRef(rec) {
			path = resolved[index.AsString(rec["id"])]
			if path == "" || !index.FileExists(path) {
				fmt.Fprintf(os.Stderr, "  Warning: file for %s not found — skipped\n", refLabel(rec))
				missing++
				continue
			}
		}
		pending = append(pending, pendingEntry{rec: rec, path: path})
	}

	// Spotlight metadata on a worker pool — one mdls fork per image is the
	// dominant fixed cost of an album build when run serially.
	var metaPaths []string
	for _, p := range pending {
		if p.path != "" {
			metaPaths = append(metaPaths, p.path)
		}
	}
	metaByPath := albumSpotlightMetaAll(metaPaths)

	var entries []albumEntry
	var memberOrder []string
	for _, p := range pending {
		id := index.AsString(p.rec["id"])
		memberOrder = append(memberOrder, id)
		entries = append(entries, albumEntryOf(p.rec, annByID[id], p.path, metaByPath[p.path]))
	}

	sort.SliceStable(entries, func(a, b int) bool {
		ea, eb := entries[a], entries[b]
		ka, kb := albumSortKey(ea), albumSortKey(eb)
		for n := 0; n < 3; n++ {
			if ka[n] != kb[n] {
				return ka[n] < kb[n]
			}
		}
		return false
	})

	// In milan mode media and thumbs share one images/ dir — one namespace, so
	// both allocations dodge both. (A separate thumb set would let a second
	// IMG_1.heic's thumb silently overwrite IMG_1.jpg's.)
	takenMedia := map[string]bool{}
	takenThumb := map[string]bool{}
	if milanMode {
		takenThumb = takenMedia
	}
	type thumbJob struct {
		idx       int
		src, dest string
	}
	var thumbJobs []thumbJob
	copyFailed := 0
	for idx := range entries {
		e := &entries[idx]
		if e.path == "" {
			continue
		}
		base := filepath.Base(e.path)
		if slug != "" {
			// All milan albums share one flat images/ folder. slug-file alone
			// is ambiguous — album x with y-z.jpg and album x-y with z.jpg both
			// made x-y-z.jpg, and one album showed the other's photo — so a
			// short hash of the binder name keeps each album's files apart.
			base = albumAssetSafe(slug + "-" + albumTag(binder) + "-" + base)
		}
		name := marshalUniqueName(base, takenMedia)
		if err := copyFile(e.path, filepath.Join(mediaDir, name)); err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: copying %s failed: %v\n", filepath.Base(e.path), err)
			copyFailed++
			continue
		}
		e.media = mediaPfx + albumURLName(name, milanMode)

		if !e.image {
			continue
		}
		tbase := strings.TrimSuffix(name, filepath.Ext(name)) + ".jpg"
		if milanMode {
			tbase = "thumb-" + tbase
		}
		tname := marshalUniqueName(tbase, takenThumb)
		e.thumb = thumbPfx + albumURLName(tname, milanMode) // provisional — cleared if the resize fails
		thumbJobs = append(thumbJobs, thumbJob{idx: idx, src: e.path, dest: filepath.Join(thumbDir, tname)})
	}

	// The sips resizes are independent CPU-bound work — a pool instead of one
	// serial run per image cuts the build to a fraction on large albums.
	thumbed := 0
	if len(thumbJobs) > 0 {
		okFlags := make([]bool, len(thumbJobs))
		var wg sync.WaitGroup
		sem := make(chan struct{}, runtime.NumCPU())
		for j, job := range thumbJobs {
			wg.Add(1)
			sem <- struct{}{}
			go func(j int, job thumbJob) {
				defer wg.Done()
				okFlags[j] = albumThumb(job.src, job.dest)
				<-sem
			}(j, job)
		}
		wg.Wait()
		for j, job := range thumbJobs {
			if okFlags[j] {
				thumbed++
			} else {
				entries[job.idx].thumb = ""
			}
		}
	}

	cssText, ok := albumCSSResolve(css)
	if !ok {
		return 1
	}
	heading, headingNote := albumFieldOf(annByID, memberOrder)
	if clearTitle {
		// Say what the heading is now, not what was attempted: a note put
		// together by hand can still name the album after ours stopped.
		switch {
		case heading != "":
			fmt.Fprintf(os.Stderr, "note: the album is still named %q by the frontmatter of %s\n", heading, headingNote)
		case cleared == "removed":
			fmt.Println("Album field removed — the heading is the binder name again")
		default:
			fmt.Println("No album field to remove — the heading is the binder name")
		}
	}
	if err := index.AtomicWrite(htmlPath, []byte(albumHTML(orDefault(heading, binder), entries, cssText))); err != nil {
		fmt.Fprintf(os.Stderr, "Error: writing %s failed: %v\n", htmlPath, err)
		return 1
	}

	fmt.Printf("Album '%s' → %s\n", binder, htmlPath)
	fmt.Printf("  entries  : %d\n", len(entries))
	fmt.Printf("  thumbs   : %d\n", thumbed)
	if missing > 0 {
		fmt.Printf("  missing  : %d\n", missing)
	}
	if copyFailed > 0 {
		fmt.Printf("  copy failed: %d\n", copyFailed)
	}
	if open {
		exec.Command("open", htmlPath).Run()
	}
	return 0
}

// albumSortKey mirrors the sort_by tuple: keyed entries (0, sort, id) first,
// then unkeyed (1, filename, id).
func albumSortKey(e albumEntry) [3]string {
	if e.hasSort {
		return [3]string{"0", e.sortKey, index.AsString(e.rec["id"])}
	}
	return [3]string{"1", e.filename, index.AsString(e.rec["id"])}
}

// albumSafeHref returns u for use as a link, or "" when its scheme runs code in
// the page (javascript:, vbscript:, data:). A URL ref can come from a container
// someone else made, and the album is also served through the local web view.
// The scheme is read as a browser reads it: leading spaces and any tab or
// newline inside are ignored ("java\tscript:" runs too).
func albumSafeHref(u string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, strings.TrimLeft(u, " \x00\x01\x02\x03\x04\x05\x06\x07\x08\x0b\x0c\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f"))
	scheme, _, found := strings.Cut(cleaned, ":")
	if !found {
		return u
	}
	switch strings.ToLower(scheme) {
	case "javascript", "vbscript", "data":
		return ""
	}
	return u
}

// albumTag is a short, stable hash of a binder name for asset file names.
func albumTag(binder string) string {
	h := fnv.New32a()
	h.Write([]byte(binder))
	return fmt.Sprintf("%08x", h.Sum32())
}

// albumURLName is a file name as it goes into src/href. A standalone album
// keeps the original names, and a #, ? or % in one broke its link; milan
// asset names are already reduced to safe characters, and the milan view
// looks them up as written.
func albumURLName(name string, milanMode bool) string {
	if milanMode {
		return name
	}
	return url.PathEscape(name)
}
