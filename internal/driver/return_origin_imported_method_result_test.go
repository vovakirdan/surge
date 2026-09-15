package driver

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"surge/internal/ast"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// A core intrinsic method whose result names its receiver parameter is typed inside a dependency
// template, and at a concrete receiver its call keeps the type its annotated binding names.
type importedMethodCall struct {
	start, end int
	snippet    string
	binding    string // a concrete source's binding whose type the call must have; "" only requires a type
}

type importedMethodSource struct {
	name, digest, text string
	result             string // a template source's result family: Option or Task
	calls              []importedMethodCall
}

func importedMethodSources() []importedMethodSource {
	return []importedMethodSource{
		{name: "range_next_twice", result: "Option", digest: "a1c0b6120a7f4d9f726a4296d65d51f94ec5652695666c03a5cbe72f8a410dfb", text: `pragma module::dep;
fn step<T>(r: Range<T>) -> Option<T> {
    let mut iter: Range<T> = r;
    let _ = iter.next();
    return iter.next();
}
`, calls: []importedMethodCall{{103, 114, "iter.next()", ""}, {127, 138, "iter.next()", ""}}},
		{name: "task_clone", result: "Task", digest: "3c04534f03fbfcf30a8a66e2f61682254d023724b02fe004461dee081cfbe539", text: `pragma module::dep;
fn duplicate<T>(handle: &Task<T>) -> Task<T> {
    return handle.clone();
}
`, calls: []importedMethodCall{{78, 92, "handle.clone()", ""}}},
		// Control: a non-intrinsic core method keeps its magic ID, so it is typed before and after.
		{name: "array_pop", result: "Option", digest: "330d846fe25d8ca3879e8423299d6ff159f0b1955e30704a97a1eb17c2b08025", text: `pragma module::dep;
fn last<T>(xs: &mut Array<T>) -> Option<T> {
    return xs.pop();
}
`, calls: []importedMethodCall{{76, 84, "xs.pop()", ""}}},
		// Controls: concrete receivers of Task, Channel and Range intrinsics, in the golden shapes.
		{name: "tasks", digest: "9aba1fa64196061f036816d5023efed0764ef2e6c479c450e4ba65353fe73105", text: `pragma module::dep;
async fn work() -> int {
    return 42;
}
async fn label() -> string {
    return "w";
}
async fn tasks() -> int {
    let t = spawn work();
    let h = t.clone();
    let r: TaskResult<int> = t.await();
    let a: int = compare h.await() { Success(v) => v; Cancelled() => 0; };
    let ts = spawn label();
    let rs: TaskResult<string> = ts.await();
    return a;
}
`, calls: []importedMethodCall{{173, 182, "t.clone()", "t"}, {213, 222, "t.await()", "r"}, {249, 258, "h.await()", "r"}, {360, 370, "ts.await()", "rs"}}},
		{name: "chans", digest: "29f4fbf95703053dca1a8ca7c2ce74d5667797b8d5eb8e83561077795496fb13", text: `pragma module::dep;
fn chans(rg0: Range<int>) -> int {
    let ch: own Channel<int> = Channel::<int>::new(10:uint);
    ch.send(42);
    let o: int? = ch.recv();
    ch.close();
    let chs: own Channel<string> = Channel::<string>::new(1:uint);
    let os: string? = chs.try_recv();
    let mut rg: Range<int> = rg0;
    let n: Option<int> = rg.next();
    return 0;
}
`, calls: []importedMethodCall{{151, 160, "ch.recv()", "o"}, {267, 281, "chs.try_recv()", "os"}, {342, 351, "rg.next()", "n"}}},
		// Controls: annotated static constructors, as the scored Channel and Map benches write them.
		{name: "statics", digest: "44bcec1bffa84943e67299823738335f5e8608930a088df278c32a4dc0830da2", text: `pragma module::dep;
fn statics() -> int {
    let ch: own Channel<int> =
        Channel::<int>::new(1:uint);
    ch.close();
    let mut m: Map<uint64, string> =
        Map::<uint64, string>.new();
    let _ = m.insert(1:uint64, "x");
    return 0;
}
`, calls: []importedMethodCall{{81, 108, "Channel::<int>::new(1:uint)", ""}, {171, 198, "Map::<uint64, string>.new()", ""}}},
	}
}

func TestAnalyzeImportedGenericMethodResults(t *testing.T) {
	for _, src := range importedMethodSources()[:3] {
		t.Run(src.name, func(t *testing.T) {
			f, got := analyzeImportedMethodSource(t, src)
			for i, ty := range got {
				name, args := importedTypeName(f, ty)
				if name != src.result || len(args) != 1 || ty != got[0] {
					t.Fatalf("%s is %s with %d arguments (type %d, first call %d), want one type %s", src.calls[i].snippet, name, len(args), ty, got[0], src.result)
				}
				if info, ok := f.authority.TypeInterner.TypeParamInfo(args[0]); !ok || info == nil {
					t.Errorf("%s: the argument is not the template's own parameter", src.calls[i].snippet)
				}
			}
		})
	}
}

