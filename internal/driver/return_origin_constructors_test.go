package driver

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/sema"
	"surge/internal/types"
)

func TestAnalyzeTypedReturnOriginConstructorEffects(t *testing.T) {
	for _, shape := range []struct {
		name, expression string
		kind             ast.ExprKind
	}{
		{"array", "[%s, 2]", ast.ExprArray},
		{"tuple", "(%s, 2)", ast.ExprTuple},
		{"struct", "Pair { first: %s, second: 2 }", ast.ExprStruct},
	} {
		for _, escaped := range []bool{false, true} {
			name, origin := shape.name+"_external", "outside"
			if escaped {
				name, origin = shape.name+"_local", "&owned"
			}
			t.Run(name, func(t *testing.T) {
				child := "{ let owned: int64 = 1; saved = " + origin + "; ret 1; }"
				declaration := "type Pair = { first: int64, second: int64 }\n"
				if shape.kind == ast.ExprStruct {
					declaration = "type Pair = { first: int, second: int }\n"
				}
				src := declaration +
					"fn probe(outside: &int64) -> int64 {\n" +
					"    let mut saved: &int64 = outside;\n" +
					"    let result = " + fmt.Sprintf(shape.expression, child) + ";\n" +
					"    return 1;\n}\n"
				t.Logf("RETURN_ORIGIN_CONSTRUCTOR_SOURCE case=%s sha256=%x source=%q", name, sha256.Sum256([]byte(src)), src)
				res := returnOriginTypedFixtureWithEscapeEvidence(t, src, true)
				logReturnOriginCallEvidence(t, map[string]any{"case": name, "source": src,
					"diagnostics": res.Bag.Items(), "expr_types": res.Sema.ExprTypes,
					"binding_types": res.Sema.BindingTypes, "expr_symbols": res.Symbols.ExprSymbols})
				var constructor ast.ExprID
				for _, sym := range res.Symbols.Table.Symbols.Data() {
					if text, _ := res.Builder.StringsInterner.Lookup(sym.Name); text == "result" && sym.Decl.Stmt.IsValid() {
						constructor = res.Builder.Stmts.Let(sym.Decl.Stmt).Value
					}
				}
				if !constructor.IsValid() || res.Builder.Exprs.Get(constructor).Kind != shape.kind || res.Sema.ExprTypes[constructor] == types.NoTypeID {
					t.Fatal("PRECONDITION: source did not produce its typed constructor")
				}
				inputs, err := collectReturnOriginUnits(res)
				if err != nil || len(inputs.units) != 1 {
					t.Fatalf("PRECONDITION: expected one real source unit: %v", err)
				}
				analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
				logReturnOriginCallEvidence(t, map[string]any{"case": name, "analysis": analysis,
					"error": errorReturnOriginCallText(err), "publication": inputs.units[0].Publication})
				if err != nil || analysis == nil {
					t.Fatalf("constructor analysis failed before its result: %v", err)
				}
				if !analysis.Complete() {
					t.Fatalf("constructor effects remain unfinished: %+v", analysis.Pending)
				}
				summary := requireReturnOriginSummary(t, analysis, "probe")
				if summary.Unknown || summary.NoNormalReturn || len(summary.ParamSlots) != 0 {
					t.Fatalf("owned scalar return has unexpected provenance: %+v", summary)
				}
				if !escaped {
					if len(analysis.Diagnostics) != 0 {
						t.Fatalf("incoming source was incorrectly refused: %+v", analysis.Diagnostics)
					}
					return
				}
				if len(analysis.Diagnostics) == 0 {
					t.Fatal("constructor skipped the child effect that escapes its local owner")
				}
				owner := strings.Index(src, "let owned: int64 = 1;")
				block := strings.Index(src, child)
				for _, d := range analysis.Diagnostics {
					if d.Code != diag.SemaBorrowEscapesReturn || d.Severity != diag.SevError || d.Primary.File != res.File.ID ||
						d.Message != "borrow of 'owned' outlives its owner when this scope exits" || len(d.Help) == 0 {
						t.Fatalf("unexpected constructor diagnostic: %+v", d)
					}
					if int(d.Primary.Start) < block || int(d.Primary.End) > block+len(child) {
						t.Fatalf("diagnostic did not identify the constructor child exit: %+v", d)
					}
					found := false
					for _, note := range d.Notes {
						found = found || (note.Span.File == res.File.ID && int(note.Span.Start) == owner && int(note.Span.End) == owner+len("let owned: int64 = 1;"))
					}
					if !found {
						t.Fatal("constructor diagnostic lost the actual local owner")
					}
				}
			})
		}
	}
}
