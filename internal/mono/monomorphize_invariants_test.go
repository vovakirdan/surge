package mono

import (
	"fmt"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/hir"
	"surge/internal/sema"
	"surge/internal/types"
)

func TestMonoHasNoTypeParams(t *testing.T) {
	src := `
fn id<T>(x: T) -> T { return x; }

fn wrap<T>(x: T) -> T {
  return id(x);
}

fn main() {
  let a = wrap(1);
  let b = wrap("x");
}
`

	mm, typesIn, err := compileAndMonomorphize(t, src)
	if err != nil {
		t.Fatalf("failed to monomorphize: %v", err)
	}
	if err := validateMonoModuleNoTypeParams(mm, typesIn); err != nil {
		t.Fatalf("mono contains type params: %v", err)
	}
	if got, want := len(mm.Funcs), 5; got != want {
		t.Fatalf("unexpected mono func count: got=%d want=%d", got, want)
	}
}

func TestMonoPreservesCrossingRepresentation(t *testing.T) {
	src := `
tag Success<T>(T);
tag Cancelled();
type TaskResult<T> = Success(T) | Cancelled;
@intrinsic @copy
type Placement = { __opaque: int };

fn cross<T>(dst: Placement, value: int) -> TaskResult<int> {
  return on dst {
    ret value;
  };
}

fn main(dst: Placement) -> TaskResult<int> {
  return cross::<int>(dst, 7);
}
`
	mm, typesIn, err := compileAndMonomorphizeWithLowerOptions(t, src, hir.LowerOptions{
		CrossingForms: map[sema.CrossingLoweringKind]bool{
			sema.CrossingLoweringOnPlacement: true,
		},
	})
	if err != nil {
		t.Fatalf("failed to monomorphize: %v", err)
	}
	if err := validateMonoModuleNoTypeParams(mm, typesIn); err != nil {
		t.Fatalf("mono contains type params: %v", err)
	}
	data := requireMonoCrossing(t, mm, sema.CrossingLoweringOnPlacement)
	if got := types.Label(typesIn, data.PayloadType); got != "int" {
		t.Fatalf("payload type = %q, want int", got)
	}
	if got := types.Label(typesIn, data.ResultType); got != "TaskResult<int>" {
		t.Fatalf("result type = %q, want TaskResult<int>", got)
	}
	if len(data.Captures) != 1 {
		t.Fatalf("captures = %d, want 1", len(data.Captures))
	}
	if got := types.Label(typesIn, data.Captures[0].Type); got != "int" {
		t.Fatalf("capture type = %q, want int", got)
	}
	if data.Captures[0].Value == nil || data.Captures[0].Value.Type == types.NoTypeID {
		t.Fatalf("capture value was not preserved through mono: %+v", data.Captures[0].Value)
	}
}

func TestCloneCrossingRepresentationDoesNotAliasSlices(t *testing.T) {
	captureValue := &hir.Expr{
		Kind: hir.ExprLiteral,
		Type: types.TypeID(1),
		Data: hir.LiteralData{Kind: hir.LiteralInt, Text: "1", IntValue: 1},
	}
	remoteReceiver := &hir.Expr{
		Kind: hir.ExprVarRef,
		Type: types.TypeID(2),
		Data: hir.VarRefData{Name: "conn"},
	}
	original := &hir.Expr{
		Kind: hir.ExprCrossing,
		Type: types.TypeID(3),
		Data: hir.CrossingData{
			Kind: sema.CrossingLoweringOnFarHandle,
			Captures: []hir.CrossingCapture{{
				Type:  types.TypeID(4),
				Value: captureValue,
			}},
			RemoteOps: []hir.CrossingRemoteOp{{
				ReceiverType: types.TypeID(5),
				Receiver:     remoteReceiver,
			}},
		},
	}

	cloned := cloneExpr(original)
	origData := original.Data.(hir.CrossingData)
	cloneData := cloned.Data.(hir.CrossingData)
	if origData.Captures[0].Value != captureValue {
		t.Fatalf("clone mutated original capture value")
	}
	if origData.RemoteOps[0].Receiver != remoteReceiver {
		t.Fatalf("clone mutated original remote-op receiver")
	}
	if cloneData.Captures[0].Value == captureValue {
		t.Fatalf("clone reused capture expression pointer")
	}
	if cloneData.RemoteOps[0].Receiver == remoteReceiver {
		t.Fatalf("clone reused remote-op receiver expression pointer")
	}

	cloneData.Captures[0].Type = types.TypeID(44)
	cloneData.RemoteOps[0].ReceiverType = types.TypeID(55)
	if origData.Captures[0].Type == cloneData.Captures[0].Type {
		t.Fatalf("clone capture slice aliases original")
	}
	if origData.RemoteOps[0].ReceiverType == cloneData.RemoteOps[0].ReceiverType {
		t.Fatalf("clone remote-op slice aliases original")
	}
}

func requireMonoCrossing(t *testing.T, mm *MonoModule, kind sema.CrossingLoweringKind) hir.CrossingData {
	t.Helper()
	for _, mf := range mm.Funcs {
		if mf == nil || mf.Func == nil || mf.Func.Body == nil {
			continue
		}
		if data, ok := findCrossingInBlock(mf.Func.Body, kind); ok {
			return data
		}
	}
	t.Fatalf("missing mono crossing kind %d", kind)
	return hir.CrossingData{}
}

func findCrossingInBlock(block *hir.Block, kind sema.CrossingLoweringKind) (hir.CrossingData, bool) {
	if block == nil {
		return hir.CrossingData{}, false
	}
	for _, stmt := range block.Stmts {
		switch data := stmt.Data.(type) {
		case hir.ReturnData:
			if out, ok := findCrossingInExpr(data.Value, kind); ok {
				return out, true
			}
		case hir.ExprStmtData:
			if out, ok := findCrossingInExpr(data.Expr, kind); ok {
				return out, true
			}
		case hir.LetData:
			if out, ok := findCrossingInExpr(data.Value, kind); ok {
				return out, true
			}
		}
	}
	return hir.CrossingData{}, false
}

func findCrossingInExpr(expr *hir.Expr, kind sema.CrossingLoweringKind) (hir.CrossingData, bool) {
	if expr == nil {
		return hir.CrossingData{}, false
	}
	if expr.Kind == hir.ExprCrossing {
		data := expr.Data.(hir.CrossingData)
		if data.Kind == kind {
			return data, true
		}
	}
	switch data := expr.Data.(type) {
	case hir.CallData:
		if out, ok := findCrossingInExpr(data.Callee, kind); ok {
			return out, true
		}
		for _, arg := range data.Args {
			if out, ok := findCrossingInExpr(arg, kind); ok {
				return out, true
			}
		}
	case hir.BlockExprData:
		return findCrossingInBlock(data.Block, kind)
	}
	return hir.CrossingData{}, false
}

func monoDiagSummary(bag *diag.Bag) string {
	if bag == nil || bag.Len() == 0 {
		return "(none)"
	}
	var b strings.Builder
	for _, d := range bag.Items() {
		if b.Len() > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "%s: %s", d.Code.ID(), d.Message)
	}
	return b.String()
}
