package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSuccTrailingNum(t *testing.T) {
	cases := map[string]string{
		"foo-2":   "foo-3",
		"foo-9":   "foo-10",
		"a-99":    "a-100",
		"x-099":   "x-100", // zero-padding width preserved until carry
		"bar-1":   "bar-2",
		"deep-19": "deep-20",
	}
	for in, want := range cases {
		if got := succTrailingNum(in); got != want {
			t.Errorf("succTrailingNum(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNextFreeID(t *testing.T) {
	// Non-numeric handle grows a -N suffix on each collision.
	db := map[string]string{"foo": "b", "foo-2": "b", "foo-3": "b"}
	if got := NextFreeID(db, "foo"); got != "foo-4" {
		t.Errorf("NextFreeID(foo) = %q, want foo-4", got)
	}
	// A free handle is returned unchanged.
	if got := NextFreeID(db, "bar"); got != "bar" {
		t.Errorf("NextFreeID(bar) = %q, want bar", got)
	}
	// Empty seed generates a fresh 9-digit id absent from db.
	got := NextFreeID(map[string]string{}, "")
	if !allDigitsRe.MatchString(got) || len(got) != 9 {
		t.Errorf("NextFreeID('') = %q, want a free 9-digit id", got)
	}
}

func TestGenerateID(t *testing.T) {
	for i := 0; i < 100; i++ {
		id := GenerateID()
		if len(id) != 9 || !allDigitsRe.MatchString(id) {
			t.Fatalf("GenerateID() = %q, want 9 digits", id)
		}
	}
}

// TestBookmarkAddGetRoundtrip drives the real engine end to end, isolating the
// bookmarks.json store by pointing $HOME at a temp dir.
func TestBookmarkAddGetRoundtrip(t *testing.T) {
	useEngine(t)
	t.Setenv("HOME", t.TempDir())

	f := filepath.Join(t.TempDir(), "doc.pdf")
	if err := os.WriteFile(f, []byte("pdf"), 0644); err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(f)
	if err != nil {
		t.Fatal(err)
	}

	defer fileAnchor().shutdown()

	id, err := BookmarkAdd(real, "")
	if err != nil {
		t.Fatalf("BookmarkAdd: %v", err)
	}
	if len(id) != 9 || !allDigitsRe.MatchString(id) {
		t.Fatalf("BookmarkAdd returned id %q, want 9 digits", id)
	}

	// bookmarks.json was written under the temp HOME.
	if _, err := os.Stat(bookmarkFile()); err != nil {
		t.Fatalf("bookmarks.json not written: %v", err)
	}

	// resolve back to the same path.
	if got := BookmarkGet(id); got != real {
		t.Errorf("BookmarkGet(%s) = %q, want %q", id, got, real)
	}

	// Idempotent: re-adding the same file returns the same id (via its xattr).
	id2, err := BookmarkAdd(real, "")
	if err != nil {
		t.Fatalf("BookmarkAdd #2: %v", err)
	}
	if id2 != id {
		t.Errorf("re-add id = %q, want %q (idempotent)", id2, id)
	}

	// BatchGet: known id resolves, unknown id yields "".
	res, err := BatchGet([]string{id, "000000001"})
	if err != nil {
		t.Fatalf("BatchGet error: %v", err)
	}
	if res[id] != real {
		t.Errorf("BatchGet[%s] = %q, want %q", id, res[id], real)
	}
	if res["000000001"] != "" {
		t.Errorf("BatchGet[unknown] = %q, want empty", res["000000001"])
	}
}
