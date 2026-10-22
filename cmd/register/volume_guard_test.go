//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The guard decides whether "dead" may be said at all, so both branches are
// pinned. Against a directory the test controls, not the real /Volumes: the
// first version of this test asserted that /Volumes/lightning was mounted,
// which held on the author's Mac and failed on the first CI runner to see it.
func TestVolumeMountedGuard(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "mounted"), 0755); err != nil {
		t.Fatal(err)
	}
	old := volumesDir
	volumesDir = root
	defer func() { volumesDir = old }()

	cases := map[string]bool{
		"":                                 true, // no path is not a volume question
		"/Users/x/foo.pdf":                 true, // boot volume, always reachable
		"/private/tmp/foo.pdf":             true,
		root + "/mounted/users/x/foo.pdf":  true,  // the volume is there
		root + "/mounted":                  true,  // the volume root itself
		root + "/detached/users/x/foo.pdf": false, // it is not
		root + "/":                         true,  // nothing named
	}
	for path, want := range cases {
		if got := volumeMounted(path); got != want {
			t.Errorf("volumeMounted(%q) = %v, want %v", path, got, want)
		}
	}
}

// volumeName is what both guards share, so its edges are pinned once.
func TestVolumeNameEdges(t *testing.T) {
	old := volumesDir
	volumesDir = "/Volumes"
	defer func() { volumesDir = old }()

	cases := map[string]string{
		"":                          "",
		"/Users/x":                  "",
		"/Volumes":                  "", // no trailing slash: not under the dir
		"/Volumes/":                 "",
		"/Volumes/disk":             "disk",
		"/Volumes/disk/":            "disk",
		"/Volumes/disk/a/b.pdf":     "disk",
		"/Volumes/with space/a.pdf": "with space",
	}
	for path, want := range cases {
		if got := volumeName(path); got != want {
			t.Errorf("volumeName(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestUnmountedVolume(t *testing.T) {
	cases := map[string]string{
		"/Volumes/no-such-volume-4711/a/b.pdf": "no-such-volume-4711",
		"/Users/someone/doc.pdf":               "",
		"/Volumes/":                            "",
		"":                                     "",
	}
	for p, want := range cases {
		if got := unmountedVolume(p); got != want {
			t.Errorf("unmountedVolume(%q) = %q, want %q", p, got, want)
		}
	}
	if volumeOf("/Volumes/lightning/x") != "/Volumes/lightning" || volumeOf("/Users/x") != "/" {
		t.Error("volumeOf")
	}
}

// Time Machine copies keep a file's name and id, so a hit there is a backup,
// never the file that went missing.
func TestTimeMachineCopyIsABackup(t *testing.T) {
	for _, p := range []string{
		"/Volumes/Backup/Backups.backupdb/Mac/2026/Users/me/rechnung.pdf",
		"/Volumes/.timemachine/abc/2026/Data/Users/me/rechnung.pdf",
	} {
		if trashOrBackup(p) != "in a Time Machine backup" {
			t.Errorf("%s is not seen as a backup", p)
		}
	}
}

// A record whose file sits on a volume that is not mounted is not judged:
// audit names it apart and does not send it to repair, nor exit 1 for it.
func TestAuditLeavesAnAbsentVolumeUnjudged(t *testing.T) {
	anchor := engineBin(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	old := volumesDir
	volumesDir = root
	defer func() { volumesDir = old }()
	vol := filepath.Join(root, "Platte")
	if err := os.MkdirAll(vol, 0755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(vol, "x.txt")
	writeFile(t, f, "x")
	notes := seedNotes(t)
	env := addEnv(t, notes, anchor)
	if _, e, code := runGoAdd(t, env, f, "--binder", "Akte"); code != 0 {
		t.Fatalf("add: %d %s", code, e)
	}
	os.RemoveAll(vol) // the volume goes away

	out, errOut, code := runGoAudit(t, env)
	if !strings.Contains(out, "ON AN ABSENT VOLUME (1)") || strings.Contains(out, "BROKEN BOOKMARK") || code != 0 {
		t.Errorf("exit %d, want 0 with the file on an absent volume:\n%s%s", code, out, errOut)
	}
}
