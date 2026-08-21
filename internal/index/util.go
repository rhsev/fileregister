package index

// util — small shared primitives of the index core: JSON serialization that
// leaves <, >, & and non-ASCII intact, plus path expansion.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// JSONVal renders v with HTML escaping off, so the byte form matches the
// JSON.generate (which leaves <, >, & and non-ASCII intact).
func JSONVal(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
	return strings.TrimRight(b.String(), "\n")
}

// ExpandPath expands a leading ~ and makes the
// path absolute against the cwd.
func ExpandPath(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			if p == "~" {
				return home
			}
			return filepath.Join(home, p[2:])
		}
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// FileExists reports whether path exists (any type).
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
