package llvm

import (
	"strings"
	"testing"

	"surge/internal/sema"
)

// RV2-DEBT-324. The anchor of an `on far_handle { ... }` block is packed into
// the crossing state like every other capture -- the body's `ch` is that
// field -- but the state does not own it: the caller keeps the handle, the
// body reaches the channel through the owner-side pin, and the state's copy
// of the token has no reader and no release. The state's drop glue used to
// release it like an owned member, so every UNSHIPPED state (a refused,
// torn-down or shutdown-swept request) freed the caller's token under the
// caller. MIR names the lease field per state (Module.CrossingLeaseFields)
// and the release glue skips it.
const anchorLeaseSource = `
async fn holder() -> int {
    let ch: far Channel<int> = channel_on::<int>(shard(1:ShardId), 1);
    let s1: TaskResult<nothing> = on ch { ch.send(41); ret nothing; };
    let _ = s1;
    return 0;
}
`

func anchorLeaseStateGlue(t *testing.T) (glue string, everywhere int) {
	t.Helper()
	mirMod, result := lowerCrossingMIRFromSource(
		t, anchorLeaseSource, sema.CrossingLoweringOnFarHandle, sema.CrossingLoweringChannelCreate)
	if len(mirMod.CrossingLeaseFields) != 1 {
		t.Fatalf("lease fields recorded for %d state types, want exactly the one anchored block: %v",
			len(mirMod.CrossingLeaseFields), mirMod.CrossingLeaseFields)
	}
	var glueName string
	for state, fields := range mirMod.CrossingLeaseFields {
		if len(fields) != 1 || fields[0] != 1 {
			t.Fatalf("the anchor is capture 0, so the lease field is 1; got %v", fields)
		}
		glueName = dropGlueName(state)
	}
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	head := "define void @" + glueName + "("
	start := strings.Index(ir, head)
	if start < 0 {
		t.Fatalf("no release glue %s for the anchored block's state in:\n%s", glueName, ir)
	}
	end := strings.Index(ir[start:], "\n}\n")
	if end < 0 {
		t.Fatalf("release glue %s has no end:\n%s", glueName, ir)
	}
	glue = ir[start : start+end]
	return glue, strings.Count(ir, "call void @rt_far_channel_handle_drop(")
}

func TestEmitAnchoredStateGlueLeavesTheCallersHandleAlone(t *testing.T) {
	glue, everywhere := anchorLeaseStateGlue(t)
	if strings.Contains(glue, "rt_far_channel_handle_drop") {
		t.Fatalf("the anchored block's state releases the caller's handle:\n%s", glue)
	}
	// The one release is still the caller's own: the handle local's drop in
	// the holder's frame, not the state's.
	if everywhere == 0 {
		t.Fatal("no far-handle release anywhere: the caller's own drop of `ch` is gone too")
	}
}

// Rule 13: the pre-fix glue, restored under the negative-control switch, must
// release the lease field -- the same body, one more far-handle drop.
func TestEmitAnchoredStateGlueNegativeControl(t *testing.T) {
	fixed, fixedEverywhere := anchorLeaseStateGlue(t)
	dropLeaseFieldsNegativeControl = true
	defer func() { dropLeaseFieldsNegativeControl = false }()
	mutant, mutantEverywhere := anchorLeaseStateGlue(t)
	if strings.Count(mutant, "rt_far_channel_handle_drop") != 1 {
		t.Fatalf("negative control did not restore the lease field's release:\n%s", mutant)
	}
	if strings.Contains(fixed, "rt_far_channel_handle_drop") {
		t.Fatalf("fixed glue releases the lease field:\n%s", fixed)
	}
	if mutantEverywhere != fixedEverywhere+1 {
		t.Fatalf("mutant emits %d far-handle releases, fixed %d; want exactly one more",
			mutantEverywhere, fixedEverywhere)
	}
}
