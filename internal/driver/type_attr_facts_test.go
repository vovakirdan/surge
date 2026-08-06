package driver

import (
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/project"
	"surge/internal/sema"
	"surge/internal/types"
)

// factRecord builds one module record whose files carry the given attribute
// facts, so the pre-pass can be driven without compiling a program.
func factRecord(path string, files ...map[types.TypeID]sema.TypeAttrFacts) *moduleRecord {
	rec := &moduleRecord{
		Meta: &project.ModuleMeta{Path: path},
		Sema: make(map[ast.FileID]*sema.Result, len(files)),
	}
	for i, facts := range files {
		fileID := ast.FileID(i + 1)
		rec.FileIDs = append(rec.FileIDs, fileID)
		rec.Sema[fileID] = &sema.Result{TypeAttrFacts: facts}
	}
	return rec
}

// TestTypeAttrFactsMergeReachesEveryRecord pins what the pre-pass exists for:
// a fact declared in any module is part of the whole-program table before
// anything reads it. Copy needs no such pass because the interner carries it.
func TestTypeAttrFactsMergeReachesEveryRecord(t *testing.T) {
	res := &DiagnoseResult{
		Sema:       &sema.Result{},
		rootRecord: factRecord("app", map[types.TypeID]sema.TypeAttrFacts{1: {Send: true}}),
		moduleRecords: map[string]*moduleRecord{
			"lib/one": factRecord("lib/one",
				map[types.TypeID]sema.TypeAttrFacts{2: {ShardMovable: true}},
				map[types.TypeID]sema.TypeAttrFacts{3: {NoSend: true}},
			),
			"lib/two": factRecord("lib/two", map[types.TypeID]sema.TypeAttrFacts{4: {ShardPinned: true}}),
		},
	}
	if err := mergeTypeAttrFactsFromRecords(res); err != nil {
		t.Fatalf("unexpected refusal: %v", err)
	}
	want := map[types.TypeID]sema.TypeAttrFacts{
		1: {Send: true},
		2: {ShardMovable: true},
		3: {NoSend: true},
		4: {ShardPinned: true},
	}
	for id, wantFacts := range want {
		if got := res.Sema.TypeAttrFacts[id]; got != wantFacts {
			t.Fatalf("type %d: got %+v, want %+v", uint32(id), got, wantFacts)
		}
	}
	if len(res.Sema.TypeAttrFacts) != len(want) {
		t.Fatalf("merged table holds %d rows, want %d: %v", len(res.Sema.TypeAttrFacts), len(want), res.Sema.TypeAttrFacts)
	}
}

// TestTypeAttrFactsMergeRefusesACrossModuleContradiction constructs the shape
// no single file can diagnose: two modules that agree the type exists and
// disagree about whether it may move between shards.
func TestTypeAttrFactsMergeRefusesACrossModuleContradiction(t *testing.T) {
	refuse := func() string {
		t.Helper()
		res := &DiagnoseResult{
			Sema:       &sema.Result{},
			rootRecord: factRecord("app", map[types.TypeID]sema.TypeAttrFacts{9: {ShardMovable: true}}),
			moduleRecords: map[string]*moduleRecord{
				"lib/pin":  factRecord("lib/pin", map[types.TypeID]sema.TypeAttrFacts{9: {ShardPinned: true}}),
				"lib/move": factRecord("lib/move", map[types.TypeID]sema.TypeAttrFacts{9: {ShardMovable: true}}),
			},
		}
		err := mergeTypeAttrFactsFromRecords(res)
		if err == nil {
			t.Fatalf("expected the merge to refuse a type that is both movable and pinned")
		}
		return err.Error()
	}

	msg := refuse()
	const want = "type 9 is @shard_movable in app, lib/move and @shard_pinned in lib/pin"
	if !strings.Contains(msg, want) {
		t.Fatalf("refusal %q does not name every contributing module as %q", msg, want)
	}
	if again := refuse(); again != msg {
		t.Fatalf("refusal is not deterministic:\nfirst  %q\nsecond %q", msg, again)
	}
}

// TestTypeAttrFactsMergeKeepsTheResultsOwnFacts guards the case where the
// result being merged into is not itself reachable from any record: its own
// facts still belong to the merged table, and still have an owner to name.
func TestTypeAttrFactsMergeKeepsTheResultsOwnFacts(t *testing.T) {
	res := &DiagnoseResult{
		Sema:       &sema.Result{TypeAttrFacts: map[types.TypeID]sema.TypeAttrFacts{5: {ShardMovable: true}}},
		rootRecord: factRecord("app"),
		moduleRecords: map[string]*moduleRecord{
			"lib/pin": factRecord("lib/pin", map[types.TypeID]sema.TypeAttrFacts{5: {ShardPinned: true}}),
		},
	}
	err := mergeTypeAttrFactsFromRecords(res)
	if err == nil {
		t.Fatalf("expected a refusal naming the result's own module")
	}
	if !strings.Contains(err.Error(), "@shard_movable in app") {
		t.Fatalf("refusal %q does not attribute the result's own fact to its module", err)
	}
}
