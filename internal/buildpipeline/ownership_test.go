package buildpipeline

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"surge/internal/mir"
)

const ownershipDevGateNegativeSource = `
tag Payload(string);
tag Empty();

type Slot = Payload(string) | Empty();

type Holder = { held: string }

// A payload bound out of a BORROWED union and stored into a projection. The
// binding names the union's own storage, so the store leaves two owners for one
// string — a real alias, and one the compiler still accepts.
//
// It replaced a walk over NodeId? that did id = *n.next, which stopped
// compiling when the shared-borrow rule landed: taking a heap-owning value out
// of a & is refused outright now, at sema, which is the earlier stage and the
// better place. That made the old program a compile error rather than a
// report-only control, and a control the default build rejects cannot test what
// the dev flag does to a build that succeeds.
//
// This shape survives for a reason worth stating rather than relying on: the
// rule refuses a deref that PRODUCES a value and an arm that ANSWERS with its
// payload, and this does neither — it stores the payload sideways. That is the
// same defect one step further out, recorded as the residual on the rule's debt
// row, and the control is honest about being an example of it.
fn stash(slot: &Slot, out: &mut Holder) -> nothing {
    compare *slot {
        Payload(s) => { out.held = s; }
        Empty() => { return nothing; }
    };
    return nothing;
}

@entrypoint
fn main() -> int {
    let s: Slot = Payload("x");
    let mut h: Holder = Holder { held = "" };
    stash(&s, &mut h);
    print(h.held.__clone());
    return 0;
}
`

type ownershipProgressRecorder struct {
	events []Event
}

func (r *ownershipProgressRecorder) OnEvent(event Event) {
	r.events = append(r.events, event)
}

func TestCompileDevOwnershipGateRejectsKnownAliasAndPreservesContext(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	path := writeAnalysisSource(t, ownershipDevGateNegativeSource)
	request := &CompileRequest{
		TargetPath:     path,
		BaseDir:        testRepoRoot(t),
		MaxDiagnostics: 20,
		Analysis:       true,
	}

	defaultResult, err := Compile(context.Background(), request)
	if err != nil {
		t.Fatalf("default compile rejected report-only negative control: %v", err)
	}
	if defaultResult.MIR == nil {
		t.Fatal("default compile returned no MIR")
	}
	expectedFindings := mir.VerifyOwnership(
		defaultResult.MIR,
		defaultResult.Diagnose.Sema.TypeInterner,
		defaultResult.Diagnose.Sema,
	)
	if len(expectedFindings) == 0 {
		t.Fatal("negative control is vacuous: report-only verifier found no ownership defect")
	}

	recorder := &ownershipProgressRecorder{}
	request.Dev = true
	request.Progress = recorder
	devResult, err := Compile(context.Background(), request)
	var ownershipErr *OwnershipVerificationError
	if !errors.As(err, &ownershipErr) {
		t.Fatalf("dev compile error = %T %v, want *OwnershipVerificationError", err, err)
	}
	if len(ownershipErr.Findings) == 0 {
		t.Fatal("typed ownership error has no findings")
	}
	if !reflect.DeepEqual(ownershipErr.Findings, expectedFindings) {
		t.Fatalf("hard gate findings differ from report-only verifier:\ngate:   %#v\nreport: %#v",
			ownershipErr.Findings, expectedFindings)
	}
	if devResult.MIR != nil {
		t.Fatal("dev ownership failure must not publish MIR")
	}
	if devResult.Diagnose == nil || devResult.Diagnose.Sema == nil {
		t.Fatal("dev ownership failure did not preserve diagnose/sema context")
	}

	var sawLowerError bool
	for _, event := range recorder.events {
		var eventErr *OwnershipVerificationError
		if event.Stage == StageLower && event.Status == StatusError && errors.As(event.Err, &eventErr) {
			sawLowerError = true
			break
		}
	}
	if !sawLowerError {
		t.Fatalf("progress events do not contain typed lower-stage failure: %+v", recorder.events)
	}

	repeatResult, repeatErr := Compile(context.Background(), request)
	var repeatOwnershipErr *OwnershipVerificationError
	if !errors.As(repeatErr, &repeatOwnershipErr) {
		t.Fatalf("repeat dev compile error = %T %v, want *OwnershipVerificationError", repeatErr, repeatErr)
	}
	if repeatResult.MIR != nil {
		t.Fatal("repeat dev ownership failure published MIR")
	}
	if repeatOwnershipErr.Error() != ownershipErr.Error() {
		t.Fatalf("ownership error is nondeterministic:\nfirst:  %s\nsecond: %s", ownershipErr, repeatOwnershipErr)
	}
}
