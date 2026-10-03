package index

import (
	"strings"
	"testing"
)

func TestBinderNameProblem(t *testing.T) {
	cases := map[string]string{
		"Steuer 2024":                  "",
		"2024:berlin":                  "",
		"Übersicht":                    "",
		"★":                            "is the managed marker ★",
		"★ Favoriten":                  "",
		strings.Repeat("b", 255):       "",
		"Müller, Hans":                 "contains a comma",
		"a\nb":                         "contains a control character",
		" Steuer":                      "has leading or trailing whitespace",
		"Steuer ":                      "has leading or trailing whitespace",
		strings.Repeat("b", 256):       "is longer than 255 bytes",
		strings.Repeat("ü", 127) + "x": "",
		strings.Repeat("ü", 128):       "is longer than 255 bytes",
	}
	for name, want := range cases {
		if got := BinderNameProblem(name); got != want {
			t.Errorf("BinderNameProblem(%.20q) = %q, want %q", name, got, want)
		}
	}
}
