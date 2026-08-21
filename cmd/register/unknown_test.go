package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestUnknownOption: a mistyped flag fails loudly (exit 1) instead of being
// silently ignored — across a switch-parser, the list validator, and parseFlags.
func TestUnknownOption(t *testing.T) {
	anchor := engineBin(t)
	n := t.TempDir()
	os.MkdirAll(filepath.Join(n, "collections"), 0755)

	o, e, c := runGoAdd(t, addEnv(t, n, anchor), "somefile.pdf", "--hind", "pdf", "--binder", "A")
	assertGolden(t, "unknown_add", cliResult(o, e, c))

	o, e, c = runGoList(t, listEnv(t, n, anchor), "--xyz")
	assertGolden(t, "unknown_list", cliResult(o, e, c))

	o, e, c = runGoOrder(t, orderEnv(t, n), "show", "A", "--bogus")
	assertGolden(t, "unknown_order", cliResult(o, e, c))
}
