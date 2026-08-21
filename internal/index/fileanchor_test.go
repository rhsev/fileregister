package index

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// useEngine points $FILEANCHOR at a real fileanchor so the test drives the
// engine rather than a stub: an explicit $FILEANCHOR wins, else one on PATH,
// else the build `make fileanchor` produces. Skips when there is none — CI
// builds the engine first, so a skip there is a broken engine step.
func useEngine(t *testing.T) {
	t.Helper()
	bin := os.Getenv("FILEANCHOR")
	if bin == "" {
		if p, err := exec.LookPath("fileanchor"); err == nil {
			bin = p
		}
	}
	if bin == "" {
		abs, err := filepath.Abs(filepath.Join("..", "..", ".build", "fileanchor"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(abs); err != nil {
			t.Skipf("no fileanchor engine: FILEANCHOR unset, none on PATH, none at %s — run `make fileanchor`", abs)
		}
		bin = abs
	}
	t.Setenv("FILEANCHOR", bin)
	// Reset the process singleton so each test spawns a fresh engine.
	ResetEngine()
}

// TestAnchorSaveResolve exercises the persistent pipe end to end: save a
// bookmark blob for a real file, then resolve it back to the same path.
func TestAnchorSaveResolve(t *testing.T) {
	useEngine(t)

	f := filepath.Join(t.TempDir(), "target.txt")
	if err := os.WriteFile(f, []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}
	// realpath — the engine round-trips the canonical path (/tmp → /private/tmp).
	real, err := filepath.EvalSymlinks(f)
	if err != nil {
		t.Fatal(err)
	}

	a := fileAnchor()
	defer a.shutdown()

	save, err := a.request(map[string]any{"op": "save", "path": real})
	if err != nil {
		t.Fatalf("save request: %v", err)
	}
	if ok, _ := save["ok"].(bool); !ok {
		t.Fatalf("save not ok: %v", save)
	}
	blob, _ := save["blob"].(string)
	if blob == "" {
		t.Fatalf("save returned empty blob: %v", save)
	}

	res, err := a.request(map[string]any{"op": "resolve", "blob": blob})
	if err != nil {
		t.Fatalf("resolve request: %v", err)
	}
	if ok, _ := res["ok"].(bool); !ok {
		t.Fatalf("resolve not ok: %v", res)
	}
	if got, _ := res["path"].(string); got != real {
		t.Errorf("resolve path = %q, want %q", got, real)
	}
}

// TestAnchorBatchOrder verifies batch responses come back in input order — the
// property batch_get relies on to zip ids to paths.
func TestAnchorBatchOrder(t *testing.T) {
	useEngine(t)

	dir := t.TempDir()
	var reqs []map[string]any
	var wantPaths []string
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(name), 0644); err != nil {
			t.Fatal(err)
		}
		real, _ := filepath.EvalSymlinks(p)
		wantPaths = append(wantPaths, real)
		reqs = append(reqs, map[string]any{"op": "save", "path": real})
	}

	a := fileAnchor()
	defer a.shutdown()

	saves, err := a.batch(reqs)
	if err != nil {
		t.Fatalf("batch save: %v", err)
	}
	var resolveReqs []map[string]any
	for _, s := range saves {
		resolveReqs = append(resolveReqs, map[string]any{"op": "resolve", "blob": s["blob"]})
	}
	resolves, err := a.batch(resolveReqs)
	if err != nil {
		t.Fatalf("batch resolve: %v", err)
	}
	if len(resolves) != len(wantPaths) {
		t.Fatalf("got %d resolves, want %d", len(resolves), len(wantPaths))
	}
	for i, r := range resolves {
		if got, _ := r["path"].(string); got != wantPaths[i] {
			t.Errorf("resolve[%d] = %q, want %q", i, got, wantPaths[i])
		}
	}
}

// TestAnchorSymbol checks the action→outcome mapping without the engine.
func TestAnchorSymbol(t *testing.T) {
	cases := []struct {
		resp map[string]any
		want string
	}{
		{map[string]any{"ok": true, "action": "added"}, "added"},
		{map[string]any{"ok": true, "action": "removed"}, "removed"},
		{map[string]any{"ok": true, "action": "set"}, "set"},
		{map[string]any{"ok": true, "action": "noop"}, "noop"},
		{map[string]any{"ok": false, "action": "added"}, "failed"},
		{map[string]any{"ok": true, "action": "weird"}, "failed"},
	}
	for _, c := range cases {
		if got := anchorSymbol(c.resp); got != c.want {
			t.Errorf("anchorSymbol(%v) = %q, want %q", c.resp, got, c.want)
		}
	}
}
