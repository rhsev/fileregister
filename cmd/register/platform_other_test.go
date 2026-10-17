//go:build !darwin

package main

import (
	"runtime"
	"testing"
)

// fakeMounts hands the volume functions a mount table and a login name the
// test controls, as volume_guard_test.go does with volumesDir on macOS: the
// real table holds whatever the machine running the test has mounted.
func fakeMounts(t *testing.T, mounts []string, login string) {
	t.Helper()
	oldMounts, oldUser, oldRoots := mountPoints, currentUser, detachableRoots
	mountPoints = func() []string { return mounts }
	currentUser = func() string { return login }
	detachableRoots = []string{"/media", "/run/media", "/mnt"}
	t.Cleanup(func() { mountPoints, currentUser, detachableRoots = oldMounts, oldUser, oldRoots })
}

var testMounts = []string{"/", "/proc", "/run", "/home", "/media/ralf/Backup", "/mnt/data", "/run/media/ralf/USB STICK"}

func TestVolumeOfTakesTheLongestMount(t *testing.T) {
	fakeMounts(t, testMounts, "ralf")
	cases := map[string]string{
		"/home/ralf/a.pdf":            "/home",
		"/etc/hosts":                  "/",
		"/media/ralf/Backup/x/y.pdf":  "/media/ralf/Backup",
		"/media/ralf/Backup":          "/media/ralf/Backup",
		"/media/ralf/BackupOld/x.pdf": "/", // a name that only starts like a mount
		"/run/media/ralf/USB STICK/a": "/run/media/ralf/USB STICK",
		"/run/user/1000/x":            "/run",
	}
	for p, want := range cases {
		if got := volumeOf(p); got != want {
			t.Errorf("volumeOf(%q) = %q, want %q", p, got, want)
		}
	}
}

// The guard decides whether "dead" may be said at all. A file on a disk that
// is not mounted must read as unreachable, never as gone.
func TestVolumeGuardOnLinux(t *testing.T) {
	fakeMounts(t, testMounts, "ralf")
	type want struct {
		name    string
		mounted bool
	}
	cases := map[string]want{
		"":                            {"", true},
		"/home/ralf/a.pdf":            {"", true}, // not a detachable volume
		"/media/ralf/Backup/x.pdf":    {"/media/ralf/Backup", true},
		"/media/ralf/Away/x.pdf":      {"/media/ralf/Away", false}, // udisks layout, not mounted
		"/media/Away/x.pdf":           {"/media/Away", false},      // older layout
		"/mnt/data/x.pdf":             {"/mnt/data", true},
		"/mnt/gone/x/y.pdf":           {"/mnt/gone", false},
		"/run/media/ralf/USB STICK/a": {"/run/media/ralf/USB STICK", true},
		"/run/media/ralf/Other/a":     {"/run/media/ralf/Other", false},
		"/media/":                     {"", true},
		"/media":                      {"", true},
		"/mediafiles/x.pdf":           {"", true}, // only starts like a root
	}
	for p, w := range cases {
		if got := volumeName(p); got != w.name {
			t.Errorf("volumeName(%q) = %q, want %q", p, got, w.name)
		}
		if got := volumeMounted(p); got != w.mounted {
			t.Errorf("volumeMounted(%q) = %v, want %v", p, got, w.mounted)
		}
		wantUnmounted := ""
		if !w.mounted {
			wantUnmounted = w.name
		}
		if got := unmountedVolume(p); got != wantUnmounted {
			t.Errorf("unmountedVolume(%q) = %q, want %q", p, got, wantUnmounted)
		}
	}
}

func TestTrashOnLinux(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "/home/ralf")
	cases := map[string]string{
		"/home/ralf/.local/share/Trash/files/a.pdf":  "in the Trash",
		"/media/ralf/Backup/.Trash-1000/files/a.pdf": "in the Trash",
		"/media/ralf/Backup/.Trash/1000/files/a.pdf": "in the Trash",
		"/home/ralf/notes/.Trash-notes.md":           "",
		"/home/ralf/doc.pdf":                         "",
		"/home/ralf/.local/share/Trashy/a.pdf":       "",
	}
	for p, want := range cases {
		if got := trashOrBackup(p); got != want {
			t.Errorf("trashOrBackup(%q) = %q, want %q", p, got, want)
		}
	}
	t.Setenv("XDG_DATA_HOME", "/data/xdg")
	if got := trashOrBackup("/data/xdg/Trash/files/a.pdf"); got != "in the Trash" {
		t.Errorf("XDG_DATA_HOME Trash: %q", got)
	}
}

func TestUnescapeMount(t *testing.T) {
	cases := map[string]string{
		`/run/media/ralf/USB\040STICK`: "/run/media/ralf/USB STICK",
		`/mnt/a\134b`:                  `/mnt/a\b`,
		`/mnt/tab\011x`:                "/mnt/tab\tx",
		"/plain":                       "/plain",
		`/short\04`:                    `/short\04`,
	}
	for in, want := range cases {
		if got := unescapeMount(in); got != want {
			t.Errorf("unescapeMount(%q) = %q, want %q", in, got, want)
		}
	}
}

// The real mount table, read once: whatever else is mounted, / is.
func TestMountPointsReadsTheRealTable(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("no /proc/self/mountinfo off Linux")
	}
	for _, m := range mountPoints() {
		if m == "/" {
			return
		}
	}
	t.Error("/ is not in /proc/self/mountinfo as read")
}

func TestLinuxNamesNoMacLayer(t *testing.T) {
	if len(tarCreateArgs) != 0 {
		t.Errorf("tarCreateArgs = %q, want none (GNU tar exits 64 on --no-mac-metadata)", tarCreateArgs)
	}
	if layerName("tags") != "tags attribute" || layerName("itemprojects") != "groups attribute" {
		t.Errorf("layerName: %q, %q", layerName("tags"), layerName("itemprojects"))
	}
	if spotlightAttrs("/x", albumMdlsKeys) != "" {
		t.Error("spotlightAttrs says something without Spotlight")
	}
}
