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

// Each source case independently uses the strict parser/SEMA/HIR/mono helper.
// Counted by-value parameters cannot be borrowed mutably in the current language;
// the separate refusal cases pin that boundary without claiming public HIR proof.
func TestPrepareBorrowedParamConcreteEffects(t *testing.T) {
	cases := []struct {
		name, source, call, readonlyCallee string
		working, incomingReference         bool
	}{
		{"readonly_arg", `fn peek(x: &int) {}
fn readonly_arg(p: int) -> int { peek(p); return p; }`, "let _ = readonly_arg(1);", "peek", false, false},
		{"readonly_self", `extern<int> { fn Peek(self: &int) {} }
fn readonly_self(p: int) -> int { p.Peek(); return p; }`, "let _ = readonly_self(1);", "Peek", false, false},
		{"direct", `fn direct(p: int) -> int { p = 2; return p; }`, "let _ = direct(1);", "", true, false},
		{"compound", `fn compound(p: int) -> int { p += 2; return p; }`, "let _ = compound(1);", "", true, false},
		{"conditional", `fn conditional(p: int, flag: bool) -> int { if flag { p = 2; } return p; }`, "let _ = conditional(1, false);", "", true, false},
		{"loop_write", `fn loop_write(p: int, flag: bool) -> int { while flag { p = 2; break; } return p; }`, "let _ = loop_write(1, false);", "", true, false},
		{"explicit_drop", `fn explicit_drop(p: int) { @drop p; }`, "explicit_drop(1);", "", true, false},
		{"drop_rebind", `fn drop_rebind(p: int) -> int { @drop p; p = 2; return p; }`, "let _ = drop_rebind(1);", "", true, false},
		{"incoming_ref", `fn incoming_ref(p: &mut int) { *p = 2; }`, "let mut n: int = 1; incoming_ref(&mut n);", "", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mm, in, err := compileAndMonomorphize(t, tc.source+"\nfn main() { "+tc.call+" }\n")
			if err != nil {
				t.Fatal(err)
			}
			fn := borrowedConcreteFunc(t, mm, tc.name)
			p := fn.Params[0]
			candidates := []symbols.SymbolID{p.SymbolID}
			if tc.incomingReference {
				if in.IsRefCounted(p.Type) {
					t.Fatal("incoming reference unexpectedly classified as counted")
				}
				candidates = nil
			}
			before := cloneBlock(fn.Body)
			prepared, working, err := PrepareBorrowedParamBody(fn, in, candidates)
			if err != nil || slices.Contains(working, p.SymbolID) != tc.working || len(working) > 1 {
				t.Fatalf("working=%v, want=%t, error=%v", working, tc.working, err)
			}
			if !reflect.DeepEqual(fn.Body, before) {
				t.Fatal("preparation mutated concrete source HIR")
			}
			if (tc.working || tc.incomingReference) && prepared != fn {
				t.Fatal("preparation unnecessarily copied the body")
			}
			if tc.readonlyCallee != "" {
				requireBorrowedReadonlyCall(t, mm, in, fn, p, tc.readonlyCallee)
			}
		})
	}
}

func TestPrepareBorrowedParamRejectsImmutableSource(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"explicit_arg", "poke(&mut p);"},
		{"implicit_arg", "poke(p);"},
		{"implicit_self", "p.Touch();"},
		{"alias_creation", "let r = &mut p; @drop r;"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := `fn poke(x: &mut int) { *x = 2; }
extern<int> { fn Touch(self: &mut int) { *self = 2; } }
fn ` + tc.name + "(p: int) { " + tc.body + " }\nfn main() { " + tc.name + "(1); }\n"
			mm, in, err := compileAndMonomorphize(t, src)
			const want = "sema errors: SEM3022: cannot take mutable borrow of 'p'"
			if err == nil || err.Error() != want || mm != nil || in != nil {
				t.Fatalf("want exactly one immutable-parameter refusal before HIR, got module=%v types=%v error=%v", mm, in, err)
			}
		})
	}
}

