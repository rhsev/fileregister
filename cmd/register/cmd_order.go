package main

// cmd_order — arrange a binder's members (ORDERING.md). The order is the
// document's: members follow their blocks in the binder's note, unless a
// `sort:` key places them. Membership stays global (the index).

import (
	"github.com/rhsev/fileregister/internal/index"

	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const orderUsage = `Usage: register order <show|move> <binder> [options]

  show    <binder> [--json]
          Print the members in order: sort: keys first, then the order their
          blocks stand in the note, then members without a block by file name.
  move    <binder> <id|aka> --after <id|aka> | --to <n>
          Move one member (1-based position). Writes a sort: key; when the
          neighbours carry no keys yet, keys are materialized for all members.

  All subcommands accept --note <file>.md to address an ordering other than
  the binder's canonical note (collections/binder_<name>.md).`

type orderCtx struct {
	notesDir, note  string
	records         []map[string]any
	overrides       map[string]map[string]any
	config          map[string]any
	inNote          int // members with a block in the note
	base, displayed []string
}

func buildOrderContext(binder, noteOpt string) (*orderCtx, bool) {
	nd, err := notesDir()
	if err != nil {
		return nil, false
	}
	note := defaultPromoteTarget(nd, binder)
	if noteOpt != "" {
		note = index.ExpandPath(noteOpt)
	}
	if !strings.HasSuffix(strings.ToLower(note), ".md") {
		fmt.Fprintln(os.Stderr, "Error: the ordering note must be a .md file")
		return nil, false
	}

	allRefs, refsOK := loadRefs(nd)
	if !refsOK {
		return nil, false
	}
	records := recordsForBinder(allRefs, binder)
	if len(records) == 0 {
		fmt.Fprintf(os.Stderr, "No records for binder '%s'.\n", binder)
		return nil, false
	}

	// Overrides are keyed by canonical member id. A hand-written block may
	// carry the record's aka in its id: slot — resolve it, so the block that
	// orderWriteOverride is willing to update is also the one consulted here.
	identToID := map[string]string{}
	for _, r := range records {
		rid := index.AsString(r["id"])
		identToID[rid] = rid
		for _, a := range index.AkaList(r) {
			if a != "" {
				identToID[a] = rid
			}
		}
	}
	// One read of the note serves both the ref overrides and the ordering
	// config block. The note is Markdown, so grubber reads it.
	blocks, gerr := grubberNoteBlocks(note)
	if gerr != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", gerr)
		return nil, false
	}
	overrides := map[string]map[string]any{}
	var config map[string]any
	isMember := map[string]bool{}
	for _, r := range records {
		isMember[index.AsString(r["id"])] = true
	}
	// Blocks come in document order; that order is the binder's, for every
	// member a sort: key does not place.
	var docOrder []string
	for _, r := range blocks {
		if t, _ := r["type"].(string); t == "ordering" {
			if config == nil {
				config = r
			}
			continue
		} else if t != "ref" {
			continue
		}
		if !index.SameBinder(index.AsString(r["binder"]), binder) {
			continue
		}
		id := identToID[index.AsString(r["id"])]
		if id == "" {
			for _, a := range index.AsStrings(r["aka"]) {
				if identToID[a] != "" {
					id = identToID[a]
					break
				}
			}
		}
		if id == "" {
			id = index.AsString(r["id"])
		}
		if id != "" {
			overrides[id] = r
			if isMember[id] {
				docOrder = append(docOrder, id)
			}
		}
	}
	// An ordering block from before the order became the document's: its
	// binder: still says which binder the note orders; a rule: line is inert.
	if config != nil && index.AsString(config["binder"]) != "" && !index.SameBinder(index.AsString(config["binder"]), binder) {
		fmt.Fprintf(os.Stderr, "Error: %s orders binder '%s', not '%s'\n", filepath.Base(note), index.AsString(config["binder"]), binder)
		return nil, false
	}

	base := baseOrder(records, docOrder)
	inNote := map[string]bool{}
	for _, id := range docOrder {
		inNote[id] = true
	}
	return &orderCtx{
		notesDir: nd, note: note, records: records, overrides: overrides,
		config: config, inNote: len(inNote), base: base,
		displayed: absoluteOrder(base, overrides),
	}, true
}

