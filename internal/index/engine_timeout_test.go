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
