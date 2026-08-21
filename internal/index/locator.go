package index

// locator — file discovery by indexed metadata (Spotlight via the fileanchor
// engine's query op). The
// portability seam for repair/audit's reverse lookups.

import (
	"path/filepath"
	"strings"
)

// locatorQuery runs a Spotlight selector through the engine, returning absolute paths.
func locatorQuery(by, value string) []string {
	resp, err := fileAnchor().request(map[string]any{"op": "query", "by": by, "value": value})
	if err != nil {
		return nil
	}
	if ok, _ := resp["ok"].(bool); !ok {
		return nil
	}
	paths, _ := resp["paths"].([]any)
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if s, ok := p.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// restrictByFilename keeps only files whose basename matches (case-insensitive).
// An empty filename returns all paths.
func restrictByFilename(paths []string, filename string) []string {
	if filename == "" {
		return paths
	}
	base := strings.ToLower(filename)
	var out []string
	for _, p := range paths {
		if strings.ToLower(filepath.Base(p)) == base {
			out = append(out, p)
		}
	}
	return out
}

// ByDescriptionID finds files whose kMDItemInformation contains the id token.
func ByDescriptionID(id string) []string { return locatorQuery("id", id) }

// ByFilename finds files by exact on-disk filename.
func ByFilename(filename string) []string { return locatorQuery("filename", filename) }

// ByXattrItemProjects finds files tagged with binder in kMDItemProjects.
func ByXattrItemProjects(binder, filename string) []string {
	return restrictByFilename(locatorQuery("groups", binder), filename)
}

// ByXattrTags finds files tagged with binder in kMDItemUserTags.
func ByXattrTags(binder, filename string) []string {
	return restrictByFilename(locatorQuery("tag", binder), filename)
}
