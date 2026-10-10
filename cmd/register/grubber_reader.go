package main

// grubber_reader — the Markdown layer is read through grubber, never parsed here.
//
// register alone is the index: identity, membership, bookmarks. Everything above
// that — captions, places, ordering keys, the album a file belongs to — lives in
// the Markdown layer, and that layer belongs to grubber. So `register album`
// asks grubber instead of parsing notes itself, which also means an inherited
// frontmatter field arrives without anyone having to look it up.

import (
	"github.com/rhsev/fileregister/internal/index"

	"bytes"
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
// how a member learns which album it is in: this reading inherits everything.
// -b keeps out notes without blocks, whose frontmatter grubber would otherwise
// return as a record of its own; --no-fill keeps each record to what its block
// and note say, where grubber would pad it with every key any block has, as
// null.
func grubberRecordsFor(notesDir, binder string) (map[string]map[string]any, error) {
	bin, err := grubberBin()
	if err != nil {
		return nil, err
	}
	records, err := grubberExtract(bin, notesDir, "-b", "--no-fill", "--extensions=.md", "-f", "binder="+binder)
	if err != nil {
		return nil, fmt.Errorf("grubber failed for binder %q: %w", binder, err)
	}
	byID := map[string]map[string]any{}
	for _, r := range records {
		if !index.SameBinder(index.AsString(r["binder"]), binder) {
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

// grubberExtract runs `grubber extract --no-config <args>` and decodes the
// record list. --no-config: the call means what its arguments say, whatever
// the user's grubber config and GRUBBER_* variables set for their own queries.
// A default filter there would otherwise hide blocks from rename, forget and
// cleanup, which decide by grubber's answer what to change.
// Numbers are kept as their digits (UseNumber): an id or sort key written
// unquoted in a block is a YAML integer, and decoded as float64 it came out of
// AsString as "2.70450536e+08", matching no record.
func grubberExtract(bin string, args ...string) ([]map[string]any, error) {
	out, err := exec.Command(bin, append([]string{"extract", "--no-config"}, args...)...).Output()
	if err != nil {
		return nil, fmt.Errorf("%w%s", err, grubberStderr(err))
	}
	var records []map[string]any
	if len(bytes.TrimSpace(out)) == 0 {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.UseNumber()
	if jerr := dec.Decode(&records); jerr != nil {
		return nil, fmt.Errorf("grubber returned something that is not a record list: %w", jerr)
	}
	return records, nil
}

// grubberNoteBlocks returns the blocks of one note, in the order they stand
// in the document, each as the block says it: nothing inherited from the
// frontmatter, where a sort: would otherwise place every member alike. A note
// that does not exist has no blocks, and grubber is not asked.
func grubberNoteBlocks(path string) ([]map[string]any, error) {
	if !index.FileExists(path) {
		return nil, nil
	}
	bin, err := grubberBin()
	if err != nil {
		return nil, err
	}
	records, err := grubberExtract(bin, path, "-b", "--no-fill", "--inherit=")
	if err != nil {
		return nil, fmt.Errorf("grubber failed for %s: %w", filepath.Base(path), err)
	}
	return records, nil
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
