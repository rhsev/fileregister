//go:build !darwin

package main

// platform_other — the names platform_darwin.go defines, for Linux. Same
// questions, other answers: removable media mount under /media, /run/media or
// /mnt instead of /Volumes; whether a search covers a place is the engine's
// answer (searchable), as on macOS, and on Linux it covers the engine's roots;
// the Trash follows the freedesktop spec; GNU tar stores no extended
// attributes unless asked; and what the engine calls its attributes is the
// engine's business (fileanchor PLATFORMS.md), so messages name the logical
// layer, not an xattr key.

import (
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rhsev/fileregister/v2/internal/index"
	unorm "golang.org/x/text/unicode/norm"
)

// detachableRoots are where Linux desktops mount removable media (udisks:
// /media/<user>/<label>, /run/media/<user>/<label>) and where people mount by
// hand (/mnt/<name>). A variable so the tests can point it at their own tree.
var detachableRoots = []string{"/media", "/run/media", "/mnt"}

// mountPoints lists where file systems are mounted now, from
// /proc/self/mountinfo. A variable so the tests can hand in a mount table.
var mountPoints = func() []string {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		if f := strings.Fields(line); len(f) > 4 {
			out = append(out, unescapeMount(f[4]))
		}
	}
	return out
}

// unescapeMount undoes the octal escapes the kernel writes into a mount point
// for space, tab, newline and backslash (\040, \011, \012, \134).
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) && isOctal(s[i+1]) && isOctal(s[i+2]) && isOctal(s[i+3]) {
			b.WriteByte((s[i+1]-'0')<<6 | (s[i+2]-'0')<<3 | (s[i+3] - '0'))
			i += 3
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isOctal(c byte) bool { return c >= '0' && c <= '7' }

// currentUser is the login name udisks puts between the root and the label.
// A variable so the tests do not depend on who runs them.
var currentUser = func() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return ""
}

// under reports whether p is m itself or lies below it.
func under(p, m string) bool {
	return p == m || m == "/" || strings.HasPrefix(p, m+"/")
}

// volumeOf is the mount point p lies on: the longest one in the mount table
// that p is under, or "/". repair binds a file found by id without asking only
// on the volume the file was on, so a copy on a backup disk that kept the id
// (rsync -X) is offered, not taken.
func volumeOf(p string) string {
	best := "/"
	for _, m := range mountPoints() {
		if m != "/" && under(p, m) && len(m) > len(best) {
			best = m
		}
	}
	return best
}

// volumeName returns the mount point of the detachable volume path lies on,
// or "" when path is not under one of detachableRoots. While the volume is
// mounted the mount table names it exactly. While it is not, the path shows
// where it was: udisks mounts at <root>/<user>/<label>, /mnt and older setups
// at <root>/<label>.
func volumeName(path string) string {
	for _, root := range detachableRoots {
		rest, ok := strings.CutPrefix(path, root+"/")
		if !ok || strings.Trim(rest, "/") == "" {
			continue
		}
		if m := volumeOf(path); m != "/" && strings.HasPrefix(m, root+"/") {
			return m
		}
		parts := strings.Split(strings.Trim(rest, "/"), "/")
		if root != "/mnt" && len(parts) >= 2 && parts[0] == currentUser() {
			return root + "/" + parts[0] + "/" + parts[1]
		}
		return root + "/" + parts[0]
	}
	return ""
}

// volumeMounted reports whether the volume a path lived on is present. An
// absent volume makes every bookmark on it look dead, so the unjudgeable case
// gets its own name (see the darwin side). A path that is not on a detachable
// volume is not a volume question.
func volumeMounted(lastPath string) bool {
	name := volumeName(lastPath)
	return name == "" || volumeOf(name) == name
}

// unmountedVolume returns the volume p lies on when that volume is not
// mounted, or "". A file there is not gone, just not reachable now.
func unmountedVolume(p string) string {
	if name := volumeName(p); name != "" && !volumeMounted(p) {
		return name
	}
	return ""
}

// trashDirRe matches a per-volume freedesktop Trash, <top>/.Trash-<uid>/.
var trashDirRe = regexp.MustCompile(`/\.Trash-\d+/`)

// trashOrBackup says where path lies when that is a Trash, or "". The
// freedesktop Trash is $XDG_DATA_HOME/Trash (~/.local/share/Trash by default)
// and, on other volumes, <top>/.Trash/<uid> or <top>/.Trash-<uid>. Linux has
// no one backup system whose copies keep a file's id, so none is recognized.
func trashOrBackup(path string) string {
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		if home, err := os.UserHomeDir(); err == nil {
			data = filepath.Join(home, ".local", "share")
		}
	}
	if data != "" && strings.HasPrefix(path, filepath.Join(data, "Trash")+"/") {
		return "in the Trash"
	}
	if strings.Contains(path, "/.Trash/") || trashDirRe.MatchString(path) {
		return "in the Trash"
	}
	return ""
}

// tarCreateArgs is empty: GNU tar stores no extended attributes unless told
// to, and does not know bsdtar's --no-mac-metadata (it exits 64 on it).
var tarCreateArgs []string

// layerName is what a backend's binder layer is called in messages. The
// engine decides which attribute holds it, so the logical name is used.
func layerName(backend string) string {
	if backend == "tags" {
		return "tags attribute"
	}
	return "groups attribute"
}

// The two places the id lives on a file, as messages name them.
const (
	idLayerName   = "id attribute"
	syncLayerName = "sync attribute"
)

// spotlightAttrs says nothing: there is no Spotlight. album's lines carry size
// and modification time alone.
func spotlightAttrs(path string, keys []string) string { return "" }

// Wording that names the search index or tells how to get a verdict.
const (
	noIndexHint      = "that place is outside fileanchor's roots — \"no file carries this id\" is unknowable there"
	unjudgedAdvice   = "Mount the volume and run again. Outside fileanchor's roots a bookmark whose file is gone stays unjudged: register repair --interactive re-binds a moved file, register forget <id> drops a record that is gone."
	auditUnjudged    = "Unreachable ones were not judged — mount the volume and run again; outside fileanchor's roots, register repair --interactive or register forget <id> settles the rest."
	direction2Source = "the engine's search"
	idRestoredNote   = "(id attribute, so a search by id finds them again)"
)

// addRefusal says why add must not take a file on Linux, or "". The engine
// finds a moved file only under its roots and keeps metadata only in
// extended attributes, and ext4 tells a precomposed name from a decomposed
// one, so a file outside those requirements (fileanchor PLATFORMS.md) would
// be registered without a way to find it again.
func addRefusal(path string) string {
	if unorm.NFC.String(path) != path {
		return "its path is decomposed (NFD); on Linux a registered file needs a precomposed (NFC) name — rename it first"
	}
	if s, _ := index.Searchable(path); !s {
		return "it lies outside fileanchor's roots, where a moved file could not be found again — add its folder to ~/.config/fileanchor/roots"
	}
	if err := index.MetaReadable(path); err != nil {
		return err.Error()
	}
	return ""
}
