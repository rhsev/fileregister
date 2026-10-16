package index

// normalize — the one Unicode form the index is written in, and the two
// comparisons that go with it.
//
// macOS hands the same text over in two byte forms. A name typed on the
// keyboard or pasted into a dialog arrives precomposed (NFC); a path that comes
// from the Finder, from Apple Mail, from DEVONthink or from shell completion
// arrives decomposed (NFD), because those writers create their files that way.
// APFS stores whichever form it is given and resolves either on lookup, so the
// difference stays invisible until something compares the bytes.
//
// fileanchor already settled this for the metadata layer: every value it
// writes is stored NFC, on every OS (fileanchor Values.swift, PLATFORMS.md —
// "neither APFS xattrs nor Linux filesystems normalize"). The index follows the
// engine rather than inventing a second rule, so the two agree on what the same
// binder is.
//
// The rule has one boundary worth keeping in mind, and it is the one that bites
// on Linux. Normalizing a LABEL is always safe: a binder name, an aka handle,
// an album title and the `filename` field are text the index compares and
// prints, never a path it opens. Normalizing a PATH is not: on APFS
// open("Pässe.md" NFC) reaches a file stored NFD, while on ext4 the two are
// different files and the NFC form is a miss. So paths are passed through as
// given and compared through PathKey (device:inode), never normalized into
// existence.

import "golang.org/x/text/unicode/norm"

// NFC is the canonical form every label the index writes is stored in.
func NFC(s string) string { return norm.NFC.String(s) }

// SameName reports whether two file names name the same file: equal once both
// are NFC, and case-folded where the platform's default filesystem folds case
// (foldCase: darwin yes, elsewhere identity). For the `filename` field and for
// basenames read back off the disk, which differ in form whenever the file was
// created by a writer that decomposes.
func SameName(a, b string) bool {
	return a == b || foldCase(NFC(a)) == foldCase(NFC(b))
}

// SameLabel reports whether two labels are the same label: equal once both are
// NFC, case kept. Binder names and aka handles are case-sensitive.
func SameLabel(a, b string) bool {
	return a == b || NFC(a) == NFC(b)
}

// CanonLabel returns the form a label is stored in. Leading and trailing
// whitespace is not trimmed here: BinderNameProblem rejects it instead, so a
// name is never silently changed into a different one.
func CanonLabel(s string) string { return NFC(s) }
