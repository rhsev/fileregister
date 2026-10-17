//go:build darwin

package main

// platform_darwin — what register knows about macOS around the engine: where
// detachable volumes appear, whether Spotlight can answer for a volume, where
// the Trash and backups are, how tar leaves Mac metadata out, and what the
// metadata layers are called in messages. The engine does the metadata work
// itself; this file only holds what register decides and says around it.
// platform_other.go is the same set of names for Linux.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

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

// unmountedVolume returns the name of the volume p lies on when that volume
// is not mounted, or "". A file there is not gone, just not reachable now.
func unmountedVolume(p string) string {
	rest, ok := strings.CutPrefix(p, "/Volumes/")
	if !ok {
		return ""
	}
	name, _, _ := strings.Cut(rest, "/")
	if name == "" {
		return ""
	}
	if _, err := os.Stat(filepath.Join("/Volumes", name)); err == nil {
		return ""
	}
	return name
}

// volumeOf is the mount a path lies on as far as its path shows: /Volumes/<name>, or /.
func volumeOf(p string) string {
	if rest, ok := strings.CutPrefix(p, "/Volumes/"); ok {
		name, _, _ := strings.Cut(rest, "/")
		return "/Volumes/" + name
	}
	return "/"
}

// trashOrBackup says where path lies when that is the Trash or a backup, or "".
// A bookmark follows its file into the Trash, and a backup holds copies with
// the same name and id.
func trashOrBackup(path string) string {
	for _, marker := range []string{"/.Trash/", "/.Trashes/"} {
		if strings.Contains(path, marker) {
			return "in the Trash"
		}
	}
	if strings.Contains(path, "/Backups.backupdb/") || strings.Contains(path, "/.MobileBackups/") ||
		strings.HasPrefix(path, "/Volumes/.timemachine/") {
		return "in a Time Machine backup"
	}
	return ""
}

// tarCreateArgs go before the archive options when marshal packs a container.
// By default bsdtar also stores each file's ACLs and extended attributes as a
// copyfile(3) AppleDouble block (tar(1), --mac-metadata), which only macOS can
// use. The attributes still travel as plain pax headers either way; GNU tar
// skips the ones it does not know with a warning and exit 0.
var tarCreateArgs = []string{"--no-mac-metadata"}

// layerName is what a backend's binder layer is called in messages: the
// attribute the engine writes it to (fileanchor PLATFORMS.md).
func layerName(backend string) string {
	if backend == "tags" {
		return "kMDItemUserTags"
	}
	return "kMDItemProjects"
}

// The two places the id lives on a file, as messages name them.
const (
	idLayerName   = "kMDItemInformation"
	syncLayerName = "com.fileregister.id#S"
)

// spotlightAttrs returns what mdls says about the named attributes of path, or
// "" when it says nothing. album parses the text (albumSpotlightFields).
func spotlightAttrs(path string, keys []string) string {
	args := make([]string, 0, len(keys)*2+1)
	for _, k := range keys {
		args = append(args, "-name", k)
	}
	raw, err := exec.Command("mdls", append(args, path)...).Output()
	if err != nil {
		return ""
	}
	return string(raw)
}

// Wording that names the search index or tells how to get a verdict.
const (
	noIndexHint      = "Spotlight does not index that volume — \"no file carries this id\" is unknowable there"
	unjudgedAdvice   = "Mount the volume (or let Spotlight index it) and run again."
	auditUnjudged    = "Unreachable ones were not judged — mount the volume and run again."
	direction2Source = "mdfind"
	idRestoredNote   = "(kMDItemInformation, so Spotlight finds them again)"
)
