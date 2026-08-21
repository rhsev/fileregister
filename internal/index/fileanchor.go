package index

// fileanchor — Go client for the fileanchor metadata engine.
//
// One persistent engine subprocess for the whole CLI run, spoken to over the
// batch JSONL protocol (one request object per line, one response per line, in
// order) — the "no fork+exec per file" win. Bookmarks and the other metadata
// clients are thin callers of this one process.
//
// The engine is a separate project (github.com/rhsev/fileanchor). Binary
// resolution, first match wins:
//   $FILEANCHOR > `fileanchor` on PATH > <bindir>/libexec/fileanchor

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// syncXattr is the cross-device id alias name (custom namespace + the `#S`
// syncable flag, part of the literal attribute name). fileregister hands it to
// the otherwise consumer-neutral engine via --sync-name.
const syncXattr = "com.fileregister.id#S"

// anchor is the process-wide fileanchor engine client: a lazily-spawned
// singleton held open for the whole run.
type anchor struct {
	mu   sync.Mutex
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  *bufio.Reader
	err  error // sticky spawn error
	done bool  // shutdown already called
}

var (
	engine     *anchor
	engineOnce sync.Once
)

// fileAnchor returns the singleton engine client, spawning it on first use.
func fileAnchor() *anchor {
	engineOnce.Do(func() {
		engine = &anchor{}
		engine.spawn()
	})
	return engine
}

// ResetEngine shuts down the current engine (if any) and clears the singleton
// so the next request spawns afresh. Test hook — CLI tests run many commands
// in one process and isolate the engine per run.
func ResetEngine() {
	if engine != nil {
		engine.shutdown()
	}
	engine = nil
	engineOnce = sync.Once{}
}

// anchorBin resolves the engine binary: $FILEANCHOR > PATH > a copy installed
// alongside the running executable (<bindir>/libexec/fileanchor, where
// `make install` puts it — see Makefile).
func anchorBin() (string, error) {
	if env := os.Getenv("FILEANCHOR"); env != "" {
		return env, nil
	}
	if p, err := exec.LookPath("fileanchor"); err == nil {
		return p, nil
	}
	// Resolve relative to the real executable path (follow symlinks, so a
	// symlinked `register` still finds its sibling libexec dir).
	self, err := os.Executable()
	if err == nil {
		if resolved, rerr := filepath.EvalSymlinks(self); rerr == nil {
			self = resolved
		}
		cand := filepath.Join(filepath.Dir(self), "libexec", "fileanchor")
		if st, serr := os.Stat(cand); serr == nil && !st.IsDir() {
			return cand, nil
		}
	}
	return "", fmt.Errorf("'fileanchor' not found on PATH, none installed alongside the binary, and FILEANCHOR is unset; build one with `make fileanchor`")
}

// spawn starts the engine process and wires up its pipes. A spawn failure is
// stored on a.err and surfaced on the first request, so a missing binary fails
// at the point of use rather than at startup.
func (a *anchor) spawn() {
	bin, err := anchorBin()
	if err != nil {
		a.err = err
		return
	}
	cmd := exec.Command(bin, "--sync-name", syncXattr)
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		a.err = err
		return
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		a.err = err
		return
	}
	if err := cmd.Start(); err != nil {
		a.err = err
		return
	}
	a.cmd = cmd
	a.in = in
	a.out = bufio.NewReader(out)
}

// request sends one request object and reads one response object. Write a line,
// read a line — the engine flushes after every response so the round-trip never
// deadlocks.
func (a *anchor) request(req map[string]any) (map[string]any, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.requestLocked(req)
}

func (a *anchor) requestLocked(req map[string]any) (map[string]any, error) {
	if a.err != nil {
		return nil, a.err
	}
	line, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := a.in.Write(append(line, '\n')); err != nil {
		return nil, a.poison(fmt.Errorf("fileanchor: write failed: %w", err))
	}
	respLine, err := a.out.ReadBytes('\n')
	if err != nil {
		if err == io.EOF && len(respLine) == 0 {
			return nil, a.poison(fmt.Errorf("fileanchor: no response (engine exited?)"))
		}
		if err != io.EOF {
			return nil, a.poison(fmt.Errorf("fileanchor: read failed: %w", err))
		}
		// EOF with a partial line: the engine died mid-write. Even a fragment
		// that happens to parse cannot be trusted as a complete response.
		return nil, a.poison(fmt.Errorf("fileanchor: truncated response %q (engine exited?)", respLine))
	}
	var resp map[string]any
	if err := json.Unmarshal(respLine, &resp); err != nil {
		// The protocol has no correlation ids — after a stray line, every later
		// request would silently read the PREVIOUS request's answer. Fail the
		// whole client instead of continuing desynced.
		return nil, a.poison(fmt.Errorf("fileanchor: bad response %q: %w", respLine, err))
	}
	return resp, nil
}

// poison marks the client broken (sticky error), reaps the engine, and returns
// err — after a protocol violation the ordered response stream can no longer be
// trusted. Caller holds a.mu.
func (a *anchor) poison(err error) error {
	a.err = err
	a.done = true
	if a.cmd != nil && a.cmd.Process != nil {
		a.cmd.Process.Kill()
		a.cmd.Wait()
	}
	return err
}

// batch sends many requests through the same persistent process, returning
// responses in input order. Held under one lock so interleaved callers can't
// desync the ordered stream.
func (a *anchor) batch(reqs []map[string]any) ([]map[string]any, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	resps := make([]map[string]any, 0, len(reqs))
	for _, r := range reqs {
		resp, err := a.requestLocked(r)
		if err != nil {
			return resps, err
		}
		resps = append(resps, resp)
	}
	return resps, nil
}

// shutdown closes stdin so the engine sees EOF and exits, then reaps it.
func (a *anchor) shutdown() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.done || a.cmd == nil {
		return
	}
	a.done = true
	if a.in != nil {
		a.in.Close()
	}
	a.cmd.Wait()
}

// anchorSymbol maps an engine action response to fileregister's add/remove
// outcome. On a failed op (ok:false) → "failed", mirroring FileAnchor.symbol.
func anchorSymbol(resp map[string]any) string {
	if ok, _ := resp["ok"].(bool); !ok {
		return "failed"
	}
	switch resp["action"] {
	case "added":
		return "added"
	case "removed":
		return "removed"
	case "set":
		return "set"
	case "noop":
		return "noop"
	default:
		return "failed"
	}
}
