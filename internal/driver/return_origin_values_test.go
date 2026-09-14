package driver

import (
	"crypto/sha256"
	"slices"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/sema"
	"surge/internal/types"
)

const returnOriginCallableMismatch = "callable return sources are not permitted by the destination promise"

// The int result keeps the escaped reference in a live outer binding. Thus a
// negative proves the normal block exit, not an unknown borrowed return summary.
func TestAnalyzeTypedReturnOriginCallableValues(t *testing.T) {
	const prefix = `fn first(a: &string, b: &string) -> &string { return a; }
fn second(a: &string, b: &string) -> &string { return b; }
`
	for _, tc := range []struct{ name, body, want, rhs string }{
		{"inferred_copy_narrow", `fn probe(outside: &string, flag: bool) -> int {
    let escaped: &string = {
        let owned: string = "local";
        let f = first; let copied = f;
        ret copied(outside, &owned);
    };
    return 1;
}
`, "clean", ""},
		{"declared_wide_copy_call", `fn probe(outside: &string, flag: bool) -> int {
    let escaped: &string = {
        let owned: string = "local";
        let f: fn(&string, &string) -> &string = first; let copied = f;
        ret copied(outside, &owned);
    };
    return 1;
}
`, "escape", ""},
		{"strong_update_narrow", `fn probe(outside: &string, flag: bool) -> int {
    let escaped: &string = {
        let owned: string = "local";
        let mut f = second; f = first; let copied = f;
        ret copied(outside, &owned);
    };
    return 1;
}
`, "clean", ""},
		{"copy_of_wide_reassign_stays_wide", `fn probe(outside: &string, flag: bool) -> int {
    let escaped: &string = {
        let owned: string = "local";
        let wide: fn(&string, &string) -> &string = first; let mut copied = wide; copied = first;
        ret copied(outside, &owned);
    };
    return 1;
}
`, "escape", ""},
		{"branch_union_local", `fn probe(outside: &string, flag: bool) -> int {
    let escaped: &string = {
        let owned: string = "local";
        let mut f = first; if flag { f = second; } let copied = f;
        ret copied(outside, &owned);
    };
    return 1;
}
`, "escape", ""},
		{"loop_union_local", `fn probe(outside: &string, flag: bool) -> int {
    let escaped: &string = {
        let owned: string = "local";
        let mut f = first; while flag { f = second; break; } let copied = f;
        ret copied(outside, &owned);
    };
    return 1;
}
`, "escape", ""},
		{"incoming_narrow_copy", `fn probe(f: fn(@return_source &string, &string) -> &string, outside: &string) -> int {
    let escaped: &string = {
        let owned: string = "local";
        let copied = f;
        ret copied(outside, &owned);
    };
    return 1;
}
`, "clean", ""},
		{"incoming_all_copy", `fn probe(f: fn(&string, &string) -> &string, outside: &string) -> int {
    let escaped: &string = {
        let owned: string = "local";
        let copied = f;
        ret copied(outside, &owned);
    };
    return 1;
}
`, "escape", ""},
		{"inferred_body_satisfies_narrow", `fn probe(outside: &string, flag: bool) -> int {
    let escaped: &string = {
        let owned: string = "local";
        let copied: fn(@return_source &string, &string) -> &string = first;
        ret copied(outside, &owned);
    };
    return 1;
}
`, "clean", ""},
		{"known_incompatible_binding", `fn probe() -> int { let narrow: fn(@return_source &string, &string) -> &string = second; return 1; }
`, "mismatch", "second"},
		{"narrowing_after_widen", `fn probe() -> int { let wide: fn(&string, &string) -> &string = first; let narrow: fn(@return_source &string, &string) -> &string = wide; return 1; }
`, "mismatch", "wide"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := prefix + tc.body
			t.Logf("RETURN_ORIGIN_VALUE_SOURCE case=%s sha256=%x source=%q", tc.name, sha256.Sum256([]byte(src)), src)
			res := returnOriginTypedFixtureWithEscapeEvidence(t, src, true)
			logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "stage": "typed",
				"source": src, "diagnostics": res.Bag.Items(), "bindings": returnOriginValueBindings(res),
				"expr_types": res.Sema.ExprTypes, "expr_symbols": res.Symbols.ExprSymbols,
				"declarations": res.Sema.ReturnSourceDeclarations, "instantiations": res.Sema.ReturnSourceInstantiations})
			inputs, err := collectReturnOriginUnits(res)
			if err != nil || len(inputs.units) != 1 {
				t.Fatalf("PRECONDITION: expected the one real source unit: units=%d error=%v", len(inputs.units), err)
			}
			analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
			logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "stage": "analysis", "source": src,
				"analysis": analysis, "error": errorReturnOriginCallText(err), "diagnostics": res.Bag.Items(),
				"publication": inputs.units[0].Publication})
			if err != nil || analysis == nil || len(analysis.Summaries) != 3 {
				t.Fatalf("PRECONDITION: private analysis did not retain the three real bodies: %v", err)
			}
			firstType, firstInfo := returnOriginValueFunction(t, res, "first")
			if !firstInfo.ReturnSources().IsAllInputs() {
				t.Fatal("unmarked first must retain its declared AllInputs type")
			}
			if tc.name == "declared_wide_copy_call" {
				boundType, _ := returnOriginValueFunction(t, res, "f")
				copyType, _ := returnOriginValueFunction(t, res, "copied")
				if firstType != boundType || boundType != copyType {
					t.Fatal("equal-TypeID widening witness no longer has equal declared types")
				}
			}
			if tc.name == "inferred_body_satisfies_narrow" {
				boundType, boundInfo := returnOriginValueFunction(t, res, "copied")
				if boundType == firstType || boundInfo.ReturnSources().IsAllInputs() || !slices.Equal(boundInfo.ReturnSources().Slots(), []uint32{0}) {
					t.Fatal("narrow destination lost the distinct expected promise before body inference")
				}
			}
			if !analysis.Complete() {
				t.Fatalf("callable values still have unfinished transfers: %+v", analysis.Pending)
			}
			for _, body := range []struct {
				name string
				slot uint32
			}{{"first", 0}, {"second", 1}} {
				summary := requireReturnOriginSummary(t, analysis, body.name)
				if summary.Unknown || summary.NoNormalReturn || !slices.Equal(summary.ParamSlots, []uint32{body.slot}) {
					t.Fatalf("known body lost its actual source: %+v", summary)
				}
			}
			probe := requireReturnOriginSummary(t, analysis, "probe")
			if probe.Unknown || probe.NoNormalReturn || len(probe.ParamSlots) != 0 {
				t.Fatalf("int result fabricated borrowed content: %+v", probe)
			}
			switch tc.want {
			case "clean":
				if len(analysis.Diagnostics) != 0 {
					t.Fatalf("safe callable value was refused: %+v", analysis.Diagnostics)
				}
			case "escape":
				if len(analysis.Diagnostics) == 0 {
					t.Fatal("normal block exit lost the local owner through the callable value")
				}
				for _, d := range analysis.Diagnostics {
					requireReturnOriginValueEscape(t, src, res, d)
				}
			case "mismatch":
				requireReturnOriginValueMismatch(t, src, tc.rhs, res, analysis)
			default:
				t.Fatal("unknown expected source outcome")
			}
		})
	}
}

