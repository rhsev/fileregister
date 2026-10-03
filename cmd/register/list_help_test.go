package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout runs fn with stdout redirected and returns what it printed.
func captureStdout(t *testing.T, fn func() int) (string, int) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	code := fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String(), code
}

// A request for help must answer, not run the command. `list` accepted -h and
// --version as known flags and then ignored them, so `register list --help`
// printed the binder list instead — and without GRUBBER_NOTES it failed with
// a config error, which is no kind of help either.
func TestListAnswersHelpAndVersion(t *testing.T) {
	for _, flag := range []string{"-h", "--help"} {
		out, code := captureStdout(t, func() int { return cmdList([]string{flag}) })
		if code != 0 || !strings.HasPrefix(out, "Usage: register list") {
			t.Errorf("list %s → exit %d, out %q", flag, code, out)
		}
	}
	for _, flag := range []string{"-v", "--version"} {
		out, code := captureStdout(t, func() int { return cmdList([]string{flag}) })
		if code != 0 || !strings.Contains(out, "register list ") {
			t.Errorf("list %s → exit %d, out %q", flag, code, out)
		}
	}
}
