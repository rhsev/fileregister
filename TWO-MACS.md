# One register on two Macs with Unison

This guide keeps one register on two Macs whose notes folder is synced with
[Unison](https://github.com/bcpierce00/unison) 2.53 or later. It assumes that
only one Mac is used at a time, and that a sync runs before switching.

## What travels and what stays

The register's index lies in the notes folder (`collections/*.jsonl`), so
Unison carries it along with the notes and the files in that folder. Both Macs
read the same records.

With `xattrs = true` in the profile, Unison also carries the extended
attributes: the Finder tags, the Finder comment, the id, the star (★ or ☆) and
whatever apps keep there. Adding and removing works in both directions.

- **A change to attributes only** is written into the existing file. The file
  keeps its inode, and with it its bookmark.
- **A change to the content** gives the other Mac a new copy with a new inode.
  The attributes come along. The bookmark there finds the copy by its path and
  is marked stale until `register refresh` renews it.
- **Bookmarks** stay on each Mac in `~/.local/share/fileregister/bookmarks.json`.
  A macOS bookmark only resolves on the Mac that made it, so this file is never
  synced.

## The Unison profile

Add two lines to the profile:

```
xattrs = true
xattrignore = Path com.apple.provenance
```

macOS sets `com.apple.provenance` itself and refuses a copy of it. Without the
ignore, every file fails.

**Switching an existing profile.** Unison's archive from before holds no
attributes, so the first run with `xattrs` counts every file whose attributes
differ between the Macs as changed on both sides and skips it. Resolve that
once, right after a normal run that had nothing left to do, with a preference
for the Mac whose attributes are complete:

```sh
unison notes -batch                                # without xattrs: Nothing to do
# add the two lines to the profile
unison notes -batch -prefer /Users/me/Notes        # once
unison notes -batch                                # Nothing to do
```

Run the `-prefer` line only on equal replicas. It also decides real content
conflicts in favour of that Mac.

## Set up the second Mac once

Install register and fileanchor on the second Mac, sync the notes folder, then
bind every record there from a snapshot of the first Mac:

```sh
# Mac A
register snapshot --no-files --out register.tar.gz

# Mac B, after copying register.tar.gz over
register restore register.tar.gz --dry-run
register restore register.tar.gz
```

A snapshot made with `--no-files` holds each file's place, size, sha256, free
Finder tags and comment, not its content. Because Mac B already has the same
index, restore does not write it. It takes over each file it finds with the
same content and leaves everything else alone.

Files that exist on Mac A only, for example on an external disk, cannot be
bound on Mac B. Map their volume to an empty folder, so that restore leaves
them without a file instead of parking them inside the notes folder:

```sh
register restore register.tar.gz --map volume:Archive=/tmp/empty
```

`register audit` on Mac B lists them as broken bookmarks. That is expected.

## Lost attributes

The free Finder tags and the comment are the one part of a file's metadata
that has no source in the index. A program that saves by replacing the file
can drop all attributes, and Unison then passes the loss on to the other Mac.

`register refresh --snapshot <file>` brings them back. A file that lost its id
lost everything, so refresh gives it the free tags and the comment that the
snapshot recorded. A file that still has its id keeps the tags it has, because
a tag missing there was taken off on purpose. The condition is a snapshot from
before the loss, so each Mac keeps its last one, taken after every sync.

## After each sync

Run the sync and the follow-up as one script on Mac A. Mac B is reachable as
`macb` over ssh, and the Unison profile is called `notes`.

```sh
#!/bin/sh
# sync-register.sh: sync the notes folder, then the register on both Macs.
last=.local/share/fileregister/last-snapshot.tar.gz   # below each home
unison notes -batch || exit 1

tmp=$(mktemp -d)
register refresh --snapshot "$HOME/$last"
register snapshot --no-files --since "$HOME/$last" --out "$tmp/a.tar.gz"
cp "$tmp/a.tar.gz" "$HOME/$last"

remote=$(ssh macb mktemp -d)
ssh macb "cat > $remote/a.tar.gz" < "$tmp/a.tar.gz"
ssh macb /bin/sh -s <<EOF
mkdir "$remote/empty"
register restore "$remote/a.tar.gz" --map volume:Archive="$remote/empty"
register refresh --snapshot "\$HOME/$last"
register snapshot --no-files --since "\$HOME/$last" --out "$remote/b.tar.gz"
cp "$remote/b.tar.gz" "\$HOME/$last"
EOF
ssh macb "cat $remote/b.tar.gz" > "$tmp/b.tar.gz"
ssh macb "rm -rf $remote"

register restore "$tmp/b.tar.gz"
rm -rf "$tmp"
```

On the first run there is no last snapshot yet, and refresh and snapshot say
so and stop. Leave out `--snapshot` and `--since` once, or take a snapshot to
that place before.

Name one `--map` for each disk that only Mac A has. Without it, restore parks
those records in `collections/import/` inside the notes folder, and the next
sync carries that folder to Mac A. `restore`, `refresh` and `snapshot` exit
with 1 when a record has no file on that Mac, so the script does not stop on
their exit status. In a non-interactive ssh session, register has to be on the
`PATH` and has to know the notes folder (`GRUBBER_NOTES` or `GRUBBER_SET`).

A cron job, a launchd agent or a button can start the script. In our setup it
is a button on a home dashboard.

## How long a run takes

Measured on a Mac mini M4 with an internal SSD and warm caches:

| Register | `snapshot --no-files` | `refresh` |
|---|---|---|
| 202 files | 0.2 s | 0.2 s |
| 2,000 files, 195 MB | 0.5 s | |
| 20,000 files | 4.5 s | 4.8 s |
| one file of 2 GB | 0.9 s | |
| 200 files, 2 GB, `--since` | 0.04 s | |

A snapshot without `--since` reads every registered file once to compute its
sha256, so its time grows with the number of files (about 0.2 ms each) and
with their total size. Hashing runs at about 2.4 GB/s, so below that the disk
sets the pace. A collection of 60 GB of photos and PDFs needs about half a
minute on a fast internal disk, and far longer on a slow or external one.

With `--since` the previous snapshot, only files whose size or modification
time changed are read. For 200 files of 10 MB each (2 GB) a snapshot took
0.83 s, the next one `--since` it 0.04 s, and 0.05 s after one file had
changed. What remains is the time per file, so frequent snapshots stay
practical for large collections too.

An estimate from these numbers, not measured: a photo collection of 60 GB in
about 20,000 files.

| Disk | Read speed | Without `--since` | With `--since` |
|---|---|---|---|
| internal SSD | 3 to 5 GB/s (hashing sets the pace) | about 30 s | about 4 s |
| external NVMe over USB4 or Thunderbolt | 2 to 3 GB/s | about 30 s | about 4 s |
| external SATA SSD over USB | about 450 MB/s | about 2.5 min | about 4 s |
| hard disk | 100 to 150 MB/s | 7 to 10 min | about 5 to 30 s |

The first snapshot has nothing to compare against and takes the time of the
middle column, once. With `--since`, a hard disk needs longer when the file
system's records of the 20,000 files are not cached and the heads have to
move for each of them. New photos add only their own reading time.

## What to expect

- **Between two syncs the Macs differ.** A record added on one Mac has no
  bookmark on the other until the next run.
- **Conflicts stay with you.** When attributes changed on both Macs, or the
  content on one and the attributes on the other, Unison skips the file and
  reports it.
- **restore refuses when the indexes differ.** That happens when the sync
  stopped on a conflict. Resolve the conflict, sync again, and run the script
  again.
- **Change records on one Mac at a time.** `add`, `remove`, `rename` and
  `forget` write the index. If both Macs change it before a sync, Unison
  reports a conflict on the index file.
- **Nothing is lost on the way.** The index is the source of truth for
  records, ids and stars, and the last snapshot is the source for free tags
  and comments.
