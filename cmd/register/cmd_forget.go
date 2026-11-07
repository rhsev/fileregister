package main

// cmd_forget — take a record out of the register for good: its index line, its
// bookmark entry, its id in the notes and on the file, and the star. What the
// notes say about it stays: a block keeps its heading and fields and only
// loses its id, so reindex cannot bring the record back from it, and promote
// reattaches it should the file ever return (by its heading, the file name).
//
// Records in no binder, and members whose file is gone. A member whose file
// exists leaves its binders with remove first, which also takes the membership
// marks off the file; forget is the last step.

import (
	"github.com/rhsev/fileregister/v2/internal/index"

	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const forgetUsage = "Usage: register forget <id|aka>... [--dry-run]"

func cmdForget(args []string) int {
	_, bools, pos, unk := parseFlags(args, nil, map[string]bool{"--dry-run": true})
	if unk != "" {
		return unknownOption("forget", unk)
	}
	if bools["--help"] {
		fmt.Println(forgetUsage)
		return 0
	}
	if bools["--version"] {
		fmt.Println("register forget " + registerVersion)
		return 0
	}
	if len(pos) == 0 {
		fmt.Fprintln(os.Stderr, forgetUsage)
		return 1
	}
	dry := bools["--dry-run"]

	nd, err := notesDir()
	if err != nil {
		return 1
	}
	refs, ok := loadRefs(nd)
	if !ok {
		return 1
	}

	// Every key must name a record in no binder, or nothing happens at all.
	var targets []map[string]any
	seen := map[string]bool{}
	for _, key := range pos {
		rec := index.ResolveKey(refs, key)
		if rec == nil {
			fmt.Fprintf(os.Stderr, "Error: '%s' does not match any record id or aka\n", key)
			return 1
		}
		if bs := nonEmptyBinders(rec); len(bs) > 0 && index.URLRef(rec) {
			fmt.Fprintf(os.Stderr, "Error: %s is in binder(s) %s — register remove it from them first; forget takes records in no binder\n",
				refLabel(rec), strings.Join(bs, ", "))
			return 1
		}
		if id := index.AsString(rec["id"]); !seen[id] {
			seen[id] = true
			targets = append(targets, rec)
		}
	}

	// The keys a block may name a record by: its id, or a handle written in
	// the id slot by hand.
	keys := map[string]bool{}
	for _, rec := range targets {
		keys[index.AsString(rec["id"])] = true
		for _, a := range index.AkaList(rec) {
			if a != "" {
				keys[a] = true
			}
		}
	}
	annos, aerr := readAnnotations(nd)
	if aerr != nil {
		fmt.Fprintf(os.Stderr, "Error: %v — nothing forgotten\n", aerr)
		return 1
	}
	blocksIn := map[string]int{}
	for _, a := range annos {
		if keys[index.AsString(a["id"])] {
			blocksIn[index.AsString(a["_note_file"])]++
		}
	}
	var notes []string
	for n := range blocksIn {
		notes = append(notes, n)
	}
	sort.Strings(notes)

	db, derr := lockedBookmarkDB()
	if derr != nil {
		fmt.Fprintf(os.Stderr, "Error: %v — nothing forgotten\n", derr)
		return 1
	}
	if why := membersNotGone(targets, db, refs); why != "" {
		fmt.Fprintln(os.Stderr, "Error: "+why)
		return 1
	}
	resolved, _ := resolveRecordPaths(targets)

	if dry {
		for _, rec := range targets {
			id := index.AsString(rec["id"])
			fmt.Printf("[dry-run] forget %s%s: index line in %s", refLabel(rec), goneMemberNote(rec), filepath.Base(index.AsString(rec["_note_file"])))
			if _, has := db[id]; has {
				fmt.Print(", bookmark entry")
			}
			if p := resolved[id]; p != "" {
				fmt.Printf(", id on %s", p)
			}
			fmt.Println()
		}
		for _, n := range notes {
			fmt.Printf("[dry-run] id out of %d block(s) in %s; the blocks stay\n", blocksIn[n], n)
		}
		return 0
	}

	// 1. The notes first: a block that kept the id would let reindex rebuild
	// the record, so nothing else changes until they are done.
	for _, n := range notes {
		if _, err := mdForgetIDs(n, keys); err != nil {
			fmt.Fprintf(os.Stderr, "Error: taking the id out of %s failed: %v — nothing else changed\n", n, err)
			return 1
		}
		fmt.Printf("  id out of %d block(s) in %s; the blocks stay\n", blocksIn[n], filepath.Base(n))
	}

	// 2. The index, one rewrite per index file.
	byFile := map[string]map[string]bool{}
	for _, rec := range targets {
		f := index.AsString(rec["_note_file"])
		if byFile[f] == nil {
			byFile[f] = map[string]bool{}
		}
		byFile[f][index.AsString(rec["id"])] = true
	}
	for f, ids := range byFile {
		if _, err := index.JSONLRewrite(f, func(r map[string]any) (map[string]any, bool) {
			return nil, ids[index.AsString(r["id"])]
		}); err != nil {
			fmt.Fprintf(os.Stderr, "Error: removing the record(s) from %s failed: %v\n", f, err)
			return 1
		}
	}

	// 3. The bookmark store, and 4. the id on the file where it still exists.
	dropped := false
	for _, rec := range targets {
		id := index.AsString(rec["id"])
		parts := []string{"index"}
		if _, has := db[id]; has {
			delete(db, id)
			dropped = true
			parts = append(parts, "bookmark entry")
		}
		if p := resolved[id]; p != "" && index.FileExists(p) {
			if removed, why := index.ForgetFileID(p, id); why != "" {
				fmt.Fprintf(os.Stderr, "  Warning: the id stays on %s: %s\n", p, why)
			} else if removed {
				parts = append(parts, "id on the file")
			}
			// The star goes with the last id: a file another record still
			// claims stays marked.
			if len(index.OfFileIDs(p)) == 0 && index.StatusUnmark(p) {
				parts = append(parts, "☆ on the file")
			}
		}
		fmt.Printf("Forgot %s%s: %s\n", refLabel(rec), goneMemberNote(rec), strings.Join(parts, ", "))
	}
	if dropped {
		if err := index.SaveDB(db); err != nil {
			fmt.Fprintf(os.Stderr, "Error: saving bookmarks.json failed: %v — the bookmark entries stay\n", err)
			return 1
		}
	}
	return 0
}

// membersNotGone checks the records still in a binder. forget takes one only
// when its file is gone, judged as audit judges a dead bookmark: the bookmark
// does not resolve, no file carries the id, and the search covers the place
// the file was. Then there are no membership marks left to take off, and
// remove has nothing to work on. Returns why a member is refused, or "".
func membersNotGone(targets []map[string]any, db map[string]string, refs []map[string]any) string {
	sub := map[string]string{}
	var members []map[string]any
	for _, rec := range targets {
		if len(nonEmptyBinders(rec)) == 0 {
			continue
		}
		id := index.AsString(rec["id"])
		blob, ok := db[id]
		if !ok {
			return fmt.Sprintf("%s is in binder(s) %s and has no bookmark on this system, so whether its file is gone cannot be told — register remove it from them first",
				refLabel(rec), strings.Join(nonEmptyBinders(rec), ", "))
		}
		sub[id] = blob
		members = append(members, rec)
	}
	if len(members) == 0 {
		return ""
	}
	rep, err := classifyBookmarks(sub, refs)
	if err != nil {
		return fmt.Sprintf("fileanchor batch resolve failed: %v — nothing forgotten", err)
	}
	verdict := map[string]bookmarkFinding{}
	for _, f := range rep.findings {
		verdict[f.id] = f
	}
	for _, rec := range members {
		f, judged := verdict[index.AsString(rec["id"])]
		if judged && f.kind == bmDead {
			continue
		}
		bs := strings.Join(nonEmptyBinders(rec), ", ")
		switch {
		case !judged:
			return fmt.Sprintf("%s is in binder(s) %s and its file still exists — register remove it from them first; forget takes records in no binder or whose file is gone",
				refLabel(rec), bs)
		case f.kind == bmBroken:
			return fmt.Sprintf("%s is in binder(s) %s and a file still carries its id — register repair finds it, register remove takes it out of a binder",
				refLabel(rec), bs)
		default:
			return fmt.Sprintf("%s is in binder(s) %s and whether its file is gone cannot be told: %s",
				refLabel(rec), bs, f.hint)
		}
	}
	return ""
}

// goneMemberNote names the binders a record leaves with forget, or "".
func goneMemberNote(rec map[string]any) string {
	if bs := nonEmptyBinders(rec); len(bs) > 0 {
		return " (file gone, was in " + strings.Join(bs, ", ") + ")"
	}
	return ""
}
