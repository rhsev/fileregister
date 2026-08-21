//go:build !darwin

package index

// foldCase is the identity off macOS: Linux filesystems are case-sensitive,
// so /path/File and /path/file are different files.
func foldCase(s string) string {
	return s
}
