package driver

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"surge/internal/sema"
)

// A plain struct declared in one owning unit is certified in that unit when an
// opaque result in another unit names it. Only the dependency module varies; the
// full core input stays present, so every assertion is local to dep/main.sg.
// A bare `@copy` or `@shard_movable` counts as plain; any other attribute, alone
// or beside them, keeps the refusal.
type crossUnitStructCase struct {
	name, text, digest string
	// clean: no Pending may remain anywhere in the dependency source.
	clean bool
	// unsupported: this declaration name must keep the unsupported refusal.
	unsupported string
	// callSite: no Pending may remain at exactly this operation.
	callSite string
}

func crossUnitStructCases() []crossUnitStructCase {
	return []crossUnitStructCase{
		{name: "owned_fields", clean: true, digest: "28aa4d455deb3ab3488013825be888e2f4e3fbf373deba8868a536d035774311",
			text: "pragma module::dep;\n@intrinsic fn make() -> Error;\nfn use_error() -> uint {\n    let e = make();\n    return e.code;\n}\n"},
		{name: "attributed_control", unsupported: "place", digest: "90f77eb455e6270b60b0e08ff8198ffdb4cf37a2685d58813f4e7a78b1d1cf04",
			text: "pragma module::dep;\n@intrinsic fn place() -> Placement;\nfn use_place() -> nothing {\n    let p = place();\n    return nothing;\n}\n"},
		{name: "same_unit_control", clean: true, digest: "25278ad36b6ed94a6b999efaf753f3b9973fbd87d76bdfc5d13e4e6194e4c0fc",
			text: "pragma module::dep;\ntype Local = { n: uint };\n@intrinsic fn local() -> Local;\nfn use_local() -> uint {\n    let l = local();\n    return l.n;\n}\n"},
		{name: "generic_call_site", callSite: "wrap::<uint>(1:uint)", digest: "91bf6167d176fbbe0ef6fcbe1c92409958c82af88d1e5add625cedaf281c2db3",
			text: "pragma module::dep;\n@intrinsic fn wrap<T>(v: T) -> Erring<T, Error>;\nfn use_wrap() -> uint {\n    let r = wrap::<uint>(1:uint);\n    return 0:uint;\n}\n"},
		{name: "copy_struct", clean: true, digest: "d87614155026459fc0e687971e96c6b3b636c6c3cfee3f656f7713f37e4fc724",
			text: "pragma module::dep;\n@copy type Held = { n: uint, gate: Channel<uint> };\n@intrinsic fn held() -> Held;\nfn use_held() -> uint {\n    let h = held();\n    return h.n;\n}\n"},
		{name: "copy_channel_payload", clean: true, digest: "54a5f4e79b4c557576719c27c40963119ef2e35cbe85ebca466f0c0cd29e9089",
			text: "pragma module::dep;\n@copy type Held = { n: uint, gate: Channel<uint> };\n@intrinsic fn ring() -> own Channel<Held>;\nfn use_ring() -> nothing {\n    let c = ring();\n    return nothing;\n}\n"},
		{name: "sealed_control", unsupported: "sealed", digest: "72c56fc88e11502d9549cfbbda9fcd52d66fc25b339144b4bd0e3b658f75c993",
			text: "pragma module::dep;\n@sealed type Sealed = { n: uint };\n@intrinsic fn sealed() -> Sealed;\nfn use_sealed() -> uint {\n    let s = sealed();\n    return s.n;\n}\n"},
		{name: "copy_shard_movable_struct", clean: true, digest: "c61297e8cd612d0fc0918335b3f424c1c3b0a53773a7db544ced5ba6cfbeadeb",
			text: "pragma module::dep;\n@copy @shard_movable type Both = { n: uint };\n@intrinsic fn both() -> Both;\nfn use_both() -> uint {\n    let b = both();\n    return b.n;\n}\n"},
		{name: "copy_placement_field_control", unsupported: "where_at", digest: "318169c7527ecffc87ad06253dbdfe8aa30ef8713d431482326f841775f0ba2d",
			text: "pragma module::dep;\n@copy type Where = { p: Placement };\n@intrinsic fn where_at() -> Where;\nfn use_where() -> nothing {\n    let w = where_at();\n    return nothing;\n}\n"},
		{name: "copy_counted_placement_field_control", unsupported: "relay", digest: "8ec637d9d76c33376e2fa9bed2a3fb6b9c5f19a357383bac1bdb191549f76022",
			text: "pragma module::dep;\n@copy type Relay = { c: Channel<Placement> };\n@intrinsic fn relay() -> Relay;\nfn use_relay() -> nothing {\n    let r = relay();\n    return nothing;\n}\n"},
		{name: "copy_argument_control", unsupported: "arg", digest: "99a8fe74b905c2d52298ca8e69669c25ee635a516d6f0703be74b8458560491b",
			text: "pragma module::dep;\n@copy(1) type Arg = { n: uint };\n@intrinsic fn arg() -> Arg;\nfn use_arg() -> uint {\n    let a = arg();\n    return a.n;\n}\n"},
		{name: "cross_unit_copy_struct", clean: true, digest: "c2cea51ab0b867151bc80a31395bdc8974416df2e5ccec7ee8943425e56f1252",
			text: "pragma module::dep;\n@intrinsic fn mutex() -> Mutex;\nfn use_mutex() -> nothing {\n    let m = mutex();\n    return nothing;\n}\n"},
		{name: "copy_fn_field_control", unsupported: "call", digest: "ed1e19264d8ac3227b04266a20f7fdef364744313b1f8783e3087349b415b84c",
			text: "pragma module::dep;\n@copy type Call = { f: fn(uint) -> uint };\n@intrinsic fn call() -> Call;\nfn use_call() -> nothing {\n    let c = call();\n    return nothing;\n}\n"},
		{name: "shard_movable_struct", clean: true, digest: "6b7ddfa51c70bc7e7fd2688e1d66ef30e16ec336ee3c8cde9edb19482b7b04eb",
			text: "pragma module::dep;\n@shard_movable type Job = { id: uint64, payload: string };\n@intrinsic fn job() -> Job;\nfn use_job() -> uint64 {\n    let j = job();\n    return j.id;\n}\n"},
		{name: "send_beside_movable_control", unsupported: "sent", digest: "be00d564881cfac2d3e97b560833860b94a3672e082cda007e8abc34dc7ce5c5",
			text: "pragma module::dep;\n@send @shard_movable type Sent = { id: int };\n@intrinsic fn sent() -> Sent;\nfn use_sent() -> int {\n    let s = sent();\n    return s.id;\n}\n"},
		{name: "copy_plus_pinned_control", unsupported: "pin", digest: "a85f5d602fb258fc5bbb7aa6ff38ad0c13763dcae192c4bb663fb35a1bc6fb13",
			text: "pragma module::dep;\n@copy @shard_pinned type Pin = { n: uint };\n@intrinsic fn pin() -> Pin;\nfn use_pin() -> uint {\n    let p = pin();\n    return p.n;\n}\n"},
	}
}

func TestAnalyzeCrossUnitPlainStructCertificate(t *testing.T) {
	for _, tc := range crossUnitStructCases() {
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
			logReturnOriginCallEvidence(t, map[string]any{"stage": "cross_unit_struct", "case": tc.name,
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
			case tc.unsupported != "":
				start := strings.Index(tc.text, "fn "+tc.unsupported+"(") + len("fn ")
				found := false
				for _, pending := range local {
					found = found || pending.Reason == genericConditionUnsupported && int(pending.Span.Start) == start
				}
				if !found {
					t.Errorf("attributed struct lost its unsupported refusal at %q: %+v", tc.unsupported, local)
				}
			default:
				start := strings.Index(tc.text, tc.callSite)
				for _, pending := range local {
					if int(pending.Span.Start) == start && int(pending.Span.End) == start+len(tc.callSite) {
						t.Errorf("generic call site left unfinished: %s", pending.Reason)
					}
				}
			}
		})
	}
}
