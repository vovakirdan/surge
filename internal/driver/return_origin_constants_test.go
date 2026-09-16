package driver

import (
	"testing"

	"surge/internal/sema"
)

// A const read is its initializer evaluated afresh, so a reference-free constant
// declared outside the function — a module const or a prelude placement — carries
// no origin. Every const here has a compile-time-constant initializer: SEM3026
// refuses anything else, which is why no loan-carrier const appears (stop S-K2),
// and CF-K1b is recorded unwitnessed rather than given an inadmissible fixture.
const constantReadSource = `const LIMIT: int = 7;
const NAME: string = "n";
fn read_limit() -> int {
    return LIMIT;
}
fn read_name() -> string {
    let s: string = NAME;
    return s;
}
fn read_pool() -> Placement {
    return pool;
}
fn read_distributed() -> Placement {
    return distributed;
}
`

const constantReadDigest = "0f14221027a43823a6a35696b3cb96f902bf6f2fe0a25854cb6e7af7e85cf83b"

const originCapturedRefusal = "captured binding requires origin finalization"

const originModuleMemberRefusal = "module member value needs its selected free-function authority"

func TestAnalyzeConstantReads(t *testing.T) {
	checkOriginSource(t, constantReadSource, constantReadDigest)
	f, analysis := analyzeOriginRoot(t, "constant_reads", constantReadSource, false, nil)
	checkOriginBodyLeaves(t, analysis, f, constantReadSource, constantReadDigest, []originBodyLeaf{
		{name: "module_const", body: "read_limit", clean: true,
			function: originSpan{48, 92, "fn read_limit() -> int {\n    return LIMIT;\n}"},
			cleared:  []originRefusal{{originSpan{84, 89, "LIMIT"}, originCapturedRefusal}}},
		{name: "module_const_string", body: "read_name", clean: true,
			function: originSpan{93, 161, "fn read_name() -> string {\n    let s: string = NAME;\n    return s;\n}"},
			cleared:  []originRefusal{{originSpan{140, 144, "NAME"}, originCapturedRefusal}}},
		{name: "prelude_pool", body: "read_pool", clean: true,
			function: originSpan{162, 210, "fn read_pool() -> Placement {\n    return pool;\n}"},
			cleared:  []originRefusal{{originSpan{203, 207, "pool"}, originCapturedRefusal}}},
		{name: "prelude_distributed", body: "read_distributed", clean: true,
			function: originSpan{211, 273, "fn read_distributed() -> Placement {\n    return distributed;\n}"},
			cleared:  []originRefusal{{originSpan{259, 270, "distributed"}, originCapturedRefusal}}},
	})
}

// The same rule reads a constant named through its module. An imported module
// contributes its own owning units, so this fixture cannot use analyzeOriginRoot:
// its eleven-unit precondition describes a prelude-only program, and the count is
// a property of the input rather than one the rule depends on.
const moduleConstSource = `import stdlib/hash as hash;

fn seed() -> uint64 {
    return hash.STABLE64_SEED;
}
`

const moduleConstDigest = "4a84ba9b8ddb1c756b1534e288ab02ef55f02f2f49fcebc03b15a25d52889e12"

func TestAnalyzeModuleQualifiedConstRead(t *testing.T) {
	site := originSpan{62, 80, "hash.STABLE64_SEED"}
	body := originSpan{29, 83, "fn seed() -> uint64 {\n    return hash.STABLE64_SEED;\n}"}
	checkOriginSource(t, moduleConstSource, moduleConstDigest, site, body)
	res := returnOriginStdlibFixture(t, moduleConstSource, false)
	if err := FinalizeInstantiationClosure(t.Context(), res, 64); err != nil {
		t.Fatalf("PRECONDITION: source closure failed: %v", err)
	}
	inputs, err := collectReturnOriginUnits(res)
	if err != nil || len(inputs.units) < 11 {
		t.Fatalf("PRECONDITION: owning units missing: units=%d error=%v", len(inputs.units), err)
	}
	owner, owners := sema.ReturnOriginUnit{}, 0
	for _, unit := range inputs.units {
		if unit.Builder.Files.Get(unit.FileID).Span.File == res.File.ID {
			owner, owners = unit, owners+1
		}
	}
	if owners != 1 {
		t.Fatalf("PRECONDITION: the test source has %d owning units", owners)
	}
	analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
	if err != nil || analysis == nil {
		t.Fatalf("return-origin analysis did not run: %v", err)
	}
	within := originPendingWithin(analysis, owner.SourceKey, body.start, body.end)
	logReturnOriginCallEvidence(t, map[string]any{"stage": "module_const_read", "units": len(inputs.units),
		"source_key": owner.SourceKey, "pending": within, "diagnostics": analysis.Diagnostics})
	if originPendingAt(analysis, owner.SourceKey, site, originModuleMemberRefusal) {
		t.Errorf("module-qualified const kept its refusal at %q: %+v", site.snippet, within)
	}
	for _, pending := range within {
		t.Errorf("seed left unfinished: %s at %d:%d", pending.Reason, pending.Span.Start, pending.Span.End)
	}
	requireOriginSummary(t, analysis, res.File.ID, "seed", false, nil)
}
