package main

// flags — the one table-driven argument parser. Commands declare their value
// and boolean flags; -h/--help and -v/--version are built in. cmd_album keeps
// a hand-rolled loop on purpose: its --milan takes an OPTIONAL value, which
// the table model doesn't express.

import "strings"

// parseFlagsMulti splits args into value flags (every occurrence kept, in
// order), boolean flags, and positional args. A value flag at the end of the
// line records an empty value, so presence stays detectable. The first
// unrecognized flag is returned as unknown.
func parseFlagsMulti(args []string, valFlags, boolFlags map[string]bool) (map[string][]string, map[string]bool, []string, string) {
	vals := map[string][]string{}
	bools := map[string]bool{}
	var pos []string
	unknown := ""
	i := 0
	for i < len(args) {
		a := args[i]
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
				case i+1 < len(args):
					i++
					vals[name] = append(vals[name], args[i])
				default:
					vals[name] = append(vals[name], "")
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
