package main

import (
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
