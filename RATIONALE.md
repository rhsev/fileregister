# Why not just macOS Tags?

macOS already lets you tag any file: the tags are Spotlight-searchable, Finder
shows them as coloured chips, Smart Folders aggregate them automatically. So what
does fileregister add?

It does not replace macOS Tags; it cooperates with them. macOS Tags are the
*view*, Markdown records are the *information*, and both layers coexist. Two structural
advantages and one resilience point follow.

## 1. Structural annotation per membership

macOS Tags are strings, nothing more: a file tagged `"Invoice 2024"` carries
exactly that string. With fileregister, the same membership carries structured
data:

```yaml
type: ref
id: abc123
binder: invoices-2024
filename: hosting-invoice.pdf
kind: pdf
amount: 142.50
paid_on: 2024-03-15
supplier: Hetzner
status: paid
```

Plus prose in the surrounding Markdown, plus per-binder enrichment: the same file
can sit in a second binder with a different `kind` or different custom fields
where that context calls for it.

macOS Tags cannot express this. If your use is purely "binder files by topic and
find them again", tags are enough. If you also have *attributes* of the
membership (amount, date, status, free-text notes), you need a structured
layer — that's what fileregister adds.

This advantage is independent of portability. Even if xattr never failed, the
structural enrichment alone justifies the layer.

## 2. Rebuildable across boundaries

macOS Tags live in extended attributes. They are robust within an APFS volume on
a single Mac, but they disappear at several boundaries:

- **Transfer to FAT32 / exFAT** — USB sticks, SD cards: gone
- **rsync without `-E`** — the default on most Linux servers: gone
- **iCloud Drive** under sync glitches or partial syncs: occasionally gone
- **Some cloud services** (variable behaviour): gone
- **Move to Linux or Windows** — even if the volume is readable, the tag layer is invisible
- **DEVONthink database corruption** — if the tags lived inside DT's database: gone
- **Time Machine restore** through a damaged mount: sometimes gone

In all these cases, macOS Tags alone mean the organization is lost — re-tagged by
hand, from memory, or not at all.

With fileregister, the record layer survives every one of these transfers. It is
plain text in regular files, backed up by git, Dropbox, syncthing, Time Machine —
whatever backup is already in place. A `register refresh` then rebuilds the xattr
cache on whatever system can carry it.

This is the multi-device, multi-OS case: the moment files leave a single macOS
volume, the tag layer is on its own.

## 3. Resilience via layering

macOS Tags alone are a *single-layer* system. The layer is convenient and fast,
but its loss is total.

fileregister runs two layers by design:

| Layer | Properties | Role |
|---|---|---|
| Plain-text records (JSONL index + Markdown annotations) | Plain text, portable, structured, versionable | Canonical / source of truth |
| Spotlight metadata (xattr) | Native, fast, Finder-integrated, fragile | Cache / day-to-day interface |

With both layers alive, the daily ergonomics of macOS Tags come *plus* the
structural richness of Markdown. When one fails (typically xattr), the other
rebuilds it. The cost of running two layers sits in the architecture, not the
usage: `register add` and `register refresh` are the whole interface.

The same strategy appears elsewhere:

- Markdown notes vs. proprietary note databases (Bear, Notion): plain text outlives the app
- git: working tree + index + objects, each reconstructible from the others
- Plain-text configuration vs. binary preference bundles
- Email in IMAP folders vs. proprietary mail clients

The pattern: keep the canonical layer plain text and portable; let the convenient
layer be derived and disposable.

## When macOS Tags alone are enough

Some profiles are well-served by macOS Tags alone:

- Single Mac, no cross-device sync
- No FAT32 / cloud / cross-OS exposure
- Pure "binder these files for later browsing", no structured attributes
- No interest in long-term archival readability
- No plans to leave macOS

For that profile, fileregister is over-engineering — use Finder Tags directly.

## When fileregister earns its keep

The profile where the two-layer cost pays off:

- Multi-Mac setup (sync between machines)
- Frequent cross-FS transfers (USB sticks, NAS, cloud)
- Linux as a real future option
- Structured per-membership data (invoices with amounts, projects with status, photos with captions beyond filename)
- DEVONthink as a current tool with a possible exit planned
- An archival timeframe in mind (readable in 10 years, on any OS)

This is not a mass-market profile but a power-user-of-personal-data one. There,
the resilience and structural enrichment pay back on every cross-boundary
operation and every multi-device workflow.

## Additive, not alternative

> fileregister uses macOS Tags; it does not replace them. The full Finder
> integration stays (Smart Folders, Spotlight, Quick Look), with a Markdown layer
> on top that makes the integration robust and the membership richer.
>
> It is additive, not alternative.

Nobody discards macOS Tags to use fileregister; the two cooperate.

This is literal: fileregister supports three xattr backends per record. The
default (`itemprojects`) caches the binder name in `kMDItemProjects` —
Spotlight-searchable, quiet, no Finder presence. `tags` caches in
`kMDItemUserTags` — the actual macOS Finder Tags, visible in Finder and iOS Files
via iCloud Drive. `none` skips the xattr cache entirely (Markdown only). The
choice is per binder: photos with Finder Tags, documents with the quiet
`itemprojects` cache, archival material with neither. See
[SPEC.md §macOS metadata layer](SPEC.md) for the mechanics.

## See also

- [SPEC.md](SPEC.md) — full data model
- [WORKFLOWS.md](WORKFLOWS.md) — organizing and querying collections
- [README.md](README.md) — the tools themselves
