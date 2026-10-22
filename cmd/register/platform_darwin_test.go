package main

import "os/exec"

// The test helpers platform_other_test.go defines for Linux, as macOS does
// it: cp keeps extended attributes, and the xattr tool edits them.

// copyKeepingAttrs copies a file with its extended attributes, the way
// Finder's Duplicate and cp do on macOS.
func copyKeepingAttrs(src, dst string) *exec.Cmd { return exec.Command("cp", src, dst) }

// stripIDAttr removes the id the engine keeps on a file.
func stripIDAttr(path string) ([]byte, error) {
	return exec.Command("xattr", "-d", "com.apple.metadata:kMDItemInformation", path).CombinedOutput()
}

// clearAttrs removes every extended attribute from a file.
func clearAttrs(path string) error { return exec.Command("xattr", "-c", path).Run() }
