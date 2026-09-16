package driver

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"surge/internal/diag"
	"surge/internal/sema"
	"surge/internal/source"
)

const implicitConversionReason = "implicit conversion needs its selected __to origin contract"

// A user conversion whose result is a view of its own receiver's storage.
const implicitConversionPrefix = `type Src = { items: uint64[4] };
extern<Src> {
    fn __to(self: &Src, _: uint64[]) -> uint64[] {
        return self.items[[0..4]];
    }
}
`

const implicitConversionLocal = "    let src: Src = Src { items = [1:uint64, 2:uint64, 3:uint64, 4:uint64] };\n"

type implicitConversionOperand struct {
	start, end uint32
	text       string
	kind       sema.ImplicitConversionKind
}

type implicitConversionRole uint8

const (
	// The converted value must leave the analysis as an unproved obligation.
	implicitConversionPends implicitConversionRole = iota
	// A certified conversion drops its operand's origins without an obligation.
	implicitConversionCertified
	// A tag injection keeps its payload's origins.
	implicitConversionKeeps
)

type implicitConversionLeaf struct {
	name, digest, text string
	operands           []implicitConversionOperand
	role               implicitConversionRole
	cleanRoot          bool
	// eagerEscape: the leaf's program is a leak, and the eager checker now refuses it too.
	eagerEscape bool
	summary     string
	slots       []uint32
}

func implicitConversionLeaves() []implicitConversionLeaf {
	to := func(start, end uint32, text string) implicitConversionOperand {
		return implicitConversionOperand{start, end, text, sema.ImplicitConversionTo}
	}
	return []implicitConversionLeaf{
		// Holes: each form returns a view of src. The eager checker admitted them until the
		// fixed-array view rule landed; the two that return the value are refused there now.
		{name: "return_converted_view", digest: "b332b5728d0adc172897d842ab488c34a66ff566f833d84cf54d2a1e0ede6f71",
			text:        implicitConversionPrefix + "fn leak() -> uint64[] {\n" + implicitConversionLocal + "    return src;\n}\n",
			operands:    []implicitConversionOperand{to(253, 256, "src")},
			eagerEscape: true},
		{name: "let_converted_view", digest: "aeb33b940523f0f9a8127c701e3b731652f1ec21de2f401141ea3da93284b4c1",
			text:        implicitConversionPrefix + "fn leak() -> uint64[] {\n" + implicitConversionLocal + "    let v: uint64[] = src;\n    return v;\n}\n",
			operands:    []implicitConversionOperand{to(264, 267, "src")},
			eagerEscape: true},
		{name: "struct_child_converted_view", digest: "c45a69c7aff4bf6a1eac37c31d1d1f272ff3456baabbc4c4558ff17aca05841a",
			text: "type Holder = { view: uint64[] };\n" + implicitConversionPrefix + "fn leak() -> Holder {\n" + implicitConversionLocal +
				"    return Holder { view = src };\n}\n",
			operands: []implicitConversionOperand{to(301, 304, "src")}},
		{name: "array_element_converted_view", digest: "61826ac55400726bb5ae49e8d2992bf104a8586cd9325fea64cc2d67579661cc",
			text: implicitConversionPrefix + "fn leak() -> uint64[][] {\n" + implicitConversionLocal + "    let out: uint64[][] = [src];\n    return out;\n}\n" +
				"fn leak_direct() -> uint64[][] {\n" + implicitConversionLocal + "    return [src];\n}\n",
			operands: []implicitConversionOperand{to(271, 274, "src"), to(417, 420, "src")}},
		// Precision pins: the converted value stays local, and the obligation is its cost.
		{name: "let_converted_local", digest: "839c16874fcdeb06d69471d36721f8dc2f0cafe3a75209a93d464a89f8014c73",
			text:     implicitConversionPrefix + "fn keep() -> nothing {\n" + implicitConversionLocal + "    let v: uint64[] = src;\n    return nothing;\n}\n",
			operands: []implicitConversionOperand{to(263, 266, "src")}},
		{name: "assign_converted_local", digest: "baccf715a62b718d8343136e3de450a745be7f1403895c4f466588f39462b876",
			text:     implicitConversionPrefix + "fn keep() -> nothing {\n" + implicitConversionLocal + "    let mut v: uint64[] = [];\n    v = src;\n    return nothing;\n}\n",
			operands: []implicitConversionOperand{to(279, 282, "src")}},
		// Pins for the certified and tag branches.
		{name: "numeric_widening_certified", digest: "0fe93fbfb8088f5f3c1cfcfa60b3a0e0517b563fe607f61b1781e3a5ac2a5e78", role: implicitConversionCertified,
			text:     "fn widen(x: int32) -> int64 {\n    let y: int64 = x;\n    return y;\n}\n",
			operands: []implicitConversionOperand{to(49, 50, "x")}},
		{name: "tag_union_upcast_keeps_payload", digest: "fa29782824a066ca473b6bda43ddb7b07ebbeab9f85c750bd83e5fe13967b7d7", role: implicitConversionKeeps,
			text:     "tag Hold<T>(T);\ntype Held<T> = Hold(T) | nothing;\nfn wrap(a: &string) -> Held<&string> {\n    return Hold::<&string>(a);\n}\n",
			operands: []implicitConversionOperand{{100, 118, "Hold::<&string>(a)", sema.ImplicitConversionTagUnion}}, summary: "wrap", slots: []uint32{0}},
		{name: "reference_source_certified", digest: "cf9bbc8d9a0848f6b38c49d742a5f881325e08fc39b32e496128f0712e076475", role: implicitConversionCertified,
			text:     "fn parse(s: &string) -> int {\n    let n: int = s;\n    return n;\n}\n",
			operands: []implicitConversionOperand{to(47, 48, "s")}, cleanRoot: true},
	}
}

