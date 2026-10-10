# fileregister: Specification

This document specifies how **collections** (the practice of organizing files into named binders that share a common context, e.g. a project, a research topic, an event) are represented, managed, and queried in the grubber ecosystem.

It is the contract between three parts: the central JSONL index (the source of truth, with its optional Markdown annotations), the bookmark store (the macOS anchor that finds each file again), and the xattrs on the file (a derived cache that Spotlight indexes). The `register` CLI in this repository implements the read/write/maintain side. [matterbase](https://github.com/rhsev/matterbase), as a query-construction TUI, is one consumer; it can browse and filter collections through standard grubber filters but has no built-in collection management UI. Managing collections is a command-line concern.

## Concept

A user's *collection* is the totality of file references the tool manages. Within it, *binders* are named buckets; a file belongs to a binder when that binder's name is in the `binder` array of its index record. A single file can belong to multiple binders without being moved, copied, or modified in place. Membership lives in a central plain-text index, the *yellow pages*, which the tool reads directly; there is no opaque database and no daemon.

Throughout this document, **collection** refers to the user-level concept (organizing files into named buckets, the comparable feature to e.g. macOS Tags). **Binder** refers to the specific data primitive: the record field, the CLI flag, the named bucket itself.

Four layers carry the information, with different roles:

| Layer | Role | Authority |
|---|---|---|
| JSONL index, the central directory ("yellow pages") | Mandatory metadata: id, binder, filename, kind | Source of truth for membership |
| Markdown YAML annotations | Optional custom metadata + prose, linked to the index by `id` | Source of truth for that custom data |
| Bookmark store (`~/.local/share/bookmarks.json`, id → bookmark blob) | Anchor that finds the file again when its path changes | Anchor; `repair` re-binds it and `unmarshal` re-creates it under the same id, `refresh` renews a stale one |
| xattrs on the file (id, binder names, ★) | Cache for Spotlight | Derived; `refresh` rebuilds it from the index |

Every record lives in the **index**, one central JSONL store per notes directory (`<notes_dir>/collections/inbox.jsonl`). It is the authoritative directory of what belongs to which binder, and the core reads it directly (no scan-and-union step). The bookmark blob is not part of the index row; it lives in `bookmarks.json`, keyed by the record's id.

A record may *additionally* carry a **Markdown annotation**: a lean YAML block linked back to the index by `id`, under a heading, where the user keeps custom fields and prose. Annotations are optional and disposable; the index entry stands on its own. grubber-over-Markdowns is the optional rich-query layer (what matterbase browses), not the core's read path. See [Capture and promote](#capture-and-promote).

If a record and its xattr cache diverge, the record layer wins. Reconciliation always flows record → xattrs, never the other way around (a manual recovery path exists but is not automatic).

The record layer survives any filesystem; the bookmark store and the xattrs are macOS-specific, and the xattrs may be lost on transfer. The index can be rebuilt from Markdown ref blocks via `register reindex`, but the canonical direction is fixed for predictability.

## Capture and promote

Records have a lifecycle in two steps:

- **Capture.** `register add` materializes a record in the central index as one JSONL line. This is the authoritative entry, and it is always written; the index is the directory and is never skipped.
- **Promote.** `register promote` adds a lean Markdown annotation for a record (or a whole binder): a heading naming the file, plus a YAML block linked to the index by `id`, where the user keeps custom fields and prose. The index entry **stays put**. Promotion is additive curation, not a move.

This keeps one chokepoint, `promote`, where cross-cutting features (validation, enrichment, field normalization) can attach uniformly later, while the index remains a complete, side-effect-free directory.

### The annotation is optional, the index is not

A record's presence is decided by one place only: the index. Whether it *also* has a Markdown annotation is a separate, optional fact (surfaced by `list --inbox` / `list --curated` on demand).

- **JSONL index** = the directory. Central, machine-managed, always present.
- **Markdown annotation** = optional custom metadata + prose, linked by `id`, placed anywhere.

This is why the index is **central**: it is the single authoritative directory the core reads. Annotations are a layer drawn on top, never a substitute for the index entry.

### Additive by design

There is **no `demote`**, and promote never deletes the index entry. Reverting an annotation means editing or deleting the Markdown by hand; the index entry is unaffected. The invariant the tooling guarantees is **the index is complete**: every record, annotated or not, has exactly one authoritative index entry. `register reindex` restores that invariant if a record exists only in Markdown (e.g. legacy data).

## Data Model

### The record (canonical)

A record describes **one file** and its complete identity: a permanent `id`, an
optional set of binder memberships, and a few plain-text anchors. Its canonical
form is one JSON line in the index:

```json
{"type":"ref","id":"abc123","binder":["project-alpha","reading"],"filename":"brief.pdf","kind":"pdf"}
```

`binder` is a **set** (a JSON array) of the binders the file belongs to. The named
bucket itself is what we call a **collection** at the concept level. The two terms
split the labor. The user has *one* collection (the body of files managed by
this tool), inside which named *binders* organize membership. Adding to a binder
is a set-insert, removing is a set-delete; the record itself stays put.

Fields:

| Field | Required | Meaning |
|---|---|---|
| `type` | yes | Always `ref` for file-reference records |
| `id` | yes | Record identifier (random 9-digit). The file's **permanent identity**, assigned once; it travels with the file and never changes (repair re-binds the bookmark under the same id). |
| `binder` | optional | **Set (JSON array)** of binder names this file belongs to. An empty or absent set leaves the record [in no binder](#records-in-no-binder), tracked by identity alone. Add/remove are set operations on this one field. A new binder name may hold no comma, no control character and no leading or trailing whitespace, and is at most 255 bytes (see [Binder names](#binder-names)). |
| `filename` | recommended | Basename of the referenced file at add-time (e.g. `brief.pdf`). Plain-text anchor that survives bookmark breakage and xattr loss. Set by `register add`; not maintained when the underlying file is later renamed. |
| `kind` | optional | Coarse classification used for display and filtering (e.g. `pdf`, `image`, `mail`, `video`). Auto-detected from the file's extension on `register add` (jpg/png/heic/… → `image`, mp4/mov/… → `video`, eml/mbox/… → `mail`, md/markdown → `md`, typ → `typst`; unknown extensions fall back to the extension itself, no-extension to `file`). `--kind` overrides the auto-detect. For URL records, derived from the scheme (`https` → `web`, `x-devonthink-item` → `devonthink`, `message` → `mail`; unknown schemes fall back to the scheme itself). |
| `url` | optional | Locator alternative to the bookmark: the record references a URL (e.g. `x-devonthink-item://…`) instead of a file. Mutually exclusive with the bookmark; see [URL records](#url-records). |
| `aka` | optional | Array of human-chosen handles that also resolve to this record (resolution matches `id` ∪ `aka`). For referencing a file by name, e.g. behind a `milan://` URL via `register resolve`. Must be unique across the index. |
| `kept_tags` | optional | Set by register, never by hand. Binder names whose Finder tag was already on the file when it joined that binder with the `tags` backend (the user's own tags, which only happen to match a binder). `remove` and `rename` never take them off the file, and `audit` does not report them as ghosts. Absent means every binder tag on the file is register's. |
| `xattr` | optional | xattr backend for this **file**: `itemprojects` (default), `tags`, or `none`. One choice per record; determines which xattr caches the binder names (see [macOS metadata layer](#macos-metadata-layer-derived)). |

Besides `id`, `binder` is the only field mirrored to xattrs on the file. Labels that distinguish files from each other (status, version, importance) are annotation, not identity. They go into the member's block as `tags:` (or any other field), where grubber and matterbase query them, and the index carries none. `register write` ignores a `tags` key and says so; an old index line that has one keeps it, unread.

**One record per file.** A file in three binders is **one** index line with a three-element `binder` array. Duplicate `(id, binder)` registrations are structurally impossible. Membership is set membership, so adding twice is a no-op and there is nothing to dedupe.

### The index file

The index is `<notes_dir>/collections/inbox.jsonl`, one record per line. Every record lives here; the core reads it directly (`read_index`). Field order is irrelevant (JSON objects are unordered), and the line does **not** store `_note_file`. It is injected at read time and set to the exact `.jsonl` file the record was read from, so records are self-locating after a read and the on-disk lines stay portable.

The historical name `inbox.jsonl` is kept for compatibility; it is no longer an *inbox* in the staging sense (records are not consumed out of it), but the authoritative directory.

**Schema version.** `collections/SCHEMA` stamps the on-disk schema. The current version is **3**, in which `id` is always a JSON string and `binder` always a JSON array (an empty array is a record in no binder). The write path emits only this form; readers still accept the legacy scalar `binder` but warn about it, since any occurrence is a straggler to fix by hand. The field-by-field wire contract follows.

### JSONL wire contract (schema 3), normative

The section every consumer of `collections/*.jsonl` relies on. Producers and consumers today: **register** (read/write), **grubber** `--from-jsonl` (read), **matterbase** via grubber (read). Field *meanings* are above; this section fixes the *wire format*.

**File form.** UTF-8, one JSON object per line, LF-terminated. Lines up to **4 MB** are guaranteed readable (register's scanner limit; grubber accepts up to 16 MB, so stay within 4 MB). Blank lines are ignored; unparseable lines are preserved verbatim by register's rewriters and skipped (with a warning) by grubber.

**Record form (`type: "ref"`):**

- `id` is always a JSON **string**, unique across the index (one record per id).
- `binder` is always a JSON **array of strings** (the membership set); an empty array is a record [in no binder](#records-in-no-binder). The legacy scalar form is still *read* but warned about. The write path never emits it; fix stragglers by hand.
- `aka` is an array of strings; `filename`, `kind`, `url`, `xattr` are strings.
- **Custom keys are the user's sphere**: any key not named above may appear and MUST be preserved verbatim by rewriters (register keeps unchanged lines byte-identical).
- Objects with other `type` values (or none) are foreign records: readers skip them, rewriters pass them through untouched.

**Injected keys.** Keys starting with `_` never appear on disk. They are injected at read time (`_note_file` = source file; grubber also injects `_mtime`) and MUST be stripped before writing back; register's serializers do this automatically.

**Write discipline.** Rewrites replace the file atomically (temp file in the same directory + rename); appending whole new lines is allowed for new ids. Writers serialize across processes via a blocking advisory lock: `collections/.register.lock` for the index files, `bookmarks.json.lock` beside the bookmark db. Readers take no lock; the atomic rename keeps their view consistent.

**Not this schema:** the `register write` stdin stream. That is a *fold stream* (one membership per line, `binder` as a scalar, `_note_file` naming the target), which register folds into schema-3 records.

**Version stamp.** `collections/SCHEMA` holds the version as a single line. `3` = the rules above (2026-07: id stringified, binder array-only). Consumers may treat a missing stamp as "unknown, read tolerantly".

### Markdown entry contract (normative)

The entry unit: a heading + optional prose + one fenced ```yaml block. Producers and consumers today: **register** (promote/annotate/delete, writing), **grubber** (read), **matterbase** (section extraction).

**Heading levels (the layering rule):**

- **H3 (`###`) is the entry heading.** The only level writers ever emit (register: `### <filename>`, ordering config `### · <binder> (ordering)`).
- **H4 (`####`) is an optional entry level for hand-written entries.** Tools treat it as an entry; writers never emit it.
- **H1/H2 are document structure, never entry headings.**

**Reader tolerance is permitted, not required.** Section extractors resolve the *nearest preceding heading of any level*. This is deliberate, because surrounding same-level context can be worth showing; they MAY be tightened to H3/H4 if that tolerance ever causes trouble. grubber ignores headings entirely; blocks are found by their fences, and a heading is not data.

**Blank-line rule.** A blank line is REQUIRED between a structure heading (H1/H2) and a following entry heading, for readability and section extraction. (register's block deletion no longer depends on it. The delete regex takes only a single H3/H4 line directly above a block as the block's own heading, so a structure heading survives either way.) After H3/H4 no blank line is needed; the block or prose may follow directly.

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

The H3 heading is the filename (human recognition); the YAML block is the back-reference plus whatever custom fields and prose the user adds. A block names a **single** `binder`; it is the file's context *in that binder*. The same file may therefore carry **several** annotation blocks, one per binder, each with its own custom fields and prose (an invoice filed under `invoices-2024` with an amount, and under `project-a` with a deadline). The index stays one record per file; the Markdown layer is per-binder context, linked back by `id`. Annotations never replace the index entry; they are the optional, disposable custom layer. `register promote` (or `add --md`) creates them; grubber-over-Markdowns reads them for rich queries (with `--explode binder --merge-on id,binder`; see [matterbase Touchpoints](#matterbase-touchpoints)).

### Binder names

A binder ends up on files as a Finder tag or a `kMDItemProjects` entry, so its
name follows fileanchor's label rule, which holds for every platform's engine
(fileanchor's PLATFORMS.md): **no comma** (Linux keeps tags as a
comma-separated list), no control characters, no leading or trailing
whitespace, at most 255 bytes. A binder may also not be named `★` alone,
because that is the managed marker, and removing such a binder would strip the
marker from files still in other binders. Spaces, colons (`2024:berlin`) and
any other Unicode are fine.

Two names name the same binder when they are equal in NFC (`index.SameBinder`). A name typed by hand or pasted from the Finder can stand in a note decomposed (NFD), looks the same, and is found by grubber's filters. Case is kept, so `Trip` and `trip` are two binders. Readers and writers compare alike, or a reader would find a block the writer then misses.

The rule is checked where a name is introduced: `add --binder` (and
`REGISTER_BINDER`), the new name of `rename`, `write`, and `unmarshal`, which
refuses the whole container before importing anything. Existing binders are
not checked, so one that breaks the rule can still be listed, removed from,
and renamed away.

### Records in no binder

A record whose `binder` set is **empty** (absent or `[]`) is **in no binder**, tracked by identity alone and resolvable by `id` or `aka` (e.g. behind a `milan://` URL). The short form is a **loose** record. This is a regular state, not a leftover. Binders are an *optional* facet of a record, not its essence; a file can live in the register by reference only.

A record in no binder arises two ways, indistinguishable and both legitimate:

- **Created directly.** `register add <file>` with no `--binder` registers the file in no binder (id + optional `aka`, empty set, no binder xattr, no ★).
- **Emptied by removal.** `register remove` deletes a binder from the set; when the last one goes, the record remains in no binder. Removal never destroys the record or its annotations.

**A record in no binder is never deleted automatically.** The record disappears only by explicit human choice, with `register forget`. Re-adding a binder is a plain set-insert on the existing record; there is nothing to "reclaim" and no duplicate to avoid.

Records in no binder are inert for membership queries: `grubber -f type=ref -f binder=X` never returns a record that lacks `X` in its set. Such a record surfaces only in the per-file view (no binder filter) or by `id`/`aka` lookup.

In Markdown, an annotation block must be preceded by a heading at any level. matterbase uses that heading to display the record's context in the preview pane. `register promote` writes H3 (the filename) by convention; matterbase reads any level.

### URL records

A `type: ref` record may reference a **URL** instead of a file, such as a DEVONthink item link (`x-devonthink-item://UUID`), a web page, or a `message:` link. The bookmark was never the essence of a record; it is the macOS anchor for the *file* locator. A record either points to a file (a **file record**) or to a URL (a **URL record**). A URL record is the same record with a different locator. Identity (`id`), membership (`binder`), aka, annotations, and promote all work identically, and `register resolve` returns the URL, so `open "$(register resolve <key>)"` works the same for both (milan needs no change).

What does **not** apply is the entire macOS side: no bookmark, no xattr cache, no ★, no Spotlight, no `repair`, no `of`. URL records are implicitly `xattr: none`, living in the index and annotations only, with the same standing `none`-backend files already have. `refresh`/`audit`/`repair` skip them (counted); `marshal` carries them as pure data (no file payload). Created with `register add --url <URL> --binder <name> [--label <name>]`; `filename` holds the optional display label (its human-recognition role; the repair-anchor role is moot). Removed with `register remove <url> --binder <name>`. Idempotent over `(url, binder)`.

### macOS metadata layer (derived)

When `register refresh` (or `register add` at write time) runs, the referenced file receives:

| xattr / metadata field | Content | Set by |
|---|---|---|
| `com.apple.metadata:kMDItemInformation` | Record `id` (multiple ids space-separated, stored as a binary-plist string, because Spotlight ignores a raw multi-id string) | The `Bookmarks` module on `add`/`repair`, via the **fileanchor** engine; `refresh` restores a missing id |
| `com.fileregister.id#S` | Record `id` (single value), the cross-device file→record key | The `Bookmarks` module on `add`/`repair`; `refresh` backfills |
| `com.apple.metadata:_kMDItemUserTags` == ★ | The managed marker (U+2605), the one status tag fileregister owns | `add` (any backend); removed by `remove` on last membership; `refresh` backfills |
| `com.apple.metadata:kMDItemProjects` | Binder names, as a real array (Spotlight matches per element; names may contain spaces) | default backend (`itemprojects`) |
| `com.apple.metadata:_kMDItemUserTags` == \<binder\> | Binder name (Finder Tag, binary plist array) | Tags backend, opt-in per record |

**Copies.** `cp` and Finder's Duplicate copy the id xattrs, so a copy carries the original's id. A file's id counts as its own only if the id's bookmark does not resolve to a *different* existing file; a copy is therefore not taken for the original by `add` or `remove`. Adding a copy gives it a new id, which replaces the copied one on the file. An id whose bookmark no longer resolves still counts; that is how a moved file is recognized. `add` reads the id from `kMDItemInformation`, else from `com.fileregister.id#S`. A file that carries no id at all, because it could not be written (a locked or read-only file, a volume without xattrs) or was stripped, is looked up among the records with its filename; the one whose bookmark resolves to this very file is it. `add` warns when it cannot store the id on a file.

The id layers (`kMDItemInformation` plus the syncable `com.fileregister.id#S`) are the id cache, derived from the record's id; ★ marks management; the Projects and UserTags layers are the membership cache. The membership backend is chosen **per file** via the one `xattr:` field on the record.

Which command writes which of these layers, and how, is one table: [WHO-WRITES-WHAT.md](WHO-WRITES-WHAT.md).

**Cross-device id.** `kMDItemInformation` is the local file→id store, but the Apple `com.apple.metadata:kMDItem*` namespace is **stripped by iCloud Drive** even with a `#S` (syncable) flag (verified on macOS 26.4, re-verified 2026-09 between 15.8 and 27.2). A **custom** `#S` name survives, so the id is mirrored to `com.fileregister.id#S`, the one xattr that lets a refreshed/AirDropped file be mapped back to its record by id on another Mac. It belongs to the id cache, is written regardless of the binder-name `--xattr` backend (even `none`), and is single-valued (one file ↔ one id). The `#S` is part of the stored name; read it back with the suffix. (It also has standalone **local** value, as a file→record mapping for `reindex` / `register of <file>` without `bookmarks.json`.) Transports that strip all custom xattrs (e.g. Resilio) lose it; reconcile then falls back to filename (see [FINDINGS-attributes.md](FINDINGS-attributes.md)).

**Managed marker (★).** Every managed file carries one Finder Tag, ★ (U+2605), stored in `_kMDItemUserTags` (with underscore). It is written on first membership by `add`, **backend-independent**, so even `--xattr none` files get it, because ★ marks *membership*, not the binder-name cache. `remove` drops it only when the file leaves its **last** binder (checked against the index across all binders); a file in two binders keeps ★ until both memberships are gone. `refresh` backfills it on every member; `repair` re-applies it to a relocated file. Whether ★ survives a sync service depends on the service (see [FINDINGS-attributes.md](FINDINGS-attributes.md)); `register refresh` puts it back after one that drops it. ★ is a recognition mark, part of the derived cache and not an anchor. It shows in Finder which files fileregister manages, and `mdfind 'kMDItemUserTags == "★"'` lists them (query key without underscore). There is exactly one status tag. Binders never enter the tag namespace (that stays the opt-in `tags` backend); see [FINDINGS-attributes.md](FINDINGS-attributes.md).

#### Backend choice

| Backend | xattr field | Where visible | Trade-off |
|---|---|---|---|
| `itemprojects` (default) | `kMDItemProjects` | Spotlight only (`mdfind`) | Quiet; no clash with Finder Tag workflows; not surfaced on iOS |
| `tags` | `kMDItemUserTags` | Finder chips, iOS Files sidebar, Spotlight | Visible cross-device via iCloud Drive; mingles with user's other Finder Tags (a tag of the same name the user had set before is recorded in `kept_tags` and never removed) |
| `none` | (no binder xattr) | (none) | Binder names in the index only; no binder xattr; minimal residue |

The choice is **per file** (one backend per record), not per binder. All of a file's binder names are cached through the same backend. Use cases vary. Photo files benefit from `tags` (Finder Cover Flow, iOS visibility); documents work well with `itemprojects` (quiet, Spotlight only); archival material may need `none` (binder names in the index only). A file's backend can be changed later; `register audit` surfaces any stale entries left in the old layer.

This layer makes a file findable by Spotlight. Finding a moved file is the bookmark's job; when the bookmark no longer resolves, `register repair` searches Spotlight for the id in `kMDItemInformation` and re-binds the bookmark. On filesystems that do not preserve xattrs (FAT32, many cloud sync services, rsync without `-E`), the layer is lost on transfer, but the index remains intact and `register refresh` rebuilds the xattrs from it.
## Record Placement

A new `add` operation **always** records to the index, and **may** additionally write a Markdown annotation:

1. **Index (always).** The authoritative record is appended to `<notes_dir>/collections/inbox.jsonl`, auto-created on first add. An explicit `--target some-file.jsonl` chooses a different index file (rarely needed).
2. **Annotation (optional).** `--md` also writes a lean annotation to `<notes_dir>/collections/binder_<sanitized>.md` (the same note `promote` would use). `--target some-note.md` names the annotation file explicitly. `--md` and `--target` are mutually exclusive.

`<notes_dir>` is resolved from the same env vars as the other subcommands: `GRUBBER_SET` (via grubber's config) takes precedence, otherwise `GRUBBER_NOTES`. The `collections/` subdirectory is created automatically. Annotation notes use the `binder_` prefix to disambiguate from unrelated notes and apply light sanitization (forward slashes, null bytes, and control characters become hyphens; unicode and colon-hierarchy names like `2024:berlin` pass through unchanged).

So `add --md` is `add` followed by `promote` in one step: the index entry plus its annotation. Capture-then-promote and add-`--md` are the deferred and immediate forms of the same curation. Both always leave the authoritative entry in the index.

### One write target, many index files

`collections/` may hold **more than one** `.jsonl` over time (an `archive-2024.jsonl`, an imported set, a merged sub-project). Writing and reading are asymmetric:

- **One write target.** `register add` (without `--target`) **always** appends to `collections/inbox.jsonl`. New records have one unambiguous home.
- **Many read sources.** `index.ReadAllRefs` reads every `*.jsonl` in `collections/` and concatenates them. Archives and imports are queryable and editable in place, but new adds never land in them.

Every command reads **all** of them or stops. An index file that cannot be read, or a line that is not exactly one JSON object, is an error naming the file and line. A command working from a partial index would report its missing records as gone, and `add`/`reindex` would mint duplicates of them. In-place rewrites edit only `type: ref` records; blank lines, foreign record types and unparseable lines stay byte for byte.

Editing stays unambiguous regardless of file count. Each record's injected `_note_file` names the specific source file, and in-place edits (`remove`, `rename`, `repair`, `cleanup`, `reindex`) rewrite exactly that file.

### The core read path

The core (`list`, `audit`, `refresh`, `repair`) reads the index directly via `index.ReadAllRefs`, that is, the `*.jsonl` files under `collections/`. It does **not** scan Markdown. The annotation layer is read where a command has to know which blocks exist, and always through grubber. `readAnnotations` is one run of `grubber extract --no-config <notes> -b --no-fill --inherit= --extensions=.md -f type=ref`. A block is a record here and is read as it stands, with nothing passed down from the note's frontmatter (a note's `url:` or `sort:` would otherwise become every block's), and a note without blocks is no record. `album` reads the same blocks *with* inheritance (no `--inherit`), because there the header is the point. Every ref block therefore carries `type` and `binder` itself, as `promote` writes them; read without inheritance, a block that took either from the header would not be found. Every grubber call carries `--no-config`. The user's grubber config and `GRUBBER_*` variables are for their own queries and do not steer what register reads (a default filter there once hid blocks from `rename`, which then left them on the old binder). Notes parked under `collections/import/` and hidden files are not annotations. register only writes notes; it has no reader of its own.

- **Editing commands** (`rename`, `cleanup`, `annotate`, `aka`, `reindex`) read annotations, so a record's `binder` set is updated in both the index and any per-binder annotation blocks, a block can be found to edit, a handle change is checked against the blocks that carry it, and the index can be rebuilt. (`remove` deliberately does not. It set-deletes in the index only, and `cleanup` reviews the stale blocks.)
- **`marshal`** reads them to pack each record's notes into the container.
- **`list --inbox` / `list --curated`**: explicit on-demand filters that consult the Markdown layer to determine annotation status. The default `list` (no flag, no binder) is pure-register and never reads Markdown.
- **The query layer** (grubber over the Markdown notes): for matterbase's queries, for `register album`, which joins a binder's curation into its lines, and for `register order`, which reads the binder's note.

All of these need grubber at runtime; whenever the Markdown comes into play, grubber reads it. Numbers in grubber's JSON are decoded as their digits (`UseNumber`). An id written unquoted is a YAML integer, and as a float it would not match its record. Writing a note stays register's own (grubber only reads).

grubber reads the notes in two ways, always with `--no-config`, so the user's grubber config does not change what register sees. Records are read as blocks stand (`-b --inherit=`) by `annotate`, `aka`, `cleanup`, `forget`, `marshal`, `reindex`, `rename`, `list --inbox/--curated` and `order` (the binder's note). The album reads them with the note's frontmatter inherited into every block (`-b`). A writer parses the one note it edits itself (`promote`, `annotate`, `order move`, `rename`, `forget`, `album --title`), because changing a block needs the text around it, and grubber does not return that.

A `type: ref` block hand-written into a project note is therefore part of the *annotation* layer; `register reindex` pulls such records into the index so the core sees them too.

### Filesystem layout

```
<notes_dir>/
├── collections/
│   ├── inbox.jsonl            ← the index; every record, one per line (the write target)
│   ├── archive-2024.jsonl     ← optional extra index file; read-merged, never written by `add`
│   ├── binder_contract.md      ← optional annotation note (promote target)
│   └── binder_project-alpha.md ← optional annotation note
├── meetings.md             ← may contain type:ref annotations; `reindex` pulls them into the index
├── project-alpha/
│   └── kickoff-notes.md    ← same; scattered annotations stay discoverable to the query layer
└── …
```

The `collections/` directory holds the index (`*.jsonl`) the core reads and writes, so its location is fixed by convention. Markdown annotations remain locatable by content anywhere under `notes_dir`; the query layer and `reindex` find them by scanning, not by path.

### Code layout: the index-core seam

The layering above is also enforced in the code. The core (JSONL index,
bookmark store, fileanchor engine client, atomic writes) lives in its own
Go package, `internal/index`; the Markdown layer and the CLI commands
sit above it in `package main` and import it. Since a Go package can never
import `main`, the dependency direction is compiler-checked: **nothing in the
core can reach the Markdown surgery.** The index therefore stays readable and
writable without any annotation machinery, as the two-layer design promises,
and a future second consumer (a library, a GUI, a stripped CLI) gets an entry
point that cannot pull the Markdown layer in.

## CLI

Collections are managed with the `register` binary in this repository, a Go CLI that dispatches to one `cmd_*.go` per subcommand. It follows the same shell-tool conventions as grubber (JSONL stdio where applicable, no daemons, no shared state beyond the bookmarks JSON file). This is the canonical interface for collection work. matterbase does not invoke it; collection lifecycle is a shell-level concern. matterbase's only relationship to collections is querying them via standard grubber filters (see [matterbase Touchpoints](#matterbase-touchpoints)).

No watchers, no auto-sync. Reconciliation is explicit, invoked when the user wants it.

### `register add <file>... [--binder <name>] [--aka KEY] [--kind K] [--md] [--target F] [--xattr BACKEND]`

(URL form: `register add --url <URL> [--binder <name>] [--label NAME] [--aka KEY]`, with no bookmark and no xattr; see [URL records](#url-records).)

The user-facing entry point for registering one or more files. With `--binder` the files join that binder (set-insert); **without `--binder` each file is registered [in no binder](#records-in-no-binder)**: an empty `binder` set, no binder xattr cache, no ★. This replaces the former `link` subcommand; a binderless add does what `link` did. The full flow:

1. **Register** every file with the bookmark functions in `internal/index/bookmarks.go`. This creates the `id`, stores the bookmark in `~/.local/share/bookmarks.json` and writes the id to the file (`kMDItemInformation` and `com.fileregister.id#S`). For batches this goes through `index.AddMany`, which loads and saves the bookmark DB **once** for the whole batch. The engine's save still runs per file (each file needs its own blob), but the DB write is O(1) instead of one full rewrite per file
2. **Write the index record** via `index.JSONLWriteMany`, which reads the target (`collections/inbox.jsonl`, or the `--target *.jsonl` index file) **once**, indexes existing records by `id`, then for each file **set-inserts** the binder into the matching record's `binder` array (or creates a new record, with an empty set when no `--binder` was given), and writes the target **once**. One record per file; a binder already in the set is a no-op, so no duplicate can arise. The chosen backend is recorded as `xattr:` in the record (omitted when default `itemprojects`)
3. **Write the annotation (optional)**. With `--md` (→ `collections/binder_<name>.md`) or `--target *.md`, a lean annotation is also written for each new record via `promoteRecords` (idempotent). The index entry from step 2 stays authoritative
4. **Write the xattr layer** for each file according to the chosen backend, through `index.XattrBackendAdd`, and set ★ on each file that joined a binder:
   - `itemprojects` (default): the binder name in `kMDItemProjects`
   - `tags`: the binder name as a Finder tag (`kMDItemUserTags`)
   - `none`: no xattr write

The `--xattr` flag selects the backend; the default is `itemprojects`. The index record is written first; annotation and xattr failures don't block it.

The `kind` field is auto-detected from the file's extension (e.g. `.jpg` → `image`, `.pdf` → `pdf`, `.eml` → `mail`). Use `--kind` to override when a finer classification matters (e.g. `--kind invoice` for a PDF that's an invoice). Pass `--kind ""` to suppress the field entirely.

Idempotent. Re-adding the same file to the same binder is a set-insert of a value already present, so there is no duplicate record and no duplicate xattr entry. If the file was previously **removed** from that binder, re-adding re-inserts it into the existing record's set; if the record was [in no binder](#records-in-no-binder) (empty set), it gains its first binder. Either way there is one record, edited in place, and nothing to reclaim.

**Batch adds.** Multiple file arguments (`register add *.pdf --binder x`) are processed in one pass. Batch-wide options (`--binder`, `--kind`, `--target`/`--md`, `--xattr`) apply to every file. The per-file option `--aka` only makes sense for a single file and is rejected when more than one file is given (`kind` defaults to the extension-derived value). Files that don't exist are reported and skipped; a per-file bookmark save failure is reported and skipped; the rest still go through. The command exits non-zero only when nothing at all was written. The batch I/O is linear, not quadratic: one bookmark-DB load/save and one target read/write for the whole batch (see steps 1 and 2 above).

### `register promote --binder <name> [--target F] [--id ID] [--edit]`

Adds a lean Markdown annotation for a binder's records. The index entry **stays put**; promote is additive. Two granularities, one engine:

- **Binder annotation** (default): for every index record in `--binder <name>`, a lean `type: ref` block (H3 = filename, YAML = `id` + `binder`) is written into a Markdown note (default `<notes_dir>/collections/binder_<sanitized>.md`, or `--target F`).
- **Record annotation** (`--id ID`): only the single record with that id is annotated.

Flow per record: read it from the index (`index.ReadAllRefs`), then append a lean annotation block to the target. The block is keyed by `(id, binder)`; it is the file's context in *this* binder. Idempotent. A record whose `id` already has a block for this binder in the target is skipped (counted as `noop`). The index is never modified.

**Reattaching a forgotten block.** Before writing a new block, promote looks for one that `forget` left behind: a `type: ref` block for this binder **without an id**, under a heading that names the file (the heading promote wrote). If there is exactly one, it gets the record's id back, fields and all, instead of the file getting a second block. Two or more such blocks cannot be told apart, so none is touched and a new block is written. `add --md` (and `add --target <note>.md`) goes through the same path.

The target must be a `.md` file. There is **no `demote`**, and promote never deletes the index entry; reverting an annotation is a manual Markdown edit (see [Capture and promote](#capture-and-promote)). Future cross-cutting features (validation, enrichment, field normalization) belong in `promote`, the one point every annotation passes through.

### `register annotate <binder> <id|aka> [--set k=v]… [--unset k]… [--prose <text>|-]`

Edits an **existing** annotation block. It is the tool form of "the user edits it directly" (see [Record Identity](#record-identity)), built for GUI clients (binderview's entry inspector) that must never grow a second Markdown engine. `annotate` never creates a block (`promote` stays the one creation chokepoint) and never touches the index.

- `--set k=v` replaces the field in place (an array-valued field collapses to the scalar, its `- ` items go with it) or appends it; `--unset k` removes field and items, absent = no-op. Values are scalars. Numerals and booleans are written plain (`amount=129.50` means a number; leading-zero numerals stay quoted strings), everything else follows promote's quoting.
- **Reserved keys are refused**: `id`, `type` (identity), `binder` (membership, see `add`/`remove`), `aka` (identity handle, see `aka --add/--remove`), `sort` (ordering, see `order move`). Identity and membership have their own verbs; `_`-prefixed keys are injected, never stored.
- `--prose` replaces the section's prose (every line of the section that is neither the heading nor a fenced `yaml`/`yml` block) with the given text (`-` reads stdin, empty clears), rewritten canonically between heading and block. The heading and the blocks themselves stay byte-identical. This is a **deliberate policy change**. register historically never touched prose; `annotate --prose` may, on explicit request, because register is the family's only Markdown writer and the alternative would be the second engine the rule exists to prevent.

Blocks are matched over id ∪ aka like every other block lookup; all matching `(id ∪ aka, binder)` blocks are edited, wherever the note lives. A block without a preceding heading refuses `--prose` (no section to rebuild). Writes are atomic, one write per file, fields and prose in the same pass.

### `register aka <id|aka> [--add <handle>]… [--remove <handle>]…`

Edits a record's `aka` handles after add time. `annotate` refuses `aka` as an identity field; this is its verb. `add --aka` sets a handle once; on an existing record it only backfills an absent `aka` on a new membership, and otherwise says so and points here.

- The record is resolved over id ∪ aka; the edit lands on the **index** record (every JSONL file holding the id), so it applies to all binders at once.
- `--add` refuses a handle that resolves to another record (uniqueness across the index) or is the record's own id. A handle already on the record, or a `--remove` of one it does not carry, is a no-op, not an error. The same handle in `--add` and `--remove` is refused.
- **Remove cleans the record's blocks.** A block is the record's when its `id:` is the record's id or one of its handles, or it has no `id:` and reaches the record through a handle. In each such block carrying a removed handle, the handle leaves the `aka:` list (an emptied list drops the key; a later re-use of the handle cannot match a stale block), and a block whose `id:` *was* the handle (or that had none) gets the real id, inserted after `type:` where missing. An aka-only block that lost its handle would be a record without a file; an aka in the `id:` slot would make `reindex` mint a phantom record. All other lines stay byte-identical. Markdown is written before the index; a block that cannot be given the id aborts before the index changes.
- **Ambiguous blocks refuse.** A block carrying a changing handle that is *not* the record's (another record's id, or an id no index record knows) is named and the command refuses, in both directions. Remove would hand it to nobody; add would silently adopt it. `--add` otherwise does not touch Markdown, because the record's blocks already match by id.

### `register refresh [--dry-run]`

Pushes index state to macOS metadata. Reads every `type: ref` record from the index via `index.ReadAllRefs`, resolves the bookmarks in one batch, and for each file record whose file resolves:

- renews the bookmark under the same id when macOS reports it as stale
- writes the id: `com.fileregister.id#S` always, and `kMDItemInformation` where the id is missing there (added, never set)
- sets ★ when the record is in at least one binder
- ensures each binder name in the layer of the record's `xattr:` backend:
  - `itemprojects` → `kMDItemProjects`
  - `tags` → `kMDItemUserTags` (Finder tags)
  - `none` → no binder xattr

URL records are skipped (they have no file). A file in the Trash or a backup, and a file that several records resolve to, are skipped too and left to `register audit`. `--dry-run` writes nothing and lists what it would write.

Exit status is 1 when a metadata write failed, a bookmark is broken or a file is missing, that is, anything that needs the user.

Direction is always index → xattr. Records are the source; xattr is the cache. The bookmark is the anchor, not part of the cache. `add`, `repair` and `unmarshal` write it; refresh renews one that still resolves but that macOS reports as stale, under the same id, and leaves a broken one to `repair`. Both id carriers derive entirely from the record's `id`, so refresh puts the id back on each resolved file in both: `com.fileregister.id#S` (the copy that travels), and `kMDItemInformation` (the one Spotlight searches) when the id is missing there. iCloud Drive strips it, and without it `repair` cannot find the file by its id. The id is added, never set. Other ids on the file stay; deciding which ids are its own is left to `add` and `repair`. An id already present is not written again (the engine answers noop).

Reports:
- Records whose bookmark does not resolve (candidates for `register repair`)
- Records whose referenced file no longer exists (dangling, for `register audit` review)
- Count of bookmarks renewed because they were stale, when any
- Count of `none`-backend records (skipped intentionally)
- Count of records skipped in the Trash or a backup, or on a file shared with another record, when any
- Count of ids restored to `kMDItemInformation`, when any (`--dry-run` lists them)

Idempotent. Safe to run repeatedly.

### `register repair [--interactive]`

For records with broken bookmarks (typical after cross-volume move or transfer through xattr-unfriendly path):

0. A record whose bookmark recorded a path on a volume that is **not mounted** is skipped, not searched. Its file is not missing, and searching would find a copy (a backup, a clone) and bind the record to it for good. fileanchor reports that path (`last_path`) and never mounts a volume while resolving.
1. Attempt to relocate the file, in this order:
   1. `mdfind 'kMDItemInformation == "*<id>*"'`: backend-agnostic; finds the file by the record id preserved in `kMDItemInformation` (most reliable when xattr survived the transfer)
   2. `mdfind 'kMDItemFSName == "<filename>"'`: by the YAML `filename` field; exact on-disk basename match, works even when xattr is gone
   3. `mdfind` on the appropriate xattr layer (`kMDItemProjects` or `kMDItemUserTags` per record) restricted to files whose basename matches `filename`
   Candidates in the Trash or a backup (`.Trash`, `.Trashes`, Time Machine), and files that are another record's (they carry a registered id of their own), are ignored.
2. If found: re-bind the bookmark **under the file's existing id** via `index.Rebind` (a fresh blob stored under the same id), then refresh the id xattrs, the binder xattr layer and, for a record in a binder, ★. The id is the file's **permanent identity**. Only the broken blob is renewed, so the id goes nowhere new. If the file was renamed, the index line's `filename` takes the new name, since it is the label `list` and `album` show; note headings stay as they are. This happens **without asking only** for a single candidate from stage 1 (the id) on the volume the file was on. A hit by name, or on another volume, is some file that may or may not be this one.
3. Otherwise: list the candidates; with `--interactive`, prompt to choose (also for a single one)
4. If not found: report; in `--interactive` mode prompt for an explicit path

Exit status is 1 while a record stays unresolved or a repaired file is missing metadata, so a monitor can tell. Records on a volume that is not mounted do not count, since they need only the volume.

### The bookmark store is shared

`~/.local/share/bookmarks.json` is a plain map of id → Foundation bookmark
blob, and it is the one piece of state outside the notes directory. Two rules
hold for anything that writes it:

- **Keys are minted ids.** Nothing else can ever be looked up, because every
  reader addresses the store by id. A key of any other shape is dead weight;
  `register audit` reports it as *malformed*.
- **Load, modify, save the whole map** under `LockBookmarks`, and never drop a
  key you did not understand. Every writer saves the map back in full, so
  discarding an unfamiliar entry on read deletes it on the next write. `LoadDB`
  therefore loads verbatim and refuses a damaged file rather than treating it
  as empty.

Tests must not reach it. `bookmarkFile()` reads `HOME` on every call, so a
`TestMain` in each test package points `HOME` at a throwaway directory for the
whole run. Otherwise any test touching `SaveDB` rewrites the developer's own
store with its fixture.

It also needs backing up alongside the notes directory. The notes hold the
records; this file holds the anchors that find their files. Without it, no id
resolves to a file.

### `register audit`

Read-only consistency report. Three directions:

- **Record → File**: records whose bookmark does not resolve, or whose target file lacks the expected xattr value in the record's chosen backend (ItemProjects, UserTags, or no check for `none`). Records in no binder are included, with a resolution check only and no xattr expectations.
- **File → Record**: files in scope that carry a binder name in their binder xattr (`kMDItemProjects` or Finder tags) but no corresponding `type: ref` record. Scans both `kMDItemProjects` and `kMDItemUserTags` to catch ghost entries regardless of backend (e.g. record was deleted, xattr manually edited, `refresh` ran with stale state)
- **Bookmark → Record**: entries in `~/.local/share/bookmarks.json` judged against the index. Needs fileanchor **1.2.0** for the `last_path` of a failed resolve; without it the dead verdict is withheld and every unresolvable entry is reported as unreachable, because a gone file could not be told from an absent volume. The capability is measured, not read off a version number. The bookmark store is the one store the other two directions cannot see into. They start from records, so an entry no record claims is invisible to them, and the store would only grow. One batch resolve covers it; Spotlight is asked only about entries that failed to resolve. Four outcomes: **orphan** (resolves, no record claims the id), **broken** (does not resolve, but a file still carries the id, so `register repair` can re-bind it), **dead** (neither, so there is nothing to repair), **malformed** (the key is not a minted id at all, which a foreign writer on the shared store can leave behind). Skipped under `--binder`, since the bookmark store has no binder and an orphan has no record to filter by. Judged against *every* ref record, including those in no binder, since a record kept in no binder on purpose is a record like any other.

Two more findings on the record side: a file whose bookmark followed it into the **Trash or a backup** (a bookmark tracks its file wherever it moves), and a **shared file** that several records resolve to (two identities on one file, the trace a bad re-bind leaves). `refresh` marks neither kind (it would re-mark a discarded file, or write the records' ids onto the file in turn).

Output is plain text. Decisions stay with the user.

### `register forget <id|aka>... [--dry-run]`

Takes a record out of the register for good. It is the one command that deletes a record. Only a record in **no binder** qualifies; a member leaves its binders with `remove` first, which also takes the membership marks off the file. Every key must name such a record, or nothing happens.

The steps run in this order, so that an interruption leaves nothing `reindex` could bring back:

1. **The notes**: every ref block whose `id:` names the record (its id, or a handle written in the id slot) loses that line. The block stays (heading, fields, prose), since what a note says belongs to the note. Without the id, `reindex` no longer rebuilds the record from it, and `promote` can reattach it should the file return (see above).
2. **The index**: the record's line, in the index file it lives in.
3. **The bookmark store**: its entry in `~/.local/share/bookmarks.json`.
4. **The file**, when its bookmark still resolves: the id leaves `kMDItemInformation` (other ids stay) and the `com.fileregister.id#S` copy, so adding the file again gives it a fresh id. A failure here is a warning; the record is gone either way.

`--dry-run` lists what would go. URL records have no bookmark and no file; their index line and blocks go the same way.

### `register remove <file> --binder <name>`

The inverse of `register add`. It removes a file from a binder without destroying its annotations. It reads only the index (`index.ReadAllRefs`); Markdown context blocks are never touched.

For the record whose `id` matches the file's id:

1. **Set-delete the binder** from the `binder` array in the index line. The record's other binders and all annotations stay intact. When the last binder is removed, the record remains [in no binder](#records-in-no-binder).
2. **Remove the binder name** from the appropriate xattr layer per record's `xattr:` backend (`kMDItemProjects` or `kMDItemUserTags`; `none`-backend records have no xattr to touch). The backend is taken from the index record. A Finder tag listed in the record's `kept_tags` is the user's and stays. When the file has no binder left, its ★ comes off.

Idempotent. Removing from a binder the file isn't in is a no-op.

Per-binder annotation blocks for the removed membership stay where they are. They are then stale against the index, and `register cleanup` reviews them (bare blocks are one keystroke to drop there; annotated ones get a human decision).

Note: this does **not** remove the bookmark blob or the record. When the last binder is removed the record remains [in no binder](#records-in-no-binder), addressable by `id`. Re-adding a binder is a plain set-insert on that same record.

### `register rename <old> <new>`

- Rewrite every record with `binder: <old>` to `<new>` in **both** the index (`index.ReadAllRefs` → `index.JSONLRenameBinder`) and any annotation copies (`readAnnotations` → `mdRenameBinder`)
- Carry the ordering layer along. An existing `type: ordering` block (written by earlier versions) is rewritten too (it names its binder in a field of its own, so a rename that skipped it would leave a block pointing at a binder that no longer exists), and the canonical note file `collections/binder_<old>.md` is renamed to `binder_<new>.md` (when the new name's note already exists, the `--merge` case, both stay and rename says so)
- For each referenced file (once per `id`, backend taken from the index): remove `<old>` and add `<new>` in the per-record xattr layer (`kMDItemProjects` or `kMDItemUserTags`, or skip for `none`)

Not atomic across many files. A subsequent `register refresh` reconciles any residue.

### `register cleanup [--interactive] [--prune] [--dry-run]`

Human-judged review of drift between the layers. Reads both stores (`index.ReadAllRefs` + `readAnnotations`); writes only on user confirmation. It **never deletes a record automatically**; a record in no binder is not cruft (Principle 4).

Surfaces, for the user to decide:

- **Stale context blocks**: a per-binder annotation block whose binder is *not* in the index record's `binder` set (e.g. an annotated block kept by `remove`). Either delete it (the context is obsolete) or re-add the binder (the membership was dropped by mistake).
- **Unindexed annotations**: a `type: ref` block whose `id` has no index record at all (legacy data, hand-written refs). Usually resolved by `register reindex`, which pulls them into the index; cleanup flags any that should instead be deleted.
- **Records in no binder, for review**: records with an empty `binder` set, listed so the user can prune ones no longer wanted. **Listed, never auto-deleted.** Pruning one is `register forget <id>`.
- **Unrepairable bookmark entries**: entries in the shared bookmark store that no repair can fix, because the file is gone or the key was never a minted id. The verdicts that are *not* cruft stay out of this section. An orphan that resolves may be held on purpose, a broken one belongs to `repair`, and one whose volume is away or unindexed was never judged. `register audit` shows all five; the judging is one implementation shared by both commands.

`--prune` drops the unrepairable entries without asking and **refuses to run while any entry could not be judged**, naming the volume or the missing engine capability. "Dead" means the blob does not resolve and no file carries the id, which is only true if the question could be asked. An unmounted volume, or a mounted one Spotlight does not index, makes every bookmark on it look dead. The mode acts only on a condition it has checked, not on what the system happens to report. `--dry-run` says what it would drop.

Duplicates do not appear here. With one record per file and `binder` as a set, duplicate `(id, binder)` registrations are structurally impossible.

This tool is **not** part of matterbase. Its workflow is a per-item review with accept/reject in the CLI, like `git add -p`'s patch mode.

### `register album <binder> [--title TEXT]`

(User guide: [ALBUM.md](ALBUM.md).)

Prints the binder as an **album: its members in order, as JSON lines**, and
writes nothing (but the name, with `--title`). Each line is the member's
`register list <binder> --json` line, built by the same function, plus:

| Key | Content |
|---|---|
| `position` | 1-based, in the order `register order show` resolves (`sort:` keys first, then the order of the blocks in the note, then members without a block by file name). There is one source of order, so the album cannot disagree with the ordering it is made of. |
| `fields` | The member's block as grubber returns it (`--no-fill`), frontmatter inherited, minus `type`, `id`, `binder`, `sort` and grubber's `_`-prefixed keys. Open set; omitted when empty. |
| `file` | What the file carries: `size`, `modified` (file system); `type`, `title`, `comment`, `place`, `date`, `lat`, `lon`, `camera`, `width`, `height`, `pages`, `duration` (Spotlight, `mdls`, one call per file on a worker pool). Keys that mean what a field means take its name, so the shown value is `file` overlaid with `fields`. A `title` that only repeats the file name (Spotlight's default for images) is dropped. Omitted for URL records and broken members. |

Absent values are omitted, never `""`/`null`. Broken members keep their line and
position with `"broken":true`, as in `list --json`. The album name is the `album`
frontmatter field, arriving in every line's `fields`; `--title TEXT` writes it to
`collections/binder_<name>.md` (NFC-normalized, as binder names are; control
characters refused, exit 1), `--title ""` removes it (and a header left
empty). Messages go to stderr; stdout is only the lines. Unknown binder: exit 1,
as `order show`.

Rendering is not fileregister's job. The HTML renderer of 1.x (`--out`, `--css`,
`--milan`, `--open`, thumbnails via `sips`) is gone, and those flags are refused
with a sentence saying so. `album` exists beside `list` because `list` reads the
index only and stays fast; `album` is the one command that joins index, Markdown
layer and Spotlight.

### `register reindex [--dry-run]`

Rebuilds the index from Markdown ref blocks, restoring the invariant that every record has an authoritative index entry. For each `type: ref` block found under `notes_dir` (`read_annotations`) whose `id` is **not** already in the index (`read_index`), it reconstructs an index line and appends it to `collections/inbox.jsonl`. Every block of an id contributes: a record annotated in several binders is rebuilt as **one** record with all memberships folded into its `binder` set. Idempotent; `--dry-run` previews the additions without writing.

This is the migration/repair path:

- **Legacy data**: records written by the old `--md` (which used to live only in Markdown, with no index line) get pulled into the index so the core read path sees them.
- **Hand-written refs**: a `type: ref` block dropped into a project note becomes an index record.
- **Recovery**: if an index file is lost but the annotations survive, reindex rebuilds what it can (a lean annotation yields a minimal record; a full legacy block yields a complete one).

Annotations are left untouched; reindex only ever *adds* to the index.

## matterbase Touchpoints

matterbase is the query-construction TUI. Its primary job is to help users assemble grubber queries (with filters, fulltext, SQL refinement) and to yank the resulting CLI command.

Collections are **not** integrated as a dedicated UI feature in matterbase. Two practical reasons:

1. The files typically added to collections (PDF, Pages, Mail, photos) are not visible in matterbase's file list, which only shows Markdown notes. A built-in add/remove UI would have nothing meaningful to operate on.
2. Binder files conventionally live under `<notes_dir>/collections/binder_<name>.md`. They're regular Markdown files and matterbase can read them like any other note, but treating them as a distinct UI concept would only add modes without enabling new workflows.

### Querying collections from matterbase

A binder is an ordinary field, so matterbase reaches it through the same filter and SQL mechanism as any other, with no dedicated collection UI. The matterbase-side keys and preset config live in [WORKFLOWS.md](WORKFLOWS.md); this section fixes only the grubber query mechanics the records rely on. Directly in the shell:

```sh
grubber extract ~/notes --blocks-only -f type=ref -f binder=receipts
```

A bare Markdown scan returns only the **annotated subset** (records that carry a per-binder context block, with their custom fields and prose). To query the **full index** (every record, annotated or not), add the JSONL store as a merge source. Because the index holds one record per file with `binder` as an array, `--explode binder` first projects each index record into one row per membership, so the per-binder Markdown blocks line up and collapse on `(id, binder)`:

```sh
grubber extract ~/notes --from-jsonl ~/notes/collections/ --explode binder --merge-on id,binder --blocks-only -f type=ref -f binder=receipts
```

So the Markdown-only scan is the rich "annotated view"; `--from-jsonl` widens it to the complete directory; `--explode binder` turns the one-per-file index into per-membership rows; and `--merge-on id,binder` ensures each membership appears once (as its annotation block, back-filled with the index fields) rather than as two entries. For a **per-file** view (one row per file, records in no binder included), omit `--explode`/`--merge-on` and filter on the array directly (`-f binder=receipts` matches a value inside the set). See [WORKFLOWS.md](WORKFLOWS.md) for the matterbase-side configuration and [grubber/IDEAS.md](https://github.com/rhsev/grubber/blob/main/IDEAS.md) for `--explode`.

### Management lives in the CLI

Collection lifecycle (add, remove, refresh, audit, repair, rename, cleanup, reindex) lives in the `cmd_*.go` files of the `register` binary. matterbase has no part in it.

## Scenario Matrix

| Event | What survives | Batch needed |
|---|---|---|
| Add file to a binder | — (new state, recorded to the index) | `register add` |
| Annotate a binder | index entry kept; annotation added | `register promote` |
| File moved within same volume | bookmark, xattr | none (bookmark follows the file) |
| File moved cross-volume, xattr preserved | xattr; bookmark broken | `register repair` |
| File copied to xattr-unfriendly FS | the index record | `register repair`, then `register refresh` |
| File deleted | the record (now a dangling reference) | `register audit` reports; manual decision |
| Binder renamed | needs propagation | `register rename` |
| User edits the index manually | record exists, metadata stale | `register refresh` |
| Record exists only in Markdown | annotation, no index entry | `register reindex` |
| User edits `kMDItemProjects` manually | binder xattr value exists, no record | `register audit` reports; manual `register add` to reconcile |
| Repository copied to Linux | the index record (no xattr) | none; matterbase operates in degraded mode (no metadata layer) |

## Non-Goals

- **No daemon**, no filesystem watcher, no real-time write-on-edit. Reconciliation is explicit.
- **No reverse sync**. The tooling does not write records from the binder xattr automatically. `register audit` exists to surface the discrepancy; the user decides.
- **No binder hierarchy** (no nested binders, no parent-child). A file is in zero or more binders.
- **No matterbase-managed index**. The index is plain JSONL the core reads directly; the optional query layer (grubber over the Markdown notes) re-scans on each invocation. No daemon maintains either.
- **No proprietary fields**. The schema uses field names a human would pick. Any other tool can read or write these records.

## Write Engine

The `write` subcommand (`register write`) is the record-write helper. It reads JSONL records from stdin and ensures each one exists in its target file. `cmdWrite` (`cmd_write.go`) groups the records by target and dispatches on the extension: a `.jsonl` target goes to `index.JSONLWriteMany`, which folds the records into a JSONL store, and a `.md` target goes to `mdFileWriteMany`, which appends YAML blocks to a Markdown note. `register add` calls `index.JSONLWriteMany` directly for its index write. The other lifecycle subcommands modify records in place via the Markdown editor (`md_writer.go`) or the JSONL editor (`jsonl_editor.go`), chosen by the record's `_note_file` extension, when more surgical edits (drop a field, rename within a record) are needed.

`register write` also handles the membership layer as an opt-in side-effect via the `_ref_path` input field, the way `add` does. The binder goes to the file through the record's backend (the stored one for an existing record, else the input's `xattr`; `none` writes nothing), plus the ★ marker. Only a write to an index `.jsonl` is a membership (a Markdown block is annotation), and a record in no binder has none.

A `.jsonl` target inside a `collections/` folder is part of an index, and `write` keeps its rules: a record whose id is recorded in *another* file of the index, or whose aka belongs to another record, is refused (the status line says where). `SCHEMA` is stamped only there. `binder` must be a single string, because the stream is one membership per line.

The subcommand was originally a standalone tool named `grubber-write`, conceived as a symmetric counterpart to grubber's extraction engine. It folded into this repository (and into the unified `register` binary) when it became clear that fileregister is its only consumer and that the lifecycle code needed direct access to the Markdown editor for the non-append operations. The `write` subcommand remains useful for its specific job: idempotent append of one ref block per input record.

### Semantic Contract

Given a record R and a target file T, `register write` ensures R is present in T. The operation is idempotent: re-running with the same input produces the same on-disk state.

Three cases:

1. **R already exists in T** → no-op
2. **R missing, T exists** → R appended to T
3. **R missing, T missing** → T created (empty), then R written

`write` works on records. Creating the file is part of writing its first record, not a separate step.

### Scope

- **Format**: Markdown (`.md`) and JSONL (`.jsonl`). The unit differs by layer. `index.JSONLWriteMany` keeps **one record per file**, set-inserting the input's `binder` into the existing record's array (idempotency key `id`); `mdFileWriteMany` appends **one block per `(id, binder)`** context (idempotency key `(id, binder)`). Both take the records for one target and return one action per record.
- **Record type**: `type: ref` only; `id` required, `binder` optional (one string)
- **Format dispatch** by target extension (`.md` or `.jsonl`, in `cmdWrite`; any other extension is refused per record)

Non-conforming input is rejected per-record with a status entry; the stream is not halted. Typst targets and other record types are deferred until reading through grubber gains symmetric support.

### Record Identity

In the JSONL index a record is identified by `id` alone (one record per file); `register write` set-inserts the input's `binder` into the existing record rather than appending a second line. In Markdown a context block is identified by `(id, binder)`. Other fields (kind, aka) may differ between input and existing record; `register write` does not rewrite an existing record's other fields, only ensures the membership is present. To change an existing record's fields, the user edits it directly (handles: `register aka`).

### Heading Convention

When writing into Markdown:

- If the record has a `filename` → prepend `### {filename}` before the YAML block
- Otherwise → bare YAML block, no heading

The heading is **structural and cosmetic**, never an information carrier. Its only job is to give Markdown viewers and matterbase's preview pane visual anchoring. Anything queryable lives in the YAML block, including the file's basename (via the `filename` field, see [Markdown entry contract](#markdown-entry-contract-normative)). This means the heading text is free for the user to rename without losing the connection to the underlying file.

matterbase's preview pane uses the nearest preceding heading to give the block context; bare blocks inherit whatever heading is already in the file.

### Interface

**Input**: JSONL on stdin. Each line is a record.

Required fields:
- `_note_file`: target file, `.md` or `.jsonl` (where the record is written)
- `type: ref`, `id`

Optional fields:
- `binder`: one string; without it the record is in no binder
- `filename`, `kind`, `aka`, `xattr`: record content (a `tags` key is ignored with a note; labels belong in the block)
- `_ref_path`: path to the referenced file. When set, the file exists and the target is an index `.jsonl`, `register write` writes the membership to the file through the record's backend and sets ★ (idempotent)

Example:
```json
{"_note_file": "/path/to/collections/inbox.jsonl", "_ref_path": "/path/to/brief.pdf", "type": "ref", "id": "abc123", "binder": "project-alpha", "filename": "brief.pdf", "kind": "pdf"}
```

**Output**: JSONL on stdout, one status entry per input record, in input order. Statuses are emitted after stdin EOF. Records are batched per target file (one read + one write per target instead of a rewrite cycle per record), so a consumer writes the whole stream, closes stdin, then reads the statuses. An oversized input line (over the 4 MB cap) or a stdin read error fails the whole run before any write is applied.

```json
{"ok": true, "_note_file": "/path/to/collections/inbox.jsonl", "action": "appended", "xattr": "added"}
{"ok": true, "_note_file": "/path/to/collections/inbox.jsonl", "action": "noop",     "xattr": "noop"}
{"ok": true, "_note_file": "/path/to/collections/binder_project-alpha.md", "action": "appended", "xattr": "skipped"}
{"ok": false, "_note_file": "/path/to/x.json", "error": "unsupported target format: .json"}
```

- `action` is `appended`, `updated` (a binder set-inserted into an existing index record) or `noop`
- `xattr` distinguishes how the membership side-effect on the file resolved:
  - `skipped`: no `_ref_path`, a Markdown target, a record in no binder, or the `none` backend
  - `missing`: `_ref_path` provided but file does not exist
  - `noop`: binder name already present in the binder backend
  - `added`: binder name added through the binder backend
  - `failed`: the engine reported a failure (the record write still succeeded)

**Exit code**: non-zero if any input record failed (record write level; xattr failures do not set non-zero exit).

### Implementation

- **Language**: Go; a single self-contained binary (only `libSystem` + `libresolv` linked)
- **Distribution**: single compiled binary (`register`), invoked from the shell or as a subprocess
- **Stream mode**: reads records until EOF, then writes batched per target file; one invocation handles a whole batch without per-record process overhead
- **Concurrency**: cross-process writers serialize via the advisory index lock (see the wire contract's write discipline), so concurrent `register` runs no longer lose writes; callers need not serialize themselves.

### Migration Path

The contract is subprocess + JSONL. `register write` has since been ported from Ruby to Go; consumers did not change. They keep invoking the same external command with the same input/output format. The same stability holds for any future reimplementation.

## Implementation Notes

- **The unified `register` CLI** follows the conventions of its sibling shell tools in the stack (`mark-twin`, etc.): a single dispatcher (`main.go`) plus one `cmd_*.go` per subcommand, few dependencies (the Go standard library plus `gopkg.in/yaml.v3`, `golang.org/x/text` and `golang.org/x/sys`), JSONL or simple text stdio where applicable, no shared state beyond the bookmarks JSON file.
- **Metadata via the fileanchor engine**: the bookmark, ItemProjects, Tags, and locator clients (in `bookmarks.go`, `meta.go`, `locator.go`) are thin clients of the **fileanchor** engine ([its own project](https://github.com/rhsev/fileanchor)), a native binary that performs all macOS metadata work in-process (bookmarks, Finder tags, Spotlight, xattrs) over a batch stdio protocol. A single fileanchor client (`fileanchor.go`) holds one engine process open for the run, so there is no fork+exec per file. `register` keeps the id→blob map in `~/.local/share/bookmarks.json`; the engine is stateless about it (`save path→blob`, `resolve blob→path`). The engine is the one external dependency and the single macOS-coupling / portability seam.
- **Xattr backend dispatch**: in `meta.go`, the add/remove/includes helpers route to ItemProjects, Tags, or skip (`none`) based on the record's `xattr:` field. Per-record granularity; mixed-backend setups are supported.
- **register add is the user-facing entry point** for adding. It is invoked from the shell, not from matterbase; matterbase's file list shows only Markdown notes, while the typical Add target (PDF/Pages/Mail/etc.) lives elsewhere on disk. No `matterbase add` subcommand needs to exist.
- **register remove is a set-delete**: it removes the named binder from the index record's `binder` set; annotation blocks are left untouched (`register cleanup` reviews the now-stale ones). It never removes the record; with an emptied set it stays in no binder. Re-adding is a set-insert on the same record (one batched index append); there is no reclaim and no duplicate to avoid.
- **JSONL edits are parse/mutate/serialize**: the JSONL editor mirrors the Markdown editor's surface (read all refs, update id, delete block, rename/add/remove binder) but operates on whole JSON lines rather than block regex, so index mutations do not depend on text matching. The record's `_note_file` extension picks which editor runs. Injected provenance fields (`_note_file`, `_mtime`) are stripped before a line is rewritten, so they never persist into the store. When multiple `*.jsonl` files exist, edits group records by `_note_file` and rewrite only the file each record came from.
- **Path identity is filesystem truth**: whether two paths name the same file is decided by the filesystem where possible. Existing files compare by device:inode (`os.SameFile`), which is exact on case-sensitive APFS and Linux alike and immune to Unicode form and symlinks, because the `stat` lookup applies the filesystem's own rules. Only two nonexistent paths fall back to string comparison (NFC-normalized everywhere; case-folded on macOS only). Used by `remove`'s path fallback and `audit`'s ghost keys.
- **Container names are form-folded for uniqueness**: payload and note names are allocated unique on a lower-cased, NFC-normalized key, so staging dirs and unpacked containers stay collision-free on case- and normalization-insensitive filesystems, regardless of the filesystem the container was built on. Basenames colliding only in case or Unicode form get a `-N` suffix instead of silently overwriting each other.
- **unmarshal confines manifest paths**: a container's `file` and `origin` are manifest strings, not tar members, so tar's own traversal defense doesn't cover them. Both are joined and then re-checked *after* `filepath.Clean` (interior `..` collapses first): the staged `file` must stay inside the extracted container, and the `origin` write target must stay inside `collections/` unless `--scatter` is given. A foreign container therefore can't read/delete host files or write above the vault; `--scatter` exists to gate that last case.
- **kMDItemInformation is multi-valued**: multiple record ids are space-separated in this string field, which fileanchor stores as a binary plist. Spotlight parses `com.apple.metadata:*` values as plists, and a raw `"id1 id2"` is not indexed at all ([FINDINGS-attributes.md](FINDINGS-attributes.md)). `register repair`'s most reliable file-location strategy is an id Spotlight lookup (engine `query by:id`). It is backend-agnostic, since the id xattr carries the record id regardless of which xattr layer the binder lives in.
- **The locator is the portability seam for file discovery**: `repair` does not query Spotlight directly; its lookups go through the locator (`locator.go`), which calls the engine's `query {by, value}` op (covering id, filename, and the groups/tags xattr layers). On Linux the engine grows a second implementation of the same op (e.g. `plocate`, `locate`, or the index); no other code changes. The finder strategy is injectable in tests, so the ordering/fallback can be unit-tested without a live engine.
- **`REGISTER_BINDER` env var**: `add`, `remove`, `promote` and `marshal` fall back to `$REGISTER_BINDER` when `--binder` is not given. Useful for session-oriented workflows (e.g. `export REGISTER_BINDER=berlin-2024; register add *.jpg --kind image`). Explicit `--binder` always wins.
- **`promote --edit`**: after a successful promote (at least one record annotated), the target note is opened in `$VISUAL` / `$EDITOR` / `vi` via `exec`, replacing the `register` process. Only fires when `--edit` is given and `promoted > 0`; a pure noop promote does not open the editor.
- **kMDItemProjects is a native array** (binary plist of strings, the macOS "projects" attribute). The engine reads it, adds the binder if missing (or removes it), and writes the array back, per element. So Spotlight matches a single binder with `== "<name>"`, and binder names may contain spaces; never a space-separated string.
- **kMDItemUserTags is natively multi-valued** (binary plist array of strings). The engine reads/writes it via Foundation's `URLResourceValues.tagNames`, which handles the binary-plist encoding and returns proper strings, with no manual plist conversion. Finder's color suffixes (`\n<digit>`) are stripped on read.
- **Binder names may contain spaces**: kMDItemProjects is a real array, so `alpha project` is one element and matches exactly. Only the opt-in `tags` backend benefits from short, space-free names (spaces clutter Finder Tag chips; convention there is hyphens, underscores, or the colon-hierarchy `vacation:2024:italy`). Not enforced; documented.
- **Backend migration is not automatic**: changing a record's `xattr:` field from one backend to another (e.g. `itemprojects` → `tags`) leaves the old xattr entry behind. `register audit` will eventually surface this as a ghost; resolve manually.
- **xattr failures are non-fatal**: when an xattr write fails, the index write has still succeeded. The canonical layer is intact; the cache is missing for that file. `register refresh` can re-attempt later.
- **Labels vs `binder`**: labels (`tags:` in a block) are annotation, never in the index and never mirrored to xattr. Only `binder` participates in the xattr layer (ItemProjects or UserTags backend).

## Possible Future Refinements

- **Promote enrichment hooks**: `promote` is the single chokepoint between capture and curation, so it is the obvious place to add validation, field normalization, or xattr-policy application as records cross into Markdown. Left out so far to keep the gate a plain pipeline.
- **Typst metadata format**: with grubber reading Typst `#metadata` blocks, ref records in `.typ` files become discoverable. The `register` tool could be extended to also *write* to `.typ` targets via `register write`.
- **Backend migration helper**: a `register migrate-backend` subcommand that, given a binder name and a new backend, cleans up stale xattr entries from the old backend and ensures the new backend is populated.
- **Auto-routing by `kind`** in `register add`: photographic kinds → tags backend by default, document kinds → itemprojects. Opt-in via config. Avoids `--xattr` repetition for users with clear use-case clusters.
- **Reverse audit at larger scope**: Spotlight-wide `mdfind` rather than directory scan, to surface files outside `notes_dir` carrying binder tags.

## Vocabulary

### Concepts

| Term | Meaning |
|---|---|
| Collection | The user's overall body of file-references managed by this tool. Conceptual term, not a specific data field. |
| Binder | A named bucket within the collection. Files belong to one or more binders by carrying their name in the YAML `binder:` field. The technical primitive. |
| Ref / Reference | A single record describing **one file**: its `id`, its `binder` set, and plain-text anchors. Canonical form: one index line; optionally mirrored as per-binder Markdown annotation blocks. |
| Loose record | A record whose `binder` set is empty, a file tracked by identity alone, in no binder. Never auto-deleted (`register forget` deletes one on request). Created directly by `register add` (no `--binder`) or left when `register remove` empties the set. |
| Index ("yellow pages") | The central JSONL store (`<notes_dir>/collections/inbox.jsonl`) holding every record's mandatory metadata, one per file. The authoritative directory the core reads directly. (The filename `inbox` is historical.) |
| JSONL store | Any `*.jsonl` under `collections/` (the live `inbox.jsonl` plus optional archives/imports). All are read-merged by `read_index`; only `inbox.jsonl` is written by `add`. |
| Annotation | An optional lean Markdown ref block (H3 = filename, YAML = `id` + a single `binder` + custom fields), linked to the index by `id`. One block per `(id, binder)`, the file's context in that binder. Carries custom metadata + prose. Created by `promote` / `add --md`. |
| Promote | Adding a Markdown annotation for a record via `register promote`. Additive; the index entry stays. |
| Binder file | A Markdown file in `<notes_dir>/collections/` named `binder_<name>.md`, holding annotations for one binder. The default `register promote` target, and the binder's default ordering file. |
| Album | An ordering of a binder, as data: each member's `list --json` line plus position, fields and file metadata. The album is named by an `album:` field in the note's frontmatter, which grubber passes down into every block of the file. Each member carries the name of the album it is in, and nothing looks it up. Absent, the binder name stands in. No record of its own, because the album's substance is the sequence, which the `sort:` keys already hold. Spec: [ALBUM.md](ALBUM.md). |
| Ordering | A presentation putting one binder's members into a sequence: the order of their blocks in the binder file, with sparse `sort:` keys where a member is placed explicitly. No configuration. The binder itself stays a pure set. Spec: [ORDERING.md](ORDERING.md). |
| Backend | The xattr layer chosen per record via the `xattr:` field: `itemprojects` (default), `tags`, or `none`. |
| ItemProjects | The macOS `kMDItemProjects` xattr, default backend for membership. Quiet, Spotlight-only. |
| Tags | The macOS `kMDItemUserTags` xattr (Finder Tags), opt-in backend per record. Visible in Finder, syncs to iOS Files via iCloud Drive. |
| Bookmark | The macOS bookmark that finds a record's file again. The built-in manager (`bookmarks.go`) keeps `~/.local/share/bookmarks.json` (record id→blob) and delegates bookmark save/resolve + `kMDItemInformation` to the fileanchor engine. `repair` re-binds it under the same id, `unmarshal` re-creates it, `refresh` renews a stale one. |

### `register` subcommands

| Subcommand | Purpose |
|---|---|
| `add` | Saves the bookmark and writes/updates the index record (set-insert the binder). With `--binder` joins a binder; **without `--binder` the record is in no binder** (empty set; folds in the former `link`). `--md`/`--target *.md` also writes an annotation. `--xattr` selects backend. |
| `promote` | Adds a per-binder Markdown context block for a binder's records (binder- or record-granularity). Additive; the index entry stays. |
| `annotate` | Edits an existing annotation block: `--set`/`--unset` custom fields, `--prose` replaces the section's prose. Reserved keys (`id`, `binder`, `sort`) are refused with the command that owns them. |
| `remove` | Set-deletes the binder from the index record and removes the binder name from the xattr layer; annotation blocks stay (`cleanup` reviews stale ones). With an emptied set the record stays, in no binder. |
| `forget` | Deletes a record in no binder: index line, bookmark entry, its id in the notes' blocks (the blocks stay) and on the file. The only command that deletes a record. |
| `refresh` | Rebuilds the derived metadata on the file from the index (the id, the ★ marker, the binder xattr through ItemProjects, Tags, or skipped for `none`) and renews stale bookmarks. Idempotent. |
| `audit` | Read-only consistency report: dangling records, broken bookmarks, mismatched/ghost xattr in both layers. |
| `repair` | Re-binds a moved file's broken bookmark under its **unchanged** `id`; locates the file via Spotlight (id lookup first, backend-agnostic). |
| `rename` | Renames a binder across both stores (index + annotations), the ordering config, the canonical note file, and the per-record xattr backend. |
| `cleanup` | Interactive, human-judged review of layer drift: stale context blocks, unindexed annotations, records in no binder (listed for review), unrepairable bookmark entries; per-item accept/reject. Never auto-deletes a record. |
| `reindex` | Rebuild the index from Markdown ref blocks so every record has an index entry. Idempotent; `--dry-run` previews. |
| `write` | Internal helper: idempotent append of one ref record per input. Shares its index writer with `add`; available as a subcommand for advanced use. |
| `marshal` | Packs a binder into a portable `tar.gz` container (manifest of index records, the files, the annotation notes). Carries no xattrs; `unmarshal` rebuilds metadata from the manifest. |
| `unmarshal` | Unpacks a container: places each file at its `origin` (inside `collections/` unless `--scatter`), re-creates its bookmark under the same id, writes the index records. Never overwrites a file or touches an already-managed id; conflicts are parked in the import folder. |
| `list` | List all binders with file counts (pure register, no Markdown scan), or files in one binder. `--inbox`/`--curated` filter by annotation status and read the optional Markdown layer on demand, through grubber. `--paths` (with a binder) prints only resolved absolute paths, one per line (unresolvable members, and paths containing a line break, are omitted with a stderr warning); `--paths --print0` separates them with NUL instead and omits none but the unresolvable; `--json` prints neutral JSONL per member (id, binder, filename, kind, aka, path/url; a member whose bookmark does not resolve carries `"broken":true` instead of a path) for a consumer to shape. Without a binder, `--json` lists the binders instead, one `{"name","count"}` line each in the table's order (filters apply; `name`, because `binder` is a record's membership and always a list), and prints nothing for an empty set, where the table prints a sentence. `--paths`/`--print0` need exactly one binder, and two binders are refused (`list` exits 1 rather than print the human table). |
| `resolve` | Forward lookup: a key (`id` or `aka`) → the file's path. `--record` prints id/binder/filename/aka/path. |
| `aka` | Add or remove a record's `aka` handles in the index, uniqueness-checked; guarded against Markdown blocks that reach the record only through the handle. |
| `of` | Reverse lookup: a file → its `id`, `aka`, and collection membership. Reads the `id` stamped on the file (so it survives rename); falls back to a filename match, else reports the file as unmanaged. |
| `album` | Prints a binder in order as JSON lines: `list --json` plus `position`, `fields` (annotation YAML via grubber) and `file` (Spotlight). Its order is the one `order show` resolves. |
| `order` | Arranges a binder's members for presentation: `show` (resolved order), `move` (one member, one `sort:` key write; keys are materialized for all members when the neighbors carry none). The order is the order of the blocks in the binder file plus sparse `sort:` keys; `set` is gone and refused. Membership stays untouched. See [ORDERING.md](ORDERING.md). |
