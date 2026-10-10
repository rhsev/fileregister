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

// SameBinder reports whether two binder names name the same binder: equal
// once both are NFC, case kept. A name typed by hand or pasted from the Finder
// can stand in a note decomposed (NFD); it looks the same and is the same
// binder, and grubber's filters already find it so. Every comparison of a
// block's binder with a binder name goes through here, the readers' and the
// writers' alike, or a reader would find a block the writer then misses.
func SameBinder(a, b string) bool {
	return a == b || norm.NFC.String(a) == norm.NFC.String(b)
}

// BinderNameProblem says why name cannot be a new binder, or "" if it can.
// Only names being introduced are checked (add, rename's new name, write,
// unmarshal); existing binders stay usable, so one with a comma can still be
// renamed away.
func BinderNameProblem(name string) string {
	// The tags backend writes a binder as a Finder tag, and ★ is the tag
	// fileregister owns as its managed marker: removing such a binder would
	// strip the marker from files still in other binders.
	if norm.NFC.String(name) == managedMarker {
		return "is the managed marker ★"
	}
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
