package llvm

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"surge/internal/driver"
	"surge/internal/layout"
	"surge/internal/types"
)

// An enum is carried as the type its constants already are.
//
// Every authority on an enum's storage says the same thing. `internal/layout`
// computes an enum's layout by descending to its base type, and the root
// collector registers that base type INSTEAD of the enum, so the registry never
// holds an entry of the enum's own. `types.IsValueComposite` excludes enums
// deliberately — an enum has no members to lay out and nothing to reclaim. Only
// this backend disagreed, spelling every enum `ptr`, which claimed eight bytes
// for a type the layout engine sizes at four.
//
// Nothing in the language could show the disagreement, because nothing in the
// language produces an enum-typed value. That is why it needed an oracle rather
// than a fixture, and these tests are it.
const enumCarrierFixture = `
enum Bare = {
	Red,
	Green,
	Blue
}

enum Narrow: uint8 = {
	Unknown = 0,
	Started = 1
}

enum Wide: int64 = {
	Low = 0,
	High = 1
}

enum Named: string = {
	Left = "left",
	Right = "right"
}

type Width = uint16;

enum Aliased: Width = {
	First = 1,
	Second = 2
}

@entrypoint
fn main() -> int {
	let bare: int = Bare::Green;
	let narrow: uint8 = Narrow::Started;
	let wide: int64 = Wide::High;
	let named: string = Named::Left;
	let aliased: uint16 = Aliased::Second;
	if bare != 1 { return 1; }
	if narrow != 1:uint8 { return 1; }
	if wide != 1:int64 { return 1; }
	if aliased != 2:uint16 { return 1; }
	if len(named) != 4 { return 1; }
	return 0;
}
`

var enumCarrierNames = []string{"Bare", "Narrow", "Wide", "Named", "Aliased"}

// The spelling of an enum occupies exactly the bytes the layout engine
// computes for it.
//
// This is the check the pointer spelling failed. A bare enum is four bytes and
// a `uint8`-based one is a single byte; both were spelled as an eight-byte
// pointer.
func TestEnumIsSpelledAtTheSizeTheLayoutEngineComputes(t *testing.T) {
	e := prepareEmitterForTest(t, enumCarrierFixture)

	ids := make([]types.TypeID, 0, len(enumCarrierNames))
	for _, name := range enumCarrierNames {
		ids = append(ids, findNamedType(t, e, name))
	}
	// Asked as roots on purpose: the production registry records an enum's base
	// type rather than the enum, so this is the only way to make the engine
	// state what it believes an enum itself is.
	registry, err := layout.FinalizeRegistry(layout.New(layout.X86_64LinuxGNU(), e.types), ids)
	if err != nil {
		t.Fatalf("finalize enum layouts: %v", err)
	}

	for i, name := range enumCarrierNames {
		published, err := registry.Require(ids[i])
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		spelling, err := e.llvmType(ids[i])
		if err != nil {
			t.Fatalf("%s: spelling: %v", name, err)
		}
		size, align, err := llvmTypeSizeAlign(spelling)
		if err != nil {
			t.Fatalf("%s: spelled %s, which has no size: %v", name, spelling, err)
		}
		if uint64(size) != published.Size || uint64(align) != published.Align {
			t.Errorf("%s is spelled %s (%d bytes, align %d) but the layout engine computed %d bytes, align %d",
				name, spelling, size, align, published.Size, published.Align)
		}
	}
}

// An enum is spelled exactly as its base type is, so the two cannot drift
// apart. A `uint8`-based enum and a `uint8` are the same eight bits.
func TestEnumIsSpelledAsItsBaseType(t *testing.T) {
	e := prepareEmitterForTest(t, enumCarrierFixture)

	for _, tc := range []struct{ enum, base string }{
		{"Bare", "int"},
		{"Narrow", "uint8"},
		{"Wide", "int64"},
		{"Named", "string"},
		{"Aliased", "uint16"},
	} {
		enumSpelling, err := e.llvmType(findNamedType(t, e, tc.enum))
		if err != nil {
			t.Fatalf("%s: %v", tc.enum, err)
		}
		baseSpelling, err := e.llvmType(findNamedType(t, e, tc.base))
		if err != nil {
			t.Fatalf("%s: %v", tc.base, err)
		}
		if enumSpelling != baseSpelling {
			t.Errorf("%s is spelled %s but its base type %s is spelled %s",
				tc.enum, enumSpelling, tc.base, baseSpelling)
		}
	}

	// An enum that names no base still has one: sema defaults it to `int`, so
	// there is no shape here that reaches the carrier without a base type.
	bare, ok := e.types.EnumInfo(findNamedType(t, e, "Bare"))
	if !ok || bare == nil || bare.BaseType == types.NoTypeID {
		t.Fatalf("an enum that names no base type reached the backend without one")
	}
}

