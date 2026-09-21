package driver

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// A counted channel stores only its payloads, `own X` holds what X holds, and a
// far core runtime handle holds what that handle holds. A far Task stays refused as
// it is not counted; a core far TcpConn and a type made far by its name alone stay
// refused because neither is a runtime handle. Only the dependency module varies;
// every assertion is local to dep/main.sg.
// A struct payload is classified through its fields when its only attributes are
// bare `@copy` or `@shard_movable`; an array field and a borrowed view keep their refusals.
type channelPayloadCase struct {
	name, text, digest string
	// clean: no Pending may remain anywhere in the dependency source.
	clean bool
	// site and reason: this exact obligation must stay.
	site, reason string
	// declaration: this declaration name must keep the unsupported refusal.
	declaration string
}

func channelPayloadCases() []channelPayloadCase {
	return []channelPayloadCase{
		{name: "channel_new", clean: true, digest: "ce76b634b7a61efb3ab1d7295184e48bd22ef9d7ee797c386bda6c41127be316",
			text: "pragma module::dep;\nfn make() -> nothing {\n    let c = Channel::<uint>::new(4:uint);\n    return nothing;\n}\n"},
		{name: "own_opaque_result", clean: true, digest: "d2f77bbe246ecb1ee3419a3aa11bdf42932a2bfb25fbc4d2dec89e07106de6e5",
			text: "pragma module::dep;\n@intrinsic fn fresh() -> own Channel<uint>;\nfn use_fresh() -> nothing {\n    let c = fresh();\n    return nothing;\n}\n"},
		{name: "generic_channel_shape", clean: true, digest: "3bc093570e82473a64484f86a1866af82f07cc7ad36be0778bd38c7181d5679c",
			text: "pragma module::dep;\nfn pass<T>(c: Channel<T>) -> Channel<T> {\n    return c;\n}\nfn use_pass() -> nothing {\n    let c = Channel::<uint>::new(1:uint);\n    let d = pass::<uint>(c);\n    return nothing;\n}\n"},
		{name: "task_control", clean: true, digest: "adabfca351003d081c6218d789f6b9ea7654153914d9d6efcafc5bd0f953d50a",
			text: "pragma module::dep;\nasync fn wait(s: &string) -> nothing {\n    return nothing;\n}\nfn start(s: &string) -> Task<nothing> {\n    return wait(s);\n}\nfn keep(s: &string) -> Task<nothing> {\n    let t = start(s);\n    return t.clone();\n}\n"},
		{name: "far_control", clean: true, digest: "f7ffea5d96542a0066be3d08b0550ad1f9247762ab570b0da95403fdd97d6ca5",
			text: "pragma module::dep;\n@intrinsic fn remote() -> far Channel<uint>;\nfn use_remote() -> nothing {\n    let c = remote();\n    return nothing;\n}\n"},
		{name: "far_task_control", declaration: "remote_task", digest: "53f62f8a3eb2910559778a5512f6f4741be4feae8b15af6547ace9971790a672",
			text: "pragma module::dep;\n@intrinsic fn remote_task() -> far Task<int>;\n"},
		{name: "far_conn_control", declaration: "remote_conn", digest: "43065341796dfa7226633e4ee9e5b3cab9387c2e4dba8ca7736e653569accb65",
			text: "pragma module::dep;\n@intrinsic fn remote_conn() -> far TcpConn;\n"},
		{name: "far_named_conn_control", declaration: "remote_named", digest: "bf39a12dd91b03b0c10da5d50b8a471da36dac885f293d10d459eab72d2f83c9",
			text: "pragma module::dep, no_std;\ntype TcpConn = { n: int64 };\n@intrinsic fn remote_named() -> far TcpConn;\n"},
		{name: "movable_payload_channel", clean: true, digest: "f98156d943583044407df33e09934cb728f93f9b8f1b80ad92d965909d1b14d9",
			text: "pragma module::dep;\n@copy @shard_movable type Pack = { a: uint64, b: uint64 };\nfn make() -> nothing {\n    let c = Channel::<Pack>::new(2:uint);\n    return nothing;\n}\nfn round(c: Channel<Pack>, n: uint64) -> uint64 {\n    let p: Pack = Pack { a = n, b = 2:uint64 };\n    c.send(own p);\n    let first: uint64 = compare c.recv() {\n        Some(v) => v.a;\n        nothing => 0:uint64;\n    };\n    let second: uint64 = compare c.try_recv() {\n        Some(v) => v.b;\n        nothing => 0:uint64;\n    };\n    return first + second;\n}\n"},
		{name: "scalar_payload_channel", clean: true, digest: "86dd991001600d1485e4306a0bcf440c6d58e7fa0931f5ac6777646d0ff44111",
			text: "pragma module::dep;\nfn make() -> nothing {\n    let c = Channel::<uint64>::new(2:uint);\n    return nothing;\n}\nfn round(c: Channel<uint64>, n: uint64) -> uint64 {\n    c.send(n);\n    let first: uint64 = compare c.recv() {\n        Some(v) => v;\n        nothing => 0:uint64;\n    };\n    let second: uint64 = compare c.try_recv() {\n        Some(v) => v;\n        nothing => 0:uint64;\n    };\n    return first + second;\n}\n"},
		{name: "movable_array_payload_control", declaration: "batch_ring", digest: "756bd076ef611f3b905e2d3f0237e35c7c642985827ed0bd803bfc401fe04841",
			text: "pragma module::dep;\n@shard_movable type Batch = { xs: uint64[] };\n@intrinsic fn batch_ring() -> own Channel<Batch>;\n"},
		{name: "view_payload_control", site: "views", reason: genericConditionRefuted, digest: "9355e3c98529e11f6b6e11989e119893286041df735bf0f7e80c7951e07b2d9c",
			text: "pragma module::dep;\n@intrinsic fn views() -> own Channel<BytesView>;\n"},
	}
}

