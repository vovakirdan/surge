package mir_test

import (
	"context"
	"sort"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/hir"
	"surge/internal/lexer"
	"surge/internal/mir"
	"surge/internal/mono"
	"surge/internal/parser"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// lowerForOwnership compiles a snippet all the way to MIR and hands back
// everything the ownership verifier needs, sema result included — without it,
// the owns-heap axis falls back to the interner's Copy bit and the fixtures
// would be testing a coarser question than the pass actually asks in a build.
func lowerForOwnership(t *testing.T, src string) (*mir.Module, *types.Interner, *sema.Result) {
	t.Helper()

	fs := source.NewFileSet()
	fileID := fs.AddVirtual("ownership_test.sg", []byte(src))
	file := fs.Get(fileID)

	sharedStrings := source.NewInterner()
	typeInterner := types.NewInterner()
	bag := diag.NewBag(100)
	lx := lexer.New(file, lexer.Options{})
	builder := ast.NewBuilder(ast.Hints{}, sharedStrings)

	parsed := parser.ParseFile(context.Background(), fs, lx, builder, parser.Options{
		Reporter:  &diag.BagReporter{Bag: bag},
		MaxErrors: 100,
	})
	requireNoDiagErrors(t, bag, "parse")

	symbolsRes := symbols.ResolveFile(builder, parsed.File, &symbols.ResolveOptions{
		Reporter:   &diag.BagReporter{Bag: bag},
		Validate:   true,
		ModulePath: "test",
		FilePath:   "ownership_test.sg",
	})
	requireNoDiagErrors(t, bag, "symbols")

	instMap := mono.NewInstantiationMap()
	semaRes := sema.Check(context.Background(), builder, parsed.File, sema.Options{
		Reporter:       &diag.BagReporter{Bag: bag},
		Symbols:        &symbolsRes,
		Types:          typeInterner,
		Instantiations: mono.NewInstantiationMapRecorder(instMap),
	})
	requireNoDiagErrors(t, bag, "sema")

	hirModule, err := hir.Lower(context.Background(), builder, parsed.File, &semaRes, &symbolsRes)
	if err != nil {
		t.Fatalf("hir lower: %v", err)
	}
	monoMod, err := mono.MonomorphizeModule(hirModule, instMap, &semaRes, mono.Options{})
	if err != nil {
		t.Fatalf("monomorphize: %v", err)
	}
	mirMod, err := mir.LowerModule(monoMod, &semaRes)
	if err != nil {
		t.Fatalf("mir lower: %v", err)
	}
	return mirMod, typeInterner, &semaRes
}

func requireNoDiagErrors(t *testing.T, bag *diag.Bag, stage string) {
	t.Helper()
	if !bag.HasErrors() {
		return
	}
	for _, d := range bag.Items() {
		t.Logf("%s: %v", stage, d)
	}
	t.Fatalf("%s reported errors", stage)
}

// findingsIn returns the findings raised inside one function, as stable
// strings.
func findingsIn(findings []mir.OwnershipFinding, fn string) []string {
	var out []string
	for _, f := range findings {
		if baseName(f.Function) != fn {
			continue
		}
		out = append(out, f.String())
	}
	sort.Strings(out)
	return out
}

func baseName(name string) string {
	if idx := strings.Index(name, "::<"); idx >= 0 {
		name = name[:idx]
	}
	if idx := strings.LastIndex(name, "::"); idx >= 0 {
		name = name[idx+2:]
	}
	return name
}

// ownershipEnv carries the real types and sema answers a hand-built fixture
// needs. They come from an actual compile rather than a bare interner because
// the owns-heap axis lives on sema's result: without it a `float` reads as
// Copy-and-therefore-unowned, and the reference-counted-scalar row — the exact
// axis RV2-DEBT-052's regression slipped through — would not be testable at
// all.
type ownershipEnv struct {
	typesIn *types.Interner
	semaRes *sema.Result

	slot    types.TypeID // an owning tagged union
	slotRef types.TypeID // &Slot
	str     types.TypeID
	flt     types.TypeID
	fltRef  types.TypeID
	cell    types.TypeID // a @copy value composite
	cellRef types.TypeID
	dup     types.TypeID // a @copy union: the scrutineeDuplicated shape
	dupRef  types.TypeID
	boolTy  types.TypeID
	holder  types.TypeID // a struct with a reference-counted-scalar field
	strMap  types.TypeID // Map<string, string>, whose ELEMENT the lowering's own
	// place walk deliberately does not resolve
}

const ownershipEnvSource = `
tag Payload(string);
tag Empty();
type Slot = Payload(string) | Empty;

@copy type Cell = { a: int, b: int };
tag Held(Cell);
tag Absent();
@copy type Duplicable = Held(Cell) | Absent;

type Holder = { v: float };

fn shapes(slot: Slot, borrowed: &Slot, s: string, f: float, fr: &float, cell: Cell, cr: &Cell, dup: Duplicable, dr: &Duplicable, holder: Holder, entries: Map<string, string>) -> int {
    return 0;
}
`

func newOwnershipEnv(t *testing.T) *ownershipEnv {
	t.Helper()
	mod, typesIn, semaRes := lowerForOwnership(t, ownershipEnvSource)
	var shapes *mir.Func
	for _, id := range mod.SortedFuncIDs() {
		if f := mod.Funcs[id]; f != nil && baseName(f.Name) == "shapes" {
			shapes = f
		}
	}
	if shapes == nil || len(shapes.Locals) < 11 {
		t.Fatalf("fixture env: `shapes` did not lower with its parameters")
	}
	return &ownershipEnv{
		typesIn: typesIn,
		semaRes: semaRes,
		boolTy:  typesIn.Builtins().Bool,
		slot:    shapes.Locals[0].Type,
		slotRef: shapes.Locals[1].Type,
		str:     shapes.Locals[2].Type,
		flt:     shapes.Locals[3].Type,
		fltRef:  shapes.Locals[4].Type,
		cell:    shapes.Locals[5].Type,
		cellRef: shapes.Locals[6].Type,
		dup:     shapes.Locals[7].Type,
		dupRef:  shapes.Locals[8].Type,
		holder:  shapes.Locals[9].Type,
		strMap:  shapes.Locals[10].Type,
	}
}

// verify runs the pass over a single hand-built function.
func (e *ownershipEnv) verify(f *mir.Func) []mir.OwnershipFinding {
	mod := &mir.Module{Funcs: map[mir.FuncID]*mir.Func{f.ID: f}}
	return mir.VerifyOwnership(mod, e.typesIn, e.semaRes)
}

// fnBuilder assembles a minimal mir.Func. Parameters must be declared before
// any other local, which is the invariant ParamCount encodes.
type fnBuilder struct {
	f *mir.Func
}

func newFn(name string) *fnBuilder {
	return &fnBuilder{f: &mir.Func{ID: 0, Name: name, Result: types.NoTypeID, Entry: 0}}
}

func (b *fnBuilder) param(name string, ty types.TypeID, ownsHeap bool) mir.LocalID {
	if len(b.f.Locals) != b.f.ParamCount {
		panic("fixture: parameters must be declared before ordinary locals")
	}
	b.f.ParamCount++
	return b.local(name, ty, ownsHeap)
}

func (b *fnBuilder) local(name string, ty types.TypeID, ownsHeap bool) mir.LocalID {
	var flags mir.LocalFlags
	if ownsHeap {
		flags |= mir.LocalFlagOwnsHeap
	}
	b.f.Locals = append(b.f.Locals, mir.Local{Name: name, Type: ty, Flags: flags})
	return mir.LocalID(len(b.f.Locals) - 1)
}

func (b *fnBuilder) block(instrs []mir.Instr, term mir.Terminator) mir.BlockID {
	id := mir.BlockID(len(b.f.Blocks))
	b.f.Blocks = append(b.f.Blocks, mir.Block{ID: id, Instrs: instrs, Term: term})
	return id
}

func (b *fnBuilder) setTerm(id mir.BlockID, term mir.Terminator) {
	b.f.Blocks[id].Term = term
}

func (b *fnBuilder) done() *mir.Func { return b.f }

func place(l mir.LocalID) mir.Place { return mir.Place{Kind: mir.PlaceLocal, Local: l} }

func opCopy(l mir.LocalID, ty types.TypeID) mir.Operand {
	return mir.Operand{Kind: mir.OperandCopy, Type: ty, Place: place(l)}
}

func opMove(l mir.LocalID, ty types.TypeID) mir.Operand {
	return mir.Operand{Kind: mir.OperandMove, Type: ty, Place: place(l)}
}

func opRetain(l mir.LocalID, ty types.TypeID) mir.Operand {
	return mir.Operand{Kind: mir.OperandRetain, Type: ty, Place: place(l)}
}

func opCopyValue(l mir.LocalID, ty types.TypeID) mir.Operand {
	return mir.Operand{Kind: mir.OperandCopyValue, Type: ty, Place: place(l)}
}

func opStr(ty types.TypeID) mir.Operand {
	return mir.Operand{
		Kind:  mir.OperandConst,
		Type:  ty,
		Const: mir.Const{Kind: mir.ConstString, Type: ty, StringValue: "x"},
	}
}

func opBool(ty types.TypeID, value bool) mir.Operand {
	return mir.Operand{
		Kind:  mir.OperandConst,
		Type:  ty,
		Const: mir.Const{Kind: mir.ConstBool, Type: ty, BoolValue: value},
	}
}

func useRV(op mir.Operand) mir.RValue { return mir.RValue{Kind: mir.RValueUse, Use: op} }

func assign(dst mir.LocalID, rv mir.RValue) mir.Instr {
	return mir.Instr{Kind: mir.InstrAssign, Assign: mir.AssignInstr{Dst: place(dst), Src: rv}}
}

func assignTo(dst mir.Place, rv mir.RValue) mir.Instr {
	return mir.Instr{Kind: mir.InstrAssign, Assign: mir.AssignInstr{Dst: dst, Src: rv}}
}

func dropL(l mir.LocalID) mir.Instr {
	return mir.Instr{Kind: mir.InstrDrop, Drop: mir.DropInstr{Place: place(l)}}
}

func retTerm() mir.Terminator { return mir.Terminator{Kind: mir.TermReturn} }

func gotoTerm(target mir.BlockID) mir.Terminator {
	return mir.Terminator{Kind: mir.TermGoto, Goto: mir.GotoTerm{Target: target}}
}

func ifTerm(cond mir.Operand, then, els mir.BlockID) mir.Terminator {
	return mir.Terminator{Kind: mir.TermIf, If: mir.IfTerm{Cond: cond, Then: then, Else: els}}
}

// derefRV reads through a reference — ALIASES, because the owner on the other
// side keeps its own reference and knows nothing about this one.
func derefRV(src mir.Operand) mir.RValue {
	return mir.RValue{Kind: mir.RValueUnaryOp, Unary: mir.UnaryOp{Op: ast.ExprUnaryDeref, Operand: src}}
}

// identityCastRV converts nothing: every backend hands the source value
// straight back, which is RV2-DEBT-097's exact shape.
func identityCastRV(src mir.Operand, target types.TypeID) mir.RValue {
	return mir.RValue{Kind: mir.RValueCast, Cast: mir.CastOp{Value: src, TargetTy: target}}
}

func tagPayloadRV(subject mir.Operand, tag string, moveOut bool) mir.RValue {
	return mir.RValue{
		Kind:       mir.RValueTagPayload,
		TagPayload: mir.TagPayload{Value: subject, TagName: tag, Index: 0, MoveOut: moveOut},
	}
}

func requireFindings(t *testing.T, got []mir.OwnershipFinding, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %d finding(s), got %d:\n%s", len(want), len(got), formatFindings(got))
	}
	for i, w := range want {
		if got[i].String() != w {
			t.Fatalf("finding %d:\n  got  %s\n  want %s", i, got[i], w)
		}
	}
}

func requireClean(t *testing.T, got []mir.OwnershipFinding) {
	t.Helper()
	if len(got) != 0 {
		t.Fatalf("expected no findings, got %d:\n%s", len(got), formatFindings(got))
	}
}

func formatFindings(fs []mir.OwnershipFinding) string {
	var b strings.Builder
	for _, f := range fs {
		b.WriteString("  ")
		b.WriteString(f.String())
		b.WriteString("\n")
	}
	return b.String()
}
