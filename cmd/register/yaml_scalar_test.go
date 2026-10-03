package main

import (
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Every value written as a YAML scalar must read back as the same string, or a
// binder like 2024_05 comes back as the number 202405 and promote no longer
// recognizes its own block.
func TestYamlScalarReadsBack(t *testing.T) {
	for _, s := range []string{
		"2024_05", "0x1F", "0o17", "0b101", "1_000", ".nan", "+.inf", "2026-08-01", "1e3",
		"a\rb", "x y", "tab\there", "Steuer 2024", "rules:MailMate", "★", "it's", "-draft", "",
	} {
		var m map[string]any
		repr := yamlScalar(s)
		if err := yaml.Unmarshal([]byte("k: "+repr), &m); err != nil {
			t.Errorf("%q → %s: %v", s, repr, err)
			continue
		}
		if got, ok := m["k"].(string); !ok || got != s {
			t.Errorf("%q → %s → %#v", s, repr, m["k"])
		}
	}
	// Ordinary names stay as they were written before.
	for s, want := range map[string]string{"Steuer 2024": "Steuer 2024", "★": "★", "42": "'42'"} {
		if got := yamlScalar(s); got != want {
			t.Errorf("yamlScalar(%q) = %s, want %s", s, got, want)
		}
	}
}

func TestMaterializedKeysStayShortAndOrdered(t *testing.T) {
	for _, n := range []int{1, 3, 17, 600, 5000} {
		ids := make([]string, n)
		for i := range ids {
			ids[i] = strconv.Itoa(i)
		}
		keys := materializeKeys(ids)
		prev := ""
		for i, id := range ids {
			k := keys[id]
			if k <= prev || strings.HasSuffix(k, "0") {
				t.Fatalf("n=%d: key %d %q after %q", n, i, k, prev)
			}
			if len(k) > 3 {
				t.Fatalf("n=%d: key %q is long", n, k)
			}
			if mid := sortKeyBetween(prev, k); !keyInRange(mid, prev, k) {
				t.Fatalf("n=%d: nothing fits between %q and %q", n, prev, k)
			}
			prev = k
		}
	}
}
