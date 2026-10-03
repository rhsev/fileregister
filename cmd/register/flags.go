package main

// flags — the one table-driven argument parser. Commands declare their value
// and boolean flags; -h/--help and -v/--version are built in. cmd_album keeps
// a hand-rolled loop on purpose: its --milan takes an OPTIONAL value, which
// the table model doesn't express.

import "strings"

// parseFlagsMulti splits args into value flags (every occurrence kept, in
// order), boolean flags, and positional args. The first unrecognized flag is
// returned as unknown — or a value flag given no value: at the end of the
// line, or followed by another --flag, which it used to swallow as its value
// (`--binder --edit` made a binder named "--edit"). A value that starts with
// -- can still be given inline (--binder=--x). A bare -- ends the options:
// everything after it is positional (`rename -- -draft final`).
func parseFlagsMulti(args []string, valFlags, boolFlags map[string]bool) (map[string][]string, map[string]bool, []string, string) {
	vals := map[string][]string{}
	bools := map[string]bool{}
	var pos []string
	unknown := ""
	i := 0
	for i < len(args) {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if a == "-h" || a == "--help" {
			bools["--help"] = true
			i++
			continue
		}
		if a == "-v" || a == "--version" {
			bools["--version"] = true
			i++
			continue
		}
		if strings.HasPrefix(a, "--") {
			name, inline, hasInline := a, "", false
			if eq := strings.Index(a, "="); eq >= 0 {
				name, inline, hasInline = a[:eq], a[eq+1:], true
			}
			if boolFlags[name] {
				bools[name] = true
				i++
				continue
			}
			if valFlags[name] {
				switch {
				case hasInline:
					vals[name] = append(vals[name], inline)
				case i+1 < len(args) && !strings.HasPrefix(args[i+1], "--"):
					i++
					vals[name] = append(vals[name], args[i])
				default:
					if unknown == "" {
						unknown = name + missingValue
					}
				}
				i++
				continue
			}
			if unknown == "" {
				unknown = name
			}
			i++
			continue
		}
		if strings.HasPrefix(a, "-") {
			if unknown == "" {
				unknown = a
			}
			i++
			continue
		}
		pos = append(pos, a)
		i++
	}
	return vals, bools, pos, unknown
}

// missingValue marks an "unknown" that is a value flag without its value.
const missingValue = " needs a value"

// parseFlags is parseFlagsMulti for single-valued flags: the last occurrence
// wins.
func parseFlags(args []string, valFlags, boolFlags map[string]bool) (map[string]string, map[string]bool, []string, string) {
	multi, bools, pos, unknown := parseFlagsMulti(args, valFlags, boolFlags)
	vals := map[string]string{}
	for k, vs := range multi {
		vals[k] = vs[len(vs)-1]
	}
	return vals, bools, pos, unknown
}
