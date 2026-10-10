# Who writes what

fileregister keeps its data in up to four places: the **index**
(`collections/*.jsonl`), the **bookmark store** (`~/.local/share/bookmarks.json`),
the **attributes on the file**, and, if you use them, the **notes** (Markdown
sidecars). Without notes, fileregister is the index and what hangs off it: the
first table is all of it, and it runs without grubber. Notes add the second
table, and they are read through grubber.

Both tables are derived from the code (the calls each command makes), not from
intent; where the two disagree, the code is what they show.

Legend: **new** creates · **set** writes or overwrites · **add** adds a value
and keeps the others · **restore** writes only where missing · **edit** changes
in place · **out** removes · — leaves alone

## Without notes: the register

On the file: `kMDItemInformation` holds the id where Spotlight searches it,
`com.fileregister.id#S` the same id where iCloud Drive leaves it; ★ is the
managed marker (members only); binder tags are the binder names in
`kMDItemProjects` or Finder tags, per the record's `xattr:` backend.
`kept_tags` is an index field: Finder tags the user had before a binder of that
name.

| Command | Index | Bookmark store | `kMDItemInformation` | `id#S` | ★ | Binder tags | `kept_tags` |
|---|---|---|---|---|---|---|---|
| `add` (new file) | new record | new | set | set | set (member) | add | set¹ |
| `add` (file has an id) | binder added (new record if the id is unknown) | — | — | — | set (member) | add | set¹ |
| `remove` | binder out | — | — | — | out (last binder) | out (not a kept tag) | — |
| `forget`† | record out | out | id out | out | — | — | — |
| `rename`† | binder renamed | — | — | — | — | old out, new add | renamed |
| `refresh` | — | renewed (stale) | restore | set | set (member) | add | — |
| `repair` | — | re-bound | add | set | set (member) | add | — |
| `unmarshal` | new records | new | add | set | set (member) | add | — |
| `write` (`.jsonl` target) | records | — | — | — | set (`_ref_path`) | add (`_ref_path`) | — |
| `cleanup --prune`, `--interactive`† | — | unrepairable entries out | — | — | — | — | — |

Read only: `list`, `resolve`, `of`, `audit`.

¹ Only with the `tags` backend, when the Finder tag was on the file before the
binder. † Also has a part in the notes (second table), and needs grubber for it,
notes or not.

- **Every attribute on the file has a restorer.** The id in both carriers, ★
  and the binder tags are derived from index and bookmark store; `refresh` and
  `repair` write them back. `refresh` restores `kMDItemInformation` only where
  it is missing, so a run with nothing to restore writes nothing.
- **The id**: a new file gets it from `add` (set: other ids on a fresh file are
  a copy's); `repair`, `refresh` and `unmarshal` add it; only `forget` takes it
  off. `add` on a file that already has its id takes it over without rewriting
  the carriers; `refresh` restores what is missing.
- **Only `forget` deletes a record**, and only one in no binder. `remove` leaves
  a bookmark.
- **★ leaves a file** only with its last binder (`remove`). A record in no
  binder carries none, so `forget` has none to take off.

## With notes

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

² `promote` and `add --md` first look for a block `forget` left: one id-less
block of the binder under a heading naming the file gets the id back, instead of
a second block being written.

grubber reads the notes in two ways, always with `--no-config`, so the user's grubber config does not change what register sees. As records, blocks as they stand
(`-b --inherit=`): `annotate`, `aka`, `cleanup`, `forget`, `marshal`, `reindex`,
`rename`, `list --inbox/--curated` and `order` (the binder's note). For the
album, with the note's frontmatter inherited into every block (`-b`). A writer
parses the one note it edits itself (`promote`, `annotate`, `order move`,
`rename`, `forget`, `album --title`): to change a block it needs the text around
it, which grubber does not give.

- **Blocks are created** by `promote`, `add --md`, `order move` and `write`;
  **deleted** only by `cleanup --interactive`, on the user's word; their **id is
  taken out** only by `forget`, which leaves the block.
- **The frontmatter has one writer** and one field: `album --title`.
