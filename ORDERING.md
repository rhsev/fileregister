# Ordering — arranging the members of a binder

A **binder** is a membership set: a file is in it or not, recorded once in the
JSONL index, queryable as a field. "binder" always means the *set*.

An **ordering** is separate: a *presentation* that puts a binder's members into
a sequence. It lives outside the index, in the binder's Markdown note, as
`sort:` keys on the member blocks. A binder may have an ordering or none; each
consumer declares which it reads — a binder (set) or an ordering (sequence).

## The data

An ordering lives in **one Markdown file** (default: the binder's canonical
note, `collections/binder_<name>.md`) holding:

- one `type: ordering` block — the config;
- lean `type: ref` blocks with a `sort:` key — one per member that has been
  *placed*. Members without a key follow in rule order, after all keyed ones.

```markdown
### · safari (ordering)
```yaml
type: ordering
binder: safari        # the membership set this arranges
rule: name            # the base order for unkeyed members
```

### IMG_2041.jpg
```yaml
type: ref
id: '270450536'
binder: safari
sort: i               # fractional lexicographic key
```
```

### Config fields (`type: ordering`)

| field | values | meaning |
|---|---|---|
| `type` | `ordering` | discriminator |
| `binder` | a binder name | which membership set this orders |
| `rule` | `name` | the base order for members without a `sort:` key |

The config block is optional for *reading* (keys apply regardless); `order
move` creates it on first write. It is a fenced block, not frontmatter, because
grubber inherits frontmatter into every block of a file.

## Resolution

1. Compute the base order per `rule` (name: filename, then id). The note is read through grubber; a binder without a note needs none.
2. Members with a `sort:` key come first, sorted by `(sort, id)` as plain
   strings.
3. Members without a key follow, in base order.

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
register order set  <binder> [--rule name]        # create/update the config
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
