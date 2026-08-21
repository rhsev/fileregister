# Why not just macOS Tags?

A reasonable question. macOS already lets you tag any file, the tags are Spotlight-searchable, Finder shows them as colored chips, Smart Folders aggregate them automatically. So what does fileregister add?

Short answer: it doesn't replace macOS Tags, it cooperates with them. macOS Tags are the *view*; Markdown records are the *truth*. Both layers coexist, each playing to its strength.

Long answer: two structural advantages and one resilience story.

## 1. Structural annotation per membership

macOS Tags are strings, nothing more. A file is tagged `"Rechnung 2024"` — that's all the information you have. With fileregister, the same membership carries structured data:

```yaml
type: ref
id: abc123
binder: rechnungen-2024
filename: hosting-invoice.pdf
kind: pdf
betrag: 142.50
bezahlt_am: 2024-03-15
lieferant: Hetzner
status: bezahlt
```

Plus prose annotation in the surrounding Markdown, plus per-binder enrichment (the same file can be in a second binder with a different `kind` or different custom fields if that makes sense in that context).

macOS Tags cannot express this. If your use case is purely "binder files by topic and find them again" — tags are enough. If you also have *attributes* of the membership (amount, date, status, free-text notes), you need a structured layer. That's what fileregister adds.

This advantage stands independent of any portability story. Even if xattr never failed, the structural enrichment alone is worth the layer.

## 2. Rebuildable across boundaries

macOS Tags live in extended attributes. They are robust within an APFS volume on a single Mac, but they disappear at several boundaries:

- **Transfer to FAT32 / exFAT** — USB sticks, SD cards: gone
- **rsync without `-E`** — the default on most Linux servers: gone
- **iCloud Drive** under sync glitches or partial syncs: occasionally gone
- **Some cloud services** (variable behavior): gone
- **Move to Linux or Windows** — even if the volume is readable, the tag layer is invisible
- **DEVONthink database corruption** — if your tags lived inside DT's database: gone
- **Time Machine restore** through a damaged mount: sometimes gone

In all these cases, macOS Tags alone mean: your organization is lost. You start tagging by hand again, from memory.

With fileregister, the record layer survives every one of these transfers. It is plain text, lives in regular files, gets backed up by git, Dropbox, syncthing, Time Machine — whatever you already trust. A `register refresh` then rebuilds the xattr cache on whatever system can carry it.

This is not hypothetical. It is the multi-device, multi-OS reality that opens up the moment you stop being contained within one macOS garden.

## 3. Resilience via layering

macOS Tags alone are a *single-layer* system. The layer is convenient and fast, but its loss is total.

fileregister deliberately runs two layers:

| Layer | Properties | Role |
|---|---|---|
| Plain-text records (JSONL index + Markdown annotations) | Plain text, portable, structured, versionable | Canonical / source of truth |
| Spotlight metadata (xattr) | Native, fast, Finder-integrated, fragile | Cache / day-to-day interface |

When both layers are alive, you get the daily ergonomics of macOS Tags *plus* the structural richness of Markdown. When one fails (typically xattr), the other rebuilds it. The conceptual cost — running two layers — is paid by the architecture; the user just calls `register add` and `register refresh` and doesn't think about layers.

This is the same strategy used elsewhere:

- Markdown notes vs. proprietary note databases (Bear, Notion): plain text outlives the app
- git: working tree + index + objects, each reconstructible from the others
- Plain-text configuration vs. binary preference bundles
- Email in IMAP folders vs. proprietary mail clients

The pattern: keep the canonical layer plain text and portable; let the convenient layer be derived and disposable.

## When the question answers itself in tags' favor

Some use cases really are well-served by macOS Tags alone:

- Single Mac, no cross-device sync
- No FAT32 / cloud / cross-OS exposure
- Pure "binder these files for later browsing", no structured attributes
- No interest in long-term archival readability
- No plans to ever leave macOS

For that profile, fileregister is over-engineering. Use Finder Tags directly. They're great.

## When fileregister earns its keep

The profile where the two-layer cost pays off:

- Multi-Mac setup (sync between machines)
- Frequent cross-FS transfers (USB sticks, NAS, cloud)
- Linux as a real future option
- Structured per-membership data (invoices with amounts, projects with status, photos with captions beyond filename)
- DEVONthink as a current tool with potential exit planned
- Archival timeframe in mind (readable in 10 years, on any OS)

This is not a mass-market profile. It is a power-user-of-personal-data profile. For that profile, the resilience and structural enrichment are not theoretical — they pay back on every cross-boundary operation and every multi-device workflow.

## The honest framing

> fileregister uses macOS Tags. It does not replace them. You keep the full Finder integration (Smart Folders, Spotlight, Quick Look). You additionally get a Markdown layer that makes the integration robust and the membership richer.
>
> It is additive, not alternative.

Said that way, the question "wozu, gibt's doch schon" loses its bite. Nobody throws macOS Tags away to use fileregister. The two cooperate.

This is literal, not metaphorical: fileregister supports two xattr backends per record. The default (`subject`) caches the binder name in `kMDItemProjects` — Spotlight-searchable, quiet, no Finder presence. The alternative (`tags`) caches in `kMDItemUserTags` — the actual macOS Finder Tags, visible in Finder and iOS Files via iCloud Drive. A third option (`none`) skips the xattr cache entirely (Markdown only). You can mix per binder: photos with Finder Tags, documents with the quiet Subject cache, archival material with neither. See [SPEC.md §macOS metadata layer](SPEC.md) for the mechanics.

## See also

- [SPEC.md](SPEC.md) — full data model
- [COLLECTIONS.md](COLLECTIONS.md) — querying collections from the matterbase TUI
- [README.md](README.md) — the tools themselves
