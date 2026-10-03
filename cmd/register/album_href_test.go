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
