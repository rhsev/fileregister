package main

import (
	"github.com/rhsev/fileregister/internal/index"

	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// registerVersion is the one version string; every subcommand's --version
// prints it.
const registerVersion = "1.3.0"

// ---------------------------------------------------------------------------
// Resolve the notes directory
// ---------------------------------------------------------------------------

// grubberConfig holds the minimal grubber config.yaml data we need.
type grubberConfig struct {
	Sets map[string]struct {
		Path string `yaml:"path"`
	} `yaml:"sets"`
}

// expandHome delegates to the one path expander (bare ~, ~/, absolutization)
// so config paths resolve like every other path the program touches. An empty
// value stays empty — ExpandPath would absolutize it to the cwd.
func expandHome(p string) string {
	if p == "" {
		return ""
	}
	return index.ExpandPath(p)
}

func grubberConfigPath() string {
	if v := os.Getenv("GRUBBER_CONFIG"); v != "" {
		return expandHome(v)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "grubber", "config.yaml")
}

// loadGrubberConfig reads the `sets.<name>.path` mapping from grubber's
// config.yaml. Unknown keys (filters, ext, …) are ignored. A real YAML parse is
// used deliberately — a
// line-by-line shortcut mis-parses sets that carry nested keys before `path:`.
func loadGrubberConfig(cfgPath string) (*grubberConfig, error) {
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, err
	}
	var cfg grubberConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}


func notesDir() (string, error) {
	grubberSet := os.Getenv("GRUBBER_SET")
	if grubberSet != "" {
		cfgPath := grubberConfigPath()

		// Try to get path from config
		cfg, cfgErr := loadGrubberConfig(cfgPath)

		if cfgErr != nil {
			// Config file missing
			fmt.Fprintf(os.Stderr, "Error: GRUBBER_SET='%s' given, but no grubber config at %s\n", grubberSet, cfgPath)
			return "", fmt.Errorf("exit 1")
		}

		var setPath string
		var setExists bool
		if cfg.Sets != nil {
			entry, ok := cfg.Sets[grubberSet]
			setExists = ok
			if ok {
				setPath = expandHome(entry.Path)
			}
		}

		if !setExists {
			// Set unknown
			fmt.Fprintf(os.Stderr, "Error: GRUBBER_SET='%s' is not a set in %s\n", grubberSet, cfgPath)
			names := make([]string, 0, len(cfg.Sets))
			for k := range cfg.Sets {
				names = append(names, k)
			}
			// Look for case-insensitive match
			lower := strings.ToLower(grubberSet)
			for _, n := range names {
				if strings.ToLower(n) == lower {
					fmt.Fprintf(os.Stderr, "Hint: did you mean '%s'? (set names are case-sensitive)\n", n)
					break
				}
			}
			if len(names) > 0 {
				sorted := make([]string, len(names))
				copy(sorted, names)
				sort.Strings(sorted)
				fmt.Fprintf(os.Stderr, "Available sets: %s\n", strings.Join(sorted, ", "))
			}
			return "", fmt.Errorf("exit 1")
		}

		if setPath == "" {
			// Set exists but no path:
			fmt.Fprintf(os.Stderr, "Error: GRUBBER_SET='%s' has no `path:` entry in %s\n", grubberSet, cfgPath)
			return "", fmt.Errorf("exit 1")
		}

		if info, err := os.Stat(setPath); err == nil && info.IsDir() {
			return setPath, nil
		}

		// Set resolves but dir missing
		fmt.Fprintf(os.Stderr, "Error: GRUBBER_SET='%s' points to a path that doesn't exist:\n", grubberSet)
		fmt.Fprintf(os.Stderr, "       %s\n", setPath)
		fmt.Fprintf(os.Stderr, "Hint: check the path entry under sets.%s in %s\n", grubberSet, cfgPath)
		return "", fmt.Errorf("exit 1")
	}

	dir := os.Getenv("GRUBBER_NOTES")
	if dir == "" {
		fmt.Fprintln(os.Stderr, "Error: GRUBBER_NOTES is not set or not a directory (and no GRUBBER_SET given)")
		return "", fmt.Errorf("exit 1")
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		fmt.Fprintln(os.Stderr, "Error: GRUBBER_NOTES is not set or not a directory (and no GRUBBER_SET given)")
		return "", fmt.Errorf("exit 1")
	}
	return dir, nil
}

// listAll — pure-register list (no Markdown scan, no suffix). Kept as the
// 1-arg entry point; the body lives in listAllFiltered
// (cmd_list.go), shared with the --inbox/--curated variants.
func listAll(notesDir string) error {
	return listAllFiltered(notesDir, "")
}

