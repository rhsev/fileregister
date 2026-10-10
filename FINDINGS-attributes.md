# Findings: macOS file attributes (for fileregister)

> Empirically verified facts about macOS xattr / Spotlight metadata. First
> verified on macOS 26.4 (2026-06). **Re-verified 2026-09-27 on macOS 15.8 and
> 27.2** (Mac mini and MacBook): all local findings, and the iCloud Drive and
> OpenCloud transports in both directions (15 → 27, 27 → 15). Resilio and AirDrop were not
> re-tested and stand as of 26.4. Several findings are counterintuitive; the
> tables below are authoritative.

## Storage name ≠ Spotlight key

Finder Tags live under **two different names** that are easily conflated:

- **xattr storage name** (written/read with `xattr`):
  `com.apple.metadata:_kMDItemUserTags` — **with leading underscore**.
- **Spotlight query key** (used in `mdfind`):
  `kMDItemUserTags` — **no underscore**.

The pattern is asymmetric:

```
write / read xattr :  com.apple.metadata:_kMDItemUserTags   (with _)
mdfind query       :  kMDItemUserTags                       (no _)
```

**Verified:** writing `_kMDItemUserTags` (via `tag`, Finder, or fileregister itself)
is found by `mdfind 'kMDItemUserTags == "…"'`. Writing the **no-underscore**
xattr `com.apple.metadata:kMDItemUserTags` is **neither Finder-visible nor
Spotlight-indexed** — a write-only dead end. (fileregister's `tags` backend used
the no-underscore key until fixed; it had been writing into a black hole.)

The underscore here is the **canonical name**, not the failed "`_kMDItem*`
workaround" Howard Oakley describes. Adding `_` to *other* attributes
(`_kMDItemKeywords`, …) to force sync fails; only `_kMDItemUserTags` is
system-special-cased.

## Attribute table — fileregister usage

| xattr (storage) | mdfind key | Use | Format | Finder-visible | Spotlight | iCloud sync |
|---|---|---|---|---|---|---|
| `com.apple.metadata:_kMDItemUserTags` | `kMDItemUserTags` | Finder Tags — ★ marker, optional binder tags | binary plist array | **yes** | yes | **yes** |
| `com.apple.metadata:kMDItemProjects` | `kMDItemProjects` | quiet binder cache (default backend) | binary plist array (per-element Spotlight match) | no | yes | no |
| `com.apple.metadata:kMDItemInformation` | `kMDItemInformation` | record id (local mdfind repair) | space-separated string, **as a binary-plist string** (see below) | no | yes (local) | **no** |
| `com.apple.metadata:kMDItemInformation#S` | — | record id + sync flag — **does NOT sync** (Apple `kMDItem*` prefix blocked regardless of `#S`) | string | no | no | **no** |
| `com.fileregister.id#S` | — | record id, cross-device — **syncs** (custom namespace + `#S`) | string | no | no | **yes — verified** |

### Spotlight reads `com.apple.metadata:*` as property lists

Spotlight parses the value of every `com.apple.metadata:*` xattr as a property
list. A **raw** string works only by accident: a single token like `123456789`
is a valid old-style ASCII plist string. Two space-separated ids,
`"123456789 987654321"`, are not — Spotlight then drops the whole value
(`mdls` shows `(null)`), and the file is found by **neither** id. The same text
stored as a binary-plist string is indexed and found by each token. fileanchor
therefore stores the id list as a binary plist (since 1.2.0). Verified on
macOS 15.8 and 27.2.

## iCloud Drive

*The default-sync change below is based on Howard Oakley's finding (eclecticlight.co), extended by the cross-device table further down.*

- iCloud Drive **stopped syncing `com.apple.metadata:kMDItem*` by default.**
  Only these still sync reliably: `_kMDItemUserTags` (Finder Tags),
  `com.apple.lastuseddate#PS`, `com.apple.quarantine`, `com.apple.TextEncoding`.
