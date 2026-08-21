package index

// AtomicWrite — crash-safe file replacement for the plain-text layer. The JSONL
// index and the Markdown notes are the source of truth; a truncate+write
// (os.WriteFile) can leave them half-written on a crash or a full disk, so
// every rewrite goes through a temp file in the same directory + rename.

import (
	"os"
	"path/filepath"
)

func AtomicWrite(path string, data []byte) error {
	// Write through a symlinked target instead of replacing the link with a
	// regular file (the link's destination would keep the stale content).
	if rp, err := filepath.EvalSymlinks(path); err == nil {
		path = rp
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once the rename has happened
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// Keep an existing file's permissions; 0644 only for a fresh file.
	mode := os.FileMode(0644)
	if st, serr := os.Stat(path); serr == nil {
		mode = st.Mode().Perm()
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	// fsync the directory so the rename itself survives a crash.
	if d, derr := os.Open(dir); derr == nil {
		d.Sync()
		d.Close()
	}
	return nil
}