func TestAnalyzeChannelPayloadOpaqueResults(t *testing.T) {
	for _, tc := range channelPayloadCases() {
		t.Run(tc.name, func(t *testing.T) {
			if got := fmt.Sprintf("%x", sha256.Sum256([]byte(tc.text))); got != tc.digest {
				t.Fatalf("PRECONDITION: frozen dependency source changed: %s", got)
			}
			f := originalGenericSignatureFixture(t, tc.text, false, true)
			analysis, err := sema.AnalyzeReturnOrigins(t.Context(), f.authority, f.inputs.units)
			if err != nil || analysis == nil {
				t.Fatalf("return-origin analysis did not run: %v", err)
			}
			var local []sema.ReturnOriginPending
			for _, pending := range analysis.Pending {
				if pending.SourceKey == f.unit.SourceKey || pending.Span.File == f.owner.File.ID {
					local = append(local, pending)
				}
			}
			logReturnOriginCallEvidence(t, map[string]any{"stage": "channel_payload", "case": tc.name,
				"source_key": f.unit.SourceKey, "pending": local, "diagnostics": analysis.Diagnostics})
			for _, d := range analysis.Diagnostics {
				if d.Primary.File == f.owner.File.ID {
					t.Errorf("unexpected dependency diagnostic: %+v", d)
				}
			}
			switch {
			case tc.clean:
				for _, pending := range local {
					t.Errorf("dependency obligation left unfinished: %s at %d:%d %q", pending.Reason,
						pending.Span.Start, pending.Span.End, tc.text[pending.Span.Start:pending.Span.End])
				}
			case tc.site != "":
				start := strings.Index(tc.text, tc.site)
				found := false
				for _, pending := range local {
					found = found || pending.SourceKey == f.unit.SourceKey && pending.Reason == tc.reason &&
						int(pending.Span.Start) == start && int(pending.Span.End) == start+len(tc.site)
				}
				if !found {
					t.Errorf("control lost its %q obligation at %q: %+v", tc.reason, tc.site, local)
				}
			default:
				start := strings.Index(tc.text, "fn "+tc.declaration+"(") + len("fn ")
				found := false
				for _, pending := range local {
					found = found || pending.Reason == genericConditionUnsupported && int(pending.Span.Start) == start
				}
				if !found {
					t.Errorf("control lost its unsupported refusal at %q: %+v", tc.declaration, local)
				}
			}
		})
	}
}

