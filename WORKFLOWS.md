# Workflows — collections and queries

A **collection** is the practice of organizing files (PDFs, Pages, photos, mail
exports, anything) into named **binders**. A file can belong to several binders
at once. Membership is a canonical plain-text record in the central **JSONL
index**; curation *adds* a lean Markdown annotation linked by id, and the file
carries a macOS Spotlight tag as a cache. The `register` CLI manages membership;
[grubber](https://github.com/rhsev/grubber) and
[matterbase](https://github.com/rhsev/matterbase) query it.

The data primitive is `binder` — the YAML field, the CLI flag, the bucket. The
practice of organizing files this way is a *collection*. Prose here uses
**collection** for the concept and **binder** for the literal field.

## The lifecycle is in `register`, the querying is in grubber/matterbase

`register add`, `promote`, `remove`, `rename`, `refresh`, `audit`, `repair` and
`cleanup` are the lifecycle — all in this repository (see [README.md](README.md)).
grubber and matterbase never modify a collection; they only read it. matterbase
re-reads from disk on every refresh, so it reflects the result of any `register`
command with no cached state.

## `binder` is an ordinary field

Membership is queried like any other field — no collection-specific code path:

```sh
grubber extract ~/notes -a -f binder=Testdateien
```

grubber's `-f` operator is case-insensitive (`binder=foo` matches `Foo`) but
otherwise byte-exact. macOS Spotlight, by contrast, is case-*sensitive*
(`mdfind 'kMDItemProjects == "…"'`), so a consistent lowercase convention avoids
surprises (see [Naming](#naming-conventions)).

## Seeing captured (unpromoted) records

`register add` records to `<notes_dir>/collections/inbox.jsonl`; a record enters
Markdown only when `register promote` annotates it. grubber reads JSONL solely
from explicit merge sources, so to include captured-but-unpromoted records the
query needs:

```sh
--from-jsonl <notes_dir>/collections/ --explode binder --merge-on id,binder
```

Passing the **directory** (not one file) reads every `*.jsonl` under
`collections/` — the live inbox plus any archives or imports — and unions it with
the Markdown scan. The index holds one record per file with `binder` as an array,
so `--explode binder` projects each record into one row per membership;
`--merge-on id,binder` then collapses the two layers: a promoted membership
exists both as an exploded index row and as a per-binder Markdown block, and the
merge folds them into one (the annotation wins, index-only fields like `filename`
and `kind` are back-filled).

Two views follow from this:

- **Everything** — with `--from-jsonl`: captured and curated records together.
- **Curated only** — without it: just the promoted Markdown refs, the
  deliberately-kept subset.

There is no double read: grubber's directory scan parses only Markdown, so the
`.jsonl` files enter solely via `--from-jsonl` even though they live under
`notes_dir`.

## Hierarchical binder names

A `<dimension>:<value>` convention (`2022:Berlin`, `2022:Paris`, `2023:Berlin`,
`project-alpha:planning`) makes each dimension queryable with grubber's operators:

```sh
grubber extract ~/notes -a -f binder^2022:      # starts-with — all 2022 entries
grubber extract ~/notes -a -f binder~Berlin     # contains — anything with Berlin
grubber extract ~/notes -a -f binder^project-alpha:
```

grubber's operators cover prefix and substring but not exact OR-combinations
(`"Berlin or Paris in 2022"`). Those go through SQL.

## SQL over the records

The records grubber emits are JSON, so DuckDB runs over them directly. The
yanked pipeline (below) or a hand-written one both reach the same place:

```sh
grubber extract ~/notes -a --from-jsonl ~/notes/collections/ --explode binder --merge-on id,binder \
  | duckdb -json -c "SELECT * FROM read_json_auto('/dev/stdin') WHERE binder = 'Testdateien'"
```

Anything DuckDB understands works — `IN`, `LIKE`, `BETWEEN`, `AND`/`OR`,
comparisons, `IS NULL`, regex:

```sql
binder = 'Testdateien'
binder IN ('2022:Berlin', '2022:Paris')
binder LIKE '2022:%'
binder LIKE '%:Berlin'
type = 'ref' AND kind = 'pdf'
```

`binder` is an ordinary SQL identifier needing no quoting — one reason the field
is named `binder` rather than `group`, which is a reserved word in standard SQL.

## Querying in matterbase

[matterbase](https://github.com/rhsev/matterbase) is a query-construction TUI
over grubber: assemble a query from presets and an SQL form, watch the records in
one table, and yank the underlying `grubber | duckdb` command for the shell.
Because a binder is a plain field, collections need no dedicated UI — they go
through the same preset and SQL mechanism as everything else, and matterbase adds
the `--from-jsonl … --merge-on id,binder` flags automatically when a
`collections/` directory is present.

Keys, preset config, and preview modes are matterbase's own and are documented in
its [README](https://github.com/rhsev/matterbase); they are deliberately not
duplicated here, so this document does not drift when its UI changes.

## Naming conventions

**Lowercase-kebab-case for binder names** (`project-alpha`, `q1-2025`,
`urlaub-2024`, not `"Project Alpha"` or `"Q1 2025"`). Two reasons: Spotlight
comparisons are case-sensitive, so mismatched case yields no results; and the
opt-in `tags` backend surfaces binder names as Finder Tag chips, where spaces and
capitals read as noise. (The default `kMDItemProjects` backend stores names as
array elements, so spaces do round-trip there — but one convention across both
backends is simpler.)

**A colon-separated hierarchy for multi-dimensional groupings** (`2022:Berlin`,
`project-alpha:contracts`), which the prefix/substring operators above turn into
natural dimension queries.

**No pluralisation drift.** `testdatei` and `testdateien` are two different
binders (grubber matches case-insensitively but otherwise byte-exact). Pick
singular or plural and keep it.

## Frequency profile

Collections resemble photo albums in Apple Photos: creation is rare (starting a
project, returning from a trip, archiving a year-end batch), querying is regular.
`register` is the put-things-in-albums half; grubber/matterbase are the
list-and-browse half. An album is itself an ordering — see [ALBUM.md](ALBUM.md)
and [ORDERING.md](ORDERING.md).

## See also

- [SPEC.md](SPEC.md) — data model and architecture (`type: ref`, bookmarks, xattr layer)
- [README.md](README.md) — the `register` CLI
- [RATIONALE.md](RATIONALE.md) — why this exists when macOS already has tags
- [matterbase](https://github.com/rhsev/matterbase) — the query TUI (keys, config, preview)
