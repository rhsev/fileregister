# Where fileregister writes what

fileregister keeps four stores consistent: the **index** (`collections/*.jsonl`),
the **bookmark store** (`~/.local/share/bookmarks.json`), the **attributes on the
file** and the **notes** (Markdown). This page says which command touches which
of them, and how. It is derived from the code (the calls each command makes), not
from intent; when the two disagree, the code is what this page shows, and the
disagreement is a finding below.

Stores on the file: `kMDItemInformation` holds the id where Spotlight searches it;
`com.fileregister.id#S` holds the same id where iCloud Drive leaves it; ★ is the
managed marker (members only); binder tags are the binder names in
`kMDItemProjects` or Finder tags, per the record's `xattr:` backend. `kept_tags`
is an index field: Finder tags the user had before a binder of that name.

## Writes

**new** creates · **set** writes or overwrites · **add** adds a value, keeps the
others · **restore** writes only where missing · **edit** changes in place ·
**out** removes · — leaves alone

| Command | Index | Bookmark store | `kMDItemInformation` | `id#S` | ★ | Binder tags | `kept_tags` | Note blocks | Frontmatter |
|---|---|---|---|---|---|---|---|---|---|
| `add` (new file) | new record | new | set | set | set (member) | add | set¹ | new (`--md`)² | — |
| `add` (file has an id) | binder added (new record if the id is unknown) | — | — | — | set (member) | add | set¹ | new (`--md`)² | — |
| `remove` | binder out | — | — | — | out (last binder) | out (not a kept tag) | — | — | — |
| `forget` | record out | out | id out | out | — | — | — | id line out | — |
| `rename` | binder renamed | — | — | — | — | old out, new add | renamed | binder line edited; note file renamed | — |
| `refresh` | — | renewed (stale) | restore | set | set (member) | add | — | — | — |
| `repair` | — | re-bound | add | set | set (member) | add | — | — | — |
| `promote` | — | — | — | — | — | — | — | new lean block, or id restored² | — |
| `annotate` | — | — | — | — | — | — | — | fields, prose edited | — |
| `aka --add` | handle added | — | — | — | — | — | — | — | — |
| `aka --remove` | handle out | — | — | — | — | — | — | handle out; id set where the handle was the link | — |
| `order move` | — | — | — | — | — | — | — | `sort:` key (new lean block if none) | — |
| `album --title` | — | — | — | — | — | — | — | — | `album:` set or out |
| `reindex` | new records from blocks | — | — | — | — | — | — | — | — |
| `cleanup --interactive` | — | unrepairable entries out | — | — | — | — | — | stale and unindexed blocks out | — |
| `cleanup --prune` | — | unrepairable entries out | — | — | — | — | — | — | — |
| `write` | records (`.jsonl` target) | — | — | — | set (`_ref_path`) | add (`_ref_path`) | — | new (`.md` target) | — |
| `unmarshal` | new records | new | add | set | set (member) | add | — | whole notes placed | — |

Read only: `list`, `resolve`, `of`, `audit`, `marshal`, `album` without
`--title`, `order show`.

¹ Only with the `tags` backend, when the Finder tag was on the file before the
binder. ² `promote` and `add --md` first look for a block `forget` left: one
id-less block of the binder under a heading naming the file gets the id back
instead of a second block being written.

## Reads

| Reads | Commands |
|---|---|
| Notes through grubber, blocks as they stand (`-b --inherit=`) | `annotate`, `aka`, `cleanup`, `forget`, `marshal`, `reindex`, `rename`, `list --inbox/--curated`, `order` (the binder's note) |
| Notes through grubber, frontmatter inherited (`-b`) | `album` (fields) |
| The fileanchor engine (bookmarks, file attributes) | everything that resolves a path or writes to a file: `add`, `remove`, `forget`, `rename`, `refresh`, `repair`, `album`, `list <binder>`, `resolve`, `of`, `audit`, `marshal`, `unmarshal`, `write` (`_ref_path`) |

Writers parse the one note they edit themselves (`promote`, `annotate`,
`order move`, `rename`, `forget`, `album --title`): to change a block they need
the text around it, which grubber does not give.

## What the table shows

- **Every attribute on the file has a restorer.** The id in both carriers,
  ★ and the binder tags are derived from index and bookmark store, and
  `refresh` and `repair` write them back. `refresh` restores
  `kMDItemInformation` only where it is missing, so a run with nothing to
  restore writes nothing.
- **The id is written in one place per situation.** A new file gets it from
  `add` (set: other ids on a fresh file are a copy's); `repair`, `refresh` and
  `unmarshal` add it; only `forget` takes it off. `add` on a file that already
  has its id takes it over without rewriting the carriers — `refresh` restores
  what is missing.
- **Only `forget` deletes a record**, and only one in no binder. `remove` leaves
  a bookmark; `cleanup` never deletes records.
- **Blocks are created** by `promote`, `add --md`, `order move` and `write`;
  **deleted** only by `cleanup --interactive`, on the user's word; their **id is
  taken out** only by `forget`, which leaves the block.
- **The frontmatter has one writer** and one field: `album --title`.
- **★ leaves a file** only with its last binder (`remove`). `forget` has nothing
  to take off: a record in no binder carries no ★.

## Findings

- `cleanupDeleteMdBlock` has a branch for `.jsonl` "notes" that removes a binder
  from an index record, or the whole record when the block named no binder. It
  is unreachable: `cleanup` gets its items from the notes, read through grubber
  as `.md` only. It is also the one place outside `forget` that could delete a
  record. Candidate for removal.
