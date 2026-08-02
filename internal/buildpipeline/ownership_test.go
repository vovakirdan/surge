package buildpipeline

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"surge/internal/mir"
)

const ownershipDevGateNegativeSource = `
type NodeId = uint;

type Node = {
    next: NodeId?,
    data: int,
}

fn walk(nodes: &Node[], start: NodeId?) -> nothing {
    let mut id: NodeId? = start;
    while true {
        compare id {
            Some(i) => {
                let n: &Node = nodes[(i to int)];
                id = *n.next;
            }
            nothing => { return nothing; }
        };
    }
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