// A static owner without a local declaration is admitted only as the resolver's
// imported copy of the export: without the flag, or with a local declaration
// record, the call keeps its refusal.
func TestAnalyzeChannelNewStaticOwnerControls(t *testing.T) {
	tc := channelPayloadCases()[0]
	const site = "Channel::<uint>::new(4:uint)"
	const refusal = "generic original call result disagrees with its substituted source signature"
	for _, control := range []struct {
		name   string
		mutate func(owner *symbols.Symbol, file source.FileID)
	}{
		{"not_imported", func(owner *symbols.Symbol, _ source.FileID) { owner.Flags &^= symbols.SymbolFlagImported }},
		{"local_declaration", func(owner *symbols.Symbol, file source.FileID) { owner.Decl.SourceFile = file }},
	} {
		t.Run(control.name, func(t *testing.T) {
			f := originalGenericSignatureFixture(t, tc.text, false, true)
			start := strings.Index(tc.text, site)
			var owner *symbols.Symbol
			for id := range f.unit.Sema.ExprTypes {
				node := f.unit.Builder.Exprs.Get(id)
				if node == nil || node.Span.File != f.owner.File.ID || int(node.Span.Start) != start || int(node.Span.End) != start+len(site) {
					continue
				}
				if call, ok := f.unit.Builder.Exprs.Call(id); ok && call != nil {
					if member, isMember := f.unit.Builder.Exprs.Member(call.Target); isMember && member != nil {
						owner = f.unit.Symbols.Table.Symbols.Get(f.unit.Symbols.ExprSymbols[member.Target])
					}
				}
			}
			if owner == nil || owner.Kind != symbols.SymbolType || owner.Flags&symbols.SymbolFlagImported == 0 || owner.Decl != (symbols.SymbolDecl{}) {
				t.Fatalf("PRECONDITION: the static owner is not the imported synthetic Channel symbol: %+v", owner)
			}
			control.mutate(owner, f.owner.File.ID)
			analysis, err := sema.AnalyzeReturnOrigins(t.Context(), f.authority, f.inputs.units)
			if err != nil || analysis == nil {
				t.Fatalf("return-origin analysis did not run: %v", err)
			}
			for _, pending := range analysis.Pending {
				if pending.SourceKey == f.unit.SourceKey && pending.Reason == refusal &&
					int(pending.Span.Start) == start && int(pending.Span.End) == start+len(site) {
					return
				}
			}
			t.Errorf("mutated static owner was admitted: %+v", analysis.Pending)
		})
	}
}

// The payload rule trusts that the counted-handle family is exactly Channel. If
// Task or Range is ever counted, this row fails before the rule widens to them.
func TestReturnOriginChannelPayloadFamilyIsChannelOnly(t *testing.T) {
	f := originalGenericSignatureFixture(t, channelPayloadCases()[0].text, false, true)
	var core *sema.ReturnOriginUnit
	for i := range f.inputs.units {
		if f.inputs.units[i].SourceKey == "core/intrinsics.sg" {
			if core != nil {
				t.Fatal("PRECONDITION: core intrinsics unit is not unique")
			}
			core = &f.inputs.units[i]
		}
	}
	if core == nil || core.Sema.TypeInterner != f.authority.TypeInterner {
		t.Fatal("PRECONDITION: core intrinsics unit is missing or has another interner")
	}
	in := f.authority.TypeInterner
	declared := make(map[string][]types.TypeID)
	for _, sym := range core.Symbols.Table.Symbols.Data() {
		name, _ := core.Builder.StringsInterner.Lookup(sym.Name)
		if sym.Kind == symbols.SymbolType && sym.Flags&symbols.SymbolFlagBuiltin != 0 && sym.Decl.ASTFile == core.FileID &&
			(name == "Channel" || name == "Task" || name == "Range") {
			declared[name] = append(declared[name], sym.Type)
		}
	}
	for _, name := range []string{"Channel", "Task", "Range"} {
		if len(declared[name]) != 1 {
			t.Fatalf("PRECONDITION: core %s type symbol is not unique: %v", name, declared[name])
		}
	}
	channel, ok := in.StructInfo(declared["Channel"][0])
	if !ok || channel == nil {
		t.Fatal("PRECONDITION: core Channel declaration has no struct info")
	}
	counted := 0
	for id := types.TypeID(1); ; id++ {
		typ, ok := in.Lookup(id)
		if !ok {
			break
		}
		// The family is marked on structs; `own` and aliases answer through them.
		if typ.Kind != types.KindStruct || !in.IsRuntimeHandleType(id) || !in.IsRefCountedHandle(id) {
			continue
		}
		info, found := in.StructInfo(id)
		if !found || info == nil || info.Name != channel.Name || info.Decl != channel.Decl {
			t.Errorf("counted runtime handle %d is not a core Channel instance: %+v", id, info)
		}
		counted++
	}
	if counted == 0 {
		t.Error("no counted Channel instance was interned")
	}
	for _, name := range []string{"Task", "Range"} {
		id := declared[name][0]
		if !in.IsRuntimeHandleType(id) || in.IsRefCountedHandle(id) {
			t.Errorf("core %s: runtime handle=%v counted=%v, want true/false", name, in.IsRuntimeHandleType(id), in.IsRefCountedHandle(id))
		}
	}
}