func recordsByID(records []map[string]any) map[string]map[string]any {
	m := map[string]map[string]any{}
	for _, r := range records {
		m[index.AsString(r["id"])] = r
	}
	return m
}

func orderMember(ctx *orderCtx, key string) (map[string]any, bool) {
	// Resolving against the binder's records is enough — a hit must be a
	// member anyway, and it spares a full index re-read per key.
	if rec := index.ResolveKey(ctx.records, key); rec != nil {
		return rec, true
	}
	fmt.Fprintf(os.Stderr, "Error: '%s' is not a member of the binder (id or aka)\n", key)
	return nil, false
}

func cmdOrder(args []string) int {
	if len(args) == 0 {
		fmt.Println(orderUsage)
		return 0
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "set":
		// There is nothing to set any more: the order is the document's.
		fmt.Fprintln(os.Stderr, "register order: 'set' is gone — the order is the order of the blocks in the note, "+
			"unless sort: keys place a member (register order move); there is no rule to set")
		return 1
	case "show":
		return orderShow(rest)
	case "move":
		return orderMove(rest)
	case "-h", "--help":
		fmt.Println(orderUsage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "register order: unknown subcommand '%s'\n", sub)
		fmt.Fprintln(os.Stderr, "Run 'register order --help' for usage.")
		return 1
	}
}

func orderShow(args []string) int {
	vals, bools, pos, unk := parseFlags(args, map[string]bool{"--note": true}, map[string]bool{"--json": true})
	if unk != "" {
		return unknownOption("order", unk)
	}
	if bools["--help"] {
		fmt.Println(orderUsage)
		return 0
	}
	if bools["--version"] {
		fmt.Println("register order " + registerVersion)
		return 0
	}
	if len(pos) == 0 {
		fmt.Fprintln(os.Stderr, "Error: <binder> is required")
		return 1
	}
	binder := pos[0]
	ctx, ok := buildOrderContext(binder, vals["--note"])
	if !ok {
		return 1
	}
	byID := recordsByID(ctx.records)

	if bools["--json"] {
		for i, id := range ctx.displayed {
			fn := ""
			if r := byID[id]; r != nil {
				fn = index.AsString(r["filename"])
			}
			fmt.Println(`{"position":` + strconv.Itoa(i+1) + `,"id":` + index.JSONVal(id) + `,"filename":` + index.JSONVal(fn) + `}`)
		}
		return 0
	}

	origin := ""
	if ctx.inNote == 0 && len(ctx.overrides) == 0 {
		origin = " (no blocks in the note yet — by file name)"
	}
	fmt.Printf("%s — %d member(s)%s\n", binder, len(ctx.displayed), origin)
	for i, id := range ctx.displayed {
		rec := byID[id]
		if rec == nil {
			rec = map[string]any{"id": id}
		}
		fmt.Printf("%3d. %s\n", i+1, refLabel(rec))
	}
	return 0
}

