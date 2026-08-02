package mir

import "testing"

func TestSplitAsyncAwaitsReindexesAfterBlockGrowth(t *testing.T) {
	blocks := make([]Block, 1)
	blocks[0] = Block{
		ID: 0,
		Instrs: []Instr{{
			Kind: InstrCrossing,
			Crossing: CrossingInstr{
				ReadyBB: NoBlockID,
				PendBB:  NoBlockID,
			},
		}},
		Term: Terminator{Kind: TermReturn},
	}
	f := &Func{Name: "crossing_reallocation", Blocks: blocks, Entry: 0}

	sites, err := splitAsyncAwaits(f)
	if err != nil {
		t.Fatalf("split async crossing: %v", err)
	}
	if len(sites) != 1 {
		t.Fatalf("split sites = %d, want 1", len(sites))
	}

	var crossings int
	for bi := range f.Blocks {
		for ii := range f.Blocks[bi].Instrs {
			if f.Blocks[bi].Instrs[ii].Kind == InstrCrossing {
				crossings++
			}
		}
	}
	if crossings != 1 {
		t.Fatalf("normalized crossing instructions = %d, want 1", crossings)
	}
	if f.Blocks[0].Term.Kind != TermGoto || f.Blocks[0].Term.Goto.Target != sites[0].pollBB {
		t.Fatalf("original block was not rewritten through poll block: %+v", f.Blocks[0].Term)
	}
	if got := collectSuspendSites(f); len(got) != 1 {
		t.Fatalf("collected suspend sites = %d, want 1", len(got))
	}
}
