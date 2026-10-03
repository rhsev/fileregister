package main

import (
	"os"
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
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}