// An implicit conversion's value is the conversion's result: the analysis must
// see what HIR passes on, not the source it converted.
func TestAnalyzeReturnOriginImplicitConversions(t *testing.T) {
	for _, leaf := range implicitConversionLeaves() {
		t.Run(leaf.name, func(t *testing.T) { checkImplicitConversionLeaf(t, leaf) })
	}
}

func checkImplicitConversionLeaf(t *testing.T, leaf implicitConversionLeaf) {
	t.Helper()
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(leaf.text))); got != leaf.digest {
		t.Fatalf("PRECONDITION: frozen source changed: %s", got)
	}
	for _, operand := range leaf.operands {
		if int(operand.end) > len(leaf.text) || leaf.text[operand.start:operand.end] != operand.text {
			t.Fatalf("PRECONDITION: operand span [%d,%d) does not hold %q", operand.start, operand.end, operand.text)
		}
	}
	res := returnOriginStdlibFixture(t, leaf.text, leaf.eagerEscape)
	if err := FinalizeInstantiationClosure(t.Context(), res, 64); err != nil {
		t.Fatalf("PRECONDITION: closure failed: %v", err)
	}
	inputs, err := collectReturnOriginUnits(res)
	if err != nil {
		t.Fatalf("PRECONDITION: owning units: %v", err)
	}
	rootKey, _ := checkReturnOriginCloneUnits(t, res, inputs.units)
	at := func(operand implicitConversionOperand) source.Span {
		return source.Span{File: res.File.ID, Start: operand.start, End: operand.end}
	}
	for _, operand := range leaf.operands {
		matches := 0
		for expr, conversion := range res.Sema.ImplicitConversions {
			if node := res.Builder.Exprs.Get(expr); node != nil && node.Span == at(operand) {
				matches++
				if conversion.Kind != operand.kind {
					t.Fatalf("PRECONDITION: conversion at %v has kind %d, want %d", at(operand), conversion.Kind, operand.kind)
				}
			}
		}
		if matches != 1 {
			t.Fatalf("PRECONDITION: %d conversions recorded at %v, want exactly one", matches, at(operand))
		}
	}
	analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
	logReturnOriginCallEvidence(t, map[string]any{"case": leaf.name, "analysis": analysis,
		"error": errorReturnOriginCallText(err), "typed_diagnostics": res.Bag.Items()})
	if err != nil || analysis == nil {
		t.Fatalf("analysis did not run: %v", err)
	}
	var root []sema.ReturnOriginPending
	for _, pending := range analysis.Pending {
		if pending.SourceKey == rootKey {
			root = append(root, pending)
		}
	}
	for _, operand := range leaf.operands {
		obligation := sema.ReturnOriginPending{SourceKey: rootKey, Span: at(operand), Reason: implicitConversionReason}
		switch leaf.role {
		case implicitConversionPends:
			if !slices.Contains(root, obligation) {
				t.Errorf("converted value at %v left no obligation: root Pending %+v", at(operand), root)
			}
		default:
			for _, pending := range root {
				if pending.Span == at(operand) || pending.Reason == implicitConversionReason || pending.Reason == backingLoanDiscard {
					t.Errorf("proven conversion at %v gained an obligation: %+v", at(operand), pending)
				}
			}
		}
	}
	if leaf.cleanRoot && len(root) != 0 {
		t.Errorf("root unit gained obligations: %+v", root)
	}
	if leaf.summary != "" {
		summary := requireReturnOriginSummary(t, analysis, leaf.summary)
		if summary.Unknown || summary.NoNormalReturn || !slices.Equal(summary.ParamSlots, leaf.slots) {
			t.Errorf("summary %s = %+v, want slots %v", leaf.summary, summary, leaf.slots)
		}
	}
}

// The assignment form never reaches the analysis: the eager checker records the
// conversion before it observes the move, so `return v;` moves a held borrow.
func TestDiagnoseImplicitConversionAssignmentStaysRefused(t *testing.T) {
	text := implicitConversionPrefix + "fn leak() -> uint64[] {\n" + implicitConversionLocal + "    let mut v: uint64[] = [];\n    v = src;\n    return v;\n}\n"
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(text))); got != "f04fbb1a1b868ad83e0fbb56d34d050a73ba6e224bc9da84d33db585a161dd19" {
		t.Fatalf("PRECONDITION: frozen source changed: %s", got)
	}
	if text[296:297] != "v" {
		t.Fatal("PRECONDITION: frozen move span moved")
	}
	stdlib := detectStdlibRootFrom(".")
	if stdlib == "" {
		t.Fatal("PRECONDITION: real stdlib unavailable")
	}
	t.Setenv("SURGE_STDLIB", stdlib)
	path := filepath.Join(t.TempDir(), "origin.sg")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := DiagnoseWithOptions(t.Context(), path, &DiagnoseOptions{Stage: DiagnoseStageAll, MaxDiagnostics: 64,
		IgnoreWarnings: true, KeepArtifacts: true})
	if err != nil || res == nil || res.Bag == nil || res.File == nil {
		t.Fatalf("public diagnosis did not finish: %v", err)
	}
	var errs []*diag.Diagnostic
	for _, d := range res.Bag.Items() {
		if d != nil && d.Severity >= diag.SevError {
			errs = append(errs, d)
		}
	}
	primary := source.Span{File: res.File.ID, Start: 296, End: 297}
	if len(errs) != 1 || errs[0].Code != diag.SemaBorrowMove || errs[0].Primary != primary ||
		errs[0].Message != "cannot move 'src' while it is shared-borrowed" {
		t.Fatalf("errors=%+v, want exactly one SEM3020 at %v", errs, primary)
	}
}
