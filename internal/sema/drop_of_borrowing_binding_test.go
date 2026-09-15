package sema

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/symbols"
)

// A value converted by a __to that borrows its receiver holds that borrow in
// its binding. `@drop` of such a binding used to release the borrow and nothing
// else, so scope exit still owed the binding and freed it a second time: VM3301
// on the VM, and a second run of a struct's drop glue natively. Every source is
// frozen: its sha256 and each byte span below were computed over these bytes.

const convertedDropPrefix = "type Named = { name: string };\n" +
	"extern<Named> {\n" +
	"    fn __to(self: &Named, _: string) -> string {\n" +
	"        return \"converted\";\n" +
	"    }\n" +
	"}\n" +
	"\n"

// In every source built on convertedDropHeader, the conversion that lends h to
// t is at [242,243) 11:9.
const convertedDropStore = "    let h: Named = Named { name = \"n\" };\n" +
	"    let mut t: string = \"\";\n" +
	"    t = h;\n"

const convertedDropReturn = "    return nothing;\n" +
	"}\n"

const convertedDropHeader = convertedDropPrefix + "fn drop_assigned() -> nothing {\n" + convertedDropStore

// snippetTables is a typed snippet together with the tables that name what it
// recorded: the builder owns every span, the symbol table every name.
type snippetTables struct {
	parseBag *diag.Bag
	semaBag  *diag.Bag
	res      *Result
	builder  *ast.Builder
	syms     symbols.Result
}

func runSemaOnSnippetTables(t *testing.T, src string) *snippetTables {
	t.Helper()
	builder, fileID, parseBag := parseSnippet(t, src)
	tables := &snippetTables{parseBag: parseBag, semaBag: diag.NewBag(32), builder: builder}
	if parseBag.Len() != 0 {
		return tables
	}
	tables.syms = symbols.ResolveFile(builder, fileID, &symbols.ResolveOptions{
		Reporter: &diag.BagReporter{Bag: tables.semaBag},
	})
	res := Check(context.Background(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: tables.semaBag},
		Symbols:  &tables.syms,
	})
	tables.res = &res
	return tables
}

// requireFrozenConvertedDrop checks the bytes and the one conversion the defect
// needs: an implicit __to at [start,end) whose receiver is a reference.
func requireFrozenConvertedDrop(t *testing.T, tables *snippetTables, src, digest string, start, end uint32) {
	t.Helper()
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(src))); got != digest {
		t.Fatalf("PRECONDITION: frozen source changed: %s", got)
	}
	if tables.parseBag.HasErrors() || tables.res == nil || tables.syms.Table == nil {
		t.Fatalf("PRECONDITION: snippet did not type: %s", diagnosticsSummary(tables.parseBag))
	}
	if len(tables.res.ImplicitConversions) != 1 {
		t.Fatalf("PRECONDITION: %d implicit conversions recorded, want exactly one", len(tables.res.ImplicitConversions))
	}
	for expr, conversion := range tables.res.ImplicitConversions {
		node := tables.builder.Exprs.Get(expr)
		if node == nil || node.Span.Start != start || node.Span.End != end || conversion.Kind != ImplicitConversionTo {
			t.Fatalf("PRECONDITION: conversion %+v is not a __to at [%d,%d)", conversion, start, end)
		}
		sym := tables.syms.Table.Symbols.Get(tables.res.ToSymbols[expr])
		if sym == nil || sym.Signature == nil || len(sym.Signature.Params) == 0 ||
			!strings.HasPrefix(strings.TrimSpace(string(sym.Signature.Params[0])), "&") {
			t.Fatal("PRECONDITION: the selected __to does not take its receiver by reference")
		}
	}
}

// stmtNames names, sorted, the symbols an obligation map holds for the
// statement that starts at byte start.
func (tables *snippetTables) stmtNames(obligations map[ast.StmtID][]symbols.SymbolID, start uint32) ([]string, bool) {
	for id, held := range obligations {
		stmt := tables.builder.Stmts.Get(id)
		if stmt == nil || stmt.Span.Start != start {
			continue
		}
		names := make([]string, 0, len(held))
		for _, symID := range held {
			name := "?"
			if sym := tables.syms.Table.Symbols.Get(symID); sym != nil {
				name, _ = tables.syms.Table.Strings.Lookup(sym.Name)
			}
			names = append(names, name)
		}
		slices.Sort(names)
		return names, true
	}
	return nil, false
}

func (tables *snippetTables) obligationsSummary(obligations map[ast.StmtID][]symbols.SymbolID) string {
	rows := make([]string, 0, len(obligations))
	for id := range obligations {
		if stmt := tables.builder.Stmts.Get(id); stmt != nil {
			names, _ := tables.stmtNames(obligations, stmt.Span.Start)
			rows = append(rows, fmt.Sprintf("%d:%v", stmt.Span.Start, names))
		}
	}
	slices.Sort(rows)
	return strings.Join(rows, " ")
}

func requireSingleSemaError(t *testing.T, bag *diag.Bag, code diag.Code, start, end uint32) {
	t.Helper()
	var errs []*diag.Diagnostic
	for _, d := range bag.Items() {
		if d != nil && d.Severity >= diag.SevError {
			errs = append(errs, d)
		}
	}
	if len(errs) != 1 || errs[0].Code != code || errs[0].Primary.Start != start || errs[0].Primary.End != end {
		t.Fatalf("errors: %s; want exactly one %s at [%d,%d)", diagnosticsSummary(bag), code.ID(), start, end)
	}
}

