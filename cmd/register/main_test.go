package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestMain points HOME at a throwaway directory for the whole package run.
//
// bookmarkFile() resolves HOME on every call, so without this any test that
// reaches SaveDB rewrites the developer's own ~/.local/share/bookmarks.json
// with whatever fixture it happened to build. That is not theoretical: on
// 2026-10-03 a two-line test of the prune path replaced a 286-entry store with
// three fixture keys, and only an APFS snapshot got it back.
//
// A test that wants the real environment has to say so by setting HOME itself.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "register-test-home-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", tmp)

	// order, rename and album read notes through grubber. Give the whole run
	// the copy `make grubber` builds when none is configured or on PATH, the
	// same lookup as grubberTestBin — the CI job has only that copy.
	if os.Getenv("GRUBBER_BIN") == "" {
		if _, err := exec.LookPath("grubber"); err != nil {
			if bin, aerr := filepath.Abs(filepath.Join("..", "..", ".build", "grubber")); aerr == nil {
				if _, serr := os.Stat(bin); serr == nil {
					os.Setenv("GRUBBER_BIN", bin)
				}
			}
		}
	}
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}
