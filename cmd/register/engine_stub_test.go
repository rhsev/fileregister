package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// engineStub is an engine for tests that need an answer the real one does
// not give on the machine the test runs on: whether Spotlight indexes a temp
// folder, a bookmark without a recorded path, an engine that ends mid-run. It
// wraps the real engine and passes every request through, except these.
//
// The stub is the test binary itself, started through a two-line sh script
// with stubEnv set (TestMain looks for it first), so the tests need nothing
// installed beyond what they are built from.
type engineStub struct {
	Real       string            `json:"real"`
	Searchable string            `json:"searchable,omitempty"` // the reply to every searchable
	DieOn      string            `json:"die_on,omitempty"`     // end when this op is asked
	NoPath     []string          `json:"no_path,omitempty"`    // blobs resolved without last_path
	Gone       map[string]string `json:"gone,omitempty"`       // blob → path it is said to resolve to
}

const stubEnv = "REGISTER_TEST_ENGINE_STUB"

// script writes the stub and returns its path, for FILEANCHOR.
func (s engineStub) script(t *testing.T) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	conf, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	confPath := filepath.Join(dir, "stub.json")
	if err := os.WriteFile(confPath, conf, 0644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "fileanchor-stub")
	body := "#!/bin/sh\n" + stubEnv + "=" + shQuote(confPath) + " exec " + shQuote(self) + " \"$@\"\n"
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
	return script
}

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// stubSearchable answers every searchable with reply.
func stubSearchable(t *testing.T, real, reply string) string {
	t.Helper()
	return engineStub{Real: real, Searchable: reply}.script(t)
}

// stubDyingOn ends the moment it is asked op, as an engine that crashes in
// the middle of a run does.
func stubDyingOn(t *testing.T, real, op string) string {
	t.Helper()
	return engineStub{Real: real, DieOn: op}.script(t)
}

// stubResolve says yes to searchable, so a miss counts where the test's
// folders are not indexed. A resolve of a blob in noPath is answered without
// last_path, one of a blob in gonePath as resolving to that path.
func stubResolve(t *testing.T, real string, noPath []string, gonePath map[string]string) string {
	t.Helper()
	return engineStub{Real: real, Searchable: `{"ok":true,"searchable":true}`, NoPath: noPath, Gone: gonePath}.script(t)
}

// runEngineStub is the stub's main: one request line in, one reply line out.
// It does not return.
func runEngineStub(confPath string) {
	data, err := os.ReadFile(confPath)
	var s engineStub
	if err != nil || json.Unmarshal(data, &s) != nil {
		fmt.Fprintln(os.Stderr, "engine stub: cannot read", confPath)
		os.Exit(2)
	}
	engine := exec.Command(s.Real, os.Args[1:]...)
	engine.Stderr = os.Stderr
	toEngine, _ := engine.StdinPipe()
	fromEnginePipe, _ := engine.StdoutPipe()
	if err := engine.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "engine stub:", err)
		os.Exit(2)
	}
	fromEngine := bufio.NewReaderSize(fromEnginePipe, 1<<20)
	in := bufio.NewReaderSize(os.Stdin, 1<<20)
	out := bufio.NewWriter(os.Stdout)
	noPath := map[string]bool{}
	for _, b := range s.NoPath {
		noPath[b] = true
	}
	for {
		line, rerr := in.ReadString('\n')
		if line != "" {
			var req struct {
				Op   string `json:"op"`
				Blob string `json:"blob"`
			}
			json.Unmarshal([]byte(line), &req) // not JSON: passed through as it is
			gone, isGone := s.Gone[req.Blob]
			switch {
			case s.DieOn != "" && req.Op == s.DieOn:
				engine.Process.Kill()
				os.Exit(1)
			case s.Searchable != "" && req.Op == "searchable":
				fmt.Fprintln(out, s.Searchable)
			case req.Op == "resolve" && isGone:
				reply, _ := json.Marshal(map[string]any{"ok": true, "path": gone})
				fmt.Fprintln(out, string(reply))
			default:
				if !strings.HasSuffix(line, "\n") {
					line += "\n"
				}
				toEngine.Write([]byte(line))
				reply, _ := fromEngine.ReadString('\n')
				reply = strings.TrimRight(reply, "\n")
				if req.Op == "resolve" && noPath[req.Blob] {
					var r map[string]any
					if json.Unmarshal([]byte(reply), &r) == nil {
						delete(r, "last_path")
						b, _ := json.Marshal(r)
						reply = string(b)
					}
				}
				fmt.Fprintln(out, reply)
			}
			out.Flush()
		}
		if rerr != nil {
			break
		}
	}
	toEngine.Close()
	engine.Wait()
	os.Exit(0)
}
