package index

// jsonl_editor — in-place edits of the JSONL index: rewrite, remove/rename a
// binder, plus the path helpers.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"golang.org/x/text/unicode/norm"
)

// JSONLRewrite streams the file through fn line by line. fn returns (out, changed):
//
//	changed=false        → keep the original line verbatim
//	changed=true, out=nil → drop the record
//	changed=true, out set → serialize out (with _-prefixed provenance keys dropped)
//
// The file is rewritten only when at least one line changed. Returns the change
// count; a failed write returns the error and counts as zero changes applied.
func JSONLRewrite(path string, fn func(rec map[string]any) (map[string]any, bool)) (int, error) {
	if err := LockIndexDir(filepath.Dir(path)); err != nil {
		return 0, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var out []string
	changes := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		rec := ParseJSONObject(line)
		if rec == nil {
			out = append(out, line) // non-object → keep verbatim
			continue
		}
		result, changed := fn(rec)
		if !changed {
			out = append(out, line)
			continue
		}
		changes++
		if result != nil {
			out = append(out, JSONVal(dropUnderscoreKeys(result)))
		}
	}
	if changes > 0 {
		content := ""
		if len(out) > 0 {
			content = strings.Join(out, "\n") + "\n"
		}
		if err := AtomicWrite(path, []byte(content)); err != nil {
			return 0, err
		}
	}
	return changes, nil
}

// dropUnderscoreKeys returns a copy of m without keys starting with "_".
func dropUnderscoreKeys(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		if !strings.HasPrefix(k, "_") {
			out[k] = v
		}
	}
	return out
}

// JSONLRemoveBinder set-deletes binder from the record with the given id, keeping
// the record (an emptied set is a bookmark). Returns the number of records changed.
func JSONLRemoveBinder(path, id, binder string) (int, error) {
	return JSONLRemoveBinderMany(path, map[string]bool{id: true}, binder)
}

// JSONLRemoveBinderMany set-deletes binder from every record whose id is in ids,
// in one file pass (one read + one rewrite instead of a cycle per record).
// Returns the number of records changed.
func JSONLRemoveBinderMany(path string, ids map[string]bool, binder string) (int, error) {
	return JSONLRewrite(path, func(rec map[string]any) (map[string]any, bool) {
		if !ids[AsString(rec["id"])] {
			return rec, false
		}
		arr := AsStrings(rec["binder"])
		found := false
		newBinder := []any{}
		for _, b := range arr {
			if b == binder {
				found = true
				continue
			}
			newBinder = append(newBinder, b)
		}
		if !found {
			return rec, false
		}
		out := map[string]any{}
		for k, v := range rec {
			out[k] = v
		}
		out["binder"] = newBinder
		return out, true
	})
}

// JSONLRenameBinder replaces oldName with newName in every record's binder set
// (deduping the set). Returns the number of records changed.
func JSONLRenameBinder(path, oldName, newName string) (int, error) {
	return JSONLRewrite(path, func(rec map[string]any) (map[string]any, bool) {
		arr := AsStrings(rec["binder"])
		has := false
		for _, b := range arr {
			if b == oldName {
				has = true
				break
			}
		}
		if !has {
			return rec, false
		}
		seen := map[string]bool{}
		nb := []any{}
		for _, b := range arr {
			v := b
			if b == oldName {
				v = newName
			}
			if !seen[v] {
				seen[v] = true
				nb = append(nb, v)
			}
		}
		out := map[string]any{}
		for k, v := range rec {
			out[k] = v
		}
		out["binder"] = nb
		return out, true
	})
}

// realOrExpanded resolves symlinks when the path exists, else expands it; then
// canonicalizes for STRING comparison: NFC-normalized (the shell hands over
// NFC, Foundation's bookmark resolution returns NFD — same file, different
// bytes) and case-folded only where the platform's default filesystem folds
// case (foldCase: darwin yes, elsewhere identity). Only the fallback for
// nonexistent paths — existing files compare by inode, see PathKey.
func realOrExpanded(p string) string {
	if rp, err := filepath.EvalSymlinks(p); err == nil {
		return foldCase(norm.NFC.String(rp))
	}
	return foldCase(norm.NFC.String(ExpandPath(p)))
}

// PathKey canonicalizes a path for identity comparison: two keys are equal iff
// the paths name the same file. For an existing file the key is its
// device:inode pair — filesystem truth on every platform, immune to case
// mapping, Unicode form and symlinks (a case-sensitive APFS or ext4 resolves
// the lookup itself, so no folding is guessed). A nonexistent path falls back
// to the normalized string form.
func PathKey(p string) string {
	if st, err := os.Stat(p); err == nil {
		if sys, ok := st.Sys().(*syscall.Stat_t); ok {
			return fmt.Sprintf("dev%d:ino%d", sys.Dev, sys.Ino)
		}
	}
	return realOrExpanded(p)
}

// PathsEqual reports whether two paths name the same file. When both exist the
// filesystem answers (os.SameFile); when exactly one exists they cannot be the
// same file — the filesystem already applied its own case/normalization rules
// during lookup; only two nonexistent paths fall back to string comparison.
func PathsEqual(a, b string) bool {
	sa, ea := os.Stat(a)
	sb, eb := os.Stat(b)
	if ea == nil && eb == nil {
		return os.SameFile(sa, sb)
	}
	if (ea == nil) != (eb == nil) {
		return false
	}
	return realOrExpanded(a) == realOrExpanded(b)
}

// JSONLSetKeptTags updates the kept_tags of records in path: changes maps
// id → tag → true (the tag was already the user's: keep it) or false
// (fileregister set it itself: release it). One rewrite for the file.
func JSONLSetKeptTags(path string, changes map[string]map[string]bool) (int, error) {
	return JSONLRewrite(path, func(rec map[string]any) (map[string]any, bool) {
		ch, ok := changes[AsString(rec["id"])]
		if !ok {
			return rec, false
		}
		kept := KeptTags(rec)
		var next []any
		seen := map[string]bool{}
		for _, t := range kept {
			if keep, touched := ch[t]; touched && !keep {
				continue
			}
			seen[t] = true
			next = append(next, t)
		}
		added := make([]string, 0, len(ch))
		for t, keep := range ch {
			if keep && !seen[t] {
				added = append(added, t)
			}
		}
		sort.Strings(added)
		for _, t := range added {
			next = append(next, t)
		}
		if len(next) == len(kept) {
			same := true
			for i, t := range next {
				if t != kept[i] {
					same = false
				}
			}
			if same {
				return rec, false
			}
		}
		if len(next) == 0 {
			delete(rec, "kept_tags")
		} else {
			rec["kept_tags"] = next
		}
		return rec, true
	})
}
