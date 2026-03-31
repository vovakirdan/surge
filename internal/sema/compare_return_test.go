package sema

import (
	"testing"

	"surge/internal/diag"
)

func TestCompareStmtAllReturnDoesNotNeedTrailingReturn(t *testing.T) {
	src := `
fn classify(flag: bool) -> int {
    compare flag {
        true => {
            return 0;
        }
        false => {
            return 1;
        }
    };
}
`
	builder, fileID, bag := parseSource(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(bag))
	}
	symRes := resolveSymbols(t, builder, fileID)
	semaBag := diag.NewBag(16)
	Check(t.Context(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: semaBag},
		Symbols:  symRes,
	})
	if len(semaBag.Items()) != 0 {
		t.Fatalf("unexpected sema diagnostics: %s", diagnosticsSummary(semaBag))
	}
}

func TestCompareStmtCatchAllBindingAllReturnDoesNotNeedTrailingReturn(t *testing.T) {
	src := `
fn print_error(flag: bool) -> int {
    compare flag {
        true => {
            return 0;
        }
        other => {
            return 1;
        }
    };
}
`
	builder, fileID, bag := parseSource(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(bag))
	}
	symRes := resolveSymbols(t, builder, fileID)
	semaBag := diag.NewBag(16)
	Check(t.Context(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: semaBag},
		Symbols:  symRes,
	})
	if len(semaBag.Items()) != 0 {
		t.Fatalf("unexpected sema diagnostics: %s", diagnosticsSummary(semaBag))
	}
}

func TestCompareStmtTaggedUnionAllReturnDoesNotNeedTrailingReturn(t *testing.T) {
	src := `
tag ResOk<T>(T);
type Result<T, E> = ResOk(T) | E;

fn classify(res: Result<int, string>) -> int {
    compare res {
        ResOk(v) => {
            return v;
        }
        err => {
            return 0;
        }
    };
}
`
	builder, fileID, bag := parseSource(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(bag))
	}
	symRes := resolveSymbols(t, builder, fileID)
	semaBag := diag.NewBag(16)
	Check(t.Context(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: semaBag},
		Symbols:  symRes,
	})
	if len(semaBag.Items()) != 0 {
		t.Fatalf("unexpected sema diagnostics: %s", diagnosticsSummary(semaBag))
	}
}

func TestCompareStmtNonExhaustiveStillNeedsReturn(t *testing.T) {
	src := `
fn classify(flag: bool) -> int {
    compare flag {
        true => {
            return 1;
        }
    };
}
`
	builder, fileID, bag := parseSource(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(bag))
	}
	symRes := resolveSymbols(t, builder, fileID)
	semaBag := diag.NewBag(16)
	Check(t.Context(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: semaBag},
		Symbols:  symRes,
	})
	if !hasCode(semaBag, diag.SemaMissingReturn) {
		t.Fatalf("expected %v diagnostic, got %s", diag.SemaMissingReturn, diagnosticsSummary(semaBag))
	}
}
