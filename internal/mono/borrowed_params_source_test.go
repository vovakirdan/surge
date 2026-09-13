package mono

import (
	"reflect"
	"slices"
	"testing"

	"surge/internal/ast"
	"surge/internal/hir"
	"surge/internal/symbols"
	"surge/internal/types"
)

// These sources pass through the existing strict parser/SEMA/HIR/mono helper.
// In particular, deferred receivers must arrive as resolved concrete addresses;
// building a synthetic &mut expression would not prove that producer contract.
const borrowedParamEffectSource = `
fn peek(x: &int) -> nothing { return nothing; }
fn poke(x: &mut int) -> nothing { *x = 2; return nothing; }
extern<int> {
    fn Touch(self: &mut int) -> nothing { *self = 2; return nothing; }
    fn Peek(self: &int) -> nothing { return nothing; }
}
contract Touches<T> { fn Touch(self: &mut T) -> nothing; }
contract Peeks<T> { fn Peek(self: &T) -> nothing; }
fn explicit_arg(p: int) -> int { poke(&mut p); return p; }
fn implicit_arg(p: int) -> int { poke(p); return p; }
fn implicit_self(p: int) -> int { p.Touch(); return p; }
fn readonly_arg(p: int) -> int { peek(p); return p; }
fn readonly_self(p: int) -> int { p.Peek(); return p; }
fn deferred<T: Touches<T>>(p: T) -> nothing { p.Touch(); return nothing; }
fn readonly_deferred<T: Peeks<T>>(p: T) -> nothing { p.Peek(); return nothing; }
fn alias_creation(p: int) -> int { let r = &mut p; @drop r; return p; }
fn direct(p: int) -> int { p = 2; return p; }
fn compound(p: int) -> int { p += 2; return p; }
fn conditional(p: int, flag: bool) -> int { if flag { p = 2; } return p; }
fn loop_write(p: int, flag: bool) -> int { while flag { p = 2; break; } return p; }
fn explicit_drop(p: int) { @drop p; }
fn drop_rebind(p: int) -> int { @drop p; p = 2; return p; }
fn incoming_ref(p: &mut int) { *p = 2; }
fn main() {
    let _ = explicit_arg(1); let _ = implicit_arg(1); let _ = implicit_self(1);
    let _ = readonly_arg(1); let _ = readonly_self(1);
    deferred::<int>(1); readonly_deferred::<int>(1);
    let _ = alias_creation(1); let _ = direct(1); let _ = compound(1);
    let _ = conditional(1, false); let _ = loop_write(1, false);
    explicit_drop(1); let _ = drop_rebind(1);
    let mut n: int = 1; incoming_ref(&mut n);
}
`

