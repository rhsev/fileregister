package index

import (
	"runtime"
	"testing"

	"golang.org/x/text/unicode/norm"
)

func TestSameLabelAcrossForms(t *testing.T) {
	nfc := "Übung"
	nfd := norm.NFD.String(nfc)
	if nfc == nfd {
		t.Fatal("the fixture has no decomposable character, so this test cannot fire")
	}
	if !SameLabel(nfc, nfd) {
		t.Error("the two forms of one name are not the same label")
	}
	if !SameBinder(nfc, nfd) {
		t.Error("the two forms of one name are not the same binder")
	}
	// Case is kept: a binder is case-sensitive.
	if SameLabel("Übung", "übung") {
		t.Error("labels must not fold case")
	}
	// And different text stays different.
	if SameLabel(nfc, "Ubung") {
		t.Error("a name without the diacritic is a different label")
	}
}

func TestSameNameAcrossForms(t *testing.T) {
	nfc := "Pässe.md"
	nfd := norm.NFD.String(nfc)
	if !SameName(nfc, nfd) {
		t.Error("the two forms of one file name are not the same name")
	}
	if SameName(nfc, "Passe.md") {
		t.Error("a name without the diacritic is a different file name")
	}
	// Case folds only where the default filesystem folds it.
	got := SameName("Pässe.md", "pässe.md")
	if runtime.GOOS == "darwin" && !got {
		t.Error("on macOS file names compare case-insensitively")
	}
	if runtime.GOOS != "darwin" && got {
		t.Error("off macOS file names are case-sensitive")
	}
}

// A binder set comes out precomposed and holds one entry per binder, so a name
// that arrived decomposed is not kept beside its own precomposed spelling.
func TestNormalizeBindersCollapsesTheForms(t *testing.T) {
	nfc := "Übung"
	nfd := norm.NFD.String(nfc)
	got := NormalizeBinders([]any{nfd, nfc, "Akte", ""})
	if len(got) != 2 {
		t.Fatalf("got %q, want two entries", got)
	}
	if AsString(got[0]) != nfc {
		t.Errorf("first entry is %q, want the precomposed %q", AsString(got[0]), nfc)
	}
	if AsString(got[1]) != "Akte" {
		t.Errorf("second entry is %q, want \"Akte\"", AsString(got[1]))
	}
}

// An aka handle resolves whichever form the key is typed in.
func TestResolveKeyFindsAkaInEitherForm(t *testing.T) {
	nfc := "grüße-käse"
	recs := []map[string]any{
		{"type": "ref", "id": "1", "aka": []any{nfc}},
	}
	for _, key := range []string{nfc, norm.NFD.String(nfc)} {
		if got := ResolveKey(recs, key); got == nil {
			t.Errorf("key %q resolved to nothing", key)
		}
	}
	if got := ResolveKey(recs, "grusse-kase"); got != nil {
		t.Error("a key without the diacritics must not resolve")
	}
}
