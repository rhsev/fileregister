package main

import "testing"

func TestAlbumSafeHref(t *testing.T) {
	for u, want := range map[string]string{
		"https://example.com/a":    "https://example.com/a",
		"x-devonthink-item://ABC":  "x-devonthink-item://ABC",
		"javascript:alert(1)":      "",
		"JavaScript://%0aalert(1)": "",
		" java\tscript:alert(1)":   "",
		"data:text/html,<script>":  "",
		"vbscript:msgbox":          "",
		"relative/page.html":       "relative/page.html",
	} {
		if got := albumSafeHref(u); got != want {
			t.Errorf("albumSafeHref(%q) = %q, want %q", u, got, want)
		}
	}
}

func TestAlbumAssetNames(t *testing.T) {
	// milan: album x with y-z.jpg and album x-y with z.jpg must not share a file.
	a := albumAssetSafe("x" + "-" + albumTag("x") + "-" + "y-z.jpg")
	b := albumAssetSafe("x-y" + "-" + albumTag("x-y") + "-" + "z.jpg")
	if a == b {
		t.Errorf("two albums share %s", a)
	}
	// standalone: a # or % in a name must not break the link.
	if got := albumURLName("a#1 50%.jpg", false); got != "a%231%2050%25.jpg" {
		t.Errorf("albumURLName = %q", got)
	}
	if got := albumURLName("x-y.jpg", true); got != "x-y.jpg" {
		t.Errorf("milan names are used as written: %q", got)
	}
}

func TestMarshalDirNameStaysInside(t *testing.T) {
	for _, b := range []string{"..", ".", ""} {
		if got := marshalDirName(b); got != "binder" {
			t.Errorf("marshalDirName(%q) = %q", b, got)
		}
	}
	if got := marshalDirName("Reise 2024"); got != "Reise 2024" {
		t.Errorf("an ordinary name changed: %q", got)
	}
}
