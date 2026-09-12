package llvm

import (
	"context"
	"strings"
	"testing"

	"surge/internal/driver"
	"surge/internal/hir"
	"surge/internal/types"
)

func TestRuntimeOpaqueABIFieldsAreFixed64(t *testing.T) {
	withRepoStdlib(t)
	const src = `fn abi_types(file: File, listener: TcpListener, conn: TcpConn,
    task: Task<nothing>, placement: Placement) -> nothing {
    return nothing;
}
@entrypoint
fn main() -> int { return 0; }
`
	_, result, _ := runtimeFixedABIFromSource(t, src)
	fn := runtimeABIFunction(t, result.HIR, "abi_types")
	if len(fn.Params) != 5 {
		t.Fatalf("abi_types has %d parameters, want five actual runtime types", len(fn.Params))
	}
	in := result.Sema.TypeInterner
	for i, name := range []string{"File", "TcpListener", "TcpConn", "Task", "Placement"} {
		t.Run(name, func(t *testing.T) {
			id := fn.Params[i].Type
			info, ok := in.StructInfo(id)
			if !ok || info == nil || len(info.Fields) != 1 || in.Strings.MustLookup(info.Fields[0].Name) != "__opaque" {
				t.Fatalf("%s lacks its single declared opaque field: %+v", name, info)
			}
			requireRuntimeABIInt(t, in, info.Fields[0].Type, types.Width64)
			if got := in.IsValueComposite(id); got != (i < 3) {
				t.Fatalf("%s inline-storage identity = %t, want %t", name, got, i < 3)
			}
			if got := in.IsRuntimeHandleType(id); got != (name == "Task") {
				t.Fatalf("%s runtime-handle identity = %t", name, got)
			}
			if got := in.IsRuntimePlacementType(id); got != (name == "Placement") {
				t.Fatalf("%s runtime-placement identity = %t", name, got)
			}
			if name == "Task" {
				payloads, ok := in.RuntimeHandlePayloads(id)
				if !ok || len(payloads) != 1 || payloads[0] != in.Builtins().Nothing {
					t.Fatalf("Task<nothing> lost its owned runtime payload: %v, %t", payloads, ok)
				}
			}
		})
	}
}

func TestNetAsyncHandleSignaturesAreFixed64(t *testing.T) {
	withRepoStdlib(t)
	const src = `import stdlib/net as net;
@entrypoint
fn main() -> int {
    let listener: TcpListener = { __opaque = 0 };
    let conn: TcpConn = { __opaque = 0 };
    let accept_task = net.accept(&listener);
    let read_task = net.read_some(&conn, 0:uint);
    let some_bytes: byte[] = [];
    let all_bytes: byte[] = [];
    let write_task = net.write_some(&conn, some_bytes);
    let all_task = net.write_all(&conn, all_bytes);
    let _ = accept_task;
    let _ = read_task;
    let _ = write_task;
    let _ = all_task;
    return 0;
}
`
	mod, result, ir := runtimeFixedABIFromSource(t, src)
	for _, call := range []string{"@rt_net_accept(", "@rt_net_read_bytes(", "@rt_net_write_bytes("} {
		if !strings.Contains(ir, "call ptr "+call) {
			t.Fatalf("imported net bodies did not reach %s in LLVM IR", call)
		}
	}
	for _, name := range []string{"listener_handle", "conn_handle", "accept_owned", "read_some_owned", "write_some_owned", "write_all_owned"} {
		t.Run(name, func(t *testing.T) {
			fn := runtimeABIFunction(t, mod, name)
			id := fn.Result
			if strings.HasSuffix(name, "_owned") {
				if !fn.IsAsync() || len(fn.Params) == 0 || fn.Params[0].Name != "handle" {
					t.Fatalf("%s lacks its async handle parameter", name)
				}
				id = fn.Params[0].Type
			}
			requireRuntimeABIInt(t, result.Sema.TypeInterner, id, types.Width64)
		})
	}
}

