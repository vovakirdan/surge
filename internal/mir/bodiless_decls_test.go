package mir_test

import (
	"context"
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

// lowerMIRPastBodyless lowers a program sema refuses only for a function
// declared without a body, so the backend's own refusal can be read: sema
// is the first line, and the MIR set is what keeps a backend from running a
// builtin that shares such a declaration's name.
func lowerMIRPastBodyless(t *testing.T, src string) *mir.Module {
	t.Helper()
	fs := source.NewFileSet()
	fileID := fs.AddVirtual("test.sg", []byte(src))
	file := fs.Get(fileID)
	strs := source.NewInterner()
	typeInterner := types.NewInterner()
	bag := diag.NewBag(100)
	builder := ast.NewBuilder(ast.Hints{}, strs)
	result := parser.ParseFile(context.Background(), fs, lexer.New(file, lexer.Options{}), builder,
		parser.Options{Reporter: &diag.BagReporter{Bag: bag}, MaxErrors: 100})
	if bag.HasErrors() {
		t.Fatalf("PRECONDITION: parse errors: %v", bag.Items())
	}
	symbolsRes := symbols.ResolveFile(builder, result.File, &symbols.ResolveOptions{
		Reporter: &diag.BagReporter{Bag: bag}, Validate: true, ModulePath: "test", FilePath: "test.sg",
	})
	instMap := mono.NewInstantiationMap()
	semaRes := sema.Check(context.Background(), builder, result.File, sema.Options{
		Reporter: &diag.BagReporter{Bag: bag}, Symbols: &symbolsRes, Types: typeInterner,
		Instantiations: mono.NewInstantiationMapRecorder(instMap),
	})
	for _, d := range bag.Items() {
		if d.Severity >= diag.SevError && d.Code != diag.SemaBodylessFunction {
			t.Fatalf("PRECONDITION: unrelated error %s: %s", d.Code.ID(), d.Message)
		}
	}
	finalizeTestInstantiationClosure(t, typeInterner, &symbolsRes, &semaRes)
	hirModule, err := hir.Lower(context.Background(), builder, result.File, &semaRes, &symbolsRes)
	if err != nil {
		t.Fatalf("PRECONDITION: HIR: %v", err)
	}
	monoMod, err := mono.MonomorphizeModule(hirModule, instMap, &semaRes, mono.Options{})
	if err != nil {
		t.Fatalf("PRECONDITION: mono: %v", err)
	}
	mirMod, err := mir.LowerModule(monoMod, &semaRes)
	if err != nil {
		t.Fatalf("PRECONDITION: MIR: %v", err)
	}
	return mirMod
}

// calleeNames returns, for each direct call in main, the callee's name and
// whether the module marks its symbol as a body-less declaration.
func calleeNames(t *testing.T, m *mir.Module) map[string]bool {
	t.Helper()
	out := make(map[string]bool)
	for _, fn := range m.Funcs {
		if fn == nil || fn.Name != "main" {
			continue
		}
		for _, bb := range fn.Blocks {
			for i := range bb.Instrs {
				ins := &bb.Instrs[i]
				if ins.Kind != mir.InstrCall || ins.Call.Callee.Kind != mir.CalleeSym {
					continue
				}
				_, bodiless := m.BodilessDecls[ins.Call.Callee.Sym]
				out[ins.Call.Callee.Name] = out[ins.Call.Callee.Name] || bodiless
			}
		}
	}
	return out
}

func TestBodilessDeclarationCallsAreMarkedForTheBackends(t *testing.T) {
	for _, tc := range []struct {
		name, src, callee string
	}{
		{"plain_function", `fn encode(x: int) -> int;
fn main() -> int { return encode(3); }
`, "encode"},
		{"extern_method_named_like_an_intrinsic", `type Box = { value: int };
extern<Box> {
    fn __len(self: &Box) -> uint;
}
fn main() -> int {
    let b = Box { value = 1 };
    let n: uint = b.__len();
    return 0;
}
`, "__len"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := calleeNames(t, lowerMIRPastBodyless(t, tc.src))
			if bodiless, called := calls[tc.callee]; !called || !bodiless {
				t.Fatalf("call of %s not marked body-less (called=%v): %v", tc.callee, called, calls)
			}
		})
	}
}

// Controls: a forward declaration completed by @override, and a call through
// a function value of a function-type alias, reach code with a body.
func TestBodilessDeclarationMarksNoCompletedOrValueCall(t *testing.T) {
	for _, tc := range []struct {
		name, src string
	}{
		{"forward_declaration_with_override", `fn encode(x: int) -> int;
@override
fn encode(x: int) -> int { return x + 40; }
fn main() -> int { return encode(2); }
`},
		{"function_type_alias_value", `type Foo = fn(int, int) -> int;
fn foo(a: int, b: int) -> int { return a + b; }
fn bar(a: Foo) -> int { return a(1, 2); }
fn main() -> int { return bar(foo); }
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for name, bodiless := range calleeNames(t, lowerMIRPastBodyless(t, tc.src)) {
				if bodiless {
					t.Fatalf("call of %s is marked body-less", name)
				}
			}
		})
	}
}
