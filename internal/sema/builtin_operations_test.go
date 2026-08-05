package sema

import (
	"testing"

	"surge/internal/symbols"
	"surge/internal/types"
)

func builtinCloneRecord(symbol symbols.SymbolID, modulePath string) CallableCandidate {
	candidate := CallableCandidate{
		Symbol:       symbol,
		Name:         "__clone",
		ReceiverKey:  "string",
		ReceiverType: 4,
		Params:       []symbols.TypeKey{"&string"},
		ParamTypes:   []types.TypeID{5},
		Result:       "string",
		ResultType:   4,
		Attrs:        []string{"intrinsic"},
		HasSelf:      true,
		Builtin:      true,
		Intrinsic:    true,
		ModulePath:   modulePath,
		SourceKey:    "builtin",
	}
	candidate.BodyKey = canonicalCallableBodyKey(&candidate)
	return candidate
}

func TestMergeKeepsOneRecordPerBuiltinOperation(t *testing.T) {
	dst := &Result{CallableCandidates: []CallableCandidate{builtinCloneRecord(11, "core/intrinsics")}}
	src := &Result{CallableCandidates: []CallableCandidate{builtinCloneRecord(12, "corpus/core_stdlib/intrinsics")}}

	MergeInstantiationGraphs(dst, src, nil)

	if len(dst.CallableCandidates) != 1 {
		t.Fatalf("merged catalog = %d records, want 1: %+v", len(dst.CallableCandidates), dst.CallableCandidates)
	}
	if got := dst.CallableCandidates[0].ModulePath; got != "core/intrinsics" {
		t.Fatalf("surviving record module = %q, want the body-key-first ingestion", got)
	}
}

func TestMergeKeepsRivalOperationsOnDifferentReceivers(t *testing.T) {
	dst := &Result{CallableCandidates: []CallableCandidate{builtinCloneRecord(11, "core/intrinsics")}}
	other := builtinCloneRecord(12, "corpus/core_stdlib/intrinsics")
	// A nominal type ingested twice has two type identities, so these records
	// dispatch on different receivers and are not one operation.
	other.ReceiverType = 1542
	other.BodyKey = canonicalCallableBodyKey(&other)
	src := &Result{CallableCandidates: []CallableCandidate{other}}

	MergeInstantiationGraphs(dst, src, nil)

	if len(dst.CallableCandidates) != 2 {
		t.Fatalf("merged catalog = %d records, want both receivers: %+v", len(dst.CallableCandidates), dst.CallableCandidates)
	}
}

func TestMergeKeepsRivalUserDeclarations(t *testing.T) {
	left := builtinCloneRecord(11, "left")
	left.Builtin = false
	left.Intrinsic = false
	left.Attrs = nil
	left.HasBody = true
	left.BodyKey = canonicalCallableBodyKey(&left)
	right := left
	right.Symbol = 12
	right.ModulePath = "right"
	right.BodyKey = canonicalCallableBodyKey(&right)

	dst := &Result{CallableCandidates: []CallableCandidate{left}}
	src := &Result{CallableCandidates: []CallableCandidate{right}}

	MergeInstantiationGraphs(dst, src, nil)

	if len(dst.CallableCandidates) != 2 {
		t.Fatalf("merged catalog = %d records, want both user bodies: %+v", len(dst.CallableCandidates), dst.CallableCandidates)
	}
}