- Appending **`#S`** to the xattr *name* (`kMDItemInformation#S`) makes FileProvider
  sync it, **but** it is then **not** Spotlight-indexed and **not** Finder-shown
  (the `#S` is treated as part of the type name by Spotlight/Finder). Useful only
  for self-resolved data (e.g. a record id), never for searchable or visible
  metadata.
- The `#S` behaviour is undocumented and has shifted between releases.

### Cross-device: iCloud Drive (fresh file, read-only on the receiver)

| xattr written on Mac A | arrived on Mac B? |
|---|---|
| `com.apple.metadata:_kMDItemUserTags` (★ Finder tag) | **yes** |
| `com.fileregister.id#S` (custom namespace + `#S`) | **yes** |
| `com.fileregister.id` (custom namespace, no flag) | **no** |
| `com.apple.metadata:kMDItemInformation#S` (Apple key + `#S`) | **no** |
| `com.apple.metadata:kMDItemInformation` (Apple key, no flag) | **no** |
| `com.apple.metadata:kMDItemProjects` (Apple key, no flag) | **no** |

Re-verified 2026-09-27 in both directions between macOS 15.8 and 27.2, with
identical results. The receiver also gets `com.apple.FinderInfo`. A custom
name needs the `#S` to travel; without it iCloud drops it like the Apple keys.

**`#S` scope:** `#S` syncs a **custom-namespace** xattr
(`com.fileregister.*`) but **not** a `com.apple.metadata:kMDItem*` name — that
prefix is blocked regardless of `#S`.

### Cross-device: Resilio Sync (fresh file, read-only on the receiver)

| xattr written on Mac A | arrived on Mac B? |
|---|---|
| `com.apple.metadata:_kMDItemUserTags` (★ Finder tag) | **yes** |
| `com.fileregister.id#S` (custom + `#S`) | **no** |
| `com.fileregister.id` (plain custom) | **no** |
| `com.apple.metadata:kMDItemProjects` | **no** |