func returnOriginValueBindings(res *DiagnoseResult) []map[string]any {
	var rows []map[string]any
	for id, typ := range res.Sema.BindingTypes {
		sym := res.Symbols.Table.Symbols.Get(id)
		if sym == nil {
			continue
		}
		name, _ := res.Builder.StringsInterner.Lookup(sym.Name)
		row := map[string]any{"symbol": id, "name": name, "type": typ, "scope": sym.Scope, "declaration": sym.Decl, "span": sym.Span}
		if info, ok := res.Sema.TypeInterner.FnInfo(typ); ok {
			row["function"], row["all_inputs"], row["slots"] = info, info.ReturnSources().IsAllInputs(), info.ReturnSources().Slots()
		}
		if sym.Decl.Stmt.IsValid() {
			if node := res.Builder.Stmts.Get(sym.Decl.Stmt); node != nil && node.Kind == ast.StmtLet {
				let := res.Builder.Stmts.Let(sym.Decl.Stmt)
				row["annotation"], row["initializer"], row["initializer_type"] = let.Type, let.Value, res.Sema.ExprTypes[let.Value]
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func returnOriginValueFunction(t *testing.T, res *DiagnoseResult, name string) (types.TypeID, *types.FnInfo) {
	t.Helper()
	var found types.TypeID
	var info *types.FnInfo
	for _, sym := range res.Symbols.Table.Symbols.Data() {
		text, _ := res.Builder.StringsInterner.Lookup(sym.Name)
		if text != name || sym.Decl.SourceFile != res.File.ID {
			continue
		}
		if current, ok := res.Sema.TypeInterner.FnInfo(sym.Type); ok {
			logReturnOriginCallEvidence(t, map[string]any{"stage": "source_callable_type", "name": name,
				"symbol": sym, "type": sym.Type, "function": current,
				"all_inputs": current.ReturnSources().IsAllInputs(), "slots": current.ReturnSources().Slots()})
			if info != nil {
				t.Fatalf("PRECONDITION: multiple source callables named %s", name)
			}
			found, info = sym.Type, current
		}
	}
	if info == nil {
		t.Fatalf("PRECONDITION: no typed source callable %s", name)
	}
	return found, info
}

func requireReturnOriginValueEscape(t *testing.T, src string, res *DiagnoseResult, d diag.Diagnostic) {
	t.Helper()
	const statement = "ret copied(outside, &owned);"
	start := strings.Index(src, statement)
	if d.Code != diag.SemaBorrowEscapesReturn || d.Severity != diag.SevError || d.Primary.File != res.File.ID ||
		int(d.Primary.Start) != start || int(d.Primary.End) != start+len(statement) ||
		d.Message != "borrow of 'owned' outlives its owner when this scope exits" || len(d.Help) == 0 {
		t.Fatalf("wrong normal-exit owner diagnostic: %+v", d)
	}
	const declaration = `let owned: string = "local";`
	owner := strings.Index(src, declaration)
	for _, note := range d.Notes {
		if note.Span.File == res.File.ID && int(note.Span.Start) == owner && int(note.Span.End) == owner+len(declaration) {
			return
		}
	}
	t.Fatal("normal-exit diagnostic did not point to the actual owned declaration")
}

func requireReturnOriginValueMismatch(t *testing.T, src, rhs string, res *DiagnoseResult, analysis *sema.ReturnOriginAnalysis) {
	t.Helper()
	if len(analysis.Diagnostics) != 1 {
		t.Fatalf("expected one completed callable promise mismatch: %+v", analysis.Diagnostics)
	}
	d := analysis.Diagnostics[0]
	start := strings.LastIndex(src, " = "+rhs+";") + len(" = ")
	if d.Code != diag.SemaError || d.Severity != diag.SevError || d.Message != returnOriginCallableMismatch ||
		d.Primary.File != res.File.ID || int(d.Primary.Start) != start || int(d.Primary.End) != start+len(rhs) || len(d.Help) == 0 {
		t.Fatalf("callable mismatch lost its actual RHS/source reason: %+v", d)
	}
	const expected = "fn(@return_source &string, &string) -> &string"
	annotation := strings.LastIndex(src, expected)
	for _, note := range d.Notes {
		if note.Span.File == res.File.ID && int(note.Span.Start) == annotation && int(note.Span.End) == annotation+len(expected) {
			return
		}
	}
	t.Fatal("callable mismatch did not identify the exact destination promise")
}
