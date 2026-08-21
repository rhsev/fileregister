//go:build darwin

package index

import "strings"

// foldCase lower-cases for path comparison: macOS default filesystems
// (APFS, HFS+) are case-insensitive. Only consulted for nonexistent paths —
// existing files compare by inode, which is exact even on case-sensitive APFS.
func foldCase(s string) string {
	return strings.ToLower(s)
}
