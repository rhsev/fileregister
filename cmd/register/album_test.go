package main

import (
	"github.com/rhsev/fileregister/v2/internal/index"

	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func runGoAlbum(t *testing.T, env []string, args ...string) (string, string, int) {
	t.Helper()
	restore := setEnv(t, env)
	defer restore()
	index.ResetEngine()

	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	code := cmdAlbum(args)

	index.ResetEngine()
	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr

	var ob, eb bytes.Buffer
	io.Copy(&ob, outR)
	io.Copy(&eb, errR)
	return ob.String(), eb.String(), code
}

func seedAlbumFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	col := filepath.Join(dir, "collections")
	if err := os.MkdirAll(col, 0755); err != nil {
		t.Fatal(err)
	}
	// Two URL records and a file ref whose bookmark does not exist: no Spotlight
	// involved, so the lines are the same on every machine.
	writeFile(t, filepath.Join(col, "inbox.jsonl"),
		`{"type":"ref","id":"1","binder":["Alb"],"url":"x-devonthink-item://A","filename":"one.pdf","kind":"pdf","aka":["eins"]}`+"\n"+
			`{"type":"ref","id":"2","binder":["Alb"],"url":"https://example.com/x?a=1&b=2","filename":"two & three.md","kind":"web"}`+"\n"+
			`{"type":"ref","id":"3","binder":["Alb"],"filename":"gone.jpg","kind":"image"}`+"\n")
	writeFile(t, filepath.Join(col, "binder_Alb.md"),
		"---\nalbum: Alben-Test\n---\n\n"+
			"### two\n```yaml\ntype: ref\nid: '2'\nbinder: Alb\ntitle: Erstes\ncomment: Ein <Test>\nmy_own: 7\nsort: \"1\"\n```\n\n"+
			"### one\n```yaml\ntype: ref\nid: '1'\nbinder: Alb\n```\n")
	return dir
}

func albumEnv(t *testing.T, notes, anchor string) []string {
	t.Helper()
	env := filterEnv(os.Environ(), "GRUBBER_SET")
	for _, k := range []string{"GRUBBER_NOTES", "HOME", "FILEANCHOR", "REGISTER_BINDER", "GRUBBER_BIN"} {
		env = filterEnv(env, k)
	}
	return append(env, "GRUBBER_NOTES="+notes, "HOME="+t.TempDir(), "FILEANCHOR="+anchor,
		"GRUBBER_BIN="+grubberTestBin(t))
}

func TestAlbum(t *testing.T) {
	anchor := engineBin(t)
	n := seedAlbumFixture(t)

	so, se, code := runGoAlbum(t, albumEnv(t, n, anchor), "Alb")
	assertGolden(t, "album_lines", cliResult(so, se, code))

	// Error: unknown binder.
	go2, ge2, gc2 := runGoAlbum(t, albumEnv(t, n, anchor), "Ghost")
	assertGolden(t, "album_ghost", cliResult(go2, ge2, gc2))
}

// The point of the format: an album line is the member's `list --json` line
// plus position, fields and file — the same keys with the same values, so a
// reader of one reads the other.
func TestAlbumLineIsListLinePlus(t *testing.T) {
	anchor := engineBin(t)
	n := seedAlbumFixture(t)
	env := albumEnv(t, n, anchor)

	listOut, _, code := runGoList(t, env, "Alb", "--json")
	if code != 0 {
		t.Fatalf("list: exit %d", code)
	}
	listByID := map[string]map[string]any{}
	for _, l := range strings.Split(strings.TrimSpace(listOut), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("list line %q: %v", l, err)
		}
		listByID[m["id"].(string)] = m
	}

	albumOut, _, code := runGoAlbum(t, env, "Alb")
	if code != 0 {
		t.Fatalf("album: exit %d", code)
	}
	lines := strings.Split(strings.TrimSpace(albumOut), "\n")
	if len(lines) != len(listByID) {
		t.Fatalf("album has %d lines, list %d", len(lines), len(listByID))
	}
	for i, l := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("album line %q: %v", l, err)
		}
		want := listByID[m["id"].(string)]
		for k, v := range want {
			if !reflect.DeepEqual(m[k], v) {
				t.Errorf("line %d, %s: album %v, list %v", i+1, k, m[k], v)
			}
		}
		for k := range m {
			if _, inList := want[k]; !inList && k != "position" && k != "fields" && k != "file" {
				t.Errorf("line %d: album adds %q beyond position/fields/file", i+1, k)
			}
		}
		if m["position"] != float64(i+1) {
			t.Errorf("line %d: position %v", i+1, m["position"])
		}
	}
}

// The renderer's flags are gone; saying so beats "unknown option".
func TestAlbumRemovedFlags(t *testing.T) {
	anchor := engineBin(t)
	n := seedAlbumFixture(t)
	for _, f := range []string{"--out", "--css", "--milan", "--open"} {
		so, se, code := runGoAlbum(t, albumEnv(t, n, anchor), "Alb", f, "x")
		if code != 1 || so != "" || !strings.Contains(se, f+" is gone") {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", f, code, so, se)
		}
	}
}

func TestAlbumSpotlightFields(t *testing.T) {
	raw := `kMDItemAcquisitionMake          = "Canon"
kMDItemAcquisitionModel         = "Canon"
kMDItemCity                     = "Okaukuejo"
kMDItemContentCreationDate      = 2026-10-03 07:12:00 +0000
kMDItemContentType              = "public.png"
kMDItemCountry                  = "Namibia"
kMDItemDescription              = (null)
kMDItemDurationSeconds          = (null)
kMDItemHeadline                 = (null)
kMDItemLatitude                 = -18.85
kMDItemLongitude                = 16.32
kMDItemNumberOfPages            = (null)
kMDItemPixelHeight              = 600
kMDItemPixelWidth               = 800
kMDItemTitle                    = "Sunset"
`
	got := albumSpotlightFields(raw)
	want := map[string]any{
		"title": "Sunset", "place": "Okaukuejo, Namibia", "camera": "Canon",
		"type": "public.png", "date": "2026-10-03T07:12:00Z",
		"lat": -18.85, "lon": 16.32, "width": int64(800), "height": int64(600),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("\n got %v\nwant %v", got, want)
	}
}

// size and modified come from the file system, so they are there even where
// Spotlight has nothing.
func TestAlbumFileMetaWithoutSpotlight(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.bin")
	writeFile(t, p, "12345")
	m := albumFileMeta(p)
	if m["size"] != int64(5) {
		t.Errorf("size = %v", m["size"])
	}
	if _, err := time.Parse(time.RFC3339, fmt.Sprint(m["modified"])); err != nil {
		t.Errorf("modified = %v", m["modified"])
	}
	for k, v := range m {
		if v == "" || v == nil {
			t.Errorf("%s present but empty", k)
		}
	}
	if _, has := m["title"]; has {
		t.Errorf("Spotlight's file-name title came through: %v", m["title"])
	}
}