// An enum has no storage of its own, so it is not a value composite and has no
// byte-run spelling to ask for.
func TestEnumHasNoInlineStorage(t *testing.T) {
	e := prepareEmitterForTest(t, enumCarrierFixture)

	for _, name := range enumCarrierNames {
		id := findNamedType(t, e, name)
		if e.types.IsValueComposite(id) {
			t.Errorf("%s reports as a value composite; an enum has no members to lay out", name)
		}
		if e.hasInlineStorage(id) {
			t.Errorf("%s reports inline storage; an enum has no storage of its own", name)
		}
		if _, err := e.storageFactsOf(id); err == nil {
			t.Errorf("%s answered with inline storage facts instead of refusing", name)
		}
	}
}

// A base type an enum may not have is refused rather than carried as whatever
// that type happens to be carried as.
func TestEnumRefusesABaseItCannotBeCarriedAs(t *testing.T) {
	e := prepareEmitterForTest(t, enumCarrierFixture)

	// Sema rejects such a declaration, so the refusal is reached through the
	// interner rather than through a source the compiler would never accept.
	bare := findNamedType(t, e, "Bare")
	info, ok := e.types.EnumInfo(bare)
	if !ok || info == nil {
		t.Fatalf("the fixture's bare enum has no enum info")
	}
	original := info.BaseType
	t.Cleanup(func() { e.types.SetEnumBaseType(bare, original) })

	e.types.SetEnumBaseType(bare, e.types.Builtins().Bool)
	if spelling, err := e.llvmType(bare); err == nil {
		t.Errorf("an enum based on bool was spelled %s instead of refused", spelling)
	}
}

// Nothing in the language produces an enum-typed value, which is why the
// pointer spelling survived unnoticed.
//
// This is the row that makes the resolution self-announcing. Each source below
// is one whole way a value could reach an enum-typed destination, and each is a
// compile-time error today: `Color::Red` has the BASE type, and sema refuses to
// assign it to anything annotated with the enum. When the language grows enum
// values, one of these starts compiling and this test says so — at which point
// the carrier needs a value-level fixture and a parity row behind it, which
// cannot be written until then.
func TestNoExpressionProducesAnEnumTypedValue(t *testing.T) {
	const decl = "enum Color: int = { Red = 0, Green = 1 }\n"
	for _, tc := range []struct {
		name string
		body string
	}{
		{"a local annotated with the enum", "@entrypoint\nfn main() -> int { let c: Color = Color::Red; return 0; }"},
		{"an argument", "fn take(c: Color) -> int { return 0; }\n@entrypoint\nfn main() -> int { return take(Color::Red); }"},
		{"a return", "fn give() -> Color { return Color::Red; }\n@entrypoint\nfn main() -> int { give(); return 0; }"},
		{"a struct field", "type Holder = { c: Color }\n@entrypoint\nfn main() -> int { let h: Holder = Holder{ c: Color::Red }; return 0; }"},
		{"an array element", "@entrypoint\nfn main() -> int { let a: Color[1] = [Color::Red]; return 0; }"},
		{"a conversion", "@entrypoint\nfn main() -> int { let c: Color = 0:Color; return 0; }"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if compilesForTest(t, decl+tc.body) {
				t.Fatalf("this source now produces an enum-typed value; the enum " +
					"carrier needs a value-level fixture and a parity row behind it")
			}
		})
	}
}

// compilesForTest reports whether a source reaches the end of sema without
// errors. Unlike lowerMIRFromSource it treats diagnostics as the answer rather
// than as a test failure.
func compilesForTest(t *testing.T, sourceCode string) bool {
	t.Helper()

	path := filepath.Join(t.TempDir(), "probe.sg")
	if err := os.WriteFile(path, []byte(sourceCode), 0o600); err != nil {
		t.Fatalf("write probe source: %v", err)
	}
	opts := driver.DiagnoseOptions{Stage: driver.DiagnoseStageSema, MaxDiagnostics: 100}
	result, err := driver.DiagnoseWithOptions(context.Background(), path, &opts)
	if err != nil {
		t.Fatalf("diagnose probe source: %v", err)
	}
	return !result.Bag.HasErrors()
}
