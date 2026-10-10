package main

import (
	"github.com/rhsev/fileregister/internal/index"

	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func runGoAudit(t *testing.T, env []string, args ...string) (string, string, int) {
	t.Helper()
	restore := setEnv(t, env)
	defer restore()
	index.ResetEngine()

	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	code := cmdAudit(args)

	index.ResetEngine()
	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr

	var ob, eb bytes.Buffer
	io.Copy(&ob, outR)
	io.Copy(&eb, errR)
	return ob.String(), eb.String(), code
}

func auditEnv(t *testing.T, notes, home, anchor string) []string {
	t.Helper()
	env := filterEnv(os.Environ(), "GRUBBER_SET")
	for _, k := range []string{"GRUBBER_NOTES", "HOME", "FILEANCHOR", "REGISTER_BINDER"} {
		env = filterEnv(env, k)
	}
	return append(env, "GRUBBER_NOTES="+notes, "HOME="+home, "FILEANCHOR="+anchor)
}

// setupAuditConsistent creates a bookmarked file record in two binders, both
// stamped, plus a URL record, so audit reports everything consistent (no paths
// in the output) and counts two records, not three memberships.
func setupAuditConsistent(t *testing.T, anchor string) (notes, home string) {
	home = t.TempDir()
	notes = t.TempDir()
	if err := os.MkdirAll(filepath.Join(notes, "collections"), 0755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "doc.txt")
	if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(f)

	blob := engineSave(t, anchor, real)
	share := filepath.Join(home, ".local", "share")
	os.MkdirAll(share, 0755)
	dbJSON, _ := json.Marshal(map[string]string{"810000001": blob})
	writeFile(t, filepath.Join(share, "bookmarks.json"), string(dbJSON))

	engineExec(t, anchor,
		`{"op":"set_meta","path":"`+real+`","key":"id","value":"810000001","mode":"add"}`,
		`{"op":"set_meta","path":"`+real+`","key":"groups","value":"AudBndOne","mode":"add"}`,
		`{"op":"set_meta","path":"`+real+`","key":"groups","value":"AudBndThree","mode":"add"}`)

	writeFile(t, filepath.Join(notes, "collections", "inbox.jsonl"),
		`{"type":"ref","id":"810000001","binder":["AudBndOne","AudBndThree"],"filename":"doc.txt"}`+"\n"+
			`{"type":"ref","id":"810000002","binder":["AudBndTwo"],"url":"x-devonthink-item://AUD","filename":"u.md"}`+"\n")
	return notes, home
}

func TestAudit(t *testing.T) {
	anchor := engineBin(t)

	// consistent: bookmarked + group-stamped file + URL record → all consistent (path-free).
	cN, cH := setupAuditConsistent(t, anchor)
	cOut, cErr, cCode := runGoAudit(t, auditEnv(t, cN, cH, anchor))
	assertGolden(t, "audit_consistent", cliResult(cOut, cErr, cCode))

	// broken: unresolvable bookmark → BROKEN BOOKMARK (normalize the notes dir).
	bN := t.TempDir()
	os.MkdirAll(filepath.Join(bN, "collections"), 0755)
	writeFile(t, filepath.Join(bN, "collections", "inbox.jsonl"),
		`{"type":"ref","id":"999999999","binder":["AudBroken"],"filename":"z.txt"}`+"\n")
	bOut, bErr, bCode := runGoAudit(t, auditEnv(t, bN, t.TempDir(), anchor))
	repl := map[string]string{bN: "<NOTES>"}
	assertGolden(t, "audit_broken", cliResult(norm(bOut, repl), norm(bErr, repl), bCode))

	// empty index.
	eN := t.TempDir()
	os.MkdirAll(filepath.Join(eN, "collections"), 0755)
	eOut, eErr, eCode := runGoAudit(t, auditEnv(t, eN, t.TempDir(), anchor))
	assertGolden(t, "audit_empty", cliResult(eOut, eErr, eCode))
}