// ---------------------------------------------------------------------------
// top-level usage / version
// ---------------------------------------------------------------------------

const registerUsage = `Usage: register <subcommand> [args...]

Register — bookmark index (identity & membership):
  add       <file>... [--binder <name>] [--aka <key>] [--kind <k>] [--md] [--xattr itemprojects|tags|none]
            (no --binder registers a bookmark: binderless ref, id + optional aka)
  add       --url <URL> [--binder <name>] [--label <name>] [--aka <key>]  (URL ref, e.g. x-devonthink-item://…)
  remove    <file>...|<url> --binder <name>
  list      [<binder>] [--inbox|--curated]  (all binders with counts, or files in one)
            [<binder> --paths [--print0]]   (absolute paths only, one per line, or NUL-separated)
            [<binder> --json]               (JSONL per member: id, aka, filename, kind, path)
  resolve   <key>                           (id or aka handle → file path; --record for details)
  of        <file>                          (reverse lookup: file → id, aka, collection(s))
  aka       <id|aka> [--add <handle>]... [--remove <handle>]...  (edit a record's aka handles)
  rename    <old-name> <new-name> [--merge]
  refresh   [--dry-run]
  audit     [--binder <name>]
  repair    [--interactive]
  reindex   [--dry-run]                    (rebuild the index from Markdown ref blocks)
  marshal   --binder <name> [--out <file>.tar.gz] [--all-notes]  (pack a binder into a portable container)
  unmarshal <container>.tar.gz [--force] [--scatter]  (unpack a container, mirror files, recreate records;
            --scatter allows mirroring to origins outside collections/)

Annotation — Markdown info layer:
  promote   --binder <name> [--target <file>.md] [--id <id>]
  annotate  <binder> <id|aka> [--set k=v]... [--unset k]... [--prose <text>|-]
            (edit an existing block: custom fields + prose; id/type/binder/aka/sort refused)
  write                         (read JSONL from stdin, write records)
  cleanup   [--interactive]
  album     <binder> [--out DIR] [--milan [DIR]] [--open]   (static HTML album from the binder)

Ordering — presentation layer (binder = set, ordering = sort: keys; ORDERING.md):
  order set     <binder> [--rule name]
  order show    <binder> [--json]
  order move    <binder> <id|aka> --after <id|aka> | --to <n>`

// unknownOption reports a mistyped/unrecognized flag and returns exit code 1, so
// a typo (e.g. --hind for --kind) fails loudly instead of being silently ignored.
func unknownOption(cmd, opt string) int {
	if flag, ok := strings.CutSuffix(opt, missingValue); ok {
		fmt.Fprintf(os.Stderr, "register %s: option '%s' needs a value\n", cmd, flag)
		return 1
	}
	fmt.Fprintf(os.Stderr, "register %s: unknown option '%s'\n", cmd, opt)
	return 1
}

// topLevel handles the cases past command dispatch: no args / help / version /
// unknown subcommand.
func topLevel(args []string) {
	switch {
	case len(args) == 0 || args[0] == "-h" || args[0] == "--help":
		fmt.Println(registerUsage)
	case args[0] == "--version" || args[0] == "-v":
		fmt.Println("register " + registerVersion)
	default:
		fmt.Fprintf(os.Stderr, "register: unknown subcommand '%s'\n", args[0])
		fmt.Fprintln(os.Stderr, "Run 'register --help' for usage.")
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

// commands maps each subcommand to its handler. Every handler takes the args
// after the subcommand and returns the process exit code.
var commands = map[string]func([]string) int{
	"add":       cmdAdd,
	"aka":       cmdAka,
	"album":     cmdAlbum,
	"annotate":  cmdAnnotate,
	"audit":     cmdAudit,
	"cleanup":   cmdCleanup,
	"list":      cmdList,
	"marshal":   cmdMarshal,
	"of":        cmdOf,
	"order":     cmdOrder,
	"promote":   cmdPromote,
	"refresh":   cmdRefresh,
	"reindex":   cmdReindex,
	"remove":    cmdRemove,
	"rename":    cmdRename,
	"repair":    cmdRepair,
	"resolve":   cmdResolve,
	"unmarshal": cmdUnmarshal,
	"write":     cmdWrite,
}

func main() {
	args := os.Args[1:]
	if len(args) > 0 {
		if run, ok := commands[args[0]]; ok {
			os.Exit(run(args[1:]))
		}
	}
	// No subcommand matched: usage, version, or an unknown name.
	topLevel(args)
}
