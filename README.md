# fileregister

A tagging and reference layer for your files in plain text, with permanent ids
and self-documenting binders that outlast renames and moves on your machine, and
carry across to another.

[![test](https://github.com/rhsev/fileregister/actions/workflows/test.yml/badge.svg)](https://github.com/rhsev/fileregister/actions/workflows/test.yml)

---

## What it does

`register` writes a record for every file you hand it: a permanent id, the
binders it belongs to, and any fields you add. The records live in a JSONL index
next to your notes: readable with `cat`, searchable with grep, kept in git.
The index is the source of truth.

Each record can carry a Markdown sidecar: a note per file that keeps the hard
facts as YAML fields (an amount, a payment date, a supplier, a caption), with
free prose alongside for everything that isn't a field. The fields are what
grubber and matterbase query; the prose is for you. That is what turns a plain
set of files into a self-documenting binder: a workbook for a piece of work, a
photo album, a collection that explains itself.

Everything written onto the file itself (the id in its xattr, a macOS bookmark,
Finder tags, Spotlight fields) is a cache derived from the index. It keeps a
reference working as the file is renamed, moved to another disk, or copied to a
second Mac, and `register refresh` rebuilds it from the index whenever it drifts
or a copy strips it.

`register` itself is portable Go; the OS-specific work lives behind the
[fileanchor](https://github.com/rhsev/fileanchor) engine, today built for macOS.
A Linux fileanchor is the only piece between here and running on Linux.

## A first session

```sh
export GRUBBER_NOTES=~/notes         # where the index and notes live

register add ~/scans/scan_0043.pdf --binder insurance --aka car-policy
register list insurance
register resolve car-policy           # prints the path, whatever it is called now
open "$(register resolve car-policy)"
```

You scanned the document under a throwaway name; `--aka car-policy` gives it a
handle you'll actually remember. Six months later the file is called
`policy-2026.pdf` and sits on a NAS. `register resolve car-policy` still prints
its path, and `register of /Volumes/nas/policy-2026.pdf` reads the id off the
file and reports which binders it is in.

## What you can do with it

**Collect the documents for a piece of work.** A binder is a set on the
record, not a folder: the contract, two scans and a spreadsheet stay where they
are across several different folders and still form one working set for the
session. Adding a file to a binder does not move it, and the same file can sit
in several binders at once. `register list` shows all binders with counts,
`register list <binder>` the members, `--paths` gives you absolute paths to
pipe somewhere.

**Attach data to a file.** `register promote` writes a Markdown note with
one YAML block per file, and you fill in whatever the binder is about: an
amount, a payment date, a supplier, a caption, prose. The same file can carry
different fields in a different binder. `register annotate` does the same edits
from a script or a GUI without you opening the note.

**Link to a file so the link keeps working.** Yes, like Hookmark, but in plain
text instead of a database. `--aka` gives a record a handle that has nothing to
do with the filename. `register resolve <handle>` turns it back into a path,
behind a `milan://` URL, in a Shortcut, or in a shell alias. The lookup goes
through the record, so the link survives renames and moves.

**Look a file up in reverse.** `register of <file>` answers "what is this, and
what is it part of?" from the id on the file, so renaming does not break it.

**Put a binder in an order.** The binder stays an unordered set; the order is
`sort:` keys in the note, edited with `register order move`. See
[ORDERING.md](ORDERING.md).

**Render a binder as a photo album.** `register album <binder> --open` produces
a static HTML folder with thumbnails, captions and a map per image, no
JavaScript. Captions are just fields in the note. With `--milan` the album is
published on the LAN. See [ALBUM.md](ALBUM.md).

**Rebuild a binder on another machine.** `register marshal` packs files, notes
and a manifest into a tar.gz; `register unmarshal` unpacks it on the other
side, mirrors the files to their origin paths and recreates the records: the
same binders, handles and working links, now on the second Mac. Mirroring to
paths outside `collections/` is opt-in and explicit (`--scatter`), so a
container from someone else can't write anywhere it likes. The container is OS-neutral, so
the trip may go through exFAT, rsync without `-E`, or a cloud folder that
eats xattrs.

**Keep the macOS side in sync.** `register refresh` pushes record state back
into tags, Spotlight fields and xattrs. `register audit` reports drift in both
directions, `register repair` re-binds files that moved, and `register cleanup`
walks you through anything that needs a human decision. None of them delete a
record on their own.

**Query the whole thing.** The index is JSONL and the notes are YAML in
Markdown, so [grubber](https://github.com/rhsev/grubber) can query both and
[matterbase](https://github.com/rhsev/matterbase) gives you a table view and a
query builder over them. See [COLLECTIONS.md](COLLECTIONS.md).

For the "macOS already has tags" question, see [RATIONALE.md](RATIONALE.md).
The short version: fileregister uses them and can rebuild them.

## Commands

```sh
register <subcommand> [args...]
```

| Subcommand | Purpose |
|---|---|
| `add` | Record files in the index (bookmark + xattr + record). With `--binder` the file joins that binder, without it you get a bookmark: a permanent id and an optional `aka`, no binder. `--md` also writes an annotation note |
| `promote` | Write the per-binder Markdown block for a binder's records; `--edit` opens the note in `$EDITOR` |
| `annotate` | Edit an existing block's fields or prose from the command line (`--set`, `--unset`, `--prose`) |
| `remove` | Take files out of a binder; a record whose binder set runs empty stays as a bookmark |
| `refresh` | Push record state into macOS metadata |
| `audit` | Read-only consistency report in both directions |
| `repair` | Re-bind a moved file's broken bookmark under its unchanged id, located via Spotlight |
| `rename` | Rename a binder across all records and xattrs; onto an existing name needs `--merge` |
| `cleanup` | Review drift between the layers (stale blocks, unindexed annotations, bookmarks) and decide per item |
| `write` | Read JSONL from stdin, write ref records to Markdown or JSONL |
| `list` | All binders with counts, or the files in one; `--inbox`/`--curated` filter by annotation status, `--paths` and `--json` for piping |
| `resolve` | Turn an id or `aka` handle into a path; `--record` prints the full record |
| `of` | Given a file, report its id, `aka` and binders |
| `marshal` | Pack a binder's files, notes and manifest into a portable tar.gz |
| `unmarshal` | Unpack a container, mirror files to their origin, recreate records; idempotent, conflicts parked. Origins outside `collections/` need `--scatter` |
| `reindex` | Rebuild the index from Markdown ref blocks; `--dry-run` to preview |
| `order` | Arrange a binder for presentation (`set`/`show`/`move`) |
| `album` | Render a binder as a static HTML album; `--milan` publishes it on the LAN |

### More examples

```sh
# A session binder, so you can omit --binder below
export REGISTER_BINDER=project-alpha

register add document.pdf --kind pdf
register add ~/scans/*.pdf --kind pdf        # one batch: one bookmark write, one append
register add document.pdf --aka alpha-brief  # no binder: a bookmark with a handle
register add document.pdf --md               # and write the annotation note

register promote                             # blocks for the whole binder
register promote --id 482910337              # or just one record
register annotate project-alpha alpha-brief --set amount=142.50 --set status=paid

register list --inbox                        # binders with records nobody annotated yet
register list project-alpha --paths

register order move project-alpha 482910337 --after 482910901
register album project-alpha --open

register marshal --binder project-alpha --out project-alpha.tar.gz
register unmarshal project-alpha.tar.gz      # on the other machine

register refresh --dry-run
register audit --binder project-alpha
register repair --interactive
register reindex --dry-run
```

## Installation

If you already have `fileanchor` on your `PATH`:

```sh
go install github.com/rhsev/fileregister/cmd/register@latest
```

Otherwise build both from a checkout:

```sh
make fileanchor        # the metadata engine (needs a Swift toolchain)
make install           # to ~/bin by default
make install PREFIX=/usr/local/bin
```

Requires Go, and macOS for the current fileanchor build. `register` is a single
self-contained Go binary with no runtime dependencies; every subcommand is
native. The CLI carries no macOS coupling of its own. All of that lives in the
fileanchor engine, so a Linux port is a fileanchor port, not a register one.

### Configuration

Point `register` at your notes directory:

```sh
export GRUBBER_NOTES=~/notes
```

Or reuse a grubber config set, which takes precedence and reads the directory
from the set's `path:` field in `~/.config/grubber/config.yaml`:

```sh
export GRUBBER_SET=contracts
```

Optional:

```sh
export REGISTER_BINDER=berlin-2024          # default binder for add/remove/promote
export GRUBBER_CONFIG=~/path/to/config.yaml # only to resolve GRUBBER_SET
export FILEANCHOR=/path/to/fileanchor       # see below
```

grubber itself is not required. `register` reads its own index directly. You
only need it to search the Markdown sidecars, where `--from-jsonl` pulls in the
index alongside them and `--explode binder --merge-on id,binder` projects the
one-record-per-file index into per-membership rows.

### The metadata engine

All macOS metadata work goes through
[fileanchor](https://github.com/rhsev/fileanchor), a native engine in its own
repo that puts move-resilient bookmarks, Finder tags, Spotlight queries and
xattrs behind one batch stdio protocol. It runs once per command and handles the
whole batch in-process, which matters because `add`, `audit`, `refresh`,
`repair` and `rename` touch every record's file; a helper spawned per file would
spend most of its time on process startup.

No binary ships in this repo. `make fileanchor` clones fileanchor at a pinned
tag and builds it with SwiftPM; `make install` puts it next to the CLI. Bump
`FILEANCHOR_VERSION` in the [Makefile](Makefile) for a newer engine. Building it
needs a Swift toolchain (Xcode or the Command Line Tools) and produces an arm64
binary for macOS 13 or later.

Resolution order, first match wins:

1. `$FILEANCHOR` — explicit path
2. `fileanchor` on your `PATH` — your own build, or a system-wide install
3. `<bindir>/libexec/fileanchor` — what `make install` puts alongside `register`

So your own build always wins: put it on `PATH` and the installed copy steps
aside.

## Two layers

The JSONL index is authoritative: every `register add` writes there. Markdown
notes are an optional annotation layer on top: `register promote` adds a block
linked by id, and the index entry stays where it is. A record with no
annotation is perfectly normal, which is what `register list --inbox` is for.

A record whose binder set is empty is a **bookmark**: a file tracked by identity
alone, in no binder. It is first-class and never auto-deleted, created either by
`register add` without `--binder` or left behind when `register remove` empties
the set. See SPEC §Bookmarks.

## Documentation

- [SPEC.md](SPEC.md) — on-disk format, data model, design decisions
- [COLLECTIONS.md](COLLECTIONS.md) — querying binders from the matterbase TUI
- [ORDERING.md](ORDERING.md) — `sort:` keys and `register order`
- [ALBUM.md](ALBUM.md) — static photo albums, and serving them on the LAN
- [RATIONALE.md](RATIONALE.md) — why this exists when macOS already has tags

A related tool, [tagback](https://github.com/rhsev/tagback), backs up Finder
tags and comments to Markdown. It shares the conventions and lives in its own
repo.

## License

[PolyForm Noncommercial 1.0.0](LICENSE) — source-available, not OSI open source.

Free to read, use, and build on for anything non-commercial.

---

*Built on [fileanchor](https://github.com/rhsev/fileanchor). Part of a family of
plain-text tools. The [profile page](https://github.com/rhsev) has the map.*
