package sema

import (
	"testing"

	"surge/internal/diag"
)

func TestTaskCreatedOutsideScopeDiagnosticContract(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
type Task<T> = { __opaque: int };

fn bad() -> Task<nothing> {
    let earlier = async { ret 1; };
    return async {
        let started = spawn earlier;
        let _ = started;
        ret;
    };
}
`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	var found *diag.Diagnostic
	for _, item := range semaBag.Items() {
		if item != nil && item.Code == diag.SemaTaskCreatedOutsideScope {
			if found != nil {
				t.Fatalf("expected one SEM3209, got %s", diagnosticsSummary(semaBag))
			}
			found = item
		}
	}
	if found == nil {
		t.Fatalf("expected SEM3209, got %s", diagnosticsSummary(semaBag))
	}
	if found.Message != "cannot spawn a task created outside the current scope" {
		t.Fatalf("unexpected headline: %q", found.Message)
	}
	if len(found.Notes) != 1 ||
		found.Notes[0].Msg != "task was created here, outside this scope" {
		t.Fatalf("unexpected creation note: %+v", found.Notes)
	}
	if found.Notes[0].Span == found.Primary {
		t.Fatalf("creation note points at spawn instead of creation: %+v", found.Notes[0])
	}
	if len(found.Help) != 1 ||
		found.Help[0].Msg != "create the task inside this scope before spawning it" {
		t.Fatalf("unexpected help: %+v", found.Help)
	}
}

func TestTaskCreatedInsideScopeMayUseOuterBinding(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
type Task<T> = { __opaque: int };

fn valid() -> Task<int> {
    let mut slot: Task<int>;
    return async {
        slot = async { ret 42; };
        let started = spawn slot;
        let _ = started;
        ret 0;
    };
}
`)
	requireNoSemaErrors(t, parseBag, semaBag)
}

func TestTaskCreatedOutsideScopeSurvivesBranchMerge(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
type Task<T> = { __opaque: int };

fn bad(flag: bool) -> Task<int> {
    let earlier = async { ret 1; };
    return async {
		let local = async { ret 2; };
		let selected = flag ? earlier : local;
		let started = spawn selected;
		let _ = started;
		ret 0;
    };
}
`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	found := 0
	for _, item := range semaBag.Items() {
		if item != nil && item.Code == diag.SemaTaskCreatedOutsideScope {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("expected one branch-merged SEM3209, got %s", diagnosticsSummary(semaBag))
	}
}
