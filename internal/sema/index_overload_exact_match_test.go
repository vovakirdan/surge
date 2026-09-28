package sema

import (
	"sort"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
)

// Index resolution prices its argument the way ordinary overload resolution
// does: an exact type costs nothing, and a fixed-width integer reaching the
// dynamic `int` formal is an implicit widening that costs one and is recorded
// as a conversion. It used to accept `int64` for `int` at no cost and without
// a conversion, and kept the first declared overload on the tie: `s[5:int64]`
// ran the `int` body, and the native build read the int64 as a dynamic
// integer's pointer.

const indexOverloadIntFirst = `type S = { n: int };
extern<S> {
    fn __index(self: &S, i: int) -> int { return i + 1000; }
    @overload fn __index(self: &S, i: int64) -> int { return 2000; }
}
fn probe(s: &S, a: int, b: int64) -> int {
    let x: int = s[a];
    let y: int = s[b];
    return x + y;
}
`

const indexOverloadInt64First = `type S = { n: int };
extern<S> {
    fn __index(self: &S, i: int64) -> int { return 2000; }
    @overload fn __index(self: &S, i: int) -> int { return i + 1000; }
}
fn probe(s: &S, a: int, b: int64) -> int {
    let x: int = s[a];
    let y: int = s[b];
    return x + y;
}
`

const indexOverloadOnlyInt = `type S = { n: int };
extern<S> {
    fn __index(self: &S, i: int) -> int { return i + 1000; }
}
fn probe(s: &S, a: int, b: int64) -> int {
    let x: int = s[a];
    let y: int = s[b];
    return x + y;
}
`

const indexOverloadTie = `type S = { n: int };
extern<S> {
    fn __index(self: &S, i: int) -> int { return i + 1000; }
    @overload fn __index(self: &S, i: own int) -> int { return i + 2000; }
}
fn probe(s: &S, a: int) -> int {
    return s[a];
}
`

// indexRows returns, in source order, each index expression's selected index
// formal and whether a conversion was recorded on its index operand.
func indexRows(t *testing.T, src string) (formals []string, converted []bool) {
	t.Helper()
	tables := runSemaOnSnippetTables(t, src)
	if tables.parseBag.HasErrors() || tables.res == nil || tables.syms.Table == nil {
		t.Fatalf("PRECONDITION: snippet did not parse: %s", diagnosticsSummary(tables.parseBag))
	}
	if tables.semaBag.HasErrors() {
		t.Fatalf("the index selection did not type: %s", diagnosticsSummary(tables.semaBag))
	}
	type row struct {
		start     uint32
		formal    string
		converted bool
	}
	rows := make([]row, 0, len(tables.res.IndexSymbols))
	for expr, symID := range tables.res.IndexSymbols {
		node := tables.builder.Exprs.Get(expr)
		idx, ok := tables.builder.Exprs.Index(expr)
		sym := tables.syms.Table.Symbols.Get(symID)
		if node == nil || !ok || idx == nil || sym == nil || sym.Signature == nil || len(sym.Signature.Params) != 2 {
			t.Fatalf("PRECONDITION: index %d has no two-parameter selection", expr)
		}
		_, conv := tables.res.ImplicitConversions[idx.Index]
		rows = append(rows, row{node.Span.Start, string(sym.Signature.Params[1]), conv})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].start < rows[j].start })
	for _, r := range rows {
		formals = append(formals, r.formal)
		converted = append(converted, r.converted)
	}
	return formals, converted
}

func TestIndexOverloadPrefersTheExactIndexType(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		formals   []string
		converted []bool
	}{
		{"int_declared_first", indexOverloadIntFirst, []string{"int", "int64"}, []bool{false, false}},
		{"int64_declared_first", indexOverloadInt64First, []string{"int", "int64"}, []bool{false, false}},
		// Control: with only the `int` overload, an int64 index still
		// resolves, and the body receives the widened value.
		{"widened_to_the_only_int_overload", indexOverloadOnlyInt, []string{"int", "int"}, []bool{false, true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			formals, converted := indexRows(t, tc.src)
			if strings.Join(formals, ",") != strings.Join(tc.formals, ",") {
				t.Fatalf("selected index formals %v, want %v", formals, tc.formals)
			}
			for i := range converted {
				if converted[i] != tc.converted[i] {
					t.Fatalf("index %d conversion recorded=%v, want %v", i, converted[i], tc.converted[i])
				}
			}
		})
	}
}

// Two overloads of one receiver that price the index the same are ambiguous,
// as in ordinary overload resolution; the first declared one no longer wins.
func TestIndexOverloadTieIsAmbiguous(t *testing.T) {
	tables := runSemaOnSnippetTables(t, indexOverloadTie)
	if tables.parseBag.HasErrors() {
		t.Fatalf("PRECONDITION: parse: %s", diagnosticsSummary(tables.parseBag))
	}
	found := false
	for _, d := range tables.semaBag.Items() {
		if d.Code == diag.SemaAmbiguousOverload && strings.Contains(d.Message, "index") {
			found = true
		}
	}
	if !found {
		t.Fatalf("tie was not reported as ambiguous: %s", diagnosticsSummary(tables.semaBag))
	}
}

const indexSetOverloadIntFirst = `type S = { n: int };
extern<S> {
    fn __index(self: &S, i: int) -> int { return i; }
    fn __index_set(self: &mut S, i: int, v: int) {}
    @overload fn __index_set(self: &mut S, i: int64, v: int) {}
}
fn probe(s: &mut S, a: int, b: int64) {
    s[a] = 1;
    s[b] = 2;
}
`

