//go:build !darwin && !linux

package index

func copyXattrs(src, dst string) {}
