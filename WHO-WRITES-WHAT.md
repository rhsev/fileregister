# Who writes what

fileregister keeps its data in up to four places: the **index**
(`collections/*.jsonl`), the **bookmark store** (`~/.local/share/bookmarks.json`),
the **attributes on the file**, and, if you use them, the **notes** (Markdown
sidecars). This page shows which command writes to which of them.

## Key points

- **Only `forget` deletes a record**, and only one in no binder. `remove` keeps
  the record, in no binder once its last binder goes.
- **A note block is deleted only by `cleanup --interactive`**, and only when you
  confirm it. `forget` takes the id out of a block and leaves the block itself.
- **Every attribute on a file can be rebuilt.** The id, ★ and the binder tags
  derive from the index and the bookmark store, and `refresh` writes them back.
  A run with nothing to restore changes no file.
- **★ leaves a file only with its last binder** (`remove`).
- **A note's frontmatter has one writer**, `album --title`, and it touches one
  field, `album:`.
- **`list`, `resolve`, `of`, `audit`, `album` (without `--title`), `order show`
  and `marshal` only read.** They write nothing to any of the four places.

The tables below give the detail. They are derived from the code (the calls each
command makes), not from intent.

Legend: **new** creates · **set** writes or overwrites · **add** adds a value
and keeps the others · **restore** writes only where missing · **edit** changes
in place · **out** removes · — leaves alone

## Without notes: the register

Without notes, fileregister is the index and what hangs off it. This table is
all of it, and it runs without grubber.

The columns for the file and the index:

- `kMDItemInformation`: the id, where Spotlight searches it
- `id#S` (`com.fileregister.id#S`): the same id, where iCloud Drive leaves it
- ★: the managed marker, on members only
- Binder tags: the binder names, in `kMDItemProjects` or in Finder tags, per the
  record's `xattr:` backend
- `kept_tags`, an index field: Finder tags the user had before a binder of that
  name

| Command | Index | Bookmark store | `kMDItemInformation` | `id#S` | ★ | Binder tags | `kept_tags` |
|---|---|---|---|---|---|---|---|
| `add` (new file) | new record | new | set | set | set (member) | add | set¹ |
| `add` (file has an id) | binder added (new record if the id is unknown) | — | — | — | set (member) | add | set¹ |
| `remove` | binder out | — | — | — | out (last binder) | out (not a kept tag) | — |
| `forget`† | record out | out | id out | out | — | — | — |
| `rename`† | binder renamed | — | — | — | — | old out, new add | renamed |
| `refresh` | — | renewed (stale) | restore | set | set (member) | add | — |
| `repair` | `filename` edited (renamed file) | re-bound | add | set | set (member) | add | — |
| `unmarshal` | new records | new | add | set | set (member) | add | — |
| `write` (`.jsonl` target) | records | — | — | — | set (`_ref_path`) | add (`_ref_path`) | — |
| `cleanup --prune`, `--interactive`† | — | unrepairable entries out | — | — | — | — | — |

Read only: `list`, `resolve`, `of`, `audit`.

¹ Only with the `tags` backend, when the Finder tag was on the file before the
binder. † Also has a part in the notes (second table), and needs grubber for it,
notes or not.

- **The id.** A new file gets it from `add`, which sets it, because any other
  id on a newly added file came with a copy. `repair`, `refresh` and `unmarshal`
  add it, and only `forget` takes it off. `add` on a file that already has its
  id takes it over without rewriting the carriers. `refresh` restores what is
  missing, and writes `kMDItemInformation` only where the id is not there yet.
- **A record in no binder carries no ★**, so `forget` has none to take off.

## With notes

Notes are read through grubber.

| Command | Index | Note blocks | Frontmatter |
|---|---|---|---|
| `promote`, `add --md` | — | new lean block, or id restored² | — |
| `annotate` | — | fields, prose edited | — |
| `order move` | — | `sort:` key (new lean block if none) | — |
| `album --title` | — | — | `album:` set or out |
| `aka --add` | handle added | — (checked for conflicts) | — |
| `aka --remove` | handle out | handle out; id set where the handle was the link | — |
| `reindex` | new records from blocks | — | — |
| `rename` | (first table) | binder line edited; note file renamed | — |
| `forget` | (first table) | id line out; the block stays | — |
| `cleanup --interactive` | — | stale and unindexed blocks out | — |
| `write` (`.md` target) | — | new | — |
| `unmarshal` | (first table) | whole notes placed | — |

Read only: `album`, `order show`, `marshal`, `list --inbox/--curated`.

² `promote` and `add --md` first look for a block that `forget` left behind. If
exactly one id-less block of the binder sits under a heading naming the file, it
gets the id back, and no second block is written.

- **Blocks are created** by `promote`, `add --md`, `order move` and `write`.
