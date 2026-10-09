# Ordering — arranging the members of a binder

A **binder** is a membership set: a file is in it or not, recorded once in the
JSONL index, queryable as a field. "binder" always means the *set*.

An **ordering** is separate: a *presentation* that puts a binder's members into
a sequence. It lives outside the index, in the binder's Markdown note, as
`sort:` keys on the member blocks. A binder may have an ordering or none; each
consumer declares which it reads — a binder (set) or an ordering (sequence).

## The data

An ordering lives in **one Markdown file**, the binder's canonical note
(`collections/binder_<name>.md`), as the member blocks themselves:

- **The document is the order.** Members follow their blocks in the order the
  blocks stand in the note. Moving a block up in the editor moves the member.
  Renaming a file moves nothing: its block stays where it is.
- **A `sort:` key sets the order** for a member where the document should not
  decide. Keyed members come first; `register order move` writes the keys.
- Members **without a block** in the note (not promoted yet) come last, by file
  name.

```markdown
### IMG_2041.jpg
```yaml
type: ref
id: '270450536'
binder: safari
sort: i               # optional: set by `order move`, wins over the document
```

### IMG_2050.jpg
```yaml
type: ref
id: '270450612'
binder: safari
```
```

There is nothing to configure. Earlier versions wrote a `type: ordering` block
with a `rule: name` line (file-name order for unkeyed members); such a block is
harmless. Its `rule:` is ignored, and its `binder:` still guards `--note`
against a note that orders another binder.

## Resolution

1. Members with a `sort:` key come first, sorted by `(sort, id)` as plain
   strings.
2. The rest follow in document order: the order their blocks stand in the note.
3. Members without a block come last, by file name, then id.

The note is read through grubber, which returns blocks in document order; a
binder without a note needs no grubber.

## Keys

`sort:` keys are fractional lexicographic strings over `0-9a-z` (base 36).
Between any two keys a new one fits, so a single move writes a single key.
Hand-editing works: **string comparison is the whole contract**. Letters with
gaps (`b`, `d`, `f`) leave room to insert (a squeeze-in is `c` or `bc`); numbers
sort lexically (`'1' < '10' < '2'`), so they make poor keys. When `order move`
cannot place a single key (an unkeyed neighbour, or a degenerate hand-edited
pair), it materializes fresh keys for **all** members in the intended order.

## The CLI

```sh
register order show <binder> [--json]     # resolved order; --json for consumers
register order move <binder> <id|aka> --after <id|aka>
register order move <binder> <id|aka> --to <n>    # 1-based position
```

All subcommands accept `--note <file>.md` to address an ordering other than the
binder's canonical note.

`order show --json` emits one object per line: `{"position":1,"id":"…",
"filename":"…"}` — the contract binderview's drag-&-drop mode consumes
(`order move --to` writes, then the GUI reloads authoritatively).

## Album

An album *is* an ordering: `register album` prints the members in the order
`order show` resolves, as `position` on each line. See [ALBUM.md](ALBUM.md).

## Multiple orderings

The data model permits several ordering files per binder (`--note`); the
current convention is one, in the canonical note. Labels (`as:`) are unused for
now.