func TestImportedIntrinsicConcreteResultsKeepTheirTypes(t *testing.T) {
	for _, src := range importedMethodSources()[3:] {
		t.Run(src.name, func(t *testing.T) {
			f, got := analyzeImportedMethodSource(t, src)
			for i, call := range src.calls {
				if call.binding == "" {
					continue
				}
				if want := importedBindingType(t, f, call.binding); got[i] != want {
					t.Errorf("%s at %d:%d is type %d, binding %s is %d", call.snippet, call.start, call.end, got[i], call.binding, want)
				}
			}
			if src.name == "tasks" && importedBindingType(t, f, "v") != f.authority.TypeInterner.Builtins().Int {
				t.Error("the compare arm does not bind v with the int payload")
			}
		})
	}
}

// analyzeImportedMethodSource analyzes a frozen dependency source and answers each frozen call's type.
func analyzeImportedMethodSource(t *testing.T, src importedMethodSource) (originalGenericFixture, []types.TypeID) {
	t.Helper()
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(src.text))); got != src.digest {
		t.Fatalf("PRECONDITION: frozen source %s changed: %s", src.name, got)
	}
	for _, call := range src.calls {
		if call.start < 0 || call.end > len(src.text) || call.start >= call.end || src.text[call.start:call.end] != call.snippet {
			t.Fatalf("PRECONDITION: frozen span %d:%d is not %q", call.start, call.end, call.snippet)
		}
	}
	f := originalGenericSignatureFixture(t, src.text, false, true)
	if _, err := sema.AnalyzeReturnOrigins(t.Context(), f.authority, f.inputs.units); err != nil {
		t.Fatalf("return-origin analysis stopped: %v", err)
	}
	got, shapes := make([]types.TypeID, len(src.calls)), make([]string, len(src.calls))
	for i, call := range src.calls {
		for id, ty := range f.unit.Sema.ExprTypes {
			if e := f.unit.Builder.Exprs.Get(id); e != nil && e.Kind == ast.ExprCall && e.Span.File == f.owner.File.ID &&
				int(e.Span.Start) == call.start && int(e.Span.End) == call.end {
				got[i] = ty
			}
		}
		if got[i] == types.NoTypeID {
			t.Fatalf("%s at %d:%d has no type", call.snippet, call.start, call.end)
		}
		shapes[i] = importedTypeShape(f, got[i])
	}
	logReturnOriginCallEvidence(t, map[string]any{"stage": "imported_method_result", "source": src.name, "shapes": shapes})
	return f, got
}

// importedTypeName answers a nominal type's name and arguments.
func importedTypeName(f originalGenericFixture, id types.TypeID) (string, []types.TypeID) {
	in := f.authority.TypeInterner
	var nameID source.StringID
	var args []types.TypeID
	if info, ok := in.UnionInfo(id); ok && info != nil {
		nameID, args = info.Name, info.TypeArgs
	} else if info, ok := in.StructInfo(id); ok && info != nil {
		nameID, args = info.Name, info.TypeArgs
	} else {
		return "", nil
	}
	strs := in.Strings
	if strs == nil {
		strs = f.unit.Builder.StringsInterner
	}
	name, _ := strs.Lookup(nameID)
	return name, args
}

// importedTypeShape spells a type by name, kind and width down its arguments, so it compares across builds.
func importedTypeShape(f originalGenericFixture, id types.TypeID) string {
	name, args := importedTypeName(f, id)
	tt, _ := f.authority.TypeInterner.Lookup(id)
	shape := fmt.Sprintf("%s/kind%d/width%d", name, tt.Kind, tt.Width)
	for _, arg := range args {
		shape += "[" + importedTypeShape(f, arg) + "]"
	}
	return shape
}

// importedBindingType answers the type of the one binding of this name declared in the test source.
func importedBindingType(t *testing.T, f originalGenericFixture, name string) types.TypeID {
	t.Helper()
	found := types.NoTypeID
	for i, sym := range f.unit.Symbols.Table.Symbols.Data() {
		if got, _ := f.unit.Builder.StringsInterner.Lookup(sym.Name); got != name || sym.Decl.ASTFile != f.unit.FileID {
			continue
		}
		ty := f.unit.Sema.BindingTypes[symbols.SymbolID(i+1)] //nolint:gosec // a table index
		if ty == types.NoTypeID {
			ty = sym.Type
		}
		if found != types.NoTypeID || ty == types.NoTypeID {
			t.Fatalf("PRECONDITION: %s is not one typed binding of the test source", name)
		}
		found = ty
	}
	if found == types.NoTypeID {
		t.Fatalf("PRECONDITION: binding %s is absent", name)
	}
	return found
}