func requireExitDrops(t *testing.T, tables *snippetTables, returnStart uint32, want ...string) {
	t.Helper()
	got, ok := tables.stmtNames(tables.res.EarlyExitDrops, returnStart)
	if !ok || !slices.Equal(got, want) {
		t.Fatalf("return at byte %d drops %v (recorded %t), want exactly %v; all exits: %s",
			returnStart, got, ok, want, tables.obligationsSummary(tables.res.EarlyExitDrops))
	}
}

// L8a: before the fix the return at 13:5 owed [t, h].
func TestDropOfConvertedBindingDropsOnce(t *testing.T) {
	const src = convertedDropHeader + "    @drop t;\n" + convertedDropReturn
	tables := runSemaOnSnippetTables(t, src)
	requireFrozenConvertedDrop(t, tables, src, "0e7623955c0af232678255c384e5fa0413b4dc962119e5ddda5377428efc81be", 242, 243)
	requireNoSemaErrors(t, tables.parseBag, tables.semaBag)
	requireExitDrops(t, tables, 262, "h")
}

// L8b: the read of t at [270,271) 13:13 was admitted before the fix.
func TestDropOfConvertedBindingThenReadIsUseAfterMove(t *testing.T) {
	const src = convertedDropHeader + "    @drop t;\n    let n = t;\n" + convertedDropReturn
	tables := runSemaOnSnippetTables(t, src)
	requireFrozenConvertedDrop(t, tables, src, "ceadb562dea8d37344bff10d7deaaeffc121b753df790dabe8d46ea011345149", 242, 243)
	requireSingleSemaError(t, tables.semaBag, diag.SemaUseAfterMove, 270, 271)
}

// L8c: before the fix the store `t = "x"` at 13:5 recorded a second
// overwritten-value drop of the value @drop had already freed.
func TestDropOfConvertedBindingThenStoreRecordsNoSecondOldDrop(t *testing.T) {
	const src = convertedDropHeader + "    @drop t;\n    t = \"x\";\n" + convertedDropReturn
	tables := runSemaOnSnippetTables(t, src)
	requireFrozenConvertedDrop(t, tables, src, "d8cd464a000b2d44409024f8d8712a24c40799cd7d1b0608ec302ab311c58740", 242, 243)
	requireNoSemaErrors(t, tables.parseBag, tables.semaBag)
	var starts []uint32
	for expr := range tables.res.ReassignOldDrops {
		if node := tables.builder.Exprs.Get(expr); node != nil {
			starts = append(starts, node.Span.Start)
		}
	}
	slices.Sort(starts)
	if !slices.Equal(starts, []uint32{238}) {
		t.Fatalf("overwritten-value drops at %v, want only the store `t = h` at byte 238", starts)
	}
}

// L8d: the second @drop's operand at [268,269) 13:11 was admitted.
func TestDropOfConvertedBindingTwiceIsUseAfterMove(t *testing.T) {
	const src = convertedDropHeader + "    @drop t;\n    @drop t;\n" + convertedDropReturn
	tables := runSemaOnSnippetTables(t, src)
	requireFrozenConvertedDrop(t, tables, src, "592a60ea8296b45272912630a8d457f033196c49cf3c6de52fd38b7aae8c4708", 242, 243)
	requireSingleSemaError(t, tables.semaBag, diag.SemaUseAfterMove, 268, 269)
}

// L8e: the pin for the release half. @drop t still ends t's borrow of h, so
// storing into h afterwards is not a mutation under a borrow. h is not
// `mut`, and this leaf relies on no sema path refusing that store, so its state
// before the fix is measured rather than assumed.
func TestDropOfConvertedBindingStillReleasesTheSourceBorrow(t *testing.T) {
	const src = convertedDropHeader + "    @drop t;\n    h = Named { name = \"m\" };\n" + convertedDropReturn
	tables := runSemaOnSnippetTables(t, src)
	requireFrozenConvertedDrop(t, tables, src, "59c51f88d828edd88ad28709b86e9eec8542070c26cd2f3cbe2f783da5c3116a", 242, 243)
	requireNoSemaErrors(t, tables.parseBag, tables.semaBag)
}

// L8f: a drop on one branch only. Before the fix the `if` at 12:5 synthesized
// no else-drop and the return at 15:5 owed [t, h]; the c path then freed t
// twice.
func TestDropOfConvertedBindingOnOneBranchDropsOnTheOther(t *testing.T) {
	const src = convertedDropPrefix + "fn drop_assigned(c: bool) -> nothing {\n" + convertedDropStore + "    if c {\n        @drop t;\n    }\n" + convertedDropReturn
	tables := runSemaOnSnippetTables(t, src)
	requireFrozenConvertedDrop(t, tables, src, "98180829181992ee21edc119e2deb0d3490f3091810fccf36ecf9bb027a6d815", 249, 250)
	requireNoSemaErrors(t, tables.parseBag, tables.semaBag)
	if got, ok := tables.stmtNames(tables.res.IfSyntheticElseDrops, 256); !ok || !slices.Equal(got, []string{"t"}) {
		t.Fatalf("synthesized else drops %v (recorded %t), want exactly [t]; all: %s",
			got, ok, tables.obligationsSummary(tables.res.IfSyntheticElseDrops))
	}
	requireExitDrops(t, tables, 290, "h")
}
