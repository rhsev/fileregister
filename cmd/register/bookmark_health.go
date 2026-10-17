package main

// bookmark_health — the identity layer judged against the index.
//
// One classifier, two readers: `audit` prints every finding, `cleanup` offers
// the three no record needs. Keeping it in one place is deliberate — the
// two commands disagreeing about what counts as an orphan is a bug this
// codebase has already had once, when cleanup judged against every ref record
// and audit against the bindered ones alone.

import (
	"github.com/rhsev/fileregister/v2/internal/index"

	"fmt"
	"path/filepath"
	"sort"
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

// prunable reports whether no record needs the entry, and so whether it is
// offered for deletion. Since 2.1 the store is register's alone, so an orphan
// is a leftover like a dead entry, not something another tool may hold. Broken
// belongs to `repair`, and unreachable was never judged.
func (f bookmarkFinding) prunable() bool {
	return f.kind == bmDead || f.kind == bmMalformed || f.kind == bmOrphan
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
	// noCoverage: the engine does not offer `searchable`, so whether its search
	// covers a place cannot be asked, and no miss is called dead.
	noCoverage bool
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
					"resolves, but no index record claims this id — register cleanup --prune drops it"})
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
		default:
			if gap := coverageGap(r.LastPath, &rep); gap != "" {
				out = append(out, bookmarkFinding{bmUnreachable, id, label, r.LastPath, gap})
			} else {
				out = append(out, bookmarkFinding{bmDead, id, label, r.LastPath,
					"neither resolvable nor carried by any file — nothing to repair"})
			}
		}
	}
	rep.findings = out
	return rep, nil
}

// coverageGap says why a miss on lastPath proves nothing, or "" when the
// engine's search covers that place and the dead verdict may stand. The engine
// answers (searchable), on every platform: on macOS whether Spotlight indexes
// the volume, on Linux whether the engine walks that place completely. An
// engine that does not offer the op cannot say, and that withholds the verdict
// the way a missing last_path does. An empty lastPath is not a place question,
// as with volumeMounted.
func coverageGap(lastPath string, rep *bookmarkReport) string {
	if lastPath == "" {
		return ""
	}
	covered, known := index.Searchable(lastPath)
	switch {
	case !known:
		rep.noCoverage = true
		return "this fileanchor cannot say whether its search covers that place — update it, so a gone file can be told from one nobody searched for"
	case !covered:
		return noIndexHint
	}
	return ""
}

// isMintedID reports whether a bookmarks.json key has the shape register gives
// an id. A hand edit can leave anything there.
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