func orderMove(args []string) int {
	vals, bools, pos, unk := parseFlags(args, map[string]bool{"--note": true, "--after": true, "--to": true}, nil)
	if unk != "" {
		return unknownOption("order", unk)
	}
	if bools["--help"] {
		fmt.Println(orderUsage)
		return 0
	}
	if bools["--version"] {
		fmt.Println("register order " + registerVersion)
		return 0
	}
	if len(pos) == 0 {
		fmt.Fprintln(os.Stderr, "Error: <binder> is required")
		return 1
	}
	binder := pos[0]
	if len(pos) < 2 {
		fmt.Fprintln(os.Stderr, "Error: <id|aka> is required")
		return 1
	}
	key := pos[1]
	_, hasAfter := vals["--after"]
	toStr, hasTo := vals["--to"]
	count := 0
	if hasAfter {
		count++
	}
	if hasTo {
		count++
	}
	if count != 1 {
		fmt.Fprintln(os.Stderr, "Error: exactly one of --after or --to is required")
		return 1
	}

	ctx, ok := buildOrderContext(binder, vals["--note"])
	if !ok {
		return 1
	}
	rec, ok := orderMember(ctx, key)
	if !ok {
		return 1
	}
	id := index.AsString(rec["id"])
	var without []string
	for _, x := range ctx.displayed {
		if x != id {
			without = append(without, x)
		}
	}

	var target int
	if hasAfter {
		predRec, ok := orderMember(ctx, vals["--after"])
		if !ok {
			return 1
		}
		pred := index.AsString(predRec["id"])
		if pred == id {
			fmt.Fprintln(os.Stderr, "Error: cannot place a member after itself")
			return 1
		}
		target = indexOfStr(without, pred) + 1
	} else {
		to, aerr := strconv.Atoi(strings.TrimSpace(toStr))
		if aerr != nil {
			fmt.Fprintf(os.Stderr, "Error: --to expects a 1-based position, got '%s'\n", toStr)
			return 1
		}
		if to < 1 {
			to = 1
		}
		if to > len(without)+1 {
			to = len(without) + 1
		}
		target = to - 1
	}

	// Single-key placement works when both bounding neighbours carry keys (an
	// open upper bound after the LAST keyed member is fine too — the new key
	// lands at the end of the keyed block, which is exactly position target).
	// An unkeyed lower neighbour, or a degenerate hand-edited key pair, cannot
	// be expressed with one key — materialize keys for the whole desired order.
	lower := ""
	if target > 0 {
		lower = sortKeyOf(ctx.overrides, without[target-1])
	}
	upper := ""
	if target < len(without) {
		upper = sortKeyOf(ctx.overrides, without[target])
	}
	single := target == 0 || lower != ""
	k := ""
	if single {
		k = sortKeyBetween(lower, upper)
		if !keyInRange(k, lower, upper) {
			single = false
		}
	}

	byID := recordsByID(ctx.records)
	if single {
		if orderWriteOverride(ctx.note, binder, rec, "sort", k) == "failed" {
			return 1
		}
	} else {
		desired := insertStr(append([]string{}, without...), target, id)
		keys := materializeKeys(desired)
		if !orderWriteOverrides(ctx.note, binder, byID, "sort", desired, keys) {
			return 1
		}
	}

	fresh, ok := buildOrderContext(binder, vals["--note"])
	if !ok {
		return 1
	}
	pos2 := indexOfStr(fresh.displayed, id)
	posStr := "?"
	if pos2 >= 0 {
		posStr = strconv.Itoa(pos2 + 1)
	}
	fmt.Printf("Moved %s → position %s of %d\n", refLabel(rec), posStr, len(fresh.displayed))
	return 0
}

// orderWriteOverride sets or removes one override field on the member's block,
// creating a lean block when the member has none. Returns
// updated/noop/created/failed.
func orderWriteOverride(note, binder string, rec map[string]any, field string, value any) string {
	id := index.AsString(rec["id"])
	var aka []string
	for _, a := range index.AsStrings(rec["aka"]) {
		if a != "" {
			aka = append(aka, a)
		}
	}
	found := false
	if index.FileExists(note) {
		_, terr := mdTransformFile(note, func(lines []string, parsed map[string]any) (string, bool) {
			if !index.SameBinder(index.AsString(parsed["binder"]), binder) {
				return "", false
			}
			pid := index.AsString(parsed["id"])
			match := pid == id || containsStr(aka, pid)
			if !match {
				for _, pa := range index.AsStrings(parsed["aka"]) {
					if pa == id || containsStr(aka, pa) {
						match = true
						break
					}
				}
			}
			if !match {
				return "", false
			}
			found = true
			var newLines []string
			ind := blockIndent(lines)
			for _, l := range lines {
				if !keyLine(l, field, ind) {
					newLines = append(newLines, l)
				}
			}
			if value != nil {
				newLines = append(newLines, ind+yamlLine(field, index.AsString(value)))
			}
			return "```yaml\n" + strings.Join(newLines, "\n") + "\n```", true
		})
		if terr != nil {
			fmt.Fprintf(os.Stderr, "Error: writing %s failed: %v\n", note, terr)
			return "failed"
		}
	}
	if found {
		return "updated"
	}
	if value == nil {
		return "noop"
	}

	fn := index.AsString(rec["filename"])
	header := fn
	if header == "" {
		header = id
	}
	yaml := yamlField("type", "ref") + yamlField("id", id) + yamlField("binder", binder) + yamlField(field, index.AsString(value))
	block := "### " + headingText(header) + "\n```yaml\n" + yaml + "```\n"
	content := block
	existingNote, crlf, err := readNote(note)
	if err == nil {
		content = chomp(existingNote) + "\n\n" + block
	} else if os.IsNotExist(err) {
		os.MkdirAll(filepath.Dir(note), 0755)
	} else {
		// Existing note, unreadable — writing now would clobber it.
		fmt.Fprintf(os.Stderr, "Error: reading %s failed: %v\n", note, err)
		return "failed"
	}
	if err := writeNote(note, content, crlf); err != nil {
		fmt.Fprintf(os.Stderr, "Error: writing %s failed: %v\n", note, err)
		return "failed"
	}
	return "created"
}

