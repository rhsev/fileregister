package main

// grubber_reader — the Markdown layer is read through grubber, never parsed here.
//
// register alone is the index: identity, membership, bookmarks. Everything above
// that — captions, places, ordering keys, the album a file belongs to — lives in
// the Markdown layer, and that layer belongs to grubber. So the album renderer
// asks grubber instead of parsing notes itself, which also means an inherited
// frontmatter field arrives without anyone having to look it up.

import (
	"github.com/rhsev/fileregister/internal/index"

	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// grubberBin resolves the query tool: $GRUBBER_BIN > `grubber` on PATH > a copy
// installed alongside the running executable. Same shape as anchorBin.
func grubberBin() (string, error) {
	if env := os.Getenv("GRUBBER_BIN"); env != "" {
		return env, nil
	}
	if p, err := exec.LookPath("grubber"); err == nil {
		return p, nil
	}
	if self, err := os.Executable(); err == nil {
		if resolved, rerr := filepath.EvalSymlinks(self); rerr == nil {
			self = resolved
		}
		cand := filepath.Join(filepath.Dir(self), "libexec", "grubber")
		if st, serr := os.Stat(cand); serr == nil && !st.IsDir() {
			return cand, nil
		}
	}
	return "", fmt.Errorf("'grubber' not found on PATH, none installed alongside the binary, and GRUBBER_BIN is unset; fileregister reads the Markdown layer through grubber (https://github.com/rhsev/grubber)")
}

// grubberRecordsFor returns the Markdown-layer records of one binder, keyed by
// id. grubber's -f is case-insensitive, so the binder is compared again here:
// two binders differing only in case are two binders.
//
// Fields a record carries beyond the index: the curation (title, comment, place,
// lat/lon, map, sort) and whatever the note's frontmatter passes down, which is
// how a member learns which album it is in.
func grubberRecordsFor(notesDir, binder string) (map[string]map[string]any, error) {
	bin, err := grubberBin()
	if err != nil {
		return nil, err
	}
	out, err := exec.Command(bin, "extract", notesDir, "-a", "-f", "binder="+binder).Output()
	if err != nil {
		return nil, fmt.Errorf("grubber failed for binder %q: %w%s", binder, err, grubberStderr(err))
	}
	var records []map[string]any
	if len(out) > 0 {
		if jerr := json.Unmarshal(out, &records); jerr != nil {
			return nil, fmt.Errorf("grubber returned something that is not a record list: %w", jerr)
		}
	}
	byID := map[string]map[string]any{}
	for _, r := range records {
		if index.AsString(r["binder"]) != binder {
			continue
		}
		id := index.AsString(r["id"])
		if id == "" {
			continue
		}
		if _, seen := byID[id]; !seen {
			byID[id] = r
		}
	}
	return byID, nil
}

// grubberStderr returns what grubber said on stderr before failing, as a
// suffix for the error: the exit status alone does not say that a config set
// is missing or a path unreadable. Kept to the first lines, so a panic trace
// does not bury the message.
func grubberStderr(err error) string {
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return ""
	}
	msg := strings.TrimSpace(string(ee.Stderr))
	if msg == "" {
		return ""
	}
	if lines := strings.SplitN(msg, "\n", 4); len(lines) > 3 {
		msg = strings.Join(lines[:3], "\n") + "\n…"
	}
	return ": " + msg
}

// albumFieldOf returns the album a set of members belongs to: the `album` field
// they inherited from their note's frontmatter. Any member answers, because they
// all carry it; the first one with a value wins, so a block that overrides it by
// hand still counts. The second value is the note that member came from, for
// saying where a name comes from when it is not where register writes it.
func albumFieldOf(byID map[string]map[string]any, order []string) (string, string) {
	for _, id := range order {
		if v := index.AsString(byID[id]["album"]); v != "" {
			return v, index.AsString(byID[id]["_note_file"])
		}
	}
	for _, r := range byID {
		if v := index.AsString(r["album"]); v != "" {
			return v, index.AsString(r["_note_file"])
		}
	}
	return "", ""
}
