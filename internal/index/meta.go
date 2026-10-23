package index

// meta — file→identity, the xattr backends, and the ★ status marker. All go
// through the one fileanchor engine process.

import (
	"errors"
	"strings"
)

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

// MetaReadable asks the engine to read the file's id attribute and returns
// the engine's refusal, if any: a file system without extended attributes, a
// file it may not read. An absent id is no error.
func MetaReadable(path string) error {
	resp, err := fileAnchor().request(map[string]any{"op": "get_meta", "path": path, "key": "id"})
	if err != nil {
		return err
	}
	if ok, _ := resp["ok"].(bool); !ok {
		msg, _ := resp["error"].(string)
		return errors.New(msg)
	}
	return nil
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

// The two Finder tags fileregister owns. Every registered file carries one of
// them: ★ (U+2605) when its record is in at least one binder, ☆ (U+2606) when
// it is in the index but in no binder. Both say "handle with care".
const (
	managedMarker    = "★"
	registeredMarker = "☆"
)

// StatusStar is the star a file whose record is (member) or is not in a
// binder carries.
func StatusStar(member bool) string {
	if member {
		return managedMarker
	}
	return registeredMarker
}

// IsStatusStar reports whether tag is one of the two stars fileregister owns.
func IsStatusStar(tag string) bool {
	return tag == managedMarker || tag == registeredMarker
}

// StatusStars reports which of the two stars the file carries.
func StatusStars(path string) (filled, open bool) {
	for _, t := range tagsGet(path) {
		switch t {
		case managedMarker:
			filled = true
		case registeredMarker:
			open = true
		}
	}
	return filled, open
}

// StatusMarked reports whether the file carries the right star, and only it.
func StatusMarked(path string, member bool) bool {
	filled, open := StatusStars(path)
	if member {
		return filled && !open
	}
	return open && !filled
}

// StatusMark gives the file the star for its state and takes the other one
// off. Returns "added" when the right star was written, "noop" when it was
// there, "failed" when the engine refused.
func StatusMark(path string, member bool) string {
	result := tagsAdd(path, StatusStar(member))
	if result != "failed" {
		tagsRemove(path, StatusStar(!member))
	}
	return result
}

// StatusUnmark takes both stars off: the file has left the register.
func StatusUnmark(path string) bool {
	a := tagsRemove(path, managedMarker) == "removed"
	b := tagsRemove(path, registeredMarker) == "removed"
	return a || b
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
// "none". URL records have no file to cache membership on → implicitly "none".
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
	// SameBinder, not ==: the engine stores every value NFC (fileanchor
	// Values.swift), so a binder the index still holds decomposed would never
	// match its own xattr. That made `audit` report a present kMDItemProjects
	// as MISSING XATTR and send the user to `register refresh`, which asks the
	// engine, gets "noop" because the name is already there, and reports
	// "Already ok" — a warning no run could clear.
	for _, m := range members {
		if SameBinder(m, binder) {
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

// FileTags returns the file's Finder tags (names only, no colors), or the
// engine's refusal: a file that cannot be read is not a file without tags.
func FileTags(path string) ([]string, error) {
	resp, err := fileAnchor().request(map[string]any{"op": "tags", "path": path})
	if err != nil {
		return nil, err
	}
	if ok, _ := resp["ok"].(bool); !ok {
		msg, _ := resp["error"].(string)
		return nil, errors.New(msg)
	}
	tags, _ := resp["tags"].([]any)
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		if s, ok := t.(string); ok {
			out = append(out, s)
		}
	}
	return out, nil
}

// AddTag adds a Finder tag the way the user would: added, noop, or the
// engine's error symbol.
func AddTag(path, tag string) string { return tagsAdd(path, tag) }

// FileComment returns the file's Finder comment as the engine reads it (the
// xattr; a comment Finder keeps only in .DS_Store is not seen), "" for none.
func FileComment(path string) (string, error) {
	resp, err := fileAnchor().request(map[string]any{"op": "get_meta", "path": path, "key": "comment"})
	if err != nil {
		return "", err
	}
	if ok, _ := resp["ok"].(bool); !ok {
		msg, _ := resp["error"].(string)
		return "", errors.New(msg)
	}
	return AsString(resp["value"]), nil
}

// SetFileComment writes the Finder comment through the engine.
func SetFileComment(path, comment string) error {
	resp, err := fileAnchor().request(map[string]any{"op": "set_meta", "path": path,
		"key": "comment", "value": comment, "mode": "set"})
	if err != nil {
		return err
	}
	if ok, _ := resp["ok"].(bool); !ok {
		msg, _ := resp["error"].(string)
		return errors.New(msg)
	}
	return nil
}

// FreeTags is the part of a file's tags that belongs to the user: all but
// the two stars and, for a record with the tags backend, its binder names. A binder name in
// kept_tags was the user's tag before it was the binder's, so it stays free.
func FreeTags(rec map[string]any, tags []string) []string {
	binderTag := map[string]bool{}
	if XattrBackend(rec) == "tags" {
		for _, b := range AsStrings(rec["binder"]) {
			binderTag[NFC(b)] = true
		}
	}
	var out []string
	for _, t := range tags {
		if IsStatusStar(t) {
			continue
		}
		if binderTag[NFC(t)] && !IsKeptTag(rec, t) {
			continue
		}
		out = append(out, t)
	}
	return out
}
