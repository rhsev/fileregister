# Findings: macOS file attributes (for fileregister)

> Hard-won, empirically verified facts about macOS xattr / Spotlight metadata.
> Verified on macOS 26.4, 2026-06. **Trust the tables, not intuition** — several
> of these are counterintuitive and cost real debugging time.

## The one that bites: storage name ≠ Spotlight key

Finder Tags live under **two different names** that constantly get conflated:

- **xattr storage name** (what you write/read with `xattr`):
  `com.apple.metadata:_kMDItemUserTags` — **with leading underscore**.
- **Spotlight query key** (what you use in `mdfind`):
  `kMDItemUserTags` — **no underscore**.

So the working pattern is asymmetric:

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
| `com.apple.metadata:kMDItemInformation` | `kMDItemInformation` | bookmark id (local mdfind repair) | space-separated string | no | yes (local) | **no** |
| `com.apple.metadata:kMDItemInformation#S` | — | bookmark id + sync flag — **does NOT sync** (Apple `kMDItem*` prefix blocked regardless of `#S`) | string | no | no | **no** |
| `com.fileregister.id#S` | — | bookmark id, cross-device — **syncs** (custom namespace + `#S`) | string | no | no | **yes — verified** |

## iCloud Drive sync (macOS 26.4 — Howard Oakley)

- iCloud Drive **stopped syncing `com.apple.metadata:kMDItem*` by default.**
  Only these still sync reliably: `_kMDItemUserTags` (Finder Tags),
  `com.apple.lastuseddate#PS`, `com.apple.quarantine`, `com.apple.TextEncoding`.
- Append **`#S`** to the xattr *name* (`kMDItemInformation#S`) → FileProvider
  syncs it — **but** it is then **not** Spotlight-indexed and **not** Finder-shown
  (the `#S` is treated as part of the type name by Spotlight/Finder). Useful only
  for data you resolve yourself (e.g. a bookmark id), never for searchable/visible
  metadata.
- The `#S` behaviour is undocumented and has shifted between releases — **always
  re-verify with a real round-trip** before relying on it.

### Verified cross-device (iCloud Drive, our test — fresh file, read-only on the receiver)

| xattr written on Mac A | arrived on Mac B? |
|---|---|
| `com.apple.metadata:_kMDItemUserTags` (★ Finder tag) | **yes** |
| `com.fileregister.id#S` (custom namespace + `#S`) | **yes** |
| `com.apple.metadata:kMDItemInformation#S` (Apple key + `#S`) | **no** |
| `com.apple.metadata:kMDItemProjects` (Apple key, no flag) | **no** |

**Refinement of Howard:** `#S` syncs a **custom-namespace** xattr
(`com.fileregister.*`) but **not** a `com.apple.metadata:kMDItem*` name — that
prefix is blocked regardless of `#S`. (Howard reported Apple-key `#S` syncing;
on this macOS build it did not. Re-verify per OS version.)

### Verified cross-device (Resilio Sync — fresh file, read-only on the receiver)

| xattr written on Mac A | arrived on Mac B? |
|---|---|
| `com.apple.metadata:_kMDItemUserTags` (★ Finder tag) | **yes** |
| `com.fileregister.id#S` (custom + `#S`) | **no** |
| `com.fileregister.id` (plain custom) | **no** |
| `com.apple.metadata:kMDItemProjects` | **no** |

**Resilio syncs Finder tags only; it strips all other xattrs** (custom and
Apple alike — `#S` is meaningless to it, it's an iCloud/FileProvider flag).

### Verified cross-device (AirDrop — fresh file, read on the receiver)

AirDrop preserves **all** xattrs verbatim — it wraps the file in an archive and
copies extended attributes unfiltered. Confirmed survivors: `_kMDItemUserTags`
(★), `com.fileregister.id#S`, `com.apple.metadata:kMDItemInformation#S`,
**and** `com.apple.metadata:kMDItemProjects` (the ones iCloud strips). It is the
most metadata-faithful transport of the three — the opposite of Resilio.

> **TCC gotcha:** AirDrop'd files land in `~/Downloads` (TCC-protected) and carry
> a `com.apple.macl` (app access-control) xattr, so a plain terminal gets
> `Operation not permitted` from `xattr`. Not a stripped attribute — grant the
> terminal Full Disk Access, or move the file (via Finder) to `~/` (home root is
> not TCC-protected) and read it there.

### Transport summary

| Transport | ★ | id (`com.fileregister.id#S`) | `kMDItemProjects` |
|---|---|---|---|
| iCloud Drive | ✅ | ✅ | ✗ |
| Resilio Sync | ✅ | ✗ | ✗ |
| AirDrop | ✅ | ✅ | ✅ |

**★ survives every transport → the cross-device keystone.** The id survives
iCloud + AirDrop, not Resilio. So reconcile is **transport-agnostic**: enumerate
via ★, read the id xattr **if present**, else match by filename against
the (separately-synced) records.

**Test discipline (this bit us repeatedly):**

- A same-Mac "Remove Download → re-download" is **not** a sync test — the local
  placeholder keeps all xattrs, so everything "survives" spuriously. You must
  read on a **different device**.
- On the receiving device, **read only** — do not `echo`/rewrite the file. A
  re-write there clobbers the synced xattrs *and syncs the bare version back*,
  poisoning the file everywhere. Use a **fresh filename** for each clean run.

## Tag value encoding

- Finder Tags are a **binary plist ARRAY of strings** — not a plain-string
  xattr. Write via a plist (`plutil -convert binary1` / Python `plistlib`) and
  `xattr -wx <hex>`, or use the `tag` CLI.
- Optional Finder **colour** suffix on a tag string: `"name\n<0-7>"`
  (newline + digit). Strip it when reading the name.

## Codepoint choice (sigils, markers)

- Prefer **ASCII** or **NFC-stable single codepoints**. mdfind matching depends
  on Unicode normalization; the wrong glyph silently fails to match.
- **Safe:** `★` U+2605, `°` U+00B0, `~` U+007E — no decomposition under any
  normalization form. (★ verified as a working tag value + mdfind match.)
- **Avoid:** composed / ZWJ / skin-tone emoji, and **dead-key diacritics** like
  `˚` U+02DA RING ABOVE — it NFKD-decomposes to space + combining ring and has
  three look-alikes (`° ∘ ◦`), so it is both unreproducible and match-fragile.

## How to verify on a Mac (the method that settled all of the above)

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
- Bookmark id → for cross-device, store with `#S` (syncs, not searchable —
  resolved via our own index, enumerated via the ★ marker).

See also: [SPEC.md](SPEC.md) §macOS metadata layer.
