package main

// container — the snapshot's tar.gz, written and read in-process. Written
// straight from the files (no staging copy), plain entries without extended
// attributes, so GNU tar and bsdtar unpack it alike. Read back with only
// regular files and folders accepted, every name kept below the target.

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// tarWriter writes entries below one top folder.
type tarWriter struct {
	f    *os.File
	gz   *gzip.Writer
	tw   *tar.Writer
	top  string
	dirs map[string]bool
}

func newTarWriter(out, top string) (*tarWriter, error) {
	f, err := os.Create(out)
	if err != nil {
		return nil, err
	}
	gz := gzip.NewWriter(f)
	return &tarWriter{f: f, gz: gz, tw: tar.NewWriter(gz), top: top, dirs: map[string]bool{}}, nil
}

// dir writes the folder entries leading to name, once each.
func (w *tarWriter) dir(name string) error {
	d := path.Dir(name)
	if d == "." || w.dirs[d] {
		return nil
	}
	if err := w.dir(d); err != nil {
		return err
	}
	w.dirs[d] = true
	return w.tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: w.top + "/" + d + "/",
		Mode: 0755, ModTime: time.Now()})
}

// bytes writes data as the file name.
func (w *tarWriter) bytes(name string, data []byte) error {
	if err := w.dir(name); err != nil {
		return err
	}
	if err := w.tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: w.top + "/" + name,
		Mode: 0644, Size: int64(len(data)), ModTime: time.Now()}); err != nil {
		return err
	}
	_, err := w.tw.Write(data)
	return err
}

// file writes the content of src as name and returns its size and sha256. A
// file that changes size while it is read fails the entry, and with it the
// archive: tar has already promised the size in the header.
func (w *tarWriter) file(name, src string) (int64, string, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, "", err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return 0, "", err
	}
	if !fi.Mode().IsRegular() {
		return 0, "", fmt.Errorf("not a regular file")
	}
	if err := w.dir(name); err != nil {
		return 0, "", err
	}
	if err := w.tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: w.top + "/" + name,
		Mode: 0644, Size: fi.Size(), ModTime: fi.ModTime()}); err != nil {
		return 0, "", err
	}
	h := sha256.New()
	n, err := io.CopyN(io.MultiWriter(w.tw, h), in, fi.Size())
	if err != nil {
		return n, "", fmt.Errorf("%s changed while it was read: %w", src, err)
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

func (w *tarWriter) close() error {
	err := w.tw.Close()
	if gerr := w.gz.Close(); err == nil {
		err = gerr
	}
	if ferr := w.f.Close(); err == nil {
		err = ferr
	}
	return err
}

// extractedEntry is one file read from a container.
type extractedEntry struct {
	path  string // where it was written
	mtime time.Time
}

// extractContainer unpacks tgz below dest and returns the top folder and the
// files by their name below it. keep decides which names are written; the
// others are skipped. Links, devices and names that would leave dest are
// refused, not skipped: a container that has them was not made by register.
func extractContainer(tgz, dest string, keep func(name string) bool) (string, map[string]extractedEntry, error) {
	f, err := os.Open(tgz)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", nil, fmt.Errorf("%s is not a gzip archive: %w", tgz, err)
	}
	tr := tar.NewReader(gz)
	top := ""
	files := map[string]extractedEntry{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return top, files, fmt.Errorf("reading %s: %w", tgz, err)
		}
		name := path.Clean(strings.TrimPrefix(h.Name, "./"))
		if name == "." || strings.HasPrefix(name, "/") || name == ".." || strings.HasPrefix(name, "../") {
			return top, files, fmt.Errorf("%s: entry '%s' leaves the container", tgz, h.Name)
		}
		first, rest, _ := strings.Cut(name, "/")
		if top == "" {
			top = first
		} else if first != top {
			return top, files, fmt.Errorf("%s: more than one top folder ('%s', '%s')", tgz, top, first)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg:
		default:
			return top, files, fmt.Errorf("%s: entry '%s' is not a file or folder", tgz, h.Name)
		}
		if rest == "" || (keep != nil && !keep(rest)) {
			continue
		}
		target, ok := containedJoin(dest, filepath.FromSlash(name))
		if !ok {
			return top, files, fmt.Errorf("%s: entry '%s' leaves the container", tgz, h.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return top, files, err
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			return top, files, err
		}
		_, cerr := io.Copy(out, tr)
		if err := out.Close(); cerr == nil {
			cerr = err
		}
		if cerr != nil {
			return top, files, cerr
		}
		os.Chtimes(target, h.ModTime, h.ModTime)
		files[rest] = extractedEntry{path: target, mtime: h.ModTime}
	}
	if top == "" {
		return "", files, fmt.Errorf("%s is empty", tgz)
	}
	return top, files, nil
}

// isIndexName reports whether name is a file restore may write into
// collections/: an index file or the schema stamp, without a folder.
func isIndexName(name string) bool {
	if name == "" || strings.ContainsAny(name, "/\\\x00") || strings.HasPrefix(name, ".") {
		return false
	}
	return name == "SCHEMA" || strings.HasSuffix(name, ".jsonl")
}
