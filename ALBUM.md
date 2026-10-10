# Albums

An album is a binder with an order: its members in sequence, with what the
note says about each of them and what each file says about itself. It is not
only for photos. A report, a reading list and the pages of a guide are albums
too. `register album` prints one as data, and nothing else. Whoever wants a
gallery, a printed report or a web page builds it from that data, with
whatever tool and look suits the purpose.

```sh
register album safari                  # the album, one JSON line per member
```

An album line is the member's `register list safari --json` line plus three
keys: `position`, `fields` and `file`. `list` stays fast because it reads only
the index; `album` is the one command that joins the index, the Markdown layer
(through grubber) and the files themselves (through Spotlight).

## Quick start

```sh
# 1. Put files into a binder
register add ~/photos/namibia/*.jpg --binder safari
register add ~/documents/entry-ticket.pdf --binder safari

# 2. Create the annotation note and say something about the members
register promote --binder safari
register aka 210322647 --add waterhole   # a handle for one photo (its id from `register list safari --json`)
register annotate safari waterhole --set title="At the waterhole" --set place="Etosha National Park, Namibia"

# 3. Put them in order, and name the album (the name stays in the note)
register order move safari waterhole --to 1
register album safari --title "Safari, Namibia 2026"
```

## The line

```json
{"id":"210322647","binder":["safari"],"filename":"waterhole.jpg","kind":"image","aka":["waterhole"],"path":"/Users/me/photos/namibia/waterhole.jpg","position":1,"fields":{"album":"Safari, Namibia 2026","title":"At the waterhole","place":"Etosha National Park, Namibia"},"file":{"date":"2026-10-03T07:12:00Z","lat":-18.85,"lon":16.32,"size":2481152,"modified":"2026-10-03T19:40:11Z","type":"public.jpeg","width":4000,"height":3000}}
```

| Key | From | Meaning |
|---|---|---|
| `id` … `path`/`url` | the index | exactly as in `list <binder> --json`: `id`, `binder` (the member's binders, a list), `filename`, `kind`, `aka`, then `path` for a file, `url` for a URL record, or `"broken": true` when the bookmark does not resolve |
| `position` | the ordering | 1-based, the order `register order show` resolves |
| `fields` | the note, via grubber | everything the member's block says, and everything the note's frontmatter hands down to it |
| `file` | the file | what the file carries: file system and Spotlight |

Keys without a value are left out rather than written as `""` or `null`. A
reader ignores keys it does not know; new keys may appear, and existing ones
keep their name and meaning.

A member whose bookmark does not resolve keeps its line and its position, with
`"broken": true` instead of a path, as in `list --json`, because dropping it
would be indistinguishable from "not a member". `register repair` fixes it.

## Naming the album

The name is a field in the note's frontmatter, and every member inherits it:

````markdown
---
album: Safari, Namibia 2026
---

### IMG_2041.jpg
```yaml
type: ref
id: '270450536'
binder: safari
title: At the waterhole
```
````

```sh
register album safari --title "Safari, Namibia 2026"   # writes or updates the field
register album safari --title ""                       # removes it again
```

Any name works: colons, quotes, emoji, words YAML would otherwise read as a
number or `yes`; the field is quoted where it has to be. `--title` stores it in
NFC, the composed Unicode form, so a name pasted from the Finder (which can
arrive decomposed and looks the same) is still found by a search typed on the
keyboard. A name is one line, so line breaks and tabs are refused.

No lookup is needed. grubber passes every frontmatter key down into each
block of the file, so it arrives in every line's `fields`, and so does any
other key the frontmatter carries (a period, an author, a client). A reader
takes the album's name from any line, and the binder name when there is none.
The same inheritance makes the album searchable:

```sh
grubber extract ~/notes -a -f album~Safari -f type=ref   # every member of that album
```

`type=ref` keeps the rest out. Without it, any other note with an `album` key
in its header matches too, and so does an ordering block written by earlier
versions.

The name belongs to the note, not to the binder. A note may hold blocks of
several binders (`promote --target` writes into any note), and then they all
carry the name its frontmatter gives. `--title` writes to the binder's own
note, `collections/binder_safari.md`, where `promote` puts its blocks by
default.

## Fields

`fields` is the member's block minus what the line already has (`type`, `id`,
`binder`, and `sort`, which `position` already reflects). The set is open: any key you
put in a block arrives as it stands, so a report can carry `amount` or `due`
and a reading list `status`. These names are a convention between you and
whatever reads the album; fileregister gives none of them a meaning:

| Field | Usual meaning |
|---|---|
| `title` | the member's title |
| `comment` | a caption or description |
| `place` | where it was taken or where it belongs |
| `lat`, `lon` | coordinates |
| `date` | when |
| `map` | `false` where a reader should not show a map |

`register annotate` sets them (`--set`, `--unset`). Prose below a block is
working notes; it does not reach the line.

## File

`file` is what the file says about itself, read, never written:

| Key | Source |
|---|---|
| `size`, `modified` | the file system |
| `type` | the content type (UTI), e.g. `public.jpeg`, `com.adobe.pdf` |
| `title`, `comment` | IPTC headline and description; a document's title |
| `place` | IPTC city and country |
| `date` | when the content was created (EXIF for photos) |
| `lat`, `lon` | EXIF GPS |
| `camera` | make and model |
| `width`, `height` | pixels, for images and video |
| `pages` | for PDFs |
| `duration` | seconds, for audio and video |

Where a key means what a field means, it has the field's name. So the value a
reader shows is `file` overlaid with `fields`. The curation wins and the file
is the fallback. One line of jq does it:

```sh
register album safari | jq -c '{position, shown: ((.file // {}) + (.fields // {}))}'
```

A reader that wants only what was curated reads `fields` and ignores `file`.

## Order

`position` is the order `register order show safari` prints, and that is the
note's order. Members follow their blocks in the order they stand in the
document.
Moving a block in the editor moves the member. A `sort` key, which `register
order move` writes, places a member ahead of the document order; members
without a block come last, by file name. See [ORDERING.md](ORDERING.md).

## Rendering

fileregister ships no renderer. The line carries the resolved `path` of each
file, so a renderer can copy, scale or link the files as it sees fit, and
`fields` and `file` give it everything to say about them.

Versions up to 1.4 rendered HTML themselves (`--out`, `--css`, `--milan`,
`--open`). Those flags are gone; `register album` says so when given one.

## Limits

- `file` needs Spotlight for everything but `size` and `modified`. A file on a
  volume Spotlight does not index, or one copied a moment ago, may carry less.
  The curated `fields` do not depend on it.
- Each file costs one `mdls` call; they run in parallel, but a four-digit album
  takes a moment. `list --json` is the fast way when only membership matters.