// The address must be the actual argument of the selected callable, whose own
// concrete parameter type is &int. An unrelated &p somewhere in the body cannot
// prove the call contract, nor can the absence of a mutable-address expression.
func requireBorrowedReadonlyCall(t *testing.T, mm *MonoModule, in *types.Interner, fn *hir.Func, p hir.Param, name string) {
	t.Helper()
	wantOriginal := borrowedOriginalFunc(t, mm, name).SymbolID
	calls := 0
	walk := borrowedBodyWalker{expr: func(e *hir.Expr) error {
		d, ok := e.Data.(hir.CallData)
		if !ok {
			return nil
		}
		selected := mm.FuncBySym[d.SymbolID]
		if selected == nil || selected.OrigSym != wantOriginal {
			return nil
		}
		if selected.Func == nil || selected.Func.SymbolID != d.SymbolID || len(selected.Func.Params) != 1 || len(d.Args) != 1 {
			t.Fatalf("selected readonly callable has no exact unary ABI: %+v", d)
		}
		expected := selected.Func.Params[0].Type
		descriptor, valid := in.Lookup(resolveAlias(in, expected))
		if !valid || descriptor.Kind != types.KindReference || descriptor.Mutable || descriptor.Elem != p.Type || p.Type != in.Builtins().Int {
			t.Fatalf("selected callable parameter is not &int: type=%d descriptor=%+v", expected, descriptor)
		}
		arg := d.Args[0]
		if arg == nil || arg.Kind != hir.ExprUnaryOp || arg.Type != expected {
			t.Fatalf("selected call argument is not the exact typed reference: %+v", arg)
		}
		address, valid := arg.Data.(hir.UnaryOpData)
		if !valid || address.Op != ast.ExprUnaryRef || address.Operand == nil || address.Operand.Type != p.Type || borrowedParamSlot(address.Operand) != p.SymbolID {
			t.Fatalf("selected call did not borrow the exact parameter slot %d: %+v", p.SymbolID, arg)
		}
		calls++
		return nil
	}}
	if err := walk.block(fn.Body); err != nil || calls != 1 {
		t.Fatalf("readonly callable proof: matching calls=%d error=%v", calls, err)
	}
}

func TestPrepareBorrowedParamConcreteCleanup(t *testing.T) {
	for _, tc := range []struct{ name, source, call string }{
		{"quiet", "fn quiet<T>(p: T) {}", "quiet(1);"},
		{"read", "fn read<T>(p: T) -> int { let r: &T = &p; @drop r; return 0; }", "let _ = read(1);"},
		{"explicit", "fn explicit<T>(p: T) { @drop p; }", "explicit(1);"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mm, in, err := compileAndMonomorphize(t, tc.source+"\nfn main() { "+tc.call+" }\n")
			if err != nil {
				t.Fatal(err)
			}
			fn := borrowedConcreteFunc(t, mm, tc.name)
			p := fn.Params[0]
			originalFn := borrowedOriginalFunc(t, mm, tc.name)
			original := originalFn.Params[0]
			if original.SymbolID != p.SymbolID || !types.ContainsGenericParam(in, original.Type) {
				t.Fatalf("generic parameter identity/type provenance was lost: original=%+v concrete=%+v", original, p)
			}
			if p.Type != in.Builtins().Int {
				t.Fatalf("fixture was not concretized to int: %v", p.Type)
			}
			before := cloneBlock(fn.Body)
			synthetic, explicit, exits := borrowedCleanupCounts(t, fn.Body, p.SymbolID)
			originalSynthetic, originalExplicit, originalExits := borrowedCleanupCounts(t, originalFn.Body, original.SymbolID)
			if synthetic != originalSynthetic || explicit != originalExplicit || exits != originalExits {
				t.Fatal("monomorphization changed same-symbol cleanup provenance")
			}
			if tc.name == "quiet" && synthetic != 1 || tc.name == "read" && exits != 1 || tc.name == "explicit" && explicit != 1 {
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
			if tc.name == "explicit" {
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
	for _, tc := range []struct{ name, activation string }{{"host_async", "async"}, {"host_blocking", "blocking"}} {
		t.Run(tc.name, func(t *testing.T) {
			src := "type Task<T> = { __opaque: int };\nfn " + tc.name + "(p: int) -> Task<int> { return " + tc.activation + " { p = 2; ret p; }; }\nfn main() { let _ = " + tc.name + "(1); }\n"
			mm, in, err := compileAndMonomorphize(t, src)
			if err != nil {
				t.Fatal(err)
			}
			fn := borrowedConcreteFunc(t, mm, tc.name)
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
