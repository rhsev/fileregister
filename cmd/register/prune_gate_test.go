package main

import (
	"github.com/rhsev/fileregister/internal/index"

	"testing"
)

// The refusal is the safeguard: an entry that could not be judged must stop the
// run, because "dead" would otherwise delete a healthy bookmark on a volume
// that merely happens to be away or unindexed.
func TestPruneRefusesWhileAnythingIsUnjudged(t *testing.T) {
	unreachable := []bookmarkFinding{{bmUnreachable, "2", "x.pdf", "/Volumes/away/x.pdf", "volume away"}}
	prunable := []bookmarkFinding{{bmDead, "1", "y.pdf", "/tmp/y.pdf", "gone"}}

	if rc := cleanupPrune(unreachable, prunable); rc != 1 {
		t.Errorf("exit = %d, want 1 (refused)", rc)
	}
	// The refusal must come before any store is touched.
	if db, err := index.LoadDB(); err == nil && len(db) != 0 {
		t.Errorf("store has %d entries after a refusal, want it untouched", len(db))
	}
}

func TestPruneDropsOnlyTheUnrepairable(t *testing.T) {
	if err := index.SaveDB(map[string]string{"1": "a", "2": "b", "keepme": "c"}); err != nil {
		t.Fatal(err)
	}
	prunable := []bookmarkFinding{
		{bmDead, "1", "y.pdf", "/tmp/y.pdf", "gone"},
		{bmMalformed, "keepme", "", "", "not an id"},
	}

	if rc := cleanupPrune(nil, prunable); rc != 0 {
		t.Fatalf("exit = %d, want 0", rc)
	}
	db, err := index.LoadDB()
	if err != nil {
		t.Fatal(err)
	}
	if _, still := db["1"]; still {
		t.Error("dead entry survived")
	}
	if _, still := db["keepme"]; still {
		t.Error("malformed entry survived")
	}
	if _, gone := db["2"]; !gone {
		t.Error("an entry nobody judged was removed")
	}
}

// The store is register's alone, so an orphan is offered with the dead and the
// malformed. A broken entry belongs to repair, an unreachable one was never
// judged: neither is offered.
func TestOrphanIsOfferedForPruning(t *testing.T) {
	for kind, want := range map[string]bool{
		bmOrphan: true, bmDead: true, bmMalformed: true,
		bmBroken: false, bmUnreachable: false,
	} {
		if got := (bookmarkFinding{kind: kind}).prunable(); got != want {
			t.Errorf("%s: prunable = %v, want %v", kind, got, want)
		}
	}
}
