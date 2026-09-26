package index

// binder — what a binder name may be. A binder ends up on files as a Finder
// tag or a kMDItemProjects entry, so it follows fileanchor's label rule
// (fileanchor PLATFORMS.md), the same on every OS: Linux keeps tags as a
// comma-separated list, and a newline ends a Finder tag name.

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// MaxBinderBytes is fileanchor's label limit, measured NFC as it is stored.
const MaxBinderBytes = 255

// BinderNameProblem says why name cannot be a new binder, or "" if it can.
// Only names being introduced are checked (add, rename's new name, write,
// unmarshal); existing binders stay usable, so one with a comma can still be
// renamed away.
func BinderNameProblem(name string) string {
	if strings.Contains(name, ",") {
		return "contains a comma"
	}
	if strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "contains a control character"
	}
	first, _ := utf8.DecodeRuneInString(name)
	last, _ := utf8.DecodeLastRuneInString(name)
	if unicode.IsSpace(first) || unicode.IsSpace(last) {
		return "has leading or trailing whitespace"
	}
	if len(norm.NFC.String(name)) > MaxBinderBytes {
		return "is longer than " + strconv.Itoa(MaxBinderBytes) + " bytes"
	}
	return ""
}
