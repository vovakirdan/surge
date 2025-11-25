package symbols

import (
	"testing"

	"surge/internal/diag"
)

func TestWildcardLetDeclarationsAllowed(t *testing.T) {
	src := `
            fn main() {
                let _ = 1;
                let _ = 2;
            }
        `
	builder, fileID, parseBag := parseSnippet(t, src)
	if parseBag.Len() != 0 {
		t.Fatalf("unexpected parse diagnostics: %d", parseBag.Len())
	}

	bag := diag.NewBag(4)
	_ = ResolveFile(builder, fileID, &ResolveOptions{Reporter: &diag.BagReporter{Bag: bag}, Validate: true})

	if bag.Len() != 0 {
		t.Fatalf("expected no diagnostics, got %d", bag.Len())
	}
}

func TestWildcardParamsAllowed(t *testing.T) {
	src := `
            fn f(_ : int) {}
            fn g(x: int, _ : string) {}

            fn main() {
                f(1);
                g(1, "hello");
            }
        `
	builder, fileID, parseBag := parseSnippet(t, src)
	if parseBag.Len() != 0 {
		t.Fatalf("unexpected parse diagnostics: %d", parseBag.Len())
	}

	bag := diag.NewBag(4)
	_ = ResolveFile(builder, fileID, &ResolveOptions{Reporter: &diag.BagReporter{Bag: bag}, Validate: true})

	if bag.Len() != 0 {
		t.Fatalf("expected no diagnostics, got %d", bag.Len())
	}
}

func TestWildcardAsValueIsError(t *testing.T) {
	src := `
            fn main() {
                let _ = 1;
                let x = _;
            }
        `
	builder, fileID, parseBag := parseSnippet(t, src)
	if parseBag.Len() != 0 {
		t.Fatalf("unexpected parse diagnostics: %d", parseBag.Len())
	}

	bag := diag.NewBag(4)
	_ = ResolveFile(builder, fileID, &ResolveOptions{Reporter: &diag.BagReporter{Bag: bag}, Validate: true})

	if bag.Len() != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", bag.Len())
	}
	if bag.Items()[0].Code != diag.SemaWildcardValue {
		t.Fatalf("expected SemaWildcardValue, got %v", bag.Items()[0].Code)
	}
}

func TestWildcardMutIsError(t *testing.T) {
	src := `
            fn main() {
                let mut _ = 1;
            }
        `
	builder, fileID, parseBag := parseSnippet(t, src)
	if parseBag.Len() != 0 {
		t.Fatalf("unexpected parse diagnostics: %d", parseBag.Len())
	}

	bag := diag.NewBag(4)
	_ = ResolveFile(builder, fileID, &ResolveOptions{Reporter: &diag.BagReporter{Bag: bag}, Validate: true})

	if bag.Len() != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", bag.Len())
	}
	if bag.Items()[0].Code != diag.SemaWildcardMut {
		t.Fatalf("expected SemaWildcardMut, got %v", bag.Items()[0].Code)
	}
}

func TestTopLevelWildcardLetsAllowed(t *testing.T) {
	src := `
            fn init_logging() {}
            fn register_metrics() {}
            let _ = init_logging();
            let _ = register_metrics();
            fn main() {}
        `
	builder, fileID, parseBag := parseSnippet(t, src)
	if parseBag.Len() != 0 {
		t.Fatalf("unexpected parse diagnostics: %d", parseBag.Len())
	}

	bag := diag.NewBag(4)
	_ = ResolveFile(builder, fileID, &ResolveOptions{Reporter: &diag.BagReporter{Bag: bag}, Validate: true})

	if bag.Len() != 0 {
		t.Fatalf("expected no diagnostics, got %d", bag.Len())
	}
}

func TestWildcardAssignmentRemainsError(t *testing.T) {
	src := `
            fn main() {
                _ = 42;
            }
        `
	builder, fileID, parseBag := parseSnippet(t, src)
	if parseBag.Len() != 0 {
		t.Fatalf("unexpected parse diagnostics: %d", parseBag.Len())
	}

	bag := diag.NewBag(4)
	_ = ResolveFile(builder, fileID, &ResolveOptions{Reporter: &diag.BagReporter{Bag: bag}, Validate: true})

	if bag.Len() != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", bag.Len())
	}
	if bag.Items()[0].Code != diag.SemaWildcardValue {
		t.Fatalf("expected SemaWildcardValue, got %v", bag.Items()[0].Code)
	}
}