**Resilio syncs Finder tags only; it strips all other xattrs** (custom and
Apple alike — `#S` is meaningless to it, it's an iCloud/FileProvider flag).

### Cross-device: AirDrop (fresh file, read on the receiver)

AirDrop preserves **all** xattrs verbatim — it wraps the file in an archive and
copies extended attributes unfiltered. Confirmed survivors: `_kMDItemUserTags`
(★), `com.fileregister.id#S`, `com.apple.metadata:kMDItemInformation#S`,
**and** `com.apple.metadata:kMDItemProjects` (the ones iCloud strips). It is the
most metadata-faithful transport of the three.

> **TCC caveat:** AirDrop'd files land in `~/Downloads` (TCC-protected) and carry
> a `com.apple.macl` (app access-control) xattr, so a plain terminal gets
> `Operation not permitted` from `xattr`. This is not a stripped attribute:
> reading it requires Full Disk Access for the terminal, or moving the file (via
> Finder) to `~/`, which is not TCC-protected.

### Cross-device: OpenCloud desktop client (fresh file, read on the receiver)

Classic sync folder (`virtualFilesMode=off`), verified 2026-09-27 in both
directions between macOS 15.8 and 27.2:

| xattr written on Mac A | arrived on Mac B? |
|---|---|
| `com.apple.metadata:_kMDItemUserTags` (★ Finder tag) | **no** |
| `com.fileregister.id#S` (custom + `#S`) | **no** |
| `com.fileregister.id` (plain custom) | **no** |
| `com.apple.metadata:kMDItemInformation` / `#S` | **no** |
| `com.apple.metadata:kMDItemProjects` | **no** |

**OpenCloud transfers file content only — not a single xattr, not even Finder
tags.** Worse for the sending Mac: when the file is edited on the other Mac,
OpenCloud *replaces* the local copy (new inode) and the local xattrs are gone
too — ★, both ids and the binder cache, within seconds. The bookmark still
resolves (by path) but reports `stale`; `register refresh` renews it, which
writes both ids back, and backfills ★ and the binder layer. Until then the
file has no ★ and no id for Finder, Spotlight or repair's id search.

### Transport summary

| Transport | ★ | id (`com.fileregister.id#S`) | `kMDItemProjects` |
|---|---|---|---|
| iCloud Drive | ✅ | ✅ | ✗ |
| Resilio Sync | ✅ | ✗ | ✗ |
| AirDrop | ✅ | ✅ | ✅ |
| OpenCloud | ✗ | ✗ | ✗ |

**★ survives every transport but OpenCloud → the cross-device keystone.** The
id survives iCloud + AirDrop, not Resilio or OpenCloud. Over OpenCloud only the
filename (and the separately-synced records) connects a file to its record,
and a file edited on the other Mac loses its metadata locally as well — run
`register refresh` after syncing. Reconcile is therefore **transport-agnostic**:
enumerate via ★, read the id xattr **if present**, else match by filename against
the (separately-synced) records.

**Test discipline:**

- Each Mac writes into its **own** folder name. If both create the same new
  folder before either has synced, iCloud keeps one and renames the other
  (`… 2`); a reader waiting at a fixed path then waits forever.
- A dormant iCloud Drive may schedule the upload and never run it
  (`brctl status`: `sync-up-scheduled`, attempts 0). Restarting the daemon
  (`killall bird`) got it going.
- A same-Mac "Remove Download → re-download" is **not** a sync test: the local
  placeholder keeps all xattrs, so everything survives spuriously. A valid test
  reads on a **different device**.
- On the receiving device, only reading is safe. A re-write there clobbers the
  synced xattrs *and syncs the bare version back*, poisoning the file everywhere.
  Each clean run needs a **fresh filename**.

## Tag value encoding

- Finder Tags are a **binary plist ARRAY of strings**, not a plain-string
  xattr. Written via a plist (`plutil -convert binary1` / Python `plistlib`) and
  `xattr -wx <hex>`, or the `tag` CLI.
- Optional Finder **colour** suffix on a tag string: `"name\n<0-7>"`
  (newline + digit); removed when reading the name.

## Codepoint choice (sigils, markers)

- **ASCII** or **NFC-stable single codepoints** are safest. mdfind matching depends
  on Unicode normalization; the wrong glyph silently fails to match.
- **Safe:** `★` U+2605, `°` U+00B0, `~` U+007E — no decomposition under any
  normalization form. (★ verified as a working tag value + mdfind match.)
- **Avoid:** composed / ZWJ / skin-tone emoji, and **dead-key diacritics** like
  `˚` U+02DA RING ABOVE — it NFKD-decomposes to space + combining ring and has
  three look-alikes (`° ∘ ◦`), so it is both unreproducible and match-fragile.

## How to verify on a Mac

1. **Find the real storage key:** let Finder (Get Info → Tags) or `tag --add`
   write a tag, then `xattr -l <file>` → the name macOS itself writes is
   authoritative.
2. **Test Spotlight indexing:** `mdimport <file>` then
   `mdfind 'kMDItemUserTags == "<tag>"'`. Empty after ~1 min in an indexed
   folder = not indexed (not lag).
3. **Test sync:** round-trip the file through the *actual* transport (iCloud
   Drive / AirDrop) and re-check on the other device — behaviour differs per
   transport (see the [Transport summary](#transport-summary) above).

## Practical consequences for fileregister

- `tags` backend & ★ marker → store in `_kMDItemUserTags`, query
  `kMDItemUserTags`.
- `kMDItemProjects` (quiet binder cache) → not Finder-visible, not synced; fine,
  it is a regenerable local cache.
- Record id → for cross-device, stored with `#S` (syncs, not searchable —
  resolved via the index, enumerated via the ★ marker).

See also: [SPEC.md](SPEC.md) §macOS metadata layer.
