package main

import (
	"os"
	"path/filepath"
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
