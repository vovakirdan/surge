package sema

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

const compareAbruptOption = "pragma no_std;\ntag Some<T>(T); type Option<T> = Some(T) | nothing;\n"

func TestCompareAbruptResultType(t *testing.T) {
	for _, name := range []string{"all_return", "option_all_return", "ordinary_nothing", "mixed_reference"} {
		t.Run(name, func(t *testing.T) { checkCompareAbruptResultType(t, name) })
	}
}

func TestCompareAbruptResultRefusal(t *testing.T) {
	for _, name := range []string{"invalid_return_value", "nonexhaustive"} {
		t.Run(name, func(t *testing.T) { checkCompareAbruptResultType(t, name) })
	}
}

func checkCompareAbruptResultType(t *testing.T, name string) {
	t.Helper()
	fixtures := map[string]string{
		"all_return": `pragma no_std;
fn probe(flag: bool) -> int {
    compare flag {
        true => { return 0; }
        false => { return 1; }
    };
}
`,
		"option_all_return": compareAbruptOption + `fn probe<T>(value: Option<T>) -> nothing {
    compare value {
        Some(_) => { return nothing; }
        _ => { return nothing; }
    };
}
`,
		"ordinary_nothing": `pragma no_std;
fn probe(flag: bool) -> nothing {
    compare flag {
        true => nothing;
        false => nothing;
    };
}
`,
		"mixed_reference": `pragma no_std;
fn probe(flag: bool, a: &string, b: &string) -> &string {
    let inferred = compare flag {
        true => { return a; }
        false => b;
    };
    let expected: &string = compare flag {
        true => { return inferred; }
        false => b;
    };
    return expected;
}
`,
		// The name resolves to its original type declaration. SEMA must reject
		// its use as a value while the surrounding return blocks remain Nothing.
		"invalid_return_value": `pragma no_std;
type Invalid = { value: int };
fn probe(flag: bool) -> nothing {
    compare flag {
        true => { return Invalid; }
        false => { return nothing; }
    };
}
`,
		"nonexhaustive": compareAbruptOption + `fn probe<T>(value: Option<T>) -> nothing {
    compare value {
        Some(_) => { return nothing; }
    };
}
`,
	}
	text := fixtures[name]
	b, file, parseBag := parseSource(t, text)
	if parseBag.Len() != 0 {
		t.Fatalf("parse prerequisites: %s", diagnosticsSummary(parseBag))
	}
	syms := resolveSymbols(t, b, file)
	bag := diag.NewBag(32)
	res := Check(t.Context(), b, file, Options{Symbols: syms, Reporter: &diag.BagReporter{Bag: bag}})
	probe := lookupSymbolByName(syms, b.StringsInterner.Intern("probe"))
	sym := syms.Table.Symbols.Get(probe)
	if sym == nil || sym.Kind != symbols.SymbolFunction {
		t.Fatal("original probe function is absent")
	}
	fn, ok := res.TypeInterner.FnInfo(sym.Type)
	if !ok || fn == nil || len(fn.Params) == 0 {
		t.Fatal("original probe has no callable descriptor")
	}
	tc := &typeChecker{builder: b, fileID: file, symbols: syms, types: res.TypeInterner, result: &res}
	nothing := res.TypeInterner.Builtins().Nothing
	compares := []map[string]any{}
	var firstSpan source.Span
	for raw := uint32(1); raw <= b.Exprs.Arena.Len(); raw++ {
		id := ast.ExprID(raw)
		node := b.Exprs.Get(id)
		if node == nil || node.Kind != ast.ExprCompare {
			continue
		}
		cmp, _ := b.Exprs.Compare(id)
		got, present := res.ExprTypes[id]
		if len(compares) == 0 {
			firstSpan = node.Span
		}
		arms, allNothing, normalArms := []map[string]any{}, true, 0
		for _, arm := range cmp.Arms {
			typ, typed := res.ExprTypes[arm.Result]
			closed := tc.compareArmAbruptExit(arm.Result)
			arms = append(arms, map[string]any{"result": arm.Result, "span": b.Exprs.Get(arm.Result).Span, "type": typ, "present": typed, "abrupt": closed})
			allNothing = allNothing && typed && typ == nothing
			if !closed {
				normalArms++
			}
			if !typed || typ == types.NoTypeID {
				t.Errorf("arm %d has no valid type", arm.Result)
			}
		}
		subject, subjectPresent := res.ExprTypes[cmp.Value]
		exhaustive := tc.compareAlwaysMatches(cmp)
		compares = append(compares, map[string]any{"id": id, "span": node.Span, "type": got, "present": present, "subject": cmp.Value, "subject_type": subject, "subject_present": subjectPresent, "arms": arms, "exhaustive": exhaustive})
		if !present || !subjectPresent || subject == types.NoTypeID {
			t.Error("compare or its subject lacks a typed entry")
		}
		if exhaustive != (name != "nonexhaustive") {
			t.Errorf("exhaustiveness=%t does not match the source control", exhaustive)
		}
		want := nothing
		switch name {
		case "mixed_reference":
			want = fn.Params[2]
			ref, valid := res.TypeInterner.Lookup(want)
			if !valid || ref.Kind != types.KindReference || ref.Mutable || normalArms != 1 || res.ExprTypes[cmp.Arms[1].Result] != want {
				t.Error("normal arm lost its exact incoming reference type")
			}
		case "ordinary_nothing":
			if !allNothing || normalArms != 2 {
				t.Error("ordinary Nothing arms unexpectedly exit abruptly")
			}
		default:
			if !allNothing || normalArms != 0 {
				t.Error("all-abrupt prerequisites are absent")
			}
		}
		if name == "option_all_return" || name == "nonexhaustive" {
			union, valid := res.TypeInterner.UnionInfo(subject)
			if !valid || union == nil || len(union.TypeArgs) != 1 || subject != fn.Params[0] {
				t.Fatal("generic Option subject lost the original parameter descriptor")
			}
			param, valid := res.TypeInterner.TypeParamInfo(union.TypeArgs[0])
			if !valid || param == nil || param.Owner != uint32(probe) {
				t.Error("generic Option argument lost its original function owner")
			}
		}
		if name == "invalid_return_value" || name == "nonexhaustive" {
			want = types.NoTypeID
		}
		if got != want {
			t.Errorf("compare %d at %v type=%d, want exact %d", id, node.Span, got, want)
		}
	}
	wantCount := 1
	if name == "mixed_reference" {
		wantCount = 2 // Inferred and explicitly expected results must both persist.
	}
	if len(compares) != wantCount || strings.Count(text, "compare ") != wantCount {
		t.Errorf("compare census=%d, want %d", len(compares), wantCount)
	}
	wantCode, wantMessage, wantSpan := diag.Code(0), "", source.Span{}
	if name == "invalid_return_value" {
		start := uint32(strings.Index(text, "return Invalid;") + len("return "))
		wantCode, wantMessage = diag.SemaTypeMismatch, "type Invalid cannot be used as a value"
		wantSpan = source.Span{File: b.Files.Get(file).Span.File, Start: start, End: start + uint32(len("Invalid"))}
	}
	if name == "nonexhaustive" {
		wantCode, wantMessage, wantSpan = diag.SemaNonexhaustiveMatch, "non-exhaustive pattern match: missing patterns for nothing", firstSpan
	}
	if wantCode != 0 {
		if bag.Len() != 1 || bag.Items()[0].Code != wantCode || bag.Items()[0].Message != wantMessage || bag.Items()[0].Primary != wantSpan {
			t.Errorf("exact original diagnostic %s at %v missing: %s", wantCode.ID(), wantSpan, diagnosticsSummary(bag))
		}
	} else if bag.Len() != 0 {
		t.Errorf("unexpected diagnostics: %s", diagnosticsSummary(bag))
	}
	data, err := json.Marshal(map[string]any{"fixture": name, "source": text, "sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(text))), "probe": probe, "fn": fn, "compares": compares, "expr_types": res.ExprTypes, "diagnostics": bag.Items()})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("COMPARE_ABRUPT_CAPTURE %s", data)
}
