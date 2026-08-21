package index

// lock — cross-process advisory locks for the two mutable stores. flock-based:
// blocking, reentrant per process, released automatically at process exit.
// They serialize the read-modify-write cycles of the JSONL index (one lock per
// collections directory) and of bookmarks.json. Markdown notes stay unlocked —
// their writes are single-file appends and the notes live in git.
//
// CLI-scoped by design: there is deliberately no Unlock — the kernel releases
// everything when the short-lived process exits. Embedding this package in a
// daemon or GUI would need explicit release (and per-operation scoping) first.

import (
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

var (
	lockMu   sync.Mutex
	lockHeld = map[string]*os.File{}
)

// flockFile takes (or reuses) an exclusive advisory lock on lockPath, blocking
// until it is free. Held until process exit.
func flockFile(lockPath string) error {
	lockMu.Lock()
	defer lockMu.Unlock()
	if _, ok := lockHeld[lockPath]; ok {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return err
	}
	lockHeld[lockPath] = f
	return nil
}

// LockIndexDir serializes writers of the JSONL files in dir.
func LockIndexDir(dir string) error {
	return flockFile(filepath.Join(dir, ".register.lock"))
}

// LockBookmarks serializes writers of bookmarks.json.
func LockBookmarks() error {
	return flockFile(bookmarkFile() + ".lock")
}
