package driver

import (
	"crypto/sha256"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/sema"
	"surge/internal/types"
)

func TestAnalyzeTypedReturnOriginNominalPayload(t *testing.T) {
	const prefix = "tag Some<T>(T); type Option<T> = Some(T) | nothing;\n" +
		"fn wrap(@return_source value: &string) -> Option<&string>;\n"
	for _, dynamic := range []bool{false, true} {
		for _, local := range []bool{false, true} {
			name, argument := "fixed_external", "outside"
			if dynamic {
				name = "dynamic_external"
			}
			if local {
				name, argument = strings.Replace(name, "external", "local", 1), "&owned"
			}
			t.Run(name, func(t *testing.T) {
				result := "ret [wrap(" + argument + ")];"
				if dynamic {
					result = "let values: Option<&string>[] = [wrap(" + argument + ")]; ret values;"
				}
				src := prefix + "fn probe(outside: &string) -> int {\n" +
					"    let escaped = { let owned: string = \"local\"; " + result + " };\n" +
					"    return 1;\n}\n"
				t.Logf("RETURN_ORIGIN_NOMINAL_SOURCE case=%s sha256=%x source=%q", name, sha256.Sum256([]byte(src)), src)
				res := returnOriginTypedFixtureWithEscapeEvidence(t, src, true)
				var arrays []map[string]any
				for id, typ := range res.Sema.ExprTypes {
					if node := res.Builder.Exprs.Get(id); node != nil && node.Kind == ast.ExprArray {
						info, ok := res.Sema.TypeInterner.StructInfo(typ)
						arrays = append(arrays, map[string]any{"expr": id, "type": typ, "struct": info, "is_struct": ok})
					}
				}
				logReturnOriginCallEvidence(t, map[string]any{"case": name, "source": src, "arrays": arrays,
					"diagnostics": res.Bag.Items(), "expr_types": res.Sema.ExprTypes,
					"binding_types": res.Sema.BindingTypes, "declarations": res.Sema.ReturnSourceDeclarations})
				if len(arrays) != 1 || len(res.Sema.ReturnSourceDeclarations) != 1 {
					t.Fatal("PRECONDITION: source lost its array or original wrap promise")
				}
				info := arrays[0]["struct"].(*types.StructInfo)
				if info == nil || len(info.TypeArgs) == 0 || len(info.Fields) != 0 {
					t.Fatal("PRECONDITION: source lacks the logical nominal payload without physical fields")
				}
				inputs, err := collectReturnOriginUnits(res)
				if err != nil || len(inputs.units) != 1 {
					t.Fatalf("PRECONDITION: expected one real source unit: %v", err)
				}
				analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
				logReturnOriginCallEvidence(t, map[string]any{"case": name, "analysis": analysis,
					"error": errorReturnOriginCallText(err), "publication": inputs.units[0].Publication})
				if err != nil || analysis == nil || !analysis.Complete() {
					t.Fatalf("nominal payload lacks a complete analysis: %+v error=%v", analysis, err)
				}
				if !local {
					if len(analysis.Diagnostics) != 0 {
						t.Fatalf("external optional payload acquired a local-owner refusal: %+v", analysis.Diagnostics)
					}
					return
				}
				owner := strings.Index(src, `let owned: string = "local";`)
				for _, d := range analysis.Diagnostics {
					if d.Code != diag.SemaBorrowEscapesReturn || d.Severity != diag.SevError || d.Primary.File != res.File.ID ||
						d.Message != "borrow of 'owned' outlives its owner when this scope exits" || len(d.Help) == 0 {
						continue
					}
					for _, note := range d.Notes {
						if note.Span.File == res.File.ID && int(note.Span.Start) == owner && int(note.Span.End) == owner+len(`let owned: string = "local";`) {
							return
						}
					}
				}
				t.Fatal("nominal array erased the optional payload's local owner")
			})
		}
	}
}
