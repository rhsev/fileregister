package main

// bookmark_health — the identity layer judged against the index.
//
// One classifier, two readers: `audit` prints every finding, `cleanup` offers
// the two that are beyond repair. Keeping it in one place is deliberate — the
// two commands disagreeing about what counts as an orphan is a bug this
// codebase has already had once, when cleanup judged against every ref record
// and audit against the bindered ones alone.

import (
	"github.com/rhsev/fileregister/internal/index"

	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Bookmark verdicts, worst-to-benign in the order a reader cares about.
const (
	bmMalformed   = "MALFORMED"   // the key was never a minted id
	bmDead        = "DEAD"        // does not resolve, no file carries the id
	bmBroken      = "BROKEN"      // does not resolve, but a file carries the id
	bmOrphan      = "ORPHAN"      // resolves, but no record claims the id
	bmUnreachable = "UNREACHABLE" // its volume is away; not judgeable
)

type bookmarkFinding struct {
	kind, id, label, path, hint string
}

// prunable reports whether a verdict is beyond repair, and so safe to offer for
// deletion. Orphans are excluded on purpose: a resolving bookmark no record
// claims may be held deliberately. Broken belongs to `repair`, and unreachable
// was never judged.
func (f bookmarkFinding) prunable() bool {
	return f.kind == bmDead || f.kind == bmMalformed
}

// classifyBookmarks judges every entry of the identity store. `records` must be
// every ref record, those in no binder included: a record kept in no binder on
// purpose is a record like any other, and judging against the bindered set alone
// would call every one of them an orphan.
// bookmarkReport is what one pass over the identity store yields.
type bookmarkReport struct {
	findings []bookmarkFinding
	ids      []string
	// blind: the engine answered a failed resolve without a recorded path, so
	// the volume guards had nothing to judge. Set only when there was something
	// to judge and none of it carried a path — a capability, tested rather than
	// read off a version number.
	blind bool
}

func classifyBookmarks(db map[string]string, records []map[string]any) (bookmarkReport, error) {
	known := map[string]bool{}
	labels := map[string]string{}
	for _, r := range records {
		id := index.AsString(r["id"])
		known[id] = true
		if fn := index.AsString(r["filename"]); fn != "" {
			labels[id] = fn
		}
	}

	ids := make([]string, 0, len(db))
	for id := range db {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	rep := bookmarkReport{ids: ids}
	if len(ids) == 0 {
		return rep, nil
	}

	res, err := index.BatchResolve(ids)
	if err != nil {
		return rep, err
	}

	// fileanchor before 1.2.0 answered a failed resolve without last_path. The
	// guards then see an empty path, read it as "not a volume question", and
	// every unresolvable bookmark lands on the dead verdict — the one a prune
	// acts on. So the capability is measured first, and when it is missing the
	// dead verdict is withheld entirely.
	failed, withPath := 0, 0
	for _, id := range ids {
		if res[id].Path == "" {
			failed++
			if res[id].LastPath != "" {
				withPath++
			}
		}
	}
	rep.blind = failed > 0 && withPath == 0

	var out []bookmarkFinding
	for _, id := range ids {
		r := res[id]
		label := labels[id]
		if label == "" {
			if r.Path != "" {
				label = filepath.Base(r.Path)
			} else if r.LastPath != "" {
				label = filepath.Base(r.LastPath)
			}
		}
		switch {
		case !isMintedID(id):
			out = append(out, bookmarkFinding{bmMalformed, id, label, "",
				"not a minted id — no record can carry it"})
		case r.Path != "":
			if !known[id] {
				out = append(out, bookmarkFinding{bmOrphan, id, label, r.Path,
					"resolves, but no index record claims this id"})
			}
		case len(index.ByDescriptionID(id)) > 0:
			out = append(out, bookmarkFinding{bmBroken, id, label, r.LastPath,
				"the file carries this id elsewhere — run: register repair"})
		case rep.blind:
			out = append(out, bookmarkFinding{bmUnreachable, id, label, "",
				"this fileanchor does not report the recorded path — update to 1.2.0 so a gone file can be told from an absent volume"})
		case !volumeMounted(r.LastPath):
			out = append(out, bookmarkFinding{bmUnreachable, id, label, r.LastPath,
				"the volume is not mounted — cannot tell a moved file from a gone one"})
		case !volumeSearchable(r.LastPath):
			out = append(out, bookmarkFinding{bmUnreachable, id, label, r.LastPath,
				"Spotlight does not index that volume — \"no file carries this id\" is unknowable there"})
		default:
			out = append(out, bookmarkFinding{bmDead, id, label, r.LastPath,
				"neither resolvable nor carried by any file — nothing to repair"})
		}
	}
	rep.findings = out
	return rep, nil
}

// isMintedID reports whether a bookmarks.json key has the shape register gives
// an id. A foreign writer on the shared store can leave anything there.
func isMintedID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// volumesDir is where detachable volumes appear. A variable rather than a
// constant so the guard can be tested against a directory the test controls:
// the real /Volumes holds whatever the developer's machine happens to have
// mounted, and a test asserting about it passes at home and fails on a runner.
var volumesDir = "/Volumes"

// volumeName returns the volume component of a path under volumesDir, or "" for
// a path that does not live on a detachable volume.
func volumeName(path string) string {
	prefix := volumesDir + "/"
	if path == "" || !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(path, prefix)
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		return rest[:i]
	}
	return rest
}

// volumeMounted reports whether the volume a path lived on is present. An
// absent volume makes every bookmark on it look dead: the blob cannot resolve
// and Spotlight cannot index what is not mounted. "Dead" is the one verdict
// something may later be deleted on, so the unjudgeable case gets its own name.
//
// Only /Volumes/<name> is treated as detachable; a path on the boot volume is
// always reachable, and an empty last-known path is not a volume question.
func volumeMounted(lastPath string) bool {
	name := volumeName(lastPath)
	if name == "" {
		return true
	}
	st, err := os.Stat(filepath.Join(volumesDir, name))
	return err == nil && st.IsDir()
}

// volumeSearchable reports whether Spotlight indexes the volume a path lived
// on. Without an index, mdfind answers "nothing" for every id, and the dead
// verdict would rest on a question that was never asked. Measured on this
// machine: /Volumes/docker is mounted and not indexed, so a bookmark to a file
// sitting right there would otherwise read as dead.
//
// mdutil is asked once per volume and cached: a store of a few hundred entries
// otherwise spawns it a few hundred times.
var volumeIndexed = map[string]bool{}

func volumeSearchable(lastPath string) bool {
	name := volumeName(lastPath)
	if name == "" {
		return true // the boot volume; its index is the one we rely on anyway
	}
	if known, seen := volumeIndexed[name]; seen {
		return known
	}
	out, err := exec.Command("mdutil", "-s", filepath.Join(volumesDir, name)).Output()
	// mdutil missing or erroring is not evidence of a missing index; assume
	// searchable rather than parking every entry on the volume.
	indexed := err != nil || !strings.Contains(string(out), "Indexing disabled")
	volumeIndexed[name] = indexed
	return indexed
}

// bookmarkFindingLine prints one finding the way both commands show it.
func bookmarkFindingLine(f bookmarkFinding) {
	lbl := f.label
	if lbl == "" {
		lbl = "(no name)"
	}
	fmt.Printf("  %s [%s] %s\n", f.kind, f.id, lbl)
	if f.path != "" {
		word := "path"
		if f.kind != bmOrphan {
			word = "last seen"
		}
		fmt.Printf("    %s: %s\n", word, f.path)
	}
	fmt.Printf("    → %s\n", f.hint)
}