func TestTermEventResizeABIIsFixed64(t *testing.T) {
	withRepoStdlib(t)
	const src = `import stdlib/term as term;
fn read_event() -> term.TermEvent { return term.term_read_event(); }
@entrypoint
fn main() -> int {
    let event = read_event();
    let _ = event;
    let size = term.term_size();
    let _ = size;
    return 0;
}
`
	mod, result, ir := runtimeFixedABIFromSource(t, src)
	in := result.Sema.TypeInterner
	if !strings.Contains(ir, "call ptr @rt_term_read_event()") || !strings.Contains(ir, "call ptr @rt_term_size()") {
		t.Fatal("terminal source did not emit both runtime ABI calls")
	}
	t.Run("resize_tag", func(t *testing.T) {
		found := 0
		for _, decl := range mod.Types {
			if decl.Kind != hir.TypeDeclTag || decl.Name != "Resize" {
				continue
			}
			found++
			sym := result.Symbols.Table.Symbols.Get(decl.SymbolID)
			if sym == nil || sym.Signature == nil || len(sym.Signature.Params) != 2 {
				t.Fatal("Resize declaration has no two-argument constructor signature")
			}
			for _, param := range sym.Signature.Params {
				if param != "int64" {
					t.Fatalf("Resize constructor parameter = %q, want int64", param)
				}
			}
		}
		if found != 1 {
			t.Fatalf("found %d actual Resize tag declarations, want one", found)
		}
	})
	t.Run("resize_union", func(t *testing.T) {
		id := runtimeABIFunction(t, mod, "read_event").Result
		info, ok := in.UnionInfo(resolveAliasAndOwn(in, id))
		if !ok || info == nil || len(info.Members) != 3 {
			t.Fatalf("TermEvent lost its three union cases: %+v", info)
		}
		found := 0
		for _, member := range info.Members {
			if member.Kind != types.UnionMemberTag || in.Strings.MustLookup(member.TagName) != "Resize" {
				continue
			}
			found++
			if len(member.TagArgs) != 2 {
				t.Fatalf("Resize union case has %d payloads, want two", len(member.TagArgs))
			}
			for _, arg := range member.TagArgs {
				requireRuntimeABIInt(t, in, arg, types.Width64)
			}
		}
		if found != 1 {
			t.Fatalf("found %d Resize union cases, want one", found)
		}
	})
	t.Run("term_size_control", func(t *testing.T) {
		id := runtimeABIFunction(t, mod, "term_size").Result
		info, ok := in.TupleInfo(resolveAliasAndOwn(in, id))
		if !ok || info == nil || len(info.Elems) != 2 {
			t.Fatalf("term_size lost its two-element tuple: %+v", info)
		}
		for _, elem := range info.Elems {
			requireRuntimeABIInt(t, in, elem, types.WidthAny)
		}
	})
}

func runtimeFixedABIFromSource(t *testing.T, src string) (*hir.Module, *driver.DiagnoseResult, string) {
	t.Helper()
	mirMod, result := lowerMIRFromSource(t, src)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit fixed ABI source: %v", err)
	}
	mod, err := driver.CombineHIRWithModules(context.Background(), result)
	if err != nil || mod == nil {
		t.Fatalf("read imported fixed ABI declarations: %v", err)
	}
	return mod, result, ir
}

func runtimeABIFunction(t *testing.T, mod *hir.Module, name string) *hir.Func {
	t.Helper()
	var found *hir.Func
	for _, fn := range mod.Funcs {
		if fn == nil || fn.Name != name {
			continue
		}
		if found != nil {
			t.Fatalf("duplicate fixed ABI function %q", name)
		}
		found = fn
	}
	if found == nil {
		t.Fatalf("missing fixed ABI function %q", name)
	}
	return found
}

func requireRuntimeABIInt(t *testing.T, in *types.Interner, id types.TypeID, width types.Width) {
	t.Helper()
	tt, ok := in.Lookup(resolveAliasAndOwn(in, id))
	if !ok || tt.Kind != types.KindInt || tt.Width != width {
		t.Fatalf("ABI type %s = %+v, want signed integer width %v", types.Label(in, id), tt, width)
	}
}
