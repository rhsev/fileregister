//go:build darwin || linux

package index

import "golang.org/x/sys/unix"

// copyXattrs copies every extended attribute of src onto dst, best effort.
// A rewrite replaces the file, and the user's Finder tags, comment and color
// live in xattrs on it: without the copy, rewriting a note dropped them.
func copyXattrs(src, dst string) {
	size, err := unix.Listxattr(src, nil)
	if err != nil || size <= 0 {
		return
	}
	names := make([]byte, size)
	if size, err = unix.Listxattr(src, names); err != nil {
		return
	}
	start := 0
	for i := 0; i < size; i++ {
		if names[i] != 0 {
			continue
		}
		name := string(names[start:i])
		start = i + 1
		if name == "" {
			continue
		}
		n, err := unix.Getxattr(src, name, nil)
		if err != nil {
			continue
		}
		value := make([]byte, n)
		if n, err = unix.Getxattr(src, name, value); err != nil {
			continue
		}
		unix.Setxattr(dst, name, value[:n], 0)
	}
}
