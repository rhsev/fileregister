package index

// WriteInPlace — how a Markdown note is rewritten. A note can itself be a
// member of a binder, and its bookmark must keep finding it: a temp file
// renamed over the note gives it a new inode, the bookmark then falls back to
// the path and breaks on the next rename (tested 2026-10-21). Overwriting the
// note keeps the file, and with it the inode, extended attributes, creation
// date, flags, ACLs and hard links.
//
// The crash safety the rename gave comes from a journal: the new content is
// written next to the note and synced first, then the note is overwritten and
// synced, then the journal goes. If the write stops halfway, the journal holds
// the complete new content, and every later write refuses until it is
// resolved. jot uses the same journal name, so either tool sees what the other
// left behind.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// JournalPath is where WriteInPlace keeps the new content while it writes:
// .<name>.journal next to the note. A fixed name, so a leftover is found.
func JournalPath(path string) string {
	return filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".journal")
}

// WriteInPlace overwrites the note at path with data. A note that does not
// exist yet is created through AtomicWrite: there is no file to keep.
func WriteInPlace(path string, data []byte) error {
	if rp, err := filepath.EvalSymlinks(path); err == nil {
		path = rp
	}
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return AtomicWrite(path, data)
	}
	journal := JournalPath(path)
	if _, err := os.Lstat(journal); err == nil {
		return fmt.Errorf("%s: an earlier write did not finish; %s holds what it meant to write. Compare the two, then remove the journal", path, journal)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	if err := writeSynced(journal, data); err != nil {
		os.Remove(journal)
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		os.Remove(journal)
		return err
	}
	// Write first, then cut: the file is never shorter than what is written.
	if _, err := f.WriteAt(data, 0); err != nil {
		f.Close()
		return fmt.Errorf("%s: writing failed halfway (%v); %s holds the new content", path, err, journal)
	}
	if err := f.Truncate(int64(len(data))); err != nil {
		f.Close()
		return fmt.Errorf("%s: writing failed halfway (%v); %s holds the new content", path, err, journal)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("%s: syncing failed (%v); %s holds the new content", path, err, journal)
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Remove(journal)
}

func writeSynced(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