func TestPrepareBorrowedParamConcreteEffects(t *testing.T) {
	mm, in, err := compileAndMonomorphize(t, borrowedParamEffectSource)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name             string
		working, address bool
		deferred         bool
	}{
		{"explicit_arg", true, true, false},
		{"implicit_arg", true, true, false},
		{"implicit_self", true, true, false},
		{"readonly_arg", false, false, false},
		{"readonly_self", false, false, false},
		{"deferred", true, true, true},
		{"readonly_deferred", false, false, true},
		{"alias_creation", true, true, false},
		{"direct", true, false, false},
		{"compound", true, false, false},
		{"conditional", true, false, false},
		{"loop_write", true, false, false},
		{"explicit_drop", true, false, false},
		{"drop_rebind", true, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fn := borrowedConcreteFunc(t, mm, tc.name)
			p := fn.Params[0]
			before := cloneBlock(fn.Body)
			prepared, working, err := PrepareBorrowedParamBody(fn, in, []symbols.SymbolID{p.SymbolID})
			if err != nil || slices.Contains(working, p.SymbolID) != tc.working || len(working) > 1 {
				t.Fatalf("working=%v, want=%t, error=%v", working, tc.working, err)
			}
			if !reflect.DeepEqual(fn.Body, before) {
				t.Fatal("preparation mutated concrete source HIR")
			}
			if tc.working && prepared != fn {
				t.Fatal("working-only preparation unnecessarily copied the body")
			}
			addresses, deferred := 0, 0
			walk := borrowedBodyWalker{expr: func(e *hir.Expr) error {
				if d, ok := e.Data.(hir.UnaryOpData); ok && d.Op == ast.ExprUnaryRefMut && borrowedParamSlot(d.Operand) == p.SymbolID {
					tt, valid := in.Lookup(resolveAlias(in, e.Type))
					if !valid || tt.Kind != types.KindReference || !tt.Mutable || tt.Elem != p.Type {
						t.Fatalf("mutable slot address lacks exact concrete type: %+v", e)
					}
					addresses++
				}
				if d, ok := e.Data.(hir.CallData); ok && d.DeferredUseID != "" {
					if !d.SymbolID.IsValid() || mm.FuncBySym[d.SymbolID] == nil {
						t.Fatalf("deferred call has no concrete callable: %+v", d)
					}
					deferred++
				}
				return nil
			}}
			if err := walk.block(fn.Body); err != nil {
				t.Fatal(err)
			}
			if (addresses > 0) != tc.address || (deferred > 0) != tc.deferred {
				t.Fatalf("producer evidence: mutable addresses=%d, deferred calls=%d", addresses, deferred)
			}
		})
	}
	t.Run("incoming_ref", func(t *testing.T) {
		fn := borrowedConcreteFunc(t, mm, "incoming_ref")
		prepared, working, err := PrepareBorrowedParamBody(fn, in, nil)
		if err != nil || prepared != fn || len(working) != 0 || in.IsRefCounted(fn.Params[0].Type) {
			t.Fatalf("incoming reference changed: working=%v error=%v", working, err)
		}
	})
}

func TestPrepareBorrowedParamConcreteCleanup(t *testing.T) {
	mm, in, err := compileAndMonomorphize(t, `
fn quiet<T>(p: T) {}
fn read<T>(p: T) -> T { return p; }
fn explicit<T>(p: T) { @drop p; }
fn main() { quiet(1); let _ = read(1); explicit(1); }
`)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"quiet", "read", "explicit"} {
		t.Run(name, func(t *testing.T) {
			fn := borrowedConcreteFunc(t, mm, name)
			p := fn.Params[0]
			original := borrowedOriginalFunc(t, mm, name).Params[0]
			if original.SymbolID != p.SymbolID || !types.ContainsGenericParam(in, original.Type) {
				t.Fatalf("generic parameter identity/type provenance was lost: original=%+v concrete=%+v", original, p)
			}
			if p.Type != in.Builtins().Int {
				t.Fatalf("fixture was not concretized to int: %v", p.Type)
			}
			before := cloneBlock(fn.Body)
			synthetic, explicit, exits := borrowedCleanupCounts(t, fn.Body, p.SymbolID)
			if name == "quiet" && synthetic == 0 || name == "read" && exits == 0 || name == "explicit" && explicit != 1 {
				t.Fatalf("missing producer witness: synthetic=%d explicit=%d exits=%d", synthetic, explicit, exits)
			}
			prepared, owners, err := PrepareBorrowedParamBody(fn, in, []symbols.SymbolID{p.SymbolID})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(fn.Body, before) {
				t.Fatal("private preparation mutated original obligations")
			}
			gotSynthetic, gotExplicit, gotExits := borrowedCleanupCounts(t, prepared.Body, p.SymbolID)
			if name == "explicit" {
				if prepared != fn || !slices.Equal(owners, []symbols.SymbolID{p.SymbolID}) || gotExplicit != 1 {
					t.Fatal("explicit drop was filtered or failed to require an owner")
				}
			} else if prepared == fn || len(owners) != 0 || gotSynthetic+gotExplicit+gotExits != 0 {
				t.Fatalf("readonly cleanup remains: %d/%d/%d owners=%v", gotSynthetic, gotExplicit, gotExits, owners)
			}
			again, againOwners, err := PrepareBorrowedParamBody(prepared, in, []symbols.SymbolID{p.SymbolID})
			if err != nil || again != prepared || !slices.Equal(againOwners, owners) {
				t.Fatalf("preparation is not idempotent: owners=%v error=%v", againOwners, err)
			}
		})
	}
}

