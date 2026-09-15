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
