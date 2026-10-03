package main

// cmd_album — render a binder as a static, self-contained HTML album. Curated data lives in the annotation YAML (sort/title/
// comment/place/lat/lon/map); IPTC/EXIF via Spotlight fills the gaps.

import (
	"github.com/rhsev/fileregister/internal/index"

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

func albumHTML(binder string, entries []albumEntry, css string) string {
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
				href = e.url
			}
			label := e.title
			if label == "" {
				label = e.filename
				if label == "" {
					label = href
				}
			}
			body = append(body, fmt.Sprintf(
				"<div class=\"card\">\n  <a href=\"%s\">%s</a>\n  %s\n</div>\n",
				albumEsc(href), albumEsc(label), cap))
		}
	}

	return "<!DOCTYPE html>\n<html lang=\"de\">\n<head>\n<meta charset=\"utf-8\">\n" +
		"<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n" +
		"<title>" + albumEsc(binder) + "</title>\n<style>" + css + "</style>\n</head>\n" +
		"<body class=\"album-page\">\n<div class=\"album\">\n<h1>" + albumEsc(binder) + "</h1>\n" +
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

func cmdAlbum(args []string) int {
	out := ""
	css := ""
	open := false
	milanMode := false
	milanDir := ""
	binder := ""
	i := 0
	for i < len(args) {
		a := args[i]
		takeVal := func() string {
			if eq := strings.Index(a, "="); strings.HasPrefix(a, "--") && eq >= 0 {
				return a[eq+1:]
			}
			if i+1 < len(args) {
				i++
				return args[i]
			}
			return ""
		}
		switch {
		case a == "--out" || strings.HasPrefix(a, "--out="):
			out = takeVal()
		case a == "--css" || strings.HasPrefix(a, "--css="):
			css = takeVal()
		case a == "--milan" || strings.HasPrefix(a, "--milan="):
			milanMode = true
			if strings.HasPrefix(a, "--milan=") {
				milanDir = strings.TrimPrefix(a, "--milan=")
			} else if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") && binder != "" {
				i++
				milanDir = args[i]
			}
		case a == "--open":
			open = true
		case a == "-h" || a == "--help":
			fmt.Println("Usage: register album <binder> [--out DIR] [--open]")
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
		fmt.Fprintln(os.Stderr, "Usage: register album <binder> [--out DIR] [--open]")
		return 1
	}

	nd, err := notesDir()
	if err != nil {
		return 1
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

	annByID := map[string]map[string]any{}
	for _, a := range readAnnotations(nd) {
		if index.AsString(a["binder"]) != binder {
			continue
		}
		id := index.AsString(a["id"])
		if _, ok := annByID[id]; !ok {
			annByID[id] = a
		}
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
	for _, p := range pending {
		entries = append(entries, albumEntryOf(p.rec, annByID[index.AsString(p.rec["id"])], p.path, metaByPath[p.path]))
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
			base = albumAssetSafe(slug + "-" + base)
		}
		name := marshalUniqueName(base, takenMedia)
		if err := copyFile(e.path, filepath.Join(mediaDir, name)); err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: copying %s failed: %v\n", filepath.Base(e.path), err)
			copyFailed++
			continue
		}
		e.media = mediaPfx + name

		if !e.image {
			continue
		}
		tbase := strings.TrimSuffix(name, filepath.Ext(name)) + ".jpg"
		if milanMode {
			tbase = "thumb-" + tbase
		}
		tname := marshalUniqueName(tbase, takenThumb)
		e.thumb = thumbPfx + tname // provisional — cleared if the resize fails
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
	if err := index.AtomicWrite(htmlPath, []byte(albumHTML(binder, entries, cssText))); err != nil {
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

