package index

import (
	"os"
	"testing"
)

// TestMain points HOME at a throwaway directory for the whole package run —
// bookmarkFile() reads HOME on every call, and a test reaching SaveDB would
// otherwise rewrite the developer's own bookmark store. See the note in
// cmd/register/main_test.go.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "index-test-home-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", tmp)
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}
