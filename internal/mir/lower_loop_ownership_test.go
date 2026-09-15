package mir

import (
	"testing"

	"surge/internal/ast"
	"surge/internal/hir"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func loopOwnershipLowerer(in *types.Interner) *funcLowerer {
	l := &funcLowerer{
		types: in, sema: &sema.Result{TypeInterner: in},
		symToLocal: make(map[symbols.SymbolID]LocalID), nextTemp: 1,
		pendingReleaseGuard: NoLocalID,
		f:                   &Func{Name: "loop_ownership", Result: in.Builtins().Nothing},
	}
	l.cur = l.newBlock()
	l.f.Entry = l.cur
	return l
}

func loopOwnershipRef(name string, sym symbols.SymbolID, ty types.TypeID) *hir.Expr {
	return &hir.Expr{Kind: hir.ExprVarRef, Type: ty, Data: hir.VarRefData{Name: name, SymbolID: sym}}
}

func loopOwnershipLet(name string, sym symbols.SymbolID, ty types.TypeID, value *hir.Expr) hir.Stmt {
	return hir.Stmt{Kind: hir.StmtLet, Data: hir.LetData{
		Name: name, SymbolID: sym, Type: ty, Value: value, GeneratedDrop: hir.GeneratedDropCountedScalar,
	}}
}

func loopOwnershipFloat(ty types.TypeID, value float64) *hir.Expr {
	return &hir.Expr{Kind: hir.ExprLiteral, Type: ty, Data: hir.LiteralData{Kind: hir.LiteralFloat, FloatValue: value}}
}

func TestGeneratedLoopOwnershipUsesActualType(t *testing.T) {
	in := types.NewInterner()
	in.Strings = source.NewInterner()
	f := in.Builtins().Float
	f32 := in.Intern(types.Type{Kind: types.KindFloat, Width: types.Width32})
	rangeTy := in.RegisterStructInstance(in.Strings.Intern("Range"), source.Span{}, []types.TypeID{f})
	arrayName := in.Strings.Intern("Array")
	in.EnsureArrayNominal(arrayName, in.Strings.Intern("T"), source.Span{}, 1)
	arrayTy := in.RegisterStructInstance(arrayName, source.Span{}, []types.TypeID{f})
	fixedName := in.Strings.Intern("ArrayFixed")
	in.EnsureArrayFixedNominal(fixedName, in.Strings.Intern("E"), in.Strings.Intern("N"), source.Span{}, 2, in.Builtins().Uint)
	fixedTy := in.RegisterStructInstanceWithValues(fixedName, source.Span{}, []types.TypeID{f}, []uint64{2})
	alias := in.RegisterAlias(in.Strings.Intern("Floats"), source.Span{})
	in.SetAliasTarget(alias, arrayTy)
	floatAlias := in.RegisterAlias(in.Strings.Intern("Number"), source.Span{})
	in.SetAliasTarget(floatAlias, f)
	ref := in.Intern(types.Type{Kind: types.KindReference, Elem: rangeTy})
	ptr := in.Intern(types.Type{Kind: types.KindPointer, Elem: arrayTy})
	task := in.RegisterStructInstance(in.Strings.Intern("Task"), source.Span{}, []types.TypeID{f})
	composite := in.RegisterTuple([]types.TypeID{f})
	for _, tc := range []struct {
		name           string
		ty             types.TypeID
		kind           hir.GeneratedDropKind
		want, resource bool
	}{
		{"float_scalar", f, hir.GeneratedDropCountedScalar, true, false},
		{"alias_scalar", floatAlias, hir.GeneratedDropCountedScalar, true, false},
		{"fixed_scalar", f32, hir.GeneratedDropCountedScalar, false, false},
		{"unmarked_scalar", f, hir.GeneratedDropNone, false, false},
		{"range", rangeTy, hir.GeneratedDropNumericIterableResource, true, true},
		{"array", arrayTy, hir.GeneratedDropNumericIterableResource, true, true},
		{"fixed_array", fixedTy, hir.GeneratedDropNumericIterableResource, true, true},
		{"alias_array", alias, hir.GeneratedDropNumericIterableResource, true, true},
		{"builtin_array", in.Intern(types.MakeArray(f, 2)), hir.GeneratedDropNumericIterableResource, true, true},
		{"reference", ref, hir.GeneratedDropNumericIterableResource, false, false},
		{"pointer", ptr, hir.GeneratedDropNumericIterableResource, false, false},
		{"fixed_elements", in.Intern(types.MakeArray(f32, 2)), hir.GeneratedDropNumericIterableResource, false, false},
		{"task", task, hir.GeneratedDropNumericIterableResource, false, false},
		{"composite", composite, hir.GeneratedDropCountedScalar, false, false},
		{"wrong_candidate", arrayTy, hir.GeneratedDropCountedScalar, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := loopOwnershipLowerer(in)
			l.f.Locals = []Local{{Name: "actual", Type: tc.ty}}
			l.pushTempDropFrame()
			l.pushTempDropFrame()
			if err := l.registerGeneratedLoopLocal(0, tc.kind); err != nil {
				t.Fatal(err)
			}
			if got := len(l.tempDropFrames[0]) == 1; got != tc.want || len(l.tempDropFrames[1]) != 0 {
				t.Fatalf("lexical ownership = %v, want %v; frames=%+v", got, tc.want, l.tempDropFrames)
			}
			if got := l.registeredNumericResource(Place{Local: 0}); got != tc.resource {
				t.Fatalf("registered resource = %v, want %v", got, tc.resource)
			}
		})
	}
}

