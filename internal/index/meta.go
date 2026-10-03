package index

// meta — file→identity, the xattr backends, and the ★ status marker. All go
// through the one fileanchor engine process.

import "strings"

// OfFileIDs returns the id(s) fileregister stamped on a file: kMDItemInformation
// (engine key "id") primary, the cross-device #S sync xattr (key "sync") as
// fallback. The file→record direction of the identity layer.
func OfFileIDs(path string) []string {
	var ids []string
	resp, err := fileAnchor().request(map[string]any{"op": "get_meta", "path": path, "key": "id"})
	if err == nil {
		if ok, _ := resp["ok"].(bool); ok {
			if values, ok := resp["values"].([]any); ok {
				for _, v := range values {
					ids = append(ids, AsString(v))
				}
			}
		}
	}
	if len(ids) == 0 {
		if s := syncID(path); strings.TrimSpace(s) != "" {
			ids = []string{s}
		}
	}
	// map(&:to_s.strip).reject(&:empty?).uniq
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// tagsGet returns the file's Finder tags as plain strings (color suffix already
// stripped by the engine's tagNames read).
func tagsGet(path string) []string {
	resp, err := fileAnchor().request(map[string]any{"op": "tags", "path": path})
	if err != nil {
		return nil
	}
	if ok, _ := resp["ok"].(bool); !ok {
		return nil
	}
	tags, _ := resp["tags"].([]any)
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		if s, ok := t.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// managedMarker is ★ (U+2605), the one Finder Tag fileregister owns.
const managedMarker = "★"

// ManagedMarked reports whether the file carries the ★ managed marker.
func ManagedMarked(path string) bool {
	for _, t := range tagsGet(path) {
		if t == managedMarker {
			return true
		}
	}
	return false
}

// ManagedMark writes the ★ managed marker (Managed.mark → Tags.add).
func ManagedMark(path string) string {
	resp, _ := fileAnchor().request(map[string]any{"op": "tag", "path": path, "value": managedMarker})
	return anchorSymbol(resp)
}

// tagsAdd adds a Finder tag (Tags.add).
func tagsAdd(path, value string) string {
	resp, _ := fileAnchor().request(map[string]any{"op": "tag", "path": path, "value": value})
	return anchorSymbol(resp)
}

// ItemProjectsAdd adds a binder to the kMDItemProjects array (ItemProjects.add).
func ItemProjectsAdd(path, value string) string {
	resp, _ := fileAnchor().request(map[string]any{"op": "set_meta", "path": path,
		"key": "groups", "value": value, "mode": "add"})
	return anchorSymbol(resp)
}

// XattrBackend reads the backend from a record: "itemprojects", "tags", or
// "none". URL refs have no file to cache membership on → implicitly "none".
func XattrBackend(rec map[string]any) string {
	if URLRef(rec) {
		return "none"
	}
	val := strings.ToLower(strings.TrimSpace(AsString(rec["xattr"])))
	switch val {
	case "itemprojects", "tags", "none":
		return val
	default:
		return "itemprojects"
	}
}

// XattrBackendAdd dispatches a binder-membership write to the chosen backend
// (XattrBackend.add): itemprojects (default), tags, or none.
func XattrBackendAdd(path, binder, backend string) string {
	switch backend {
	case "tags":
		return tagsAdd(path, binder)
	case "none":
		return "skipped"
	default:
		return ItemProjectsAdd(path, binder)
	}
}

// tagsRemove removes a Finder tag (Tags.remove → untag).
func tagsRemove(path, value string) string {
	resp, _ := fileAnchor().request(map[string]any{"op": "untag", "path": path, "value": value})
	return anchorSymbol(resp)
}

// itemProjectsRemove removes a binder from kMDItemProjects (ItemProjects.remove).
func itemProjectsRemove(path, value string) string {
	resp, _ := fileAnchor().request(map[string]any{"op": "set_meta", "path": path,
		"key": "groups", "value": value, "mode": "remove"})
	return anchorSymbol(resp)
}

// XattrBackendRemove dispatches a binder-membership removal (XattrBackend.remove).
func XattrBackendRemove(path, binder, backend string) string {
	switch backend {
	case "tags":
		return tagsRemove(path, binder)
	case "none":
		return "skipped"
	default:
		return itemProjectsRemove(path, binder)
	}
}

// ManagedUnmark drops the ★ managed marker (Managed.unmark → Tags.remove).
func ManagedUnmark(path string) string {
	return tagsRemove(path, managedMarker)
}

// itemProjectsMembers returns the binders in the file's kMDItemProjects array.
func itemProjectsMembers(path string) []string {
	resp, err := fileAnchor().request(map[string]any{"op": "get_meta", "path": path, "key": "groups"})
	if err != nil {
		return nil
	}
	if ok, _ := resp["ok"].(bool); !ok {
		return nil
	}
	values, _ := resp["values"].([]any)
	out := make([]string, 0, len(values))
	for _, v := range values {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// XattrBackendIncludes reports whether the binder is present in the file's
// backend xattr layer (XattrBackend.includes?).
func XattrBackendIncludes(path, binder, backend string) bool {
	var members []string
	switch backend {
	case "tags":
		members = tagsGet(path)
	case "none":
		return false
	default:
		members = itemProjectsMembers(path)
	}
	for _, m := range members {
		if m == binder {
			return true
		}
	}
	return false
}

// KeptTags returns a record's kept_tags: binder names whose Finder tag was on
// the file before fileregister set it. The tags backend never removes those —
// they are the user's own tags, which only happen to match a binder.
func KeptTags(rec map[string]any) []string {
	return AsStrings(rec["kept_tags"])
}

// IsKeptTag reports whether tag is one of the record's kept_tags.
func IsKeptTag(rec map[string]any, tag string) bool {
	for _, t := range KeptTags(rec) {
		if t == tag {
			return true
		}
	}
	return false
}
