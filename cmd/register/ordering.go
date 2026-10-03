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
		// No digit fits between: keep lower's digit and go above the rest of
		// lower. Only one digit further (the mid of it and 36) failed when
		// that digit is z — "0zr" and "101" gave "0z", below the lower bound.
		result = append(result, sortDigits[da])
		rest := ""
		if i+1 < len(a) {
			rest = string(a[i+1:])
		}
		return string(result) + sortKeyBetween(rest, "")
	}
}

// keyInRange reports whether k sits strictly between lower and upper ("" = open).
func keyInRange(k, lower, upper string) bool {
	return k > lower && (upper == "" || k < upper)
}

// materializeKeys assigns a fresh, evenly spaced key to every id in order.
// All keys get one width, the smallest that leaves a free slot between
// neighbours, so the keys stay short: appending each after the last grew them
// by a character every six members (~100 characters at 600; now 2).
// No key ends in 0 — nothing fits between "a" and "a0".
func materializeKeys(ids []string) map[string]string {
	out := map[string]string{}
	n := len(ids)
	if n == 0 {
		return out
	}
	width, space := 1, 36
	for space/(n+1) < 2 {
		width++
		space *= 36
	}
	step := space / (n + 1)
	for i, id := range ids {
		v := (i + 1) * step
		if v%36 == 0 {
			v++
		}
		out[id] = base36Key(v, width)
	}
	return out
}

// base36Key renders v in base 36 with sortDigits, zero-padded to width.
func base36Key(v, width int) string {
	b := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		b[i] = sortDigits[v%36]
		v /= 36
	}
	return string(b)
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
