# fileregister

A tagging and reference layer for your files in plain text, with permanent ids
and self-documenting binders that outlast renames and moves on your machine, and
carry across to another.

[![test](https://github.com/rhsev/fileregister/actions/workflows/test.yml/badge.svg)](https://github.com/rhsev/fileregister/actions/workflows/test.yml)

---

## What it does

Files end up spread across folders, disks and machines, and even their names
might change. What you know about them (which invoice this is, whether it was
paid, which project it belongs to) lives in your head, in a spreadsheet next to
the files, or in the database of one app. fileregister keeps that knowledge in plain text
and ties it to the file by a permanent id instead of its name or path.

`register` writes a record for every file you pass to it: a permanent id, the
binders it belongs to, and any fields you add. The records live in a JSONL index
in the root of your notes, readable with `cat`, searchable with grep, kept in git.
The index is the source of truth.

Each record can have a block in its binder's Markdown note. The block keeps the
data as YAML fields (an amount, a payment date, a supplier, a caption), and free
text below it holds everything that isn't a field. The fields are what grubber
and matterbase query; the text is for you. That is what turns a plain set of
files into a self-documenting binder, such as a workbook for a project or a
photo album.

Each file is anchored by a macOS bookmark, kept in its own store
(`~/.local/share/bookmarks.json`). The bookmark finds the file again after a
rename or a move to another disk. When it breaks, `register repair` binds it anew
under the same id, and on a second Mac `register unmarshal` does the same. What
is written onto the file itself (xattrs for the id, the binder names and Finder
tags) is a cache derived from the index, and `register refresh` rebuilds it
whenever it drifts or a copy strips it.

Most of the OS-specific work lives behind the
[fileanchor](https://github.com/rhsev/fileanchor) engine, today built for macOS.
A Linux port needs a Linux fileanchor, plus replacements for a few direct calls
in register (`mdls`, `mdutil` and bsdtar's `--no-mac-metadata`).

## A first session

```sh
export GRUBBER_NOTES=~/notes         # where the index and notes live

register add ~/scans/scan_0043.pdf --binder insurance --aka car-policy
register list insurance
register resolve car-policy           # prints the path, whatever it is called now
open "$(register resolve car-policy)"
```

You scanned the document and saved it under whatever name came to mind;
`--aka car-policy` gives it a handle you will remember. Six months later the
file is renamed and moved to a different place. `register resolve car-policy`
still prints its path, and `register of /Volumes/nas/policy-2026.pdf` reads the id off the
file and reports which binders it is in.

## What you can do with it

**Collect the documents for a project.** A binder is a set on the record, not
a folder. The contract, two scans and a spreadsheet stay where they are, across
several folders, and still form one working set. Adding a file to a binder
does not move it, and the same file can sit in several binders at once. Finder
tags can group files too, but a tag is only a name. `register list` shows all
binders with counts, `register list <binder>` the members, `--paths` gives you
absolute paths to pipe somewhere.

**Attach data to a file.** `register promote` writes a Markdown note with
one YAML block per file, and you fill in whatever the binder is about, such as
an amount, a payment date, a supplier or a caption, with free text below the
block. The same file can carry different fields in a different binder.
`register annotate` does the same edits from a script or a GUI without opening
the note. Without fileregister, this data usually ends up in a spreadsheet that
does not notice when a file moves.

**Link to a file so the link keeps working.** Yes, like Hookmark, but in plain
text instead of a database. `--aka` gives a record a handle that has nothing to
do with the filename. `register resolve <handle>` turns it back into a path,
so the handle works behind a `ref://` URL (with mi.lan and ticker installed), in
a Shortcut or in a shell alias. The lookup goes through the record, so the link
survives renames and moves. Handles can be added or dropped later with
`register aka <key> --add/--remove`.

**Look a file up in reverse.** `register of <file>` answers "what is this, and
what is it part of?" from the id on the file, so renaming does not break it.

**Put a binder in an order.** Members follow the order of their blocks in the
note. `sort:` keys override it, and `register order move` writes them. See
[ORDERING.md](ORDERING.md).

**A binder as an album, as data.** `register album <binder>` prints the members
in order, one JSON line each: the `list --json` line plus its position, the
fields from the note and what the file itself carries (date, place,
coordinates, size). A gallery, a report or a web page is built from that by
whatever reads it; fileregister ships no renderer. See [ALBUM.md](ALBUM.md).

**Rebuild a binder on another machine.** `register marshal` packs files, notes
and a manifest into a tar.gz; `register unmarshal` unpacks it on the other
side, mirrors the files to their origin paths and recreates the records, with the
same binders, handles and working links, now on the second Mac. Mirroring to
paths outside `collections/` is opt-in and explicit (`--scatter`), so a
container from someone else can't write anywhere it likes. The container is
OS-neutral, so the trip may go through exFAT, rsync without `-E`, or a cloud
folder that eats xattrs.

**Keep macOS Spotlight up to date.** `register refresh` rewrites the xattrs from
the index, and Spotlight indexes them from there. `register audit` reports drift
in both directions, `register repair` re-binds the bookmark of a file that moved,
and `register cleanup` walks you through anything that needs a decision. None of
them delete a record on their own.
[WHO-WRITES-WHAT.md](WHO-WRITES-WHAT.md) shows, per command, what it writes to
the index, the bookmark store, the file and the notes.

**Query everything.** The index is JSONL and the notes are YAML in
Markdown, so [grubber](https://github.com/rhsev/grubber) can query both and
[matterbase](https://github.com/rhsev/matterbase) gives you a table view and a
query builder over them. See [WORKFLOWS.md](WORKFLOWS.md).

If you work on one Mac, group files only by topic and attach no data to them,
Finder tags are enough.

## Commands

```sh
register <subcommand> [args...]
```

| Subcommand | Purpose |
|---|---|
| `add` | Record files in the index (bookmark + xattr + record). With `--binder` the file joins that binder, without it the record is in no binder, with a permanent id and an optional `aka`. `--md` also writes an annotation note |
| `promote` | Write the per-binder Markdown block for a binder's records; `--edit` opens the note in `$EDITOR` |
| `annotate` | Edit an existing block's fields or note from the command line (`--set`, `--unset`, `--prose`) |
| `remove` | Take files out of a binder; a record whose binder set runs empty stays, in no binder |
| `forget` | Delete a record in no binder for good: index line, bookmark entry, its id in the notes and on the file; the notes' blocks stay, and `promote` reattaches one if the file returns |
| `refresh` | Rebuild the attributes on each file from the index, and renew stale bookmarks |
| `audit` | Read-only consistency report in three directions: record → file, file → record, and bookmark → record; exits 1 while something needs you |
| `repair` | Re-bind a moved file's broken bookmark under its unchanged id, located via Spotlight |
| `rename` | Rename a binder across all records and xattrs; onto an existing name needs `--merge` |
| `cleanup` | Review drift between the layers (stale blocks, unindexed annotations, records in no binder, unrepairable bookmark entries) and decide per item; `--prune` drops the unrepairable ones without asking, and refuses while an entry cannot be judged (its volume is away) |
| `write` | Read JSONL from stdin, write ref records to Markdown or JSONL |
| `list` | All binders with counts, or the files in one; `--inbox`/`--curated` filter by annotation status, `--paths` (with `--print0` for NUL-separated) and `--json` for piping; without a binder, `--json` lists the binders (`name`, `count`) |
| `resolve` | Turn an id or `aka` handle into a path; `--record` prints the full record |
| `of` | Given a file, report its id, `aka` and binders |
| `aka` | Add or remove a record's `aka` handles later (`--add`, `--remove`) |
| `marshal` | Pack a binder's files, notes and manifest into a portable tar.gz |
| `unmarshal` | Unpack a container, mirror files to their origin, recreate records; idempotent, conflicts parked. Origins outside `collections/` need `--scatter` |
| `reindex` | Rebuild the index from Markdown ref blocks; `--dry-run` to preview |
| `order` | Arrange a binder for presentation (`show`/`move`) |
| `album` | The binder in order as JSON lines: each member's `list --json` line plus `position`, `fields` (its block, read through grubber) and `file` (Spotlight); `--title` names the album in the note's frontmatter |

### More examples

```sh
# A session binder, so you can omit --binder below
export REGISTER_BINDER=project-alpha

register add document.pdf --kind pdf
register add ~/scans/*.pdf --kind pdf        # one batch: one bookmark write, one append
REGISTER_BINDER= register add document.pdf --aka alpha-brief  # no binder: a loose record with a handle
register aka alpha-brief --add brief         # a second handle, later
register add document.pdf --md               # and write the annotation note

register promote                             # blocks for the whole binder
register promote --id 482910337              # or just one record
register annotate project-alpha alpha-brief --set amount=142.50 --set status=paid

register list --inbox                        # binders with records nobody annotated yet
register list project-alpha --paths

register order move project-alpha 482910337 --after 482910901
register album project-alpha --title "Project Alpha, Q1" > alpha.jsonl

register marshal --binder project-alpha --out project-alpha.tar.gz
register unmarshal project-alpha.tar.gz      # on the other machine

register refresh --dry-run
register audit --binder project-alpha
register repair --interactive
register cleanup                             # report drift, change nothing
register cleanup --prune --dry-run           # what it would drop from the bookmark store
register reindex --dry-run
```

## Installation

**Apple Silicon: download the prebuilt bundle** from the [latest release](https://github.com/rhsev/fileregister/releases/latest). `fileregister-macos-arm64.tar.gz` unpacks to a folder with `register`, the `fileanchor` engine and `grubber`, in the layout register expects. Fetched with `curl` it runs as-is; after a browser download, clear the quarantine flag once in that folder: `xattr -dr com.apple.quarantine register libexec`.

Or build it yourself. If you already have `fileanchor` on your `PATH`:

```sh
go install github.com/rhsev/fileregister/cmd/register@latest
```

Otherwise build both from a checkout:

```sh
make fileanchor        # the metadata engine (needs a Swift toolchain)
make grubber           # the grubber release binary, for the notes layer
make install           # to ~/bin by default
make install PREFIX=/usr/local/bin
```

Requires Go, and macOS for the current fileanchor build. `register` is a single
Go binary, and every subcommand is native.

### Companion releases

`register` spawns the **fileanchor** engine ([rhsev/fileanchor](https://github.com/rhsev/fileanchor)) for every metadata operation, so a working fileanchor is required at runtime. The **grubber** query tool ([rhsev/grubber](https://github.com/rhsev/grubber)) reads the Markdown layer. Without it, register manages only the index. The core commands (`add`, `list`, `resolve`, `audit`, `repair`, `refresh`, `remove`) run without it. The commands that read notes need it: `album`, `order`, `annotate`, `aka`, `rename`, `forget`, `marshal`, `reindex`, `cleanup` and `list --inbox`/`--curated`, because register has no Markdown reader of its own. This release is tested against **fileanchor 1.2.0** and **grubber v0.19.0**. `make fileanchor` builds the engine at its tag, `make grubber` downloads the grubber release binary into `.build/`, and `make install` puts both next to register.

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
export GRUBBER_BIN=/path/to/grubber         # else PATH, then libexec/ next to register
```

Most commands that read notes need grubber (see Companion releases). You also
use it directly to search the Markdown sidecars, where `--from-jsonl` pulls in the
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
needs a Swift toolchain (Xcode or the Command Line Tools) and produces a
binary for macOS 13 or later.

Resolution order, first match wins:

1. `$FILEANCHOR`, an explicit path
2. `fileanchor` on your `PATH`, your own build or a system-wide install
3. `<bindir>/libexec/fileanchor`, which `make install` puts alongside `register`

A build of your own on `PATH` therefore takes precedence over the installed copy.

## Two layers

The JSONL index is authoritative, and every `register add` writes there.
Markdown notes are an optional annotation layer on top. `register promote` adds
a block linked by id, and the index entry stays where it is. A record without
annotation is normal; `register list --inbox` shows the binders that have such
records.

A record whose binder set is empty is **in no binder**, tracked by identity
alone (a loose record, for short). It is never deleted automatically. It comes
from `register add` without `--binder`, or stays behind when `register remove`
empties the set. See SPEC §Records in no binder.

## Documentation

- [SPEC.md](SPEC.md): on-disk format, data model, design decisions
- [WHO-WRITES-WHAT.md](WHO-WRITES-WHAT.md): what each command writes to the index, the bookmark store, the file and the notes
- [WORKFLOWS.md](WORKFLOWS.md): organizing and querying collections
- [ORDERING.md](ORDERING.md): `sort:` keys and `register order`
- [ALBUM.md](ALBUM.md): a binder in order, as data

## License

[PolyForm Noncommercial 1.0.0](LICENSE). Source-available, not OSI open source.

Free to read, use, and build on for anything non-commercial.

---

*Built on [fileanchor](https://github.com/rhsev/fileanchor). Part of a family of
plain-text tools. The [profile page](https://github.com/rhsev) provides an overview.*
