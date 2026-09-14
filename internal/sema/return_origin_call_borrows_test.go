package sema

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/types"
)

const returnOriginOwnedSharedCall = `fn same(value: &string) -> &string { return value; }
fn probe() -> nothing {
    let owned: string = "value";
    let observed = same(owned);
}
`
const returnOriginOwnedMutCall = `fn same(value: &mut string) -> &mut string { return value; }
fn probe() -> nothing {
    let mut owned: string = "value";
    let observed = same(owned);
}
`
const returnOriginIncomingCall = `fn same(value: &string) -> &string { return value; }
fn probe(owned: string, value: &string) -> &string { return same(value); }
`

// This exercises the existing call transfer with real typed AST, symbols and
// a summary inferred from same's real body. Corruptions affect only a detached
// evidence slice; this is not a whole-program acceptance or a new borrow checker.
func TestReturnOriginCallBorrowEvidence(t *testing.T) {
	for _, name := range []string{"owned_shared", "owned_mut", "incoming_reference", "missing", "ambiguous", "wrong_kind", "reserved", "expired_storage", "wrong_from_expr", "unknown_incoming"} {
		t.Run(name, func(t *testing.T) {
			src := returnOriginOwnedSharedCall
			if name == "owned_mut" || name == "reserved" {
				src = returnOriginOwnedMutCall
			}
			incoming := name == "incoming_reference" || name == "expired_storage" || name == "unknown_incoming"
			if incoming {
				src = returnOriginIncomingCall
			}
			t.Logf("RETURN_ORIGIN_CALL_SOURCE case=%s sha256=%x source=%q", name, sha256.Sum256([]byte(src)), src)
			result, unit := returnOriginPublicationFixture(t, src, false)
			index, err := indexReturnOriginUnit(unit, result)
			if err != nil {
				t.Fatal(err)
			}
			var caller, callee *returnOriginFunction
			for _, fn := range index.functions {
				switch fn.name {
				case "probe":
					caller = fn
				case "same":
					callee = fn
				}
			}
			if caller == nil || callee == nil {
				t.Fatal("PRECONDITION: missing actual caller/callee bodies")
			}
			var callID ast.ExprID
			for id := range result.ExprTypes {
				if call, ok := unit.Builder.Exprs.Call(id); ok && call != nil {
					if callID.IsValid() {
						t.Fatal("PRECONDITION: source contains multiple calls")
					}
					callID = id
				}
			}
			call, ok := unit.Builder.Exprs.Call(callID)
			if !ok || call == nil || len(call.Args) != 1 || result.ExprTypes[callID] == types.NoTypeID || unit.Symbols.ExprSymbols[callID] != callee.symbol {
				t.Fatal("PRECONDITION: actual typed call identity/arity is missing")
			}
			arg := call.Args[0].Value
			binding := unit.Symbols.ExprSymbols[arg]
			sym := unit.Symbols.Table.Symbols.Get(binding)
			if sym == nil || !binding.IsValid() || result.ExprTypes[arg] == types.NoTypeID {
				t.Fatal("PRECONDITION: argument has no typed binding")
			}
			original := slices.Clone(result.Borrows)
			matching := -1
			for i, borrow := range original {
				if borrow.Life.FromExpr == arg {
					if matching >= 0 {
						t.Fatal("PRECONDITION: original auto-borrow is ambiguous")
					}
					matching = i
				}
			}
			if !incoming {
				wantKind := BorrowShared
				if name == "owned_mut" || name == "reserved" {
					wantKind = BorrowMut
				}
				if matching < 0 || original[matching].Kind != wantKind || original[matching].Reserved || !original[matching].Place.IsValid() {
					t.Fatal("PRECONDITION: checker did not admit the expected auto-borrow")
				}
			}
			result.Borrows = slices.Clone(original)
			switch name {
			case "missing":
				result.Borrows = append(result.Borrows[:matching], result.Borrows[matching+1:]...)
			case "ambiguous":
				duplicate := original[matching]
				duplicate.ID += 1000
				result.Borrows = append(result.Borrows, duplicate)
			case "wrong_kind":
				result.Borrows[matching].Kind = BorrowMut
			case "reserved":
				result.Borrows[matching].Reserved = true
			case "wrong_from_expr":
				result.Borrows[matching].Life.FromExpr = call.Target
			}
			a := &returnOriginAnalyzer{ctx: t.Context(), functions: []*returnOriginFunction{callee},
				units:  []*returnOriginUnitIndex{index},
				bodies: map[string]*returnOriginFunction{callee.key: callee}, summaries: make(map[string]returnOriginSummaryFact), report: &ReturnOriginAnalysis{}}
			if err := a.solveBodies(); err != nil {
				t.Fatal(err)
			}
			if len(a.report.Pending) != 0 || len(a.report.Diagnostics) != 0 || !a.summaries[callee.key].value.equal(returnOriginValueOf(returnOrigin{kind: returnOriginParam, param: 0})) {
				t.Fatal("PRECONDITION: actual same body did not infer exactly formal source 0")
			}
			a.report = &ReturnOriginAnalysis{}
			b := &returnOriginBody{analyzer: a, function: caller}
			value := returnOriginValueOf()
			want := returnOrigin{kind: returnOriginLocal, binding: binding, scope: sym.Scope}
			if incoming {
				want = returnOrigin{kind: returnOriginParam, param: 1}
				if name == "expired_storage" {
					owner := caller.params[0]
					want = returnOrigin{kind: returnOriginLocal, binding: owner, scope: caller.scope, expired: true}
				}
				if name == "unknown_incoming" {
					want = returnOrigin{kind: returnOriginUnknown}
				}
				value = returnOriginValueOf(want)
			}
			env := newReturnOriginEnv().assign(binding, sym.Scope, value)
			out, err := b.call(callID, env, returnOriginTargets{scope: sym.Scope})
			encoded, marshalErr := json.Marshal(map[string]any{"case": name, "source": src, "source_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(src))),
				"call": callID, "argument": arg, "binding": binding, "original_borrows": original, "effective_borrows": result.Borrows, "analysis": a.report})
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			t.Logf("RETURN_ORIGIN_CALL_TRANSFER=%s value=%+v error=%v", encoded, out.value, err)
			if err != nil || !out.flow.normal.reachable || !out.value.normal {
				t.Fatalf("typed call transfer did not return normally: %v", err)
			}
			unproved := name == "missing" || name == "ambiguous" || name == "wrong_kind" || name == "reserved" || name == "wrong_from_expr" || name == "unknown_incoming"
			if unproved {
				if len(a.report.Pending) == 0 {
					t.Fatal("unproved auto-borrow unexpectedly completed")
				}
				requireReturnOriginRoot(t, out.value, returnOrigin{kind: returnOriginUnknown})
				return
			}
			if len(a.report.Pending) != 0 || !out.value.equal(returnOriginValueOf(want)) {
				t.Fatalf("argument lost current storage/content origin: got=%+v want=%+v pending=%+v", out.value, want, a.report.Pending)
			}
			if name == "expired_storage" {
				if len(a.report.Diagnostics) != 1 || a.report.Diagnostics[0].Code != diag.SemaBorrowEscapesReturn {
					t.Fatal("expired incoming origin was revived by the call")
				}
			} else if len(a.report.Diagnostics) != 0 {
				t.Fatal("call diagnosed an owner before it expired")
			}
		})
	}
}