func TestGeneratedResourceOnlyReplacesLegacyWholeLocalRelease(t *testing.T) {
	in := types.NewInterner()
	in.Strings = source.NewInterner()
	f := in.Builtins().Float
	rangeTy := in.RegisterStructInstance(in.Strings.Intern("Range"), source.Span{}, []types.TypeID{f})
	in.MarkRuntimeHandleType(rangeTy)
	for _, tc := range []struct {
		name                             string
		cursor, scalar, caller, residual bool
		want                             int
	}{
		{"cursor", true, false, false, false, 0},
		{"whole_resource", false, false, false, false, 0},
		{"scalar", false, true, false, false, 1},
		{"caller", false, false, true, false, 1},
		{"residual_resource", false, false, false, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := loopOwnershipLowerer(in)
			ty := rangeTy
			if tc.scalar {
				ty = f
			}
			l.f.Locals = []Local{{Name: "value", Type: ty}}
			l.symToLocal[1] = 0
			l.tempDropFrames = [][]tempDropEntry{{{local: 0, numericResource: !tc.scalar && !tc.caller}}}
			value := loopOwnershipRef("value", 1, ty)
			st := hir.Stmt{Kind: hir.StmtDrop, Data: hir.DropData{Value: value}}
			if tc.cursor {
				st = hir.Stmt{Kind: hir.StmtEnvelopeRelease, Data: hir.EnvelopeReleaseData{Value: value, Cursor: true}}
			}
			if tc.residual {
				st.Data = hir.DropData{Value: value, Steps: []sema.DropStep{{Shallow: true}}}
			}
			if err := l.lowerStmt(&st); err != nil {
				t.Fatal(err)
			}
			if got := len(l.curBlock().Instrs); got != tc.want {
				t.Fatalf("legacy release instructions = %d, want %d", got, tc.want)
			}
			if l.registeredNumericResource(Place{Local: 0, Proj: []PlaceProj{{Kind: PlaceProjField, FieldIdx: 0}}}) ||
				l.registeredNumericResource(Place{Kind: PlaceGlobal, Global: 0}) {
				t.Fatal("resource membership escaped the exact whole local")
			}
		})
	}
}

func TestNumericPostActualOwnershipPreservesReferences(t *testing.T) {
	in := types.NewInterner()
	in.Strings = source.NewInterner()
	f := in.Builtins().Float
	ref := in.Intern(types.Type{Kind: types.KindReference, Elem: f})
	own := in.Intern(types.Type{Kind: types.KindOwn, Elem: f})
	alias := func(name string, target types.TypeID) types.TypeID {
		id := in.RegisterAlias(in.Strings.Intern(name), source.Span{})
		in.SetAliasTarget(id, target)
		return id
	}
	for _, tc := range []struct {
		name string
		ty   types.TypeID
		want bool
	}{
		{"float", f, true},
		{"alias_float", alias("Number", f), true},
		{"own_float", own, true},
		{"alias_own_float", alias("OwnedNumber", own), true},
		{"reference", ref, false},
		{"own_reference", in.Intern(types.Type{Kind: types.KindOwn, Elem: ref}), false},
		{"alias_reference", alias("NumberRef", ref), false},
		{"pointer", in.Intern(types.Type{Kind: types.KindPointer, Elem: f}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := loopOwnershipLowerer(in)
			original := &hir.Expr{Kind: hir.ExprBinaryOp, Type: tc.ty, Data: hir.BinaryOpData{
				Op: ast.ExprBinaryAssign, Left: loopOwnershipRef("current", 1, tc.ty), DropOverwritten: !tc.want,
			}}
			post := l.numericLoopPost(original)
			if post == original || post.Data.(hir.BinaryOpData).DropOverwritten != tc.want ||
				original.Data.(hir.BinaryOpData).DropOverwritten != !tc.want {
				t.Fatal("concrete latch ownership is wrong or mutated its source HIR")
			}
		})
	}
}