// orderWriteOverrides sets one override field on MANY member blocks in a single
// note pass — one read + one rewrite for the transformed blocks, one appended
// write for members without a block — instead of a full read/rewrite cycle per
// member (O(N²) on a materialize). keys maps id → value; order fixes the append
// order. Returns false on any failure.
func orderWriteOverrides(note, binder string, byID map[string]map[string]any, field string, order []string, keys map[string]string) bool {
	identToID := map[string]string{}
	for _, mid := range order {
		identToID[mid] = mid
		if rec := byID[mid]; rec != nil {
			for _, a := range index.AsStrings(rec["aka"]) {
				if a != "" {
					identToID[a] = mid
				}
			}
		}
	}

	written := map[string]bool{}
	if index.FileExists(note) {
		_, terr := mdTransformFile(note, func(lines []string, parsed map[string]any) (string, bool) {
			if !index.SameBinder(index.AsString(parsed["binder"]), binder) {
				return "", false
			}
			mid := identToID[index.AsString(parsed["id"])]
			if mid == "" {
				for _, pa := range index.AsStrings(parsed["aka"]) {
					if identToID[pa] != "" {
						mid = identToID[pa]
						break
					}
				}
			}
			if mid == "" {
				return "", false
			}
			written[mid] = true
			var newLines []string
			ind := blockIndent(lines)
			for _, l := range lines {
				if !keyLine(l, field, ind) {
					newLines = append(newLines, l)
				}
			}
			newLines = append(newLines, ind+yamlLine(field, keys[mid]))
			return "```yaml\n" + strings.Join(newLines, "\n") + "\n```", true
		})
		if terr != nil {
			fmt.Fprintf(os.Stderr, "Error: writing %s failed: %v\n", note, terr)
			return false
		}
	}

	var blocks []string
	for _, mid := range order {
		if written[mid] {
			continue
		}
		rec := byID[mid]
		if rec == nil {
			rec = map[string]any{"id": mid}
		}
		header := index.AsString(rec["filename"])
		if header == "" {
			header = mid
		}
		y := yamlField("type", "ref") + yamlField("id", mid) + yamlField("binder", binder) + yamlField(field, keys[mid])
		blocks = append(blocks, "### "+headingText(header)+"\n```yaml\n"+y+"```\n")
	}
	if len(blocks) == 0 {
		return true
	}
	body := strings.Join(blocks, "\n")
	content := body
	existingNote, crlf, err := readNote(note)
	if err == nil {
		content = chomp(existingNote) + "\n\n" + body
	} else if os.IsNotExist(err) {
		os.MkdirAll(filepath.Dir(note), 0755)
	} else {
		fmt.Fprintf(os.Stderr, "Error: reading %s failed: %v\n", note, err)
		return false
	}
	if err := writeNote(note, content, crlf); err != nil {
		fmt.Fprintf(os.Stderr, "Error: writing %s failed: %v\n", note, err)
		return false
	}
	return true
}