const indexSetOverloadOnlyInt = `type S = { n: int };
extern<S> {
    fn __index(self: &S, i: int) -> int { return i; }
    fn __index_set(self: &mut S, i: int, v: int) {}
}
fn probe(s: &mut S, a: int, b: int64, c: int32) {
    s[a] = 1;
    s[b] = 2;
    s[c] = 3;
}
`

const indexSetOverloadTie = `type S = { n: int };
extern<S> {
    fn __index(self: &S, i: int) -> int { return i; }
    fn __index_set(self: &mut S, i: int, v: int) {}
    @overload fn __index_set(self: &mut S, i: own int, v: int) {}
}
fn probe(s: &mut S, a: int) {
    s[a] = 1;
}
`

// indexSetRows returns, in source order, each indexed assignment's selected
// index formal and whether a conversion was recorded on its index operand.
func indexSetRows(t *testing.T, src string) (formals []string, converted []bool) {
	t.Helper()
	tables := runSemaOnSnippetTables(t, src)
	if tables.parseBag.HasErrors() || tables.res == nil || tables.syms.Table == nil {
		t.Fatalf("PRECONDITION: snippet did not parse: %s", diagnosticsSummary(tables.parseBag))
	}
	if tables.semaBag.HasErrors() {
		t.Fatalf("the index selection did not type: %s", diagnosticsSummary(tables.semaBag))
	}
	type row struct {
		start     uint32
		formal    string
		converted bool
	}
	rows := make([]row, 0, len(tables.res.IndexSetSymbols))
	for expr, symID := range tables.res.IndexSetSymbols {
		node := tables.builder.Exprs.Get(expr)
		idx, ok := tables.builder.Exprs.Index(expr)
		sym := tables.syms.Table.Symbols.Get(symID)
		if node == nil || !ok || idx == nil || sym == nil || sym.Signature == nil || len(sym.Signature.Params) != 3 {
			t.Fatalf("PRECONDITION: indexed assignment %d has no three-parameter selection", expr)
		}
		_, conv := tables.res.ImplicitConversions[idx.Index]
		rows = append(rows, row{node.Span.Start, string(sym.Signature.Params[1]), conv})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].start < rows[j].start })
	for _, r := range rows {
		formals = append(formals, r.formal)
		converted = append(converted, r.converted)
	}
	return formals, converted
}

func TestIndexSetOverloadPrefersTheExactIndexType(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		formals   []string
		converted []bool
	}{
		{"int_declared_first", indexSetOverloadIntFirst, []string{"int", "int64"}, []bool{false, false}},
		{"widened_to_the_only_int_overload", indexSetOverloadOnlyInt, []string{"int", "int", "int"}, []bool{false, true, true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			formals, converted := indexSetRows(t, tc.src)
			if strings.Join(formals, ",") != strings.Join(tc.formals, ",") {
				t.Fatalf("selected index formals %v, want %v", formals, tc.formals)
			}
			for i := range converted {
				if converted[i] != tc.converted[i] {
					t.Fatalf("assignment %d conversion recorded=%v, want %v", i, converted[i], tc.converted[i])
				}
			}
		})
	}
}

func TestIndexSetOverloadTieIsAmbiguous(t *testing.T) {
	tables := runSemaOnSnippetTables(t, indexSetOverloadTie)
	if tables.parseBag.HasErrors() {
		t.Fatalf("PRECONDITION: parse: %s", diagnosticsSummary(tables.parseBag))
	}
	for _, d := range tables.semaBag.Items() {
		if d.Code == diag.SemaAmbiguousOverload && strings.Contains(d.Message, "index assignment") {
			return
		}
	}
	t.Fatalf("tie was not reported as ambiguous: %s", diagnosticsSummary(tables.semaBag))
}

// A generic formal that the receiver instantiates to the other overload's
// type is the same candidate, not a tie: `g[a]` on G<int> selects the
// declaration the method call `g.get(a)` of the same shape selects.
const indexGenericFormalSource = `type G<T> = { v: T };
extern<G<T>> {
    fn __index(self: &G<T>, i: T) -> int { return 1; }
    @overload fn __index(self: &G<T>, i: int) -> int { return 2; }
    fn get(self: &G<T>, i: T) -> int { return 1; }
    @overload fn get(self: &G<T>, i: int) -> int { return 2; }
}
fn probe(g: &G<int>, a: int) -> int {
    return g[a] + g.get(a);
}
`

func TestIndexGenericFormalSelectsAsTheMethodCallDoes(t *testing.T) {
	tables := runSemaOnSnippetTables(t, indexGenericFormalSource)
	if tables.parseBag.HasErrors() || tables.semaBag.HasErrors() || tables.res == nil {
		t.Fatalf("index over a generic formal did not type: %s %s", diagnosticsSummary(tables.parseBag), diagnosticsSummary(tables.semaBag))
	}
	var index, method string
	for expr, symID := range tables.res.IndexSymbols {
		if sym := tables.syms.Table.Symbols.Get(symID); sym != nil && sym.Signature != nil && tables.builder.Exprs.Get(expr) != nil {
			index = string(sym.Signature.Params[1])
		}
	}
	for expr, symID := range tables.syms.ExprSymbols {
		node := tables.builder.Exprs.Get(expr)
		sym := tables.syms.Table.Symbols.Get(symID)
		if node == nil || node.Kind != ast.ExprCall || sym == nil || sym.Signature == nil || tables.builder.StringsInterner.MustLookup(sym.Name) != "get" {
			continue
		}
		method = string(sym.Signature.Params[1])
	}
	if index == "" || method == "" || index != method {
		t.Fatalf("g[a] selected formal %q, g.get(a) selected %q", index, method)
	}
}
