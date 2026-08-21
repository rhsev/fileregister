package main

// ordering — the pure algebra for arranging a binder's members (ORDERING.md).
// Absolute-only: an ordering is `sort:` keys on the member blocks; members
// without a key follow in rule order. No disk IO (that lives in cmd_order.go).

import (
	"sort"

	"github.com/rhsev/fileregister/internal/index"
)

var orderRules = []string{"name"}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// --- fractional lexicographic keys (base 36) --------------------------------

const sortDigits = "0123456789abcdefghijklmnopqrstuvwxyz"

func sortVal(c byte) int {
	for i := 0; i < len(sortDigits); i++ {
		if sortDigits[i] == c {
			return i
		}
	}
	return 0
}

// sortKeyBetween returns a key k with lower < k < upper. "" means before/after all.
// Degenerate bounds (equal keys, or an upper directly adjacent to lower, both
// only possible via hand-edited keys) can yield an out-of-range key — callers
// verify with keyInRange and fall back to materializeKeys.
func sortKeyBetween(lower, upper string) string {
	a, b := []byte(lower), []byte(upper)
	var result []byte
	i := 0
	for {
		da := 0
		if i < len(a) {
			da = sortVal(a[i])
		}
		db := 36
		if i < len(b) {
			db = sortVal(b[i])
		}
		if da == db {
			result = append(result, sortDigits[da])
			i++
			continue
		}
		mid := (da + db) / 2
		if mid != da {
			result = append(result, sortDigits[mid])
			return string(result)
		}
		result = append(result, sortDigits[da])
		i++
		nxt := 0
		if i < len(a) {
			nxt = sortVal(a[i])
		}
		result = append(result, sortDigits[(nxt+36)/2])
		return string(result)
	}
}

// keyInRange reports whether k sits strictly between lower and upper ("" = open).
func keyInRange(k, lower, upper string) bool {
	return k > lower && (upper == "" || k < upper)
}

// materializeKeys assigns a fresh, evenly spaced key to every id in order.
func materializeKeys(ids []string) map[string]string {
	out := map[string]string{}
	prev := ""
	for _, id := range ids {
		k := sortKeyBetween(prev, "")
		out[id] = k
		prev = k
	}
	return out
}

// baseOrder returns member ids in `rule` order (name: by filename, then id).
func baseOrder(records []map[string]any, rule string) ([]string, error) {
	switch rule {
	case "name", "":
		sorted := make([]map[string]any, len(records))
		copy(sorted, records)
		sort.SliceStable(sorted, func(i, j int) bool {
			fi, fj := index.AsString(sorted[i]["filename"]), index.AsString(sorted[j]["filename"])
			if fi != fj {
				return fi < fj
			}
			return index.AsString(sorted[i]["id"]) < index.AsString(sorted[j]["id"])
		})
		ids := make([]string, len(sorted))
		for i, r := range sorted {
			ids[i] = index.AsString(r["id"])
		}
		return ids, nil
	default:
		return nil, errRule(rule)
	}
}

type ruleError struct{ rule string }

func (e ruleError) Error() string {
	return "rule '" + e.rule + "' is not available (only: name)"
}
func errRule(rule string) error { return ruleError{rule} }

// sortKeyOf returns the override "sort" for id, or "".
func sortKeyOf(overrides map[string]map[string]any, id string) string {
	if o := overrides[id]; o != nil {
		return index.AsString(o["sort"])
	}
	return ""
}

// absoluteOrder: keyed members sorted by (sort, id); unkeyed after, in base order.
func absoluteOrder(base []string, overrides map[string]map[string]any) []string {
	var keyed, unkeyed []string
	for _, id := range base {
		if sortKeyOf(overrides, id) != "" {
			keyed = append(keyed, id)
		} else {
			unkeyed = append(unkeyed, id)
		}
	}
	sort.SliceStable(keyed, func(i, j int) bool {
		ki, kj := sortKeyOf(overrides, keyed[i]), sortKeyOf(overrides, keyed[j])
		if ki != kj {
			return ki < kj
		}
		return keyed[i] < keyed[j]
	})
	return append(keyed, unkeyed...)
}

func indexOfStr(xs []string, s string) int {
	for i, x := range xs {
		if x == s {
			return i
		}
	}
	return -1
}

func insertStr(xs []string, at int, s string) []string {
	xs = append(xs, "")
	copy(xs[at+1:], xs[at:])
	xs[at] = s
	return xs
}
