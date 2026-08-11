package llvm

import (
	"testing"

	"surge/internal/driver"
	"surge/internal/mir"
	"surge/internal/types"
)

// One callee, two ways of reaching its signature, one answer.
//
// A direct call resolves the definition it names and reads that definition's
// classification. A call through a function value never sees the definition; it
// has only the callee's TYPE. If those two routes could disagree about which
// result travels through a hidden caller-owned destination, or which argument
// travels by address, the emitted IR would verify cleanly and then have caller
// and callee reading different argument positions — a miscompile with no
// symptom at build time.
//
// The check is worth making now rather than after the destination protocol
// lands, because after it lands a disagreement is a wrong program and before it
// lands it is only a wrong table.
func TestDefinitionAndTypeReachTheSameCallContract(t *testing.T) {
	sources := map[string]string{
		"composite results and arguments": `
@copy type Leaf = { x: int, y: int }
type Holder = { name: string, leaf: Leaf }

fn make_leaf(x: int, y: int) -> Leaf { return Leaf{ x: x, y: y }; }
fn take_leaf(l: Leaf) -> int { return l.x + l.y; }
fn make_holder(name: string) -> Holder {
	return Holder{ name: name, leaf: Leaf{ x: 1, y: 2 } };
}
fn holder_sum(h: Holder) -> int { return h.leaf.x + h.leaf.y; }

@entrypoint
fn main() -> int {
	let l: Leaf = make_leaf(3, 4);
	let h: Holder = make_holder("h");
	return take_leaf(l) + holder_sum(h);
}
`,
		"the same signatures through function values": `
@copy type Leaf = { x: int, y: int }

fn make_leaf(x: int, y: int) -> Leaf { return Leaf{ x: x, y: y }; }
fn take_leaf(l: Leaf) -> int { return l.x + l.y; }

@entrypoint
fn main() -> int {
	let make: fn(int, int) -> Leaf = make_leaf;
	let take: fn(Leaf) -> int = take_leaf;
	return take(make(3, 4));
}
`,
		"zero-sized and scalar results": `
type Empty = { }

fn nothing_at_all() -> Empty { return Empty { }; }
fn scalar(n: int) -> int { return n + 1; }
fn takes_empty(e: Empty) -> int { return 7; }

@entrypoint
fn main() -> int {
	let e: Empty = nothing_at_all();
	return scalar(takes_empty(e));
}
`,
		"tuple, union and fixed-array members": `
@copy type Leaf = { x: int, y: int }
tag Wrapped(Leaf);
tag Bare();
type Choice = Wrapped(Leaf) | Bare;
type Nested = { pair: (int, int), choice: Choice, cells: Leaf[3] }

fn build() -> Nested {
	return Nested{
		pair: (1, 2),
		choice: Wrapped(Leaf{ x: 3, y: 4 }),
		cells: [Leaf{x=1,y=1}, Leaf{x=2,y=2}, Leaf{x=3,y=3}],
	};
}
fn consume(n: Nested) -> int { return n.cells[0].x; }

@entrypoint
fn main() -> int {
	let n: Nested = build();
	return consume(n);
}
`,
	}

	for name, src := range sources {
		t.Run(name, func(t *testing.T) {
			e := prepareEmitterForTest(t, src)
			compared := 0
			composites := 0
			for _, id := range e.mod.SortedFuncIDs() {
				fn := e.mod.Funcs[id]
				if fn == nil {
					continue
				}
				fromDefinition, ok := e.funcSigs[mir.FuncID(id)]
				if !ok {
					t.Fatalf("%s: no prepared signature", fn.Name)
				}
				// The type a function VALUE of this callee would have. The
				// interner hands back the existing entry for a signature it
				// already holds, so this is the same type an indirect call site
				// carries rather than a fresh lookalike.
				fnType := e.types.RegisterFn(fromDefinition.paramTypes, definitionResultType(&fromDefinition))
				fromType, err := e.surgeABIForType(fnType)
				if err != nil {
					t.Fatalf("%s: classifying by type: %v", fn.Name, err)
				}
				assertSameABI(t, fn.Name, fromDefinition.abi, fromType)
				compared++
				composites += countComposites(&fromDefinition.abi)
			}
			// A pass over nothing is not a pass, and a pass over nothing but
			// scalars would not have asked the question either.
			if compared == 0 {
				t.Fatalf("no function was reachable both ways; the check proved nothing")
			}
			if composites == 0 {
				t.Fatalf("no composite was classified across %d signatures; the fixture asks nothing", compared)
			}
		})
	}
}

// definitionResultType is the result type the definition site classified, which
// is the declared one unless lowering left the declaration behind and the
// emitter had to read it off a returned operand.
func definitionResultType(sig *funcSig) types.TypeID {
	return sig.abi.Ret.Type
}

// countComposites reports how many members of a signature travel under the
// composite parts of the contract, so a fixture that exercises none says so.
func countComposites(abi *mir.SurgeABI) int {
	n := 0
	if abi.Ret.Class == mir.RetSret {
		n++
	}
	for i := range abi.Params {
		if abi.Params[i].Class == mir.ParamByval {
			n++
		}
	}
	return n
}

func assertSameABI(t *testing.T, fnName string, fromDefinition, fromType mir.SurgeABI) {
	t.Helper()
	if fromDefinition.Ret != fromType.Ret {
		t.Errorf("%s: result classified as %+v from its definition and %+v from its type",
			fnName, fromDefinition.Ret, fromType.Ret)
	}
	if len(fromDefinition.Params) != len(fromType.Params) {
		t.Fatalf("%s: %d parameters from its definition, %d from its type",
			fnName, len(fromDefinition.Params), len(fromType.Params))
	}
	for i := range fromDefinition.Params {
		if fromDefinition.Params[i] != fromType.Params[i] {
			t.Errorf("%s: parameter %d classified as %+v from its definition and %+v from its type",
				fnName, i, fromDefinition.Params[i], fromType.Params[i])
		}
	}
}

// prepareEmitterForTest runs the signature preparation EmitModule runs, and
// stops there: the question is what the signatures say, not what was emitted.
func prepareEmitterForTest(t *testing.T, src string) *Emitter {
	t.Helper()
	e, _ := prepareEmitterAndResultForTest(t, src)
	return e
}

// prepareEmitterAndResultForTest hands back the semantic result beside the
// emitter, for the tests that have to ask sema and the backend about ONE
// compilation of one program.
func prepareEmitterAndResultForTest(t *testing.T, src string) (*Emitter, *driver.DiagnoseResult) {
	t.Helper()
	mirMod, result := lowerMIRFromSource(t, src)
	e := &Emitter{
		mod:          mirMod,
		types:        result.Sema.TypeInterner,
		syms:         result.Symbols.Table,
		stringConsts: make(map[string]*stringConst),
		fnRefs:       make(map[mir.FuncID]struct{}),
		funcNames:    make(map[mir.FuncID]string),
		funcSigs:     make(map[mir.FuncID]funcSig),
		globalNames:  make(map[mir.GlobalID]string),
		runtimeSigs:  runtimeSigMap(),
	}
	if err := e.prepareGlobals(); err != nil {
		t.Fatalf("prepare globals: %v", err)
	}
	if err := e.collectParamCounts(); err != nil {
		t.Fatalf("collect param counts: %v", err)
	}
	if err := e.prepareFunctions(); err != nil {
		t.Fatalf("prepare functions: %v", err)
	}
	return e, result
}