func TestPrepareBorrowedParamConcreteActivationBoundaries(t *testing.T) {
	mm, in, err := compileAndMonomorphize(t, `
type Task<T> = { __opaque: int };
fn host_async(p: int) -> Task<int> { return async { p = 2; ret p; }; }
fn host_blocking(p: int) -> Task<int> { return blocking { p = 2; ret p; }; }
fn main() { let _ = host_async(1); let _ = host_blocking(1); }
`)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"host_async", "host_blocking"} {
		t.Run(name, func(t *testing.T) {
			fn := borrowedConcreteFunc(t, mm, name)
			p := fn.Params[0].SymbolID
			children := 0
			walk := borrowedBodyWalker{expr: func(e *hir.Expr) error {
				var body *hir.Block
				switch d := e.Data.(type) {
				case hir.AsyncData:
					body = d.Body
				case hir.BlockingData:
					body = d.Body
				default:
					return nil
				}
				child := &hir.Func{Name: "captured", Params: fn.Params, Body: body}
				_, owners, err := PrepareBorrowedParamBody(child, in, []symbols.SymbolID{p})
				if err != nil || !slices.Equal(owners, []symbols.SymbolID{p}) {
					t.Fatalf("nested body did not actually write same symbol %d: %v, %v", p, owners, err)
				}
				children++
				return nil
			}}
			if err := walk.block(fn.Body); err != nil || children != 1 {
				t.Fatalf("activation witness: children=%d error=%v", children, err)
			}
			_, owners, err := PrepareBorrowedParamBody(fn, in, []symbols.SymbolID{p})
			if err != nil || len(owners) != 0 {
				t.Fatalf("child write was attributed to parent: %v, %v", owners, err)
			}
		})
	}
}

func borrowedConcreteFunc(t *testing.T, mm *MonoModule, name string) *hir.Func {
	t.Helper()
	original := borrowedOriginalFunc(t, mm, name).SymbolID
	var found *hir.Func
	for _, key := range mm.SortedFuncKeys() {
		mf := mm.Funcs[key]
		if mf.OrigSym == original && original.IsValid() {
			if found != nil {
				t.Fatalf("more than one concrete fixture instance for %s", name)
			}
			found = mf.Func
		}
	}
	if found == nil {
		t.Fatalf("concrete fixture %s missing", name)
	}
	return found
}

func borrowedOriginalFunc(t *testing.T, mm *MonoModule, name string) *hir.Func {
	t.Helper()
	for _, fn := range mm.Source.Funcs {
		if fn.Name == name {
			return fn
		}
	}
	t.Fatalf("original fixture %s missing", name)
	return nil
}

func borrowedCleanupCounts(t *testing.T, body *hir.Block, sym symbols.SymbolID) (synthetic, explicit, exits int) {
	t.Helper()
	walk := borrowedBodyWalker{stmt: func(st *hir.Stmt) (bool, error) {
		var drops []hir.DropLocal
		switch d := st.Data.(type) {
		case hir.DropData:
			if borrowedParamSlot(d.Value) == sym {
				if d.Synthetic {
					synthetic++
				} else {
					explicit++
				}
			}
		case hir.ReturnData:
			drops = d.DropsAfterValue
		case hir.RetData:
			drops = d.DropsAfterValue
		}
		for _, drop := range drops {
			if drop.SymbolID == sym {
				exits++
			}
		}
		return true, nil
	}}
	if err := walk.block(body); err != nil {
		t.Fatal(err)
	}
	return
}
