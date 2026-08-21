# Collections — Specification

This document specifies how **collections** — the practice of organizing files into named binders that share a common context (e.g. a project, a research topic, an event) — are represented, managed, and queried in the grubber ecosystem.

It is the contract between the canonical data layer (the central JSONL index and its optional Markdown annotations) and the operating-system metadata layer (macOS bookmarks and Spotlight metadata). The `register` CLI in this repository implements the read/write/maintain side. [matterbase](https://github.com/rhsev/matterbase), as a query-construction TUI, is one consumer — it can browse and filter collections through standard grubber filters but has no built-in collection management UI. Collections live and breathe on the command line.

## Concept

A user's *collection* is the totality of file references the tool manages. Within it, *binders* are named buckets — a file belongs to a binder by carrying that binder's name in its YAML record. A single file can belong to multiple binders without being moved, copied, or modified in place. Membership lives in a central plain-text index — the *yellow pages* — that the tool reads directly; there is no opaque database and no daemon.

Throughout this document, **collection** refers to the user-level concept (organizing files into named buckets, the comparable feature to e.g. macOS Tags). **Binder** refers to the specific data primitive: the YAML field, the CLI flag, the named bucket itself.

Three layers carry the information, with different roles:

| Layer | Role | Authority |
|---|---|---|
| JSONL index — the central directory ("yellow pages") | Mandatory metadata: id, binder, filename, kind, bookmark | Source of truth for membership |
| Markdown YAML annotations | Optional custom metadata + prose, linked to the index by `id` | Source of truth for that custom data |
| macOS bookmark + Spotlight metadata (xattr) | Cache for Spotlight, robustness against path changes | Derived |

Every record lives in the **index** — one central JSONL store per notes directory (`<notes_dir>/collections/inbox.jsonl`). It is the authoritative directory of what belongs to which binder, and the core reads it directly (no scan-and-union step).

A record may *additionally* carry a **Markdown annotation**: a lean YAML block linked back to the index by `id`, under a heading, where the user keeps custom fields and prose. Annotations are optional and disposable — the index entry stands on its own. grubber-over-Markdowns is the optional rich-query layer (what matterbase browses), not the core's read path. See [Capture and promote](#capture-and-promote).

If a record and its metadata cache diverge, the record layer wins. Reconciliation always flows record → metadata, never the other way around (a manual recovery path exists but is not automatic).

The record layer survives any filesystem; the metadata layer is macOS-specific and may be lost on transfer. The index can be rebuilt from Markdown ref blocks via `register reindex`, but the canonical direction is fixed for predictability.

## Capture and promote

Records have a lifecycle: **capture cheaply, annotate deliberately.**

- **Capture.** `register add` materializes a record in the central index — one JSONL line. This is the authoritative entry, and it is always written: the index is the directory, never skipped.
- **Promote.** `register promote` adds a lean Markdown annotation for a record (or a whole binder): a heading naming the file, plus a YAML block linked to the index by `id`, where the user keeps custom fields and prose. The index entry **stays put**. Promotion is additive curation, not a move.

This keeps one chokepoint — `promote` — where cross-cutting features (validation, enrichment, field normalization) can attach uniformly later, while the index remains a complete, side-effect-free directory.

### The annotation is optional, the index is not

A record's presence is decided by one place only: the index. Whether it *also* has a Markdown annotation is a separate, optional fact (surfaced by `list --inbox` / `list --curated` on demand).

- **JSONL index** = the directory. Central, machine-managed, always present.
- **Markdown annotation** = optional custom metadata + prose, linked by `id`, placed anywhere.

This is why the index is **central**: it is the single authoritative directory the core reads. Annotations are a layer drawn on top, never a substitute for the index entry.

### Additive by design

There is **no `demote`**, and promote never deletes the index entry. Reverting an annotation means editing or deleting the Markdown by hand; the index entry is unaffected. The invariant the tooling guarantees is **the index is complete**: every record — annotated or not — has exactly one authoritative index entry. `register reindex` restores that invariant if a record exists only in Markdown (e.g. legacy data).

## Data Model

### The record (canonical)

A record describes **one file** and its complete identity: a permanent `id`, an
optional set of binder memberships, and a few plain-text anchors. Its canonical
form is one JSON line in the index:

```json
{"type":"ref","id":"abc123","binder":["project-alpha","lesen"],"filename":"brief.pdf","kind":"pdf"}
```

`binder` is a **set** (a JSON array) of the binders the file belongs to. The named
bucket itself is what we call a **collection** at the concept level. The two terms
split labor cleanly: the user has *one* collection (the body of files managed by
this tool), inside which named *binders* organize membership. Adding to a binder
is a set-insert, removing is a set-delete; the record itself stays put.

Fields:

| Field | Required | Meaning |
|---|---|---|
| `type` | yes | Always `ref` for file-reference records |
| `id` | yes | Bookmark identifier (random 9-digit). The file's **permanent identity** — assigned once, travels with the file, never changes (repair re-binds the blob under the same id). |
| `binder` | optional | **Set (JSON array)** of binder names this file belongs to. An empty or absent set is a [bookmark](#bookmarks-binderless-records) — a tracked file in no binder. Add/remove are set operations on this one field. |
| `filename` | recommended | Basename of the referenced file at add-time (e.g. `brief.pdf`). Plain-text anchor that survives bookmark breakage and xattr loss. Set by `register add`; not maintained when the underlying file is later renamed. |
| `kind` | optional | Coarse classification used for display and filtering (e.g. `pdf`, `image`, `mail`, `video`). Auto-detected from the file's extension on `register add` (jpg/png/heic/… → `image`, mp4/mov/… → `video`, eml/mbox/… → `mail`, md/markdown → `md`, typ → `typst`; unknown extensions fall back to the extension itself, no-extension to `file`). `--kind` overrides the auto-detect. For URL refs, derived from the scheme (`https` → `web`, `x-devonthink-item` → `devonthink`, `message` → `mail`; unknown schemes fall back to the scheme itself). |
| `url` | optional | Locator alternative to the bookmark: the record references a URL (e.g. `x-devonthink-item://…`) instead of a file. Mutually exclusive with the bookmark identity — see [URL refs](#url-refs). |
| `aka` | optional | Array of human-chosen handles that also resolve to this record (resolution matches `id` ∪ `aka`). For referencing a file by name — e.g. behind a `milan://` URL via `register resolve`. Must be unique across the index. |
| `tags` | optional | Free-form annotation labels local to this ref (e.g. `[draft, v2]`). Not synchronized to macOS Spotlight metadata. |
| `xattr` | optional | xattr backend for this **file**: `itemprojects` (default), `tags`, or `none`. One choice per record; determines which macOS metadata field caches the binder names — see [macOS metadata layer](#macos-metadata-layer-derived). |

`binder` is the only field synchronized with the macOS Spotlight metadata layer. The optional `tags:` array is reserved for record-level annotations — labels that distinguish files from each other (status, version, importance) without affecting Spotlight. Use it only when something more specific than `kind` needs to be expressed.

**One record per file.** A file in three binders is **one** index line with a three-element `binder` array. Duplicate `(id, binder)` registrations are structurally impossible: membership is set membership, so adding twice is a no-op and there is nothing to dedupe.

### The index file

The index is `<notes_dir>/collections/inbox.jsonl`, one record per line. Every record lives here; the core reads it directly (`read_index`). Field order is irrelevant (JSON objects are unordered), and the line does **not** store `_note_file` — it is injected at read time and set to the exact `.jsonl` file the record was read from, so records are self-locating after a read and the on-disk lines stay clean and portable.

The historical name `inbox.jsonl` is kept for compatibility; it is no longer an *inbox* in the staging sense (records are not consumed out of it), but the authoritative directory.

**Schema version.** `collections/SCHEMA` stamps the on-disk schema; the current version is **3**: `id` is always a JSON string, `binder` always a JSON array (empty array = bookmark). The write path emits only this form; readers still accept the legacy scalar `binder` but warn about it, since any occurrence is a straggler to fix by hand. The field-by-field wire contract follows.

### JSONL wire contract (schema 3) — normative

The section every consumer of `collections/*.jsonl` relies on. Producers and consumers today: **register** (read/write), **grubber** `--from-jsonl` (read), **matterbase** via grubber (read). Field *meanings* are above; this section fixes the *wire format*.

**File form.** UTF-8, one JSON object per line, LF-terminated. Lines up to **4 MB** are guaranteed readable (register's scanner limit; grubber accepts up to 16 MB — stay within 4 MB). Blank lines are ignored; unparseable lines are preserved verbatim by register's rewriters and skipped (with a warning) by grubber.

**Record form (`type: "ref"`):**

- `id` is always a JSON **string**, unique across the index — one record per id.
- `binder` is always a JSON **array of strings** (the membership set); an empty array is a bookmark. The legacy scalar form is still *read* but warned about — the write path never emits it; fix stragglers by hand.
- `aka` is an array of strings; `filename`, `kind`, `url`, `xattr` are strings.
- **Custom keys are the user's sphere**: any key not named above may appear and MUST be preserved verbatim by rewriters (register keeps unchanged lines byte-identical).
- Objects with other `type` values (or none) are foreign records: readers skip them, rewriters pass them through untouched.

**Injected keys.** Keys starting with `_` never appear on disk. They are injected at read time (`_note_file` = source file; grubber also injects `_mtime`) and MUST be stripped before writing back — register's serializers do this automatically.

**Write discipline.** Rewrites replace the file atomically (temp file in the same directory + rename); appending whole new lines is allowed for new ids. Writers serialize across processes via a blocking advisory lock — `collections/.register.lock` for the index files, `bookmarks.json.lock` beside the bookmark db. Readers take no lock; the atomic rename keeps their view consistent.

**Not this schema:** the `register write` stdin stream. That is a *fold stream* — one membership per line, `binder` as a scalar, `_note_file` naming the target — which register folds into schema-3 records.

**Version stamp.** `collections/SCHEMA` holds the version as a single line. `3` = the rules above (2026-07: id stringified, binder array-only). Consumers may treat a missing stamp as "unknown, read tolerantly".

### Markdown entry contract — normative

The entry unit: a heading + optional prose + one fenced ```yaml block. Producers and consumers today: **register** (promote/annotate/delete — write), **grubber** (read), **matterbase** (section extraction).

**Heading levels (the layering rule):**

- **H3 (`###`) is the entry heading.** The only level writers ever emit (register: `### <filename>`, ordering config `### · <binder> (ordering)`).
- **H4 (`####`) is an optional entry level for hand-written entries.** Tools treat it as an entry; writers never emit it.
- **H1/H2 are document structure, never entry headings.**

**Reader tolerance is permitted, not required.** Section extractors resolve the *nearest preceding heading of any level* — deliberate, because surrounding same-level context can be worth showing; they MAY be tightened to H3/H4 if that tolerance ever causes trouble. grubber ignores headings entirely: blocks are found by their fences, a heading is not data.

**Blank-line rule.** A blank line is REQUIRED between a structure heading (H1/H2) and a following entry heading — for readability and clean section extraction. (register's block deletion no longer depends on it: the delete regex takes only the single heading line directly above a block, so an adjacent structure heading survives either way.) After H3/H4 no blank line is needed; the block or prose may follow directly.

### Markdown annotation (optional)

A record may additionally carry a lean Markdown annotation, linked to the index by `id`:

````markdown
### brief.pdf
```yaml
type: ref
id: abc123
binder: project-alpha
```
````

The H3 heading is the filename (human recognition); the YAML block is the back-reference plus whatever custom fields and prose the user adds. A block names a **single** `binder` — it is the file's context *in that binder*. The same file may therefore carry **several** annotation blocks, one per binder, each with its own custom fields and prose (an invoice filed under `rechnungen-2024` with an amount, and under `projekt-a` with a deadline). The index stays one record per file; the Markdown layer is per-binder context, linked back by `id`. Annotations never replace the index entry — they are the optional, disposable custom layer. `register promote` (or `add --md`) creates them; grubber-over-Markdowns reads them for rich queries (with `--explode binder --merge-on id,binder`; see [matterbase Touchpoints](#matterbase-touchpoints)).

### Bookmarks (binderless records)

A record whose `binder` set is **empty** (absent or `[]`) is a **bookmark**: a file tracked by identity alone — resolvable by `id` or `aka` (e.g. behind a `milan://` URL), in no binder. This is a first-class state, not a leftover. Binders are an *optional* facet of a record, not its essence; a file can live in the register by reference only.

A bookmark arises two ways, indistinguishable and both legitimate:

- **Created directly** — `register add <file>` with no `--binder` registers the file as a pure bookmark (id + optional `aka`, empty set, no binder xattr cache, no ★).
- **Emptied by removal** — `register remove` deletes a binder from the set; when the last one goes, what remains is a bookmark. Removal never destroys the record or its annotations.

**A bookmark is never deleted automatically.** The record disappears only when the file itself is gone, or by explicit human choice. Re-adding a binder is a plain set-insert on the existing record — there is nothing to "reclaim" and no duplicate to avoid.

Bookmarks are inert for membership queries: `grubber -f type=ref -f binder=X` never returns a record that lacks `X` in its set. A bookmark surfaces only in the per-file view (no binder filter) or by `id`/`aka` lookup.

In Markdown, an annotation block must be preceded by a heading at any level. matterbase uses that heading to display the record's context in the preview pane. `register promote` writes H3 (the filename) by convention; matterbase reads any level.

### URL refs

A `type: ref` record may reference a **URL** instead of a file — a DEVONthink item link (`x-devonthink-item://UUID`), a web page, a `message:` link. The bookmark was never the essence of a ref; it is the macOS accelerator for the *file* locator. A URL ref is the same record with a different locator: identity (`id`), membership (`binder`), aka, tags, annotations, and promote all work identically, and `register resolve` returns the URL — `open "$(register resolve <key>)"` is uniform for both (milan needs no change).

What does **not** apply is the entire derived layer: no bookmark, no xattr cache, no ★, no Spotlight, no `repair`, no `of`. URL refs are implicitly `xattr: none` — index + annotation citizens, the same standing `none`-backend files already have. `refresh`/`audit`/`repair` skip them (counted); `marshal` carries them as pure data (no file payload). Created with `register add --url <URL> --binder <name> [--label <name>]`; `filename` holds the optional display label (its human-recognition role — the repair-anchor role is moot). Removed with `register remove <url> --binder <name>`. Idempotent over `(url, binder)`.

### macOS metadata layer (derived)

When `register refresh` (or `register add` at write time) runs, the referenced file receives:

| xattr / metadata field | Content | Set by |
|---|---|---|
| `com.apple.metadata:kMDItemInformation` | Bookmark `id` (multiple IDs space-separated) | The `Bookmarks` module, via the **fileanchor** engine |
| `com.fileregister.id#S` | Bookmark `id` (single value) — the cross-device file→record key | The `Bookmarks` module on `add`/`repair`; `refresh` backfills |
| `com.apple.metadata:_kMDItemUserTags` == ★ | The managed marker (U+2605) — one status tag fileregister owns | `add` (any backend); removed by `remove` on last membership; `refresh` backfills |
| `com.apple.metadata:kMDItemProjects` | Binder names — a real array (Spotlight matches per element; names may contain spaces) | default backend (`itemprojects`) |
| `com.apple.metadata:_kMDItemUserTags` == \<binder\> | Binder name (Finder Tag, binary plist array) | Tags backend — opt-in per record |

The id layers (`kMDItemInformation` plus the syncable `com.fileregister.id#S`) are the bookmark cache (file-identity); ★ marks management; the Projects and UserTags layers are the membership cache. The membership backend is chosen **per file** via the one `xattr:` field on the record.

**Cross-device id.** `kMDItemInformation` is the local file→id store, but the Apple `com.apple.metadata:kMDItem*` namespace is **stripped by iCloud Drive** even with a `#S` (syncable) flag (verified, macOS 26.4). A **custom** `#S` name survives, so the id is mirrored to `com.fileregister.id#S` — the one xattr that lets a refreshed/AirDropped file be mapped back to its record by id on another Mac. It is the **identity** layer, written regardless of the binder-name `--xattr` backend (even `none`), and is single-valued (one file ↔ one id). The `#S` is part of the stored name — read it back with the suffix. (It also has standalone **local** value: file→record mapping for `reindex` / `register of <file>` without `bookmarks.json`.) Transports that strip all custom xattrs (e.g. Resilio) lose it; reconcile then falls back to filename — see [FINDINGS-attributes.md](FINDINGS-attributes.md).

**Managed marker (★).** Every managed file carries one Finder Tag, ★ (U+2605), stored in `_kMDItemUserTags` (with underscore). It is written on first membership by `add` — **backend-independent**, so even `--xattr none` files get it, because ★ marks *membership*, not the binder-name cache. `remove` drops it only when the file leaves its **last** binder (checked against the index across all binders); a file in two binders keeps ★ until both memberships are gone. `refresh` backfills it on every member; `repair` re-applies it to a relocated file. As a Finder Tag, ★ syncs on **all** transports (iCloud Drive, Resilio, AirDrop — verified), which makes it the cross-device keystone and the enumeration/reconcile anchor (`mdfind 'kMDItemUserTags == "★"'`, query key without underscore). Exactly one status tag — binders never enter the tag namespace (that stays the opt-in `tags` backend); see [FINDINGS-attributes.md](FINDINGS-attributes.md).

#### Backend choice

| Backend | xattr field | Where visible | Trade-off |
|---|---|---|---|
| `itemprojects` (default) | `kMDItemProjects` | Spotlight only (`mdfind`) | Quiet; no clash with Finder Tag workflows; not surfaced on iOS |
| `tags` | `kMDItemUserTags` | Finder chips, iOS Files sidebar, Spotlight | Visible cross-device via iCloud Drive; mingles with user's other Finder Tags |
| `none` | (no xattr) | (none) | Markdown-only; no Spotlight cache; minimal residue |

The choice is **per file** (one backend per record), not per binder. All of a file's binder names are cached through the same backend. Use cases vary: photo files benefit from `tags` (Finder Cover Flow, iOS visibility); documents work well with `itemprojects` (quiet Spotlight cache); archival material may need `none` (Markdown-only). A file's backend can be changed later — `register audit` surfaces any stale entries left in the old layer.

This layer makes a file findable by Spotlight and recoverable on the same volume even when paths change. On filesystems that do not preserve xattr (FAT32, many cloud sync services, rsync without `-E`), the layer is lost on transfer — but the Markdown records remain intact and can re-populate it.

## Record Placement

A new `add` operation **always** records to the index, and **may** additionally write a Markdown annotation:

1. **Index (always).** The authoritative record is appended to `<notes_dir>/collections/inbox.jsonl`, auto-created on first add. An explicit `--target some-file.jsonl` chooses a different index file (rarely needed).
2. **Annotation (optional).** `--md` also writes a lean annotation to `<notes_dir>/collections/binder_<sanitized>.md` — the same note `promote` would use. `--target some-note.md` names the annotation file explicitly. `--md` and `--target` are mutually exclusive.

`<notes_dir>` is resolved from the same env vars as the other subcommands: `GRUBBER_SET` (via grubber's config) takes precedence, otherwise `GRUBBER_NOTES`. The `collections/` subdirectory is created automatically. Annotation notes use the `binder_` prefix to disambiguate from unrelated notes and apply light sanitization (forward slashes, null bytes, and control characters become hyphens; unicode and colon-hierarchy names like `2024:berlin` pass through unchanged).

So `add --md` is `add` followed by `promote` in one step: the index entry plus its annotation. Capture-then-promote and add-`--md` are the deferred and immediate forms of the same curation — and both always leave the authoritative entry in the index.

### One write target, many index files

`collections/` may legitimately hold **more than one** `.jsonl` over time — an `archive-2024.jsonl`, an imported set, a merged sub-project. The asymmetry is clean:

- **One write target.** `register add` (without `--target`) **always** appends to `collections/inbox.jsonl`. New records have one unambiguous home.
- **Many read sources.** `read_index` reads every `*.jsonl` in `collections/` and concatenates them. Archives and imports are queryable and editable in place, but new adds never land in them.

Editing stays unambiguous regardless of file count: each record's injected `_note_file` names the specific source file, and in-place edits (`remove`, `rename`, `repair`, `cleanup`, `reindex`) rewrite exactly that file.

### The core read path

The core (`list`, `audit`, `refresh`, `repair`, `marshal`) reads the index directly via `read_index` — the `*.jsonl` files under `collections/`. It does **not** scan Markdown. The optional annotation layer is read in only three narrow cases:

- **Editing commands** (`rename`, `cleanup`) also read annotations via `read_annotations`, so a record's `binder` set is updated in both the index and any per-binder annotation blocks. (`remove` deliberately does not: it set-deletes in the index only, and `cleanup` reviews the stale blocks.)
- **`list --inbox` / `list --curated`** — explicit on-demand filters that consult the Markdown layer to determine annotation status. The default `list` (no flag, no binder) is pure-register and never reads Markdown.
- **The query layer** — grubber-over-Markdowns, used by matterbase — reads annotations for rich, full-text queries. It is optional and never on the core path.

A `type: ref` block hand-written into a project note is therefore part of the *annotation* layer; `register reindex` pulls such records into the index so the core sees them too.

### Filesystem layout

```
<notes_dir>/
├── collections/
│   ├── inbox.jsonl            ← the index; every record, one per line (the write target)
│   ├── archive-2024.jsonl     ← optional extra index file; read-merged, never written by `add`
│   ├── binder_vertrag.md       ← optional annotation note (promote target)
│   └── binder_projekt-alpha.md ← optional annotation note
├── meetings.md             ← may contain type:ref annotations; `reindex` pulls them into the index
├── projekt-alpha/
│   └── kickoff-notes.md    ← same; scattered annotations stay discoverable to the query layer
└── …
```

The `collections/` directory holds the index (`*.jsonl`) the core reads and writes, so it is load-bearing by convention. Markdown annotations remain locatable by content anywhere under `notes_dir` — the query layer and `reindex` find them by scanning, not by path.

### Code layout: the index-core seam

The layering above is also enforced in the code. The core — JSONL index,
bookmark database, fileanchor engine client, atomic writes — lives in its own
Go package, `internal/index`; the Markdown layer and the CLI commands
sit above it in `package main` and import it. Since a Go package can never
import `main`, the dependency direction is compiler-checked: **nothing in the
core can reach the Markdown surgery.** That keeps the index readable and
writable without any annotation machinery — the property the two-layer design
promises — and gives a future second consumer (a library, a GUI, a stripped
CLI) a clean entry point that is guaranteed not to drag the Markdown layer in.

## CLI

Collections are managed via the unified `register` binary in this repository — a Go CLI dispatching to one `cmd_*.go` per subcommand. Following the same shell-tool conventions as bookmarker and grubber itself (JSONL stdio where applicable, no daemons, no shared state beyond the bookmarks JSON file). This is the canonical interface for collection work. matterbase does not invoke it — collection lifecycle is a shell-level concern. matterbase's only relationship to collections is querying them via standard grubber filters (see [matterbase Touchpoints](#matterbase-touchpoints)).

No watchers, no auto-sync. Reconciliation is explicit, invoked when the user wants it.

### `register add <file>... [--binder <name>] [--aka KEY] [--kind K] [--md] [--target F] [--xattr BACKEND]`

(URL form: `register add --url <URL> [--binder <name>] [--label NAME] [--aka KEY]` — no bookmark, no xattr; see [URL refs](#url-refs).)

The user-facing entry point for registering one or more files. With `--binder` the files join that binder (set-insert); **without `--binder` each file is registered as a [bookmark](#bookmarks-binderless-records)** — an empty `binder` set, no binder xattr cache, no ★. This folds in the former `link` subcommand: a binderless add *is* a link. Wraps the full flow:

1. **Register** every file with the internal `Bookmarks` module — creates `id`, writes `kMDItemInformation` to the file. For batches this goes through `Bookmarks.add_many`, which loads and saves the bookmark DB **once** for the whole batch (the `bookmark save` subprocess still runs per file — each file needs its own blob — but the DB write is O(1) instead of one full rewrite per file)
2. **Write the index record** — via `write_many`, which reads the target (`collections/inbox.jsonl`, or the `--target *.jsonl` index file) **once**, indexes existing records by `id`, then for each file **set-inserts** the binder into the matching record's `binder` array (or creates a new record — with an empty set when no `--binder` was given), and writes the target **once**. One record per file; a binder already in the set is a no-op, so no duplicate can arise. The chosen backend is recorded as `xattr:` in the record (omitted when default `itemprojects`)
3. **Write the annotation (optional)** — with `--md` (→ `collections/binder_<name>.md`) or `--target *.md`, a lean annotation is also written for each new record via `promote_records` (idempotent). The index entry from step 2 stays authoritative
4. **Write the xattr layer** for each file according to the chosen backend:
   - `itemprojects` (default): `kMDItemProjects` via the write side-effect
   - `tags`: `Tags.add` writes the Finder Tag (`kMDItemUserTags`)
   - `none`: no xattr write

The `--xattr` flag selects the backend; the default is `itemprojects`. The index record is written first; annotation and xattr failures don't block it.

The `kind` field is auto-detected from the file's extension (e.g. `.jpg` → `image`, `.pdf` → `pdf`, `.eml` → `mail`). Use `--kind` to override when a finer classification matters (e.g. `--kind invoice` for a PDF that's an invoice). Pass `--kind ""` to suppress the field entirely.

Idempotent: re-adding the same file to the same binder is a set-insert of a value already present — a no-op, no duplicate record and no duplicate xattr entry. If the file was previously **removed** from that binder, re-adding re-inserts it into the existing record's set; if the file was a [bookmark](#bookmarks-binderless-records) (empty set), it gains its first binder. Either way: one record, edited in place — there is nothing to reclaim.

**Batch adds.** Multiple file arguments (`register add *.pdf --binder x`) are processed in one pass. Batch-wide options — `--binder`, `--kind`, `--target`/`--md`, `--xattr` — apply to every file. The per-file option `--aka` only makes sense for a single file and is rejected when more than one file is given (`kind` defaults to the extension-derived value). Files that don't exist are reported and skipped; a per-file `bookmark save` failure is reported and skipped; the rest still go through. The command exits non-zero only when nothing at all was written. The batch I/O is linear, not quadratic: one bookmark-DB load/save and one target read/write for the whole batch (see steps 1 and 3 above).

### `register promote --binder <name> [--target F] [--id ID] [--edit]`

Adds a lean Markdown annotation for a binder's records. The index entry **stays put**; promote is additive. Two granularities, one engine:

- **Binder annotation** (default): for every index record in `--binder <name>`, a lean `type: ref` block (H3 = filename, YAML = `id` + `binder`) is written into a Markdown note (default `<notes_dir>/collections/binder_<sanitized>.md`, or `--target F`).
- **Record annotation** (`--id ID`): only the single record with that id is annotated.

Flow per record: read it from the index (`read_index`), then append a lean annotation block to the target. The block is keyed by `(id, binder)` — it is the file's context in *this* binder. Idempotent: a record whose `id` already has a block for this binder in the target is skipped (counted as `noop`). The index is never modified.

The target must be a `.md` file. There is **no `demote`**, and promote never deletes the index entry — reverting an annotation is a manual Markdown edit (see [Capture and promote](#capture-and-promote)). `promote` is the natural home for future cross-cutting features (validation, enrichment, field normalization); keeping it a clean pipeline lets those slot in at one point.

### `register annotate <binder> <id|aka> [--set k=v]… [--unset k]… [--prose <text>|-]`

Edits an **existing** annotation block — the tool-shaped form of "the user edits it directly" (see [Record Identity](#record-identity)), built for GUI clients (binderview's entry inspector) that must never grow a second Markdown engine. `annotate` never creates a block (`promote` stays the one creation chokepoint) and never touches the index.

- `--set k=v` replaces the field in place (an array-valued field collapses to the scalar, its `- ` items go with it) or appends it; `--unset k` removes field and items, absent = no-op. Values are scalars: numerals and booleans are written plain (`amount=129.50` means a number; leading-zero numerals stay quoted strings), everything else follows promote's quoting.
- **Reserved keys are refused**: `id`, `type` (identity), `binder` (membership — `add`/`remove`), `aka` (identity handle — `add --aka`), `sort` (ordering — `order move`). Identity and membership have their own verbs; `_`-prefixed keys are injected, never stored.
- `--prose` replaces the section's prose — every line of the section that is neither the heading nor a fenced `yaml`/`yml` block — with the given text (`-` reads stdin, empty clears), rewritten canonically between heading and block. The heading and the blocks themselves stay byte-identical. This is a **deliberate policy change**: register historically never touched prose; `annotate --prose` may, on explicit request, because register is the family's only Markdown writer and the alternative would be exactly the second engine the rule exists to prevent.

Blocks are matched over id ∪ aka like every other block lookup; all matching `(id ∪ aka, binder)` blocks are edited, wherever the note lives. A block without a preceding heading refuses `--prose` (no section to rebuild). Writes are atomic, one write per file, fields and prose in the same pass.

### `register refresh [--dry-run]`

Pushes index state to macOS metadata. Reads all active `type: ref` records from the index via `read_index`, then for each record refreshes the appropriate xattr layer based on the record's `xattr:` backend choice:

- `itemprojects` → ensures binder name in `kMDItemProjects`
- `tags` → ensures binder name in `kMDItemUserTags`
- `none` → skips xattr; no-op

Direction is always index → xattr. Records are the source; xattr is the cache. The bookmark blob and `kMDItemInformation` are maintained by `register add` and `register repair`; refresh does not touch them — but it does backfill the regenerable id xattr (`com.fileregister.id#S`) on each resolved file, since that value derives entirely from the record's `id`.

Reports:
- Records whose bookmark does not resolve (candidates for `register repair`)
- Records whose referenced file no longer exists (dangling — for `register audit` review)
- Count of `none`-backend records (skipped intentionally)

Idempotent. Safe to run repeatedly.

### `register repair [--interactive]`

For records with broken bookmarks (typical after cross-volume move or transfer through xattr-unfriendly path):

1. Attempt to relocate the file, in this order:
   1. `mdfind 'kMDItemInformation == "*<id>*"'` — backend-agnostic; finds the file by its preserved bookmark-id xattr (most reliable when xattr survived the transfer)
   2. `mdfind 'kMDItemFSName == "<filename>"'` — by the YAML `filename` field; exact on-disk basename match, works even when xattr is gone
   3. `mdfind` on the appropriate xattr layer (`kMDItemProjects` or `kMDItemUserTags` per record) restricted to files whose basename matches `filename`
2. If found: re-bind the bookmark **under the file's existing id** via `Bookmarks.rebind` (a fresh blob stored under the same id), then refresh the id xattrs and the binder xattr layer. The id is the file's **permanent identity** — only the broken blob is renewed, so nothing is propagated to the index or annotations.
3. If multiple candidates: list them; with `--interactive`, prompt to choose
4. If not found: report; in `--interactive` mode prompt for an explicit path

### `register audit`

Read-only consistency report. Two directions:

- **Record → File**: records whose bookmark does not resolve, or whose target file lacks the expected xattr value in the record's chosen backend (ItemProjects, UserTags, or — for `none` — no check). Binderless bookmarks are included — resolution check only, no xattr expectations.
- **File → Record**: files in scope that carry a Spotlight tag matching a known binder but no corresponding `type: ref` record. Scans both `kMDItemProjects` and `kMDItemUserTags` to catch ghost entries regardless of backend (e.g. record was deleted, xattr manually edited, `refresh` ran with stale state)

Output is plain text. Decisions stay with the user.

### `register remove <file> --binder <name>`

The inverse of `register add` — removes a file from a binder without destroying its annotations. It reads only the index (`read_index`); Markdown context blocks are never touched.

For the record whose `id` matches the file's bookmark id:

1. **Set-delete the binder** from the `binder` array in the index line. The record's other binders and all annotations stay intact. When the last binder is removed, the record becomes a [bookmark](#bookmarks-binderless-records).
2. **Remove the binder name** from the appropriate xattr layer per record's `xattr:` backend (`kMDItemProjects` or `kMDItemUserTags`; `none`-backend records have no xattr to touch). The backend is taken from the index record.

Idempotent: removing from a binder the file isn't in is a no-op.

Per-binder annotation blocks for the removed membership stay where they are — now stale against the index, which is exactly what `register cleanup` reviews (bare blocks are one keystroke to drop there; annotated ones get a human decision).

Note: this does **not** remove the bookmark blob or the record. When the last binder is removed the record remains as a [bookmark](#bookmarks-binderless-records), addressable by `id`. Re-adding a binder is a plain set-insert on that same record.

### `register rename <old> <new>`

- Rewrite every record with `binder: <old>` to `<new>` in **both** the index (`read_index` → `JsonlEditor`) and any annotation copies (`read_annotations` → `MdEditor`)
- Carry the ordering layer along: the `type: ordering` config block is rewritten too, and the canonical note file `collections/binder_<old>.md` is renamed to `binder_<new>.md` (when the new name's note already exists — the `--merge` case — both stay and rename says so)
- For each referenced file (once per `id`, backend taken from the index): remove `<old>` and add `<new>` in the per-record xattr layer (`kMDItemProjects` or `kMDItemUserTags`, or skip for `none`)

Not atomic across many files. A subsequent `register refresh` reconciles any residue.

### `register cleanup [--interactive]`

Human-judged review of drift between the layers. Reads both stores (`read_index` + `read_annotations`); writes only on user confirmation. It **never deletes a record automatically** — a binderless record is a bookmark, not cruft (Principle 4).

Surfaces, for the user to decide:

- **Stale context blocks** — a per-binder annotation block whose binder is *not* in the index record's `binder` set (e.g. an annotated block kept by `remove`). Either delete it (the context is obsolete) or re-add the binder (the membership was dropped by mistake).
- **Unindexed annotations** — a `type: ref` block whose `id` has no index record at all (legacy data, hand-written refs). Usually resolved by `register reindex`, which pulls them into the index; cleanup flags any that should instead be deleted.
- **Bookmarks for review** — records with an empty `binder` set, listed so the user can prune ones no longer wanted. **Listed, never auto-deleted.**

Duplicates do not appear here: with one record per file and `binder` as a set, duplicate `(id, binder)` registrations are structurally impossible.

This tool is **not** part of matterbase. Its workflow (per-item review with accept/reject) suits a small dedicated TUI or interactive CLI (like `git add -p`'s patch mode). Implementation form to be decided when the drift becomes painful enough to motivate the tool.

### `register album <binder> [--out DIR] [--open]`

(User guide: [ALBUM.md](ALBUM.md).)

Renders a binder as a **static, self-contained HTML photo album** (folder with
`index.html`, `media/`, `thumbs/`) — copy it, zip it, or serve it in the LAN
(dylan). The album layer is pure render: the curated data lives as YAML fields
in the annotation blocks, grubber-readable like everything else:

| Field | Meaning |
|---|---|
| `sort` | Order key, plain string sort (`"1" < "10" < "2" < "a"` — computer order). Records without it follow, by filename. |
| `title` | Entry title. Fallback: IPTC headline (via Spotlight), then filename. |
| `comment` | Caption shown in the album. Fallback: IPTC description. |
| `map` | Per-image map switch for the detail view. Without the field the map appears automatically when the photo carries GPS data; `map: false` suppresses it. |
| `place`, `lat`, `lon` | Curated location: display text and coordinates. Fallback is the IPTC/EXIF data **inside** the image (read via Spotlight) — durable in the file itself, but lost to EXIF-stripping transports and unreadable without Spotlight. The YAML fields preserve the location independently; the image file is never written to. |

The album title is the binder name. Location and date come from Spotlight's
IPTC/EXIF index (`mdls` — City/Country/creation date), so the standards photo
apps ignore are finally on display. Markdown **prose** under a block is private
working notes and is never rendered. Non-image members (PDF — a ticket, a map)
render as link cards: albums are not limited to photos. Thumbnails via `sips`
(JPEG, browser-safe even for HEIC originals). Renderer, not a photo manager —
editing the album IS editing the annotation note.

Clicking a photo opens a **detail view** (CSS-only `:target` overlay, no
JavaScript): image left, metadata (title, caption, place · date, camera) top
right, and — when the photo carries GPS — an OpenStreetMap embed bottom right
(lazy iframe, no API key; the map is the album's only internet dependency).
The stable markup contract for custom stylesheets: everything lives inside an
`.album` wrapper (`​.album h1`, `.album .grid`, `figure`/`figcaption`, `.meta`,
`.card`, and `.detail` with `.detail-media`, `.detail-info`, `.detail-map`,
`.detail-close`); page chrome goes on `body.album-page`, which only the
standalone document carries — so nothing leaks into a host page when the
album is served embedded (dylan Stage).

### `register reindex [--dry-run]`

Rebuilds the index from Markdown ref blocks, restoring the invariant that every record has an authoritative index entry. For each `type: ref` block found under `notes_dir` (`read_annotations`) whose `id` is **not** already in the index (`read_index`), it reconstructs an index line and appends it to `collections/inbox.jsonl`. Every block of an id contributes: a record annotated in several binders is rebuilt as **one** record with all memberships folded into its `binder` set. Idempotent; `--dry-run` previews the additions without writing.

This is the migration/repair path:

- **Legacy data** — records written by the old `--md` (which used to live only in Markdown, with no index line) get pulled into the index so the core read path sees them.
- **Hand-written refs** — a `type: ref` block dropped into a project note becomes a first-class index record.
- **Recovery** — if an index file is lost but the annotations survive, reindex rebuilds what it can (a lean annotation yields a minimal record; a full legacy block yields a complete one).

Annotations are left untouched; reindex only ever *adds* to the index.

## matterbase Touchpoints

matterbase is the query-construction TUI. Its primary job is to help users assemble grubber queries (with filters, fulltext, SQL refinement) and to yank the resulting CLI command.

Collections are **not** integrated as a dedicated UI feature in matterbase. Two practical reasons:

1. The files typically added to collections (PDF, Pages, Mail, photos) are not visible in matterbase's file list, which only shows Markdown notes. A built-in add/remove UI would have nothing meaningful to operate on.
2. Binder files conventionally live under `<notes_dir>/collections/binder_<name>.md`. They're regular Markdown files and matterbase can read them like any other note, but treating them as a distinct UI concept would only add modes without enabling new workflows.

### Querying collections from matterbase

Collections are queryable like any other field via matterbase's existing filter mechanism. Two ways:

**Filter buttons in config.yml** — declarative, persistent:

```yaml
filters:
  - label: "Testdateien"
    query: ["binder=Testdateien"]
  - label: "Project Alpha"
    query: ["binder=project-alpha"]
```

Clicking the button narrows the visible records to those matching. The yanked grubber command reflects the filter, ready to pipe.

**Direct in the shell** — exploratory, ad-hoc:

```sh
grubber extract ~/notes --blocks-only -f type=ref -f binder=Testdateien
```

No special matterbase support is required for either approach.

A bare Markdown scan returns only the **annotated subset** — records that carry a per-binder context block, with their custom fields and prose. To query the **full index** (every record, annotated or not), add the JSONL store as a merge source. Because the index holds one record per file with `binder` as an array, `--explode binder` first projects each index record into one row per membership, so the per-binder Markdown blocks line up and collapse on `(id, binder)`:

```sh
grubber extract ~/notes --from-jsonl ~/notes/collections/ --explode binder --merge-on id,binder --blocks-only -f type=ref -f binder=Testdateien
```

So the Markdown-only scan is the rich "annotated view"; `--from-jsonl` widens it to the complete directory; `--explode binder` turns the one-per-file index into per-membership rows; and `--merge-on id,binder` ensures each membership appears once — as its annotation block, back-filled with the index fields — rather than as two entries. For a **per-file** view (one row per file, bookmarks included), omit `--explode`/`--merge-on` and filter on the array directly (`-f binder=Testdateien` matches a value inside the set). See [COLLECTIONS.md](COLLECTIONS.md) for the matterbase-side configuration and [grubber/IDEAS.md](https://github.com/rhsev/grubber/blob/main/IDEAS.md) for `--explode`.

### Management lives in the CLI

Collection lifecycle (add, remove, refresh, audit, repair, rename, cleanup, reindex) lives in the `cmd_*.go` files of the `register` binary. matterbase has no part in it.

## Scenario Matrix

| Event | What survives | Batch needed |
|---|---|---|
| Add file to a binder | — (new state, recorded to the index) | `register add` |
| Annotate a binder | index entry kept; annotation added | `register promote` |
| File moved within same volume | bookmark, xattr | none (alias follows file) |
| File moved cross-volume, xattr preserved | xattr; bookmark broken | `register repair` |
| File copied to xattr-unfriendly FS | the index record | `register repair` after `register refresh` on the copy |
| File deleted | the record (now a dangling reference) | `register audit` reports; manual decision |
| Binder renamed | needs propagation | `register rename` |
| User edits the index manually | record exists, metadata stale | `register refresh` |
| Record exists only in Markdown | annotation, no index entry | `register reindex` |
| User edits `kMDItemProjects` manually | xattr value exists, no record | `register audit` reports; manual `register add` to reconcile |
| Repository copied to Linux | the index record (no xattr) | none; matterbase operates in degraded mode (no metadata layer) |

## Non-Goals

- **No daemon**, no filesystem watcher, no real-time write-on-edit. Reconciliation is explicit.
- **No reverse sync**. The tooling does not write records from xattr/ItemProjects state automatically. `register audit` exists to surface the discrepancy; the user decides.
- **No binder hierarchy** (no nested binders, no parent-child). A file is in zero or more binders, period.
- **No matterbase-managed index**. The index is plain JSONL the core reads directly; the optional query layer (grubber-over-Markdowns) re-scans on each invocation. No daemon maintains either.
- **No proprietary fields**. The schema uses field names a human would naturally pick. Any other tool can read or write these records.

## Write Engine

The `write` subcommand (`register write`) is the record-write helper. It reads JSONL records from stdin and ensures each one exists in its target file, dispatching on the target extension: a YAML block appended to a Markdown note, or one JSON line appended to an JSONL store. `register add` invokes it internally; the other lifecycle subcommands modify records in place via the Markdown editor (`md_writer.go`) or the JSONL editor (`jsonl_editor.go`) — chosen by the record's `_note_file` extension — when more surgical edits (drop a field, rename within a record) are needed.

`register write` also handles the derived `kMDItemProjects` layer as an opt-in side-effect via the `_ref_path` input field. This is the path used by `register add` with `backend=itemprojects` (the default). For `backend=tags`, `register add` invokes `Tags.add` separately after the write call; for `backend=none`, no xattr write occurs.

The subcommand was originally a standalone tool named `grubber-write`, conceived as a symmetric counterpart to grubber's extraction engine. It folded into this repository (and into the unified `register` binary) when it became clear that fileregister is its only consumer and that the lifecycle code needed direct MdEditor access for the non-append operations. The `write` subcommand remains useful for its specific job: idempotent append of one ref block per input record.

### Semantic Contract

Given a record R and a target file T, `register write` ensures R is present in T. The operation is idempotent: re-running with the same input produces the same on-disk state.

Three cases:

1. **R already exists in T** → no-op
2. **R missing, T exists** → R appended to T
3. **R missing, T missing** → T created (empty), then R written

The semantic unit is the **record**, not the file. File creation is incidental to record-creation, not a separate concern.

### Scope

- **Format**: Markdown (`.md`) and JSONL (`.jsonl`). The writer dispatches on the target extension and the unit differs by layer: `JsonlFileWriter` keeps **one record per file**, set-inserting the input's `binder` into the existing record's array (idempotency key `id`); `MdFileWriter` appends **one block per `(id, binder)`** context (idempotency key `(id, binder)`). Both share the same `write(record, target)` contract.
- **Record type**: `type: ref` only — `id` and `binder` required
- **Format dispatch** by target extension (a `WRITERS` table keyed on `.md` / `.jsonl`)

Non-conforming input is rejected per-record with a status entry; the stream is not halted. Typst targets and other record types are deferred until grubber-read gains symmetric support.

### Record Identity

In the JSONL index a record is identified by `id` alone (one record per file); `register write` set-inserts the input's `binder` into the existing record rather than appending a second line. In Markdown a context block is identified by `(id, binder)`. Other fields (kind, aka, tags) may differ between input and existing record — `register write` does not rewrite an existing record's other fields, only ensures the membership is present. To change an existing record's fields, the user edits it directly.

### Heading Convention

When writing into Markdown:

- If the record has a `filename` → prepend `### {filename}` before the YAML block
- Otherwise → bare YAML block, no heading

The heading is **structural and cosmetic**, never an information carrier — its sole job is to give Markdown viewers and matterbase's preview pane visual anchoring. Anything queryable lives in the YAML block, including the file's basename (via the `filename` field, see [YAML block](#yaml-block-canonical)). This means the heading text is free for the user to rename without losing the connection to the underlying file.

matterbase's preview pane uses the nearest preceding heading to give the block context; bare blocks simply inherit whatever heading is already in the file.

### Interface

**Input**: JSONL on stdin. Each line is a record.

Required fields:
- `_note_file` — target Markdown file (where the record is written)
- `type: ref`, `id`, `binder`

Optional fields:
- `filename`, `kind`, `aka`, `tags`, `xattr` — record content
- `_ref_path` — path to the referenced file. When set and the file exists, `register write` adds the `binder` to that file's `kMDItemProjects` array (idempotent)

Example:
```json
{"_note_file": "/path/to/collections/binder_project-alpha.md", "_ref_path": "/path/to/brief.pdf", "type": "ref", "id": "abc123", "binder": "project-alpha", "filename": "brief.pdf", "kind": "pdf"}
```

**Output**: JSONL on stdout, one status entry per input record, in input order. Statuses are emitted after stdin EOF — records are batched per target file (one read + one write per target instead of a rewrite cycle per record), so a consumer writes the whole stream, closes stdin, then reads the statuses. An oversized input line (over the 4 MB cap) or a stdin read error fails the whole run before any write is applied.

```json
{"ok": true, "_note_file": "/path/to/collections/binder_project-alpha.md", "action": "appended", "xattr": "added"}
{"ok": true, "_note_file": "/path/to/collections/binder_project-alpha.md", "action": "noop",     "xattr": "noop"}
{"ok": true, "_note_file": "/path/to/collections/binder_project-alpha.md", "action": "appended", "xattr": "skipped"}
{"ok": false, "_note_file": "/path/to/x.json", "error": "unsupported target format: .json"}
```

- `action` distinguishes Markdown no-op from actual append
- `xattr` distinguishes how the kMDItemProjects side-effect resolved:
  - `skipped` — no `_ref_path` provided
  - `missing` — `_ref_path` provided but file does not exist
  - `noop` — binder name already in kMDItemProjects
  - `added` — binder name appended to kMDItemProjects
  - `failed` — xattr command exited non-zero (Markdown write still succeeded)

**Exit code**: non-zero if any input record failed (Markdown write level; xattr failures do not set non-zero exit).

### Implementation

- **Language**: Go — originally a Ruby CLI, ported to Go; a single self-contained binary (only `libSystem` + `libresolv` linked)
- **Distribution**: single compiled binary (`register`), invoked from the shell or as a subprocess
- **Stream mode**: reads records until EOF, then writes batched per target file; one invocation handles a whole batch without per-record process overhead
- **Concurrency**: cross-process writers serialize via the advisory index lock (see the wire contract's write discipline), so concurrent `register` runs no longer lose writes; callers need not serialize themselves.

### Migration Path

The contract is subprocess + JSONL. `register write` has since been ported from Ruby to Go; consumers did not change — they keep invoking the same external command with the same input/output format. The same stability holds for any future reimplementation.

## Implementation Notes

- **The unified `register` CLI** follows the conventions of its sibling shell tools in the stack (grubber-twin, etc.): a single dispatcher (`main.go`) plus one `cmd_*.go` per subcommand, no dependencies beyond the Go stdlib, JSONL or simple text stdio where applicable, no shared state beyond the bookmarks JSON file.
- **Metadata via the fileanchor engine**: the bookmark, ItemProjects, Tags, and locator clients (in `bookmarks.go`, `meta.go`, `locator.go`) are thin clients of the **fileanchor** engine ([its own project](https://github.com/rhsev/fileanchor)) — a native binary that performs all macOS metadata in-process (bookmarks, Finder tags, Spotlight, xattrs) over a batch stdio protocol. A single fileanchor client (`fileanchor.go`) holds one engine process open for the run, so there is no fork+exec per file. `register` keeps the id→blob map in `~/.local/share/bookmarks.json`; the engine is stateless about it (`save path→blob`, `resolve blob→path`). The engine is the one external dependency and the single macOS-coupling / portability seam.
- **Xattr backend dispatch**: in `meta.go`, the add/remove/includes helpers route to ItemProjects, Tags, or skip (`none`) based on the record's `xattr:` field. Per-record granularity; mixed-backend setups are supported.
- **register add is the user-facing entry point** for adding. It is invoked from the shell, not from matterbase — matterbase's file list shows only Markdown notes, while the typical Add target (PDF/Pages/Mail/etc.) lives elsewhere on disk. No `matterbase add` subcommand needs to exist.
- **register remove is a set-delete**: it removes the named binder from the index record's `binder` set; annotation blocks are left untouched (`register cleanup` reviews the now-stale ones). It never removes the record — an emptied set is a bookmark. Re-adding is a set-insert on the same record (one batched index append); there is no reclaim and no duplicate to avoid.
- **JSONL edits are parse/mutate/serialize**: the JSONL editor mirrors the Markdown editor's surface (read all refs, update id, delete block, rename/add/remove binder) but operates on whole JSON lines rather than block regex, so index mutations are robust by construction. The record's `_note_file` extension picks which editor runs. Injected provenance fields (`_note_file`, `_mtime`) are stripped before a line is rewritten, so they never persist into the store. When multiple `*.jsonl` files exist, edits group records by `_note_file` and rewrite only the file each record came from.
- **Path identity is filesystem truth**: whether two paths name the same file is decided by the filesystem where possible — existing files compare by device:inode (`os.SameFile`), which is exact on case-sensitive APFS and Linux alike and immune to Unicode form and symlinks, because the `stat` lookup applies the filesystem's own rules. Only two nonexistent paths fall back to string comparison (NFC-normalized everywhere; case-folded on macOS only). Used by `remove`'s path fallback and `audit`'s ghost keys.
- **Container and album names are form-folded for uniqueness**: payload, note and media names are allocated unique on a lower-cased, NFC-normalized key, so staging dirs and unpacked containers stay collision-free on case- and normalization-insensitive filesystems — regardless of the filesystem the container was built on. Basenames colliding only in case or Unicode form get a `-N` suffix instead of silently overwriting each other.
- **unmarshal confines manifest paths**: a container's `file` and `origin` are manifest strings, not tar members, so tar's own traversal defense doesn't cover them. Both are joined and then re-checked *after* `filepath.Clean` (interior `..` collapses first): the staged `file` must stay inside the extracted container, and the `origin` write target must stay inside `collections/` unless `--scatter` is given. A foreign container therefore can't read/delete host files or write above the vault — the threat `--scatter` exists to gate.
- **kMDItemInformation is multi-valued**: multiple bookmark IDs are space-separated in this string field. `register repair`'s most reliable file-location strategy is an id Spotlight lookup (engine `query by:id`) — backend-agnostic, since the id xattr carries the bookmark ID regardless of which xattr layer the binder lives in.
- **The locator is the portability seam for file discovery**: `repair` does not query Spotlight directly; its lookups go through the locator (`locator.go`), which calls the engine's `query {by, value}` op (covering id, filename, and the groups/tags xattr layers). On Linux the engine grows a second implementation of the same op (e.g. `plocate`, `locate`, or the index) — no other code changes. The finder strategy is injectable in tests, so the ordering/fallback can be unit-tested without a live engine.
- **`REGISTER_BINDER` env var**: `add`, `remove`, and `promote` fall back to `ENV["REGISTER_BINDER"]` when `--binder` is not given. Useful for session-oriented workflows (e.g. `export REGISTER_BINDER=berlin-2024; register add *.jpg --kind image`). Explicit `--binder` always wins.
- **`promote --edit`**: after a successful promote (at least one record moved), the target note is opened in `$VISUAL` / `$EDITOR` / `vi` via `exec`, replacing the `register` process. Only fires when `--edit` is given and `promoted > 0`; a pure noop promote does not open the editor.
- **kMDItemProjects is a native array** (binary plist of strings — the macOS "projects" attribute). The engine reads it, adds the binder if missing (or removes it), and writes the array back — per element. So Spotlight matches a single binder with `== "<name>"`, and binder names may contain spaces; never a space-separated string.
- **kMDItemUserTags is natively multi-valued** (binary plist array of strings). The engine reads/writes it via Foundation's `URLResourceValues.tagNames`, which handles the binary-plist encoding and returns proper strings — no manual plist conversion. Finder's color suffixes (`\n<digit>`) are stripped on read.
- **Binder names may contain spaces**: kMDItemProjects is a real array, so `alpha project` is one element and matches exactly. Only the opt-in `tags` backend benefits from short, space-free names (spaces clutter Finder Tag chips — convention there: hyphens, underscores, or the colon-hierarchy `urlaub:2024:italien`). Not enforced; documented.
- **Backend migration is not automatic**: changing a record's `xattr:` field from one backend to another (e.g. `itemprojects` → `tags`) leaves the old xattr entry behind. `register audit` will eventually surface this as a ghost; resolve manually.
- **xattr failures are non-fatal**: when an xattr write fails, the Markdown write has still succeeded. The canonical layer is intact; the cache is missing for that file. `register refresh` can re-attempt later.
- **`tags` (annotation field) vs `binder`**: a record's optional `tags:` array is record-level annotation, never mirrored to xattr. Only `binder` participates in the xattr layer (ItemProjects or UserTags backend).

## Possible Future Refinements

- **Promote enrichment hooks** — `promote` is the single chokepoint between capture and curation, so it is the natural place to add validation, field normalization, or xattr-policy application as records cross into Markdown. Kept out of v1 to keep the gate a clean pipeline.
- **Typst metadata format**: with grubber 0.9 reading Typst `#metadata` blocks, ref records in `.typ` files become discoverable. The `register` tool could be extended to also *write* to `.typ` targets via `register write`.
- **Backend migration helper**: a `register migrate-backend` subcommand that, given a binder name and a new backend, cleans up stale xattr entries from the old backend and ensures the new backend is populated.
- **Auto-routing by `kind`** in `register add`: photographic kinds → tags backend by default, document kinds → itemprojects. Opt-in via config. Avoids `--xattr` repetition for users with clear use-case clusters.
- **Reverse audit at larger scope** — Spotlight-wide `mdfind` rather than directory scan, to surface files outside `notes_dir` carrying binder tags.

## Vocabulary

### Concepts

| Term | Meaning |
|---|---|
| Collection | The user's overall body of file-references managed by this tool. Conceptual term, not a specific data field. |
| Binder | A named bucket within the collection. Files belong to one or more binders by carrying their name in the YAML `binder:` field. The technical primitive. |
| Ref / Reference | A single record describing **one file**: its `id`, its `binder` set, and plain-text anchors. Canonical form: one index line; optionally mirrored as per-binder Markdown annotation blocks. |
| Bookmark | A record whose `binder` set is empty — a file tracked by identity alone, in no binder. First-class, never auto-deleted. Created directly by `register add` (no `--binder`) or left when `register remove` empties the set. |
| Index ("yellow pages") | The central JSONL store (`<notes_dir>/collections/inbox.jsonl`) holding every record's mandatory metadata, one per file. The authoritative directory the core reads directly. (The filename `inbox` is historical.) |
| JSONL store | Any `*.jsonl` under `collections/` — the live `inbox.jsonl` plus optional archives/imports. All are read-merged by `read_index`; only `inbox.jsonl` is written by `add`. |
| Annotation | An optional lean Markdown ref block (H3 = filename, YAML = `id` + a single `binder` + custom fields), linked to the index by `id`. One block per `(id, binder)` — the file's context in that binder. Carries custom metadata + prose. Created by `promote` / `add --md`. |
| Promote | Adding a Markdown annotation for a record via `register promote`. Additive — the index entry stays. |
| Binder file | A Markdown file in `<notes_dir>/collections/` named `binder_<name>.md`, holding annotations for one binder. The default `register promote` target — and the binder's default ordering file. |
| Ordering | A presentation record putting one binder's members into a sequence: a `type: ordering` config block plus sparse per-member overrides, in the binder file. The binder itself stays a pure set. Spec: [ORDERING.md](ORDERING.md). |
| Backend | The xattr layer chosen per record via the `xattr:` field: `itemprojects` (default), `tags`, or `none`. |
| ItemProjects | The macOS `kMDItemProjects` xattr — default backend for membership. Quiet, Spotlight-only. |
| Tags | The macOS `kMDItemUserTags` xattr (Finder Tags) — opt-in backend per record. Visible in Finder, syncs to iOS Files via iCloud Drive. |
| Bookmarks | The built-in id↔file manager (`bookmarks.go`). Keeps `~/.local/share/bookmarks.json` (id→blob) and delegates bookmark save/resolve + `kMDItemInformation` to the fileanchor engine. |

### `register` subcommands

| Subcommand | Purpose |
|---|---|
| `add` | Bookmark + write/update the index record (set-insert the binder). With `--binder` joins a binder; **without `--binder` creates a bookmark** (empty set — folds in the former `link`). `--md`/`--target *.md` also writes an annotation. `--xattr` selects backend. |
| `promote` | Adds a per-binder Markdown context block for a binder's records (binder- or record-granularity). Additive; the index entry stays. |
| `remove` | Set-deletes the binder from the index record and removes the binder name from the xattr layer; annotation blocks stay (`cleanup` reviews stale ones). An emptied set is a bookmark — the record stays. |
| `refresh` | Reconciles index state to the xattr layer (ItemProjects, Tags, or skipped for `none`). Idempotent. |
| `audit` | Read-only consistency report — dangling records, broken bookmarks, mismatched/ghost xattr in both layers. |
| `repair` | Re-binds a moved file's broken bookmark under its **unchanged** `id`; locates the file via Spotlight (id lookup first — backend-agnostic). |
| `rename` | Renames a binder across both stores (index + annotations), the ordering config, the canonical note file, and the per-record xattr backend. |
| `cleanup` | Interactive, human-judged review of layer drift — stale context blocks, unindexed annotations, bookmarks for review; per-item accept/reject. Never auto-deletes a record. |
| `reindex` | Rebuild the index from Markdown ref blocks so every record has an index entry. Idempotent; `--dry-run` previews. |
| `write` | Internal helper: idempotent append of one ref record per input. Used by `add`; available as a subcommand for advanced use. |
| `list` | List all binders with file counts (pure register, no Markdown scan), or files in one binder. `--inbox`/`--curated` filter by annotation status and read the optional Markdown layer on demand. `--paths` (with a binder) prints only resolved absolute paths, one per line (unresolvable members are omitted with a stderr warning); `--json` prints neutral JSONL per member (id, binder, filename, kind, aka, path/url — a member whose bookmark does not resolve carries `"broken":true` instead of a path) for a consumer to shape. |
| `resolve` | Forward lookup: a key (`id` or `aka`) → the file's path. `--record` prints id/binder/filename/aka/path. |
| `of` | Reverse lookup: a file → its `id`, `aka`, and collection membership. Reads the `id` stamped on the file (so it survives rename); falls back to a filename match, else reports the file as unmanaged. |
| `album` | Renders a binder as a static, self-contained HTML album (`sort`/`title`/`comment` from the annotation YAML, IPTC via Spotlight as fallback). An album *is* an `absolute` ordering. |
| `order` | Arranges a binder's members for presentation: `set` (config), `show` (resolved order), `move` (one member, one override write). Orderings are absolute-only — materialized `sort:` keys. Membership stays untouched. See [ORDERING.md](ORDERING.md). |
