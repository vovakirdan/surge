package mir_test

import (
	"testing"

	"surge/internal/mir"
)

func TestLowerBlockRetDoesNotReturnFromFunction(t *testing.T) {
	src := `fn main() -> int {
		let x = { ret 1; };
		let y = 2;
		return x + y;
	}`

	mirMod, _, err := parseAndLowerMIR(t, src)
	if err != nil {
		t.Fatalf("failed to lower: %v", err)
	}
	if mirMod == nil || len(mirMod.Funcs) == 0 {
		t.Fatalf("expected MIR function, got %+v", mirMod)
	}

	var fnEntryChecked bool
	var foundLaterReturn bool
	for _, fn := range mirMod.Funcs {
		if fn == nil || fn.Name != "main" {
			continue
		}
		entryIdx := int(fn.Entry)
		if entryIdx < 0 || entryIdx >= len(fn.Blocks) {
			t.Fatalf("entry block %d out of range", fn.Entry)
		}
		fnEntryChecked = true
		if fn.Blocks[entryIdx].Term.Kind != mir.TermGoto {
			t.Fatalf("expected block-ret to exit via goto from entry, got %s", fn.Blocks[entryIdx].Term.Kind)
		}
		for idx, block := range fn.Blocks {
			if idx == entryIdx {
				continue
			}
			if block.Term.Kind == mir.TermReturn {
				foundLaterReturn = true
				break
			}
		}
	}

	if !fnEntryChecked {
		t.Fatal("main function not found in MIR")
	}
	if !foundLaterReturn {
		t.Fatal("expected a function return after block-ret exit block")
	}
}
