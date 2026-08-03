package sema

import "testing"

// A tuple-pattern binding names a projected element, not the whole compare
// subject. When the subject came through a borrow, a move-only element remains
// the tuple owner's storage under another name and the arm must not release it.
//
// The two owned controls pin the opposite answer: an owned tuple transfers both
// elements into the arm, and handing one element onward only retracts that one
// obligation.
func TestCompareBorrowedTupleBindingsAreAliases(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		wantDrops int
	}{
		{
			name: "borrowed tuple decomposition",
			source: `
fn peek(x: &string) -> int { return 1; }

fn inspect(pair: &(string, string)) -> int {
    return compare *pair {
        (left, right) => peek(&left) + peek(&right);
    };
}
`,
			wantDrops: 0,
		},
		{
			name: "owned tuple decomposition",
			source: `
fn peek(x: &string) -> int { return 1; }

fn inspect(pair: (string, string)) -> int {
    return compare pair {
        (left, right) => peek(&left) + peek(&right);
    };
}
`,
			wantDrops: 2,
		},
		{
			name: "copy tuple decomposition owns its clone",
			source: `
@copy type Pair = (string, string);

fn peek(x: &string) -> int { return 1; }

fn inspect(pair: &Pair) -> int {
    return compare *pair {
        (left, right) => peek(&left) + peek(&right);
    };
}
`,
			wantDrops: 2,
		},
		{
			name: "copy tuple alias decomposition owns its clone",
			source: `
@copy type CopyPair = (string, string);
type PairAlias = CopyPair;

fn peek(x: &string) -> int { return 1; }

fn inspect(pair: &PairAlias) -> int {
    return compare *pair {
        (left, right) => peek(&left) + peek(&right);
    };
}
`,
			wantDrops: 2,
		},
		{
			name: "copy tuple two-alias decomposition owns its clone",
			source: `
@copy type CopyPair = (string, string);
type PairAlias = CopyPair;
type PairAliasTwice = PairAlias;

fn peek(x: &string) -> int { return 1; }

fn inspect(pair: &PairAliasTwice) -> int {
    return compare *pair {
        (left, right) => peek(&left) + peek(&right);
    };
}
`,
			wantDrops: 2,
		},
		{
			name: "noncopy tuple alias chain remains borrowed",
			source: `
type MovePair = (string, string);
type PairAlias = MovePair;
type PairAliasTwice = PairAlias;

fn peek(x: &string) -> int { return 1; }

fn inspect(pair: &PairAliasTwice) -> int {
    return compare *pair {
        (left, right) => peek(&left) + peek(&right);
    };
}
`,
			wantDrops: 0,
		},
		{
			name: "owned tuple element handed onward",
			source: `
fn take_left(pair: (string, string)) -> string {
    return compare pair {
        (left, right) => left;
    };
}
`,
			wantDrops: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parseBag, semaBag, res := runSemaOnSnippetResult(t, tt.source)
			requireNoSemaErrors(t, parseBag, semaBag)
			if res == nil {
				t.Fatal("no sema result")
			}

			gotDrops := 0
			for _, drops := range res.ArmDropsExpr {
				gotDrops += len(drops)
			}
			if gotDrops != tt.wantDrops {
				t.Fatalf("expected %d tuple-pattern drop obligations, got %d across %d arms", tt.wantDrops, gotDrops, len(res.ArmDropsExpr))
			}
		})
	}
}
