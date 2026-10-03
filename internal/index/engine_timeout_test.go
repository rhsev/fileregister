package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// An engine that stops answering must not hang register (which holds its
// locks for the whole run): the request fails after requestTimeout.
func TestHungEngineTimesOut(t *testing.T) {
	fake := filepath.Join(t.TempDir(), "fileanchor")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexec sleep 60\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FILEANCHOR", fake)
	defer func(d time.Duration) { requestTimeout = d }(requestTimeout)
	requestTimeout = 300 * time.Millisecond
	ResetEngine()
	defer ResetEngine()

	start := time.Now()
	_, err := fileAnchor().request(map[string]any{"op": "tags", "path": "/"})
	if err == nil || !strings.Contains(err.Error(), "no answer") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("took %s", time.Since(start))
	}
	if EngineError() == nil {
		t.Error("the engine is not marked broken after the timeout")
	}
}

// A resolve that answers stale:true reaches the caller, which renews it.
func TestBatchResolvePassesStale(t *testing.T) {
	fake := filepath.Join(t.TempDir(), "fileanchor")
	script := "#!/bin/sh\nwhile read l; do echo '{\"ok\":true,\"path\":\"/tmp\",\"stale\":true}'; done\n"
	if err := os.WriteFile(fake, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FILEANCHOR", fake)
	t.Setenv("HOME", t.TempDir())
	os.MkdirAll(filepath.Dir(bookmarkFile()), 0755)
	os.WriteFile(bookmarkFile(), []byte(`{"111111111":"Ym9vaw=="}`), 0644)
	ResetEngine()
	defer ResetEngine()

	res, err := BatchResolve([]string{"111111111"})
	if err != nil || !res["111111111"].Stale || res["111111111"].Path != "/tmp" {
		t.Errorf("BatchResolve = %+v, %v", res, err)
	}
}
