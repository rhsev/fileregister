# Photo albums with fileregister

An album is a rendered binder: membership lives in the index, the narrative
(title, comments, order, place) as YAML fields in the annotation note —
grubber-readable like everything else. `register album` turns that into a
static, self-contained HTML folder. No catalog, no daemon, no lock-in: editing
an album means editing Markdown.

## Quick start

```sh
# 1. Put images (and anything else) into a binder
register add ~/photos/namibia/*.jpg --binder safari
register add ~/documents/entry-ticket.pdf --binder safari

# 2. Create the annotation scaffold and open it
register promote --binder safari --edit

# 3. Fill in the album fields per block in the note (see below)

# 4. Render and look at it
register album safari --open
```

The result lands in `<notes>/collections/albums/safari/` (`--out DIR` picks
another location, e.g. a dylan path for the LAN): `index.html`, `media/`
(copies of the originals), `thumbs/` (JPEG via sips, HEIC included). Copy, zip,
or serve the folder — it is complete.

## The album fields

A curated block looks like this:

````markdown
### IMG_2041.jpg
```yaml
type: ref
id: '270450536'
binder: safari
sort: b
title: At the waterhole
comment: Early morning, before the heat came. The elephants were already there.
place: Etosha National Park, Namibia
```
Private note: exposure is tight — shot with the old Pentax.
````

| Field | Effect | Fallback when absent |
|---|---|---|
| `sort` | Order, pure string sort — use letters with gaps (`b < d < f`, a `c` or `bc` always fits between); numbers sort as strings (`'1' < '10' < '2'`). Ties are broken by id | no key sorts to the end, by filename |
| `title` | Image title | IPTC headline, then filename |
| `comment` | Caption in the album | IPTC description |
| `place` | Place text | IPTC city/country |
| `lat`, `lon` | Coordinates for the map | EXIF GPS in the image |
| `map` | `map: false` suppresses the map for this image | the map appears as soon as coordinates exist |

`sort` is the same field as in the ordering model — an album *is* an ordering of
the binder; `register order move` writes the keys instead of numbering them by
hand (see [ORDERING.md](ORDERING.md)).

The cascade is the same everywhere: **curation wins, what the image carries is
the fallback.** IPTC/EXIF is read (via Spotlight), never written — the original
stays untouched. `lat`/`lon` are worth it when EXIF was stripped, or when the
place should be preserved independently of the file.

**Prose below the block is private** (working notes) and never reaches the HTML.

A note on sorting: letters with gaps (`b`, `d`, `f`) — inserting between them is
then a `c` (or `bc`), never a renumbering. It is a pure string sort, so avoid
numbers (`'10' < '2'`). If you would rather not write keys by hand,
`register order move` computes them.

## Detail view

Clicking an image opens the overlay (pure CSS, no JavaScript): image on the
left, metadata top right (title, comment, place · date, camera), map bottom
right as an OpenStreetMap embed. The × closes and jumps back to the thumbnail;
the browser's back button works too.

The map is the album's **only internet dependency** — offline, it is the one
thing missing.

## Non-images

PDFs and other files in the binder appear as cards among the photos — the entry
ticket belongs in the album. `title`/`comment`/`sort` work just the same.

## Your own look

```sh
register album safari --css my-style.css            # per call
cp my-style.css ~/.config/fileregister/album.css    # for every album
```

Your stylesheet replaces the built-in one entirely. Starting points: the
`albumCSS` constant in `cmd/register/cmd_album.go`, or the bundled
`album-styles/link-board.css` (a compact, linkding-like list look for binders of
URL refs). The markup is the stable contract — everything lives inside an
`.album` wrapper, so your rules address `.album h1`, `.album .grid`,
`.album figure`/`figcaption`, `.meta`, `.card`, and `.detail` with
`.detail-media`, `.detail-info`, `.detail-map`, `.detail-close`. Page chrome
(background, margins) belongs on `body.album-page` — only the standalone
document carries that class, so nothing leaks into a host page when Stage embeds
the album. Add `@page` rules and "Save as PDF" turns it into a printable photo
book — without a line of code.

## milan & dylan: albums on the LAN

`--milan` renders into the milan notes layout: one shared source folder (default
`<notes>/collections/albums/milan/`), holding `<binder>.html` per album and all
assets flat under `images/`. Three configuration steps:

```yaml
# 1. mi.lan/config.yaml — register the folder as a notes source:
milan:
  notes:
    - id: alben
      path: /path/to/notes/collections/albums/milan

# 2. dy.lan/config/stage.yaml — a button on the Stage:
    - title: "Albums"
      buttons:
        - id: alben
          label: "📷 Photo albums"
          type: notes
          source: alben
```

```sh
# 3. Render (once per album, again after changes):
register album safari --milan
```

The Stage button then lists every album; a click renders it in the browser —
detail view and map included.

**Direct link without Stage:** `quickaction/milan-album.rb` (symlink it to
`mi.lan/scripts/custom/album.rb`) turns every album into a URL:

```
http://localhost:8080/album/safari          # show (renders on first call)
http://localhost:8080/album/safari/fresh    # re-render first, then show
http://mi.lan/<agent>/stream/album/safari/fresh   # re-render without the 5s timeout
```

The script serves the finished HTML with asset paths rewritten onto the notes
route — one link, and the album appears. (Needs milan's HTML sniffing for script
output, mi.lan June 2026 or later.)

## Sharing and searching

- **LAN:** point `--out` at a dylan path, done.
- **Handing it on:** zip the album folder — or `register marshal --binder safari`
  for the full container (originals + note + manifest).
- **Searching:** matterbase/grubber read the album fields like any ref block:
  `grubber extract ~/notes --blocks-only -f type=ref -f binder=safari` plus
  full text finds the image by its comment, not by `IMG_2041`.

## Limits, honestly

- IPTC/EXIF fallbacks need Spotlight: freshly copied files may not be indexed
  yet (wait a moment, or curate the fields).
- HEIC: thumbnails are always JPEG; the linked original may show nothing outside
  Safari.
- Very large albums stay smooth thanks to `content-visibility` — but a curated
  album with a four-digit image count is usually a sign that it is really
  several albums.
