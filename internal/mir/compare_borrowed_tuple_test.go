package mir_test

import (
	"testing"

	"surge/internal/mir"
)

const compareBorrowedTupleMIRSource = `
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

func TestMIRCompareTupleBindingDropsMatchSubjectOwnership(t *testing.T) {
	compiled := compileCrossingMIR(t, compareBorrowedTupleMIRSource, nil)
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
			var fn *mir.Func
			for _, id := range compiled.mod.SortedFuncIDs() {
				candidate := compiled.mod.Funcs[id]
				if candidate != nil && candidate.Name == tt.fn {
					fn = candidate
					break
				}
			}
			if fn == nil {
				t.Fatalf("function %q did not reach MIR", tt.fn)
			}
			gotDrops := 0
			for i := range fn.Blocks {
				for _, ins := range fn.Blocks[i].Instrs {
					if ins.Kind != mir.InstrDrop || ins.Drop.Place.Kind != mir.PlaceLocal {
						continue
					}
					local := ins.Drop.Place.Local
					if local < 0 || int(local) >= len(fn.Locals) {
						continue
					}
					switch fn.Locals[local].Name {
					case "left", "right":
						gotDrops++
					}
				}
			}
			if gotDrops != tt.wantDrops {
				t.Fatalf("%s: tuple-binding MIR drops = %d, want %d", tt.fn, gotDrops, tt.wantDrops)
			}
		})
	}
}
