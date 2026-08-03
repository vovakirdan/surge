package hir_test

import (
	"testing"

	"surge/internal/hir"
)

const compareBorrowedTupleHIRSource = `
@copy type CopyPair = (string, string);
type CopyPairAlias = CopyPair;
type CopyPairAliasTwice = CopyPairAlias;
type MovePair = (string, string);
type MovePairAlias = MovePair;
type MovePairAliasTwice = MovePairAlias;

fn peek(x: &string) -> int { return 1; }

fn borrowed_tuple(pair: &(string, string)) -> int {
    return compare *pair {
        (left, right) => peek(&left) + peek(&right);
    };
}

fn owned_tuple(pair: (string, string)) -> int {
    return compare pair {
        (left, right) => peek(&left) + peek(&right);
    };
}

fn copied_tuple(pair: &CopyPair) -> int {
    return compare *pair {
        (left, right) => peek(&left) + peek(&right);
    };
}

fn copied_tuple_alias(pair: &CopyPairAlias) -> int {
    return compare *pair {
        (left, right) => peek(&left) + peek(&right);
    };
}

fn copied_tuple_alias_twice(pair: &CopyPairAliasTwice) -> int {
    return compare *pair {
        (left, right) => peek(&left) + peek(&right);
    };
}

fn borrowed_tuple_alias_twice(pair: &MovePairAliasTwice) -> int {
    return compare *pair {
        (left, right) => peek(&left) + peek(&right);
    };
}

fn hand_left_onward(pair: (string, string)) -> string {
    return compare pair {
        (left, right) => left;
    };
}
`

func TestLowerCompareTupleBindingDropsMatchSubjectOwnership(t *testing.T) {
	mod, _, err := parseAndLower(t, compareBorrowedTupleHIRSource)
	if err != nil {
		t.Fatalf("failed to lower tuple compare controls: %v", err)
	}

	var countBlock func(*hir.Block) int
	var countExpr func(*hir.Expr) int
	countExpr = func(expr *hir.Expr) int {
		if expr == nil {
			return 0
		}
		switch data := expr.Data.(type) {
		case hir.BlockExprData:
			return countBlock(data.Block)
		case hir.IfData:
			return countExpr(data.Then) + countExpr(data.Else)
		case hir.OwnedTempData:
			return countExpr(data.Inner)
		case hir.RaiseReleaseGuardData:
			return countExpr(data.Inner)
		default:
			return 0
		}
	}
	countBlock = func(block *hir.Block) int {
		if block == nil {
			return 0
		}
		total := 0
		for i := range block.Stmts {
			stmt := &block.Stmts[i]
			switch data := stmt.Data.(type) {
			case hir.LetData:
				total += countExpr(data.Value)
			case hir.ExprStmtData:
				total += countExpr(data.Expr)
			case hir.AssignData:
				total += countExpr(data.Value)
			case hir.ReturnData:
				total += len(data.DropsAfterValue) + countExpr(data.Value)
			case hir.RetData:
				total += len(data.DropsAfterValue) + countExpr(data.Value)
			case hir.IfStmtData:
				total += countBlock(data.Then) + countBlock(data.Else)
			case hir.WhileData:
				total += countBlock(data.Body)
			case hir.ForData:
				total += countBlock(data.Body)
			case hir.BlockStmtData:
				total += countBlock(data.Block)
			}
		}
		return total
	}

	tests := []struct {
		fn        string
		wantDrops int
	}{
		{"borrowed_tuple", 0},
		{"owned_tuple", 2},
		{"copied_tuple", 2},
		{"copied_tuple_alias", 2},
		{"copied_tuple_alias_twice", 2},
		{"borrowed_tuple_alias_twice", 0},
		{"hand_left_onward", 1},
	}
	for _, tt := range tests {
		t.Run(tt.fn, func(t *testing.T) {
			var fn *hir.Func
			for _, candidate := range mod.Funcs {
				if candidate != nil && candidate.Name == tt.fn {
					fn = candidate
					break
				}
			}
			if fn == nil {
				t.Fatalf("function %q did not reach HIR", tt.fn)
			}
			if got := countBlock(fn.Body); got != tt.wantDrops {
				t.Fatalf("%s: carried exit drops = %d, want %d", tt.fn, got, tt.wantDrops)
			}
		})
	}
}
