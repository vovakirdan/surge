package driver

import (
	"testing"
)

// A default value of a provably Defaultable type carries no origin, whether the
// program writes `default::<T>()`, declares a binding without a value, or SEMA
// synthesizes the call. A type whose Defaultable walk cannot be proven keeps the
// answer it has today: the widening is additive, never a replacement.
const defaultValueSource = `type Leaf = { id: int, label: string };
type Tree = { leaves: Leaf[], fixed: Leaf[2] };
fn made() -> Tree {
    let t = default::<Tree>();
    return t;
}
fn declared() -> int {
    let x: int;
    let _t: Tree;
    return x;
}
fn nothing_ref() -> Option<&int> {
    let o: Option<&int>;
    return o;
}
fn placement() -> Placement {
    return default::<Placement>();
}
fn safe_int(o: Option<int>) -> int {
    return o.safe();
}
`

const defaultValueDigest = "7208742e4b71ce35e3c14b09326abcb9aa8c62f68b1ad04c5c9b0e641348269a"

const originUninitializedRefusal = "uninitialized binding has no proven reference contents"

const originUseWithoutOperation = "generic use lacks its original typed operation"

const originCalleeRefusal = "callee returned an unproved source"

// The use row this widening clears lives in core, not in the test source, so it is
// asserted by source key rather than through the leaf table, whose filter is the
// program's own key and byte range (return_origin_clone_selection_test.go:155, 169).
const originGenericOpaqueUse = "generic opaque use requires its type-dependent effect transfer"

const coreOptionSourceKey = "core/option.sg"

func TestAnalyzeDefaultValues(t *testing.T) {
	checkOriginSource(t, defaultValueSource, defaultValueDigest)
	f, analysis := analyzeOriginRoot(t, "default_values", defaultValueSource, false, nil)
	checkOriginBodyLeaves(t, analysis, f, defaultValueSource, defaultValueDigest, []originBodyLeaf{
		{name: "written_default", body: "made", clean: true,
			function: originSpan{88, 154, "fn made() -> Tree {\n    let t = default::<Tree>();\n    return t;\n}"},
			cleared: []originRefusal{
				{originSpan{120, 137, "default::<Tree>()"}, genericConditionUnsupported},
				{originSpan{120, 137, "default::<Tree>()"}, originCalleeRefusal},
			}},
		{name: "declared_without_value", body: "declared", clean: true,
			function: originSpan{155, 227, "fn declared() -> int {\n    let x: int;\n    let _t: Tree;\n    return x;\n}"},
			cleared: []originRefusal{
				{originSpan{182, 193, "let x: int;"}, originUninitializedRefusal},
				{originSpan{198, 211, "let _t: Tree;"}, originUninitializedRefusal},
				{originSpan{198, 211, "let _t: Tree;"}, originUseWithoutOperation},
			}},
		// The union arm of the Defaultable walk returns on the first `nothing`
		// member without walking the others, and the value the lowering builds is
		// that `nothing` tag (internal/vm/intrinsic_default.go:128-137), which holds
		// no reference. This leaf is the witness of that premise and is mandatory.
		{name: "defaultable_union_of_reference", body: "nothing_ref", clean: true,
			function: originSpan{228, 303, "fn nothing_ref() -> Option<&int> {\n    let o: Option<&int>;\n    return o;\n}"},
			cleared: []originRefusal{
				{originSpan{267, 287, "let o: Option<&int>;"}, originUninitializedRefusal},
				{originSpan{267, 287, "let o: Option<&int>;"}, originUseWithoutOperation},
			}},
		// Additivity control: `Placement` is `@intrinsic`, so its Defaultable walk is
		// unsupported and the arm must fall through to the answer it has today.
		{name: "unproven_default_control", body: "placement",
			function: originSpan{304, 370, "fn placement() -> Placement {\n    return default::<Placement>();\n}"},
			stays:    []originRefusal{{originSpan{345, 367, "default::<Placement>()"}, genericConditionUnsupported}}},
	})
	// The row `safe_int` exercises lives in core, outside every leaf's filter, so it
	// is asserted here by source key. `safe_int` stays in the frozen source as the
	// program that instantiates `Option<int>.safe`, but it is not a leaf.
	coreUse := originSpan{260, 274, "default::<T>()"}
	if originPendingAt(analysis, coreOptionSourceKey, coreUse, originGenericOpaqueUse) {
		t.Errorf("core `safe` default use kept its refusal at %s %d:%d: %+v", coreOptionSourceKey,
			coreUse.start, coreUse.end, originPendingWithin(analysis, coreOptionSourceKey, coreUse.start, coreUse.end))
	}
}

// An `@entrypoint` whose result implements the exit conversion has its synthetic
// `main().__to(int)` checked by the callee's own generic promise.
const entrypointExitSource = `@entrypoint
fn main() -> int? {
    return nothing;
}
`

const entrypointExitDigest = "0a8c5c07df9d51771c8207cb71d0ae9876ea77293040998caaebb63bc122d02b"

func TestAnalyzeEntrypointExitConversion(t *testing.T) {
	site := originSpan{22, 29, "-> int?"}
	checkOriginSource(t, entrypointExitSource, entrypointExitDigest, site)
	f, analysis := analyzeOriginRoot(t, "entrypoint_exit", entrypointExitSource, false, nil)
	if originPendingAt(analysis, f.unit.SourceKey, site, originUseWithoutOperation) {
		t.Errorf("entrypoint exit conversion kept its refusal at %q: %+v", site.snippet,
			originPendingWithin(analysis, f.unit.SourceKey, 0, len(entrypointExitSource)))
	}
}
