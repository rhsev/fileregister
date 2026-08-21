# Using collections with matterbase

Collections are the practice of organizing files (PDFs, Pages, photos, mail exports, anything) into named **binders**. A file can belong to several binders at once. Membership is recorded as a canonical plain-text record in a central **JSONL index**; curation *adds* a lean Markdown annotation linked by id (the index entry stays put) — plus a macOS Spotlight metadata tag on the file (cache). The `register` CLI manages this membership; [matterbase](https://github.com/rhsev/matterbase) is one consumer that lets you *query* it.

> **Seeing index records in matterbase.** Every record lives in `<notes_dir>/collections/inbox.jsonl` — promoted or not. grubber only reads the index when explicitly told to, so the grubber call must add `--from-jsonl <notes_dir>/collections/ --explode binder --merge-on id,binder` to see the full directory (matterbase detects `collections/` and adds these flags automatically). Without them you get the **annotated-only view** (just the Markdown refs) — a legitimate toggle. See [Including inbox records](#including-inbox-records) below.

The data primitive is called `binder` (the YAML field, the CLI flag, the named bucket itself). The user-facing concept — the practice of organizing files this way — is called *collections*. This document uses both: prose discusses **collections** conceptually, technical examples use the literal field name **binder**.

This document is the matterbase-side workflow: how to query collections from the TUI, configure useful filters, and pipe results into other tools. The lifecycle operations (adding, removing, refreshing, repairing, renaming, cleanup) are subcommands of the `register` CLI in this repository — see [README.md](README.md).

## What matterbase does

matterbase is a query-construction TUI. It helps you assemble a grubber query, see the results in real time, and yank the resulting shell command for use in scripts or pipelines. Collections are exposed through the same filter mechanism as any other field — there is no dedicated collection UI, by design.

Two practical ways to query collections in matterbase:

1. **Filter buttons** — declarative, persistent, defined in `config.yml`
2. **Table-mode SQL** — interactive, ad-hoc, full DuckDB expressions

Both end up in the yanked shell command, so you can leave matterbase entirely and keep working.

## Filter buttons for binders

The simplest, most ergonomic way. Add entries to your `config.yml`:

```yaml
filters:
  - label: "Testdateien"
    query: ["binder=Testdateien"]
  - label: "Project Alpha"
    query: ["binder=project-alpha"]
  - label: "All refs"
    query: ["type=ref"]
```

The buttons appear in matterbase's top-left filter row. Click a button to narrow the visible records to the matching binder. With `multi_select: true` in config, clicking multiple buttons AND-combines them.

This works because binder-membership is just another field — grubber's `-f` operator matches it without any matterbase-specific code path.

## Including inbox records

`register add` captures new records to `<notes_dir>/collections/inbox.jsonl` by default; they only enter Markdown when you run `register promote`. grubber reads JSONL solely from explicit merge sources, so to see captured-but-unpromoted binders, the grubber invocation needs:

```sh
--from-jsonl <notes_dir>/collections/ --explode binder --merge-on id,binder
```

Pass the **directory** (not a single file) so every `*.jsonl` under `collections/` — the live inbox plus any archives or imports — is read and unioned with the Markdown scan. The index holds **one record per file** with `binder` as an array, so `--explode binder` first projects each index record into one row per membership; `--merge-on id,binder` then deduplicates the two layers: a promoted membership exists both as an exploded index row and as a per-binder Markdown block, and the merge collapses them into one — the annotation wins, index-only fields (`filename`, `kind`) are back-filled. matterbase configures all of this automatically when `<notes_dir>/collections/` exists; from then on filter buttons and SQL operate over the merged union transparently.

Two views, your choice:

- **Everything** (recommended default) — include `--from-jsonl`; captured and curated records appear together.
- **Curated only** — omit it; matterbase shows just the promoted Markdown refs. Useful when you want to browse the deliberately-kept subset and ignore the capture backlog.

There is no double-read: grubber's directory scan only parses Markdown, so the `.jsonl` files come in solely via `--from-jsonl` even though they live under `notes_dir`.

## Hierarchical binder names

If your binders follow a naming convention like `<year>:<location>` (e.g. `2022:Berlin`, `2022:Paris`, `2023:Berlin`), grubber's filter operators give you the dimensions:

```yaml
filters:
  - label: "2022"
    query: ["binder^2022:"]    # starts with — all 2022 trips
  - label: "Berlin"
    query: ["binder~Berlin"]   # contains — all Berlin trips
```

For exact OR-combinations across binders (e.g. "Berlin or Paris in 2022"), grubber alone can't express it; use table-mode SQL instead (next section).

## Table-mode SQL queries

Press `t` to open the metadata table. An SQL WHERE input field appears above the table. Type any DuckDB expression and Enter:

```sql
binder = 'Testdateien'
binder IN ('2022:Berlin', '2022:Paris')
binder LIKE '2022:%'
binder LIKE '%:Berlin'
type = 'ref' AND binder LIKE '2022:%'
type = 'ref' AND kind = 'pdf'
```

DuckDB runs over the JSON records grubber emits. Anything DuckDB understands works: `IN`, `LIKE`, `BETWEEN`, `AND`, `OR`, comparison operators, `IS NULL`, regex, full SQL.

To make a particular table view the default on startup, set `table_query` in config:

```yaml
table_query: "type = 'ref' AND binder LIKE '2022:%'"
```

For useful columns in the table view:

```yaml
table_columns: [binder, filename, kind, _note_file]
```

## Yank: continue in the shell

`y` copies the current grubber + DuckDB command to the clipboard. `Y` does the same and quits matterbase. The yanked pipeline is ready for any shell:

```sh
# from filter buttons (curated Markdown only):
grubber extract ~/notes -a -f binder=Testdateien

# include captured-but-unpromoted inbox records (merged with their annotations):
grubber extract ~/notes -a --from-jsonl ~/notes/collections/ --explode binder --merge-on id,binder -f binder=Testdateien

# from table-mode SQL:
grubber extract ~/notes -a --from-jsonl ~/notes/collections/ --explode binder --merge-on id,binder | duckdb -json -c "SELECT * FROM read_json_auto('/dev/stdin') WHERE binder = 'Testdateien'"
```

Note that `binder` is an ordinary SQL identifier — no quoting needed in raw DuckDB contexts. (This is one reason the field is named `binder` rather than `group`: `group` is a reserved word in standard SQL and would have to be quoted as `"group"` everywhere.)

Pipe into anything that takes JSON: `jq`, `xsv`, your own scripts. matterbase has done its job once you have the command.

## Inspecting a single ref record

In table mode, each row is one `type: ref` record. The right pane shows:

- the Markdown context around the YAML block (heading + prose)
- press `f` to switch to a preview of the referenced file (resolved via bookmarker)
- press `o` to open the referenced file in its default app
- `Enter` opens the referenced file

This lets you browse a binder's members visually, even though the referenced files (PDFs etc.) themselves aren't in `notes_dir`.

## What matterbase does NOT do

By design:

- **Adding files to collections** — use `register add` in the shell (matterbase's file list only shows Markdown notes, not the typical add-targets like PDFs/Pages)
- **Promoting captured records into a curated note** — use `register promote`
- **Removing files from collections** — use `register remove`
- **Renaming a binder** — use `register rename`
- **Reconciling xattr ↔ records** — use `register refresh`
- **Checking consistency** — use `register audit`
- **Repairing broken bookmarks** — use `register repair`
- **Reviewing layer drift / bookmarks** — use `register cleanup`

These are the collection-* tools in this repository. See [README.md](README.md). matterbase will see the results of any of these operations automatically the next time you `r`efresh (since matterbase re-reads from disk on every refresh — no cached state).

## Naming convention recommendations

A few habits that pay off later:

**Use lowercase-kebab-case for binder names.**

```
project-alpha       not    "Project Alpha"
q1-2025             not    "Q1 2025"
urlaub-2024         not    "Urlaub 2024"
```

Two reasons:

1. **macOS Spotlight comparisons are case-sensitive** (`mdfind 'kMDItemProjects == "..."'`). Mismatched case yields no results. grubber's filter operator is forgiving (`-f binder=foo` matches `Foo`), but Spotlight is strict — a consistent lowercase convention saves surprises.
2. **The opt-in `tags` backend surfaces binder names as Finder Tag chips**, where spaces and capitals read as noise. The default `kMDItemProjects` backend stores names as array elements, so spaces *do* round-trip there — but one convention across both backends is simpler than remembering which allows what.

**For multi-dimensional groupings, use a colon-separated hierarchy.**

```
2022:Berlin
2022:Paris
2023:Berlin
project-alpha:planning
project-alpha:contracts
```

Pattern queries become natural:

- `binder^2022:` — all 2022 entries
- `binder~Berlin` — anything with Berlin
- `binder^project-alpha:` — all parts of project-alpha

**Avoid pluralisation drift.**

`testdatei` vs. `testdateien` would be two different binders (grubber: case-insensitive matching, but otherwise byte-exact). Pick singular or plural and stick.

## A typical workflow

You add files to a collection occasionally — when starting a project, when returning from a trip, when archiving a year-end batch. Each `register add` captures cheaply into the inbox; when a binder is worth annotating you `register promote` it into a Markdown note. Day-to-day, you use matterbase to query: "what's in `project-alpha`?", "all PDFs in `2022:Berlin`?", "which refs belong to any active binder?" (matterbase includes the not-yet-promoted captures automatically when `collections/` exists). The results pipe into whatever processing you need.

This frequency profile is similar to photo albums in Apple Photos: rare creation, regular querying. matterbase is the album-list-and-browse half. The `register` tool in this repository is the put-things-in-albums half.

## See also

- [SPEC.md](SPEC.md) — full data model and architecture (`type: ref`, bookmarks, xattr layer, binder files, refresh semantics)
- [README.md](README.md) — the `register` CLI (the "how to put things in albums" side)
- [RATIONALE.md](RATIONALE.md) — why not just macOS Tags
- [matterbase](https://github.com/rhsev/matterbase) — the TUI itself (installation, configuration, general use)
