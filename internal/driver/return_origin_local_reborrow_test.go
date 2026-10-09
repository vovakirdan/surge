package driver

import "testing"

const localReborrowSource = `type Board = { cells: int[2][2] };
fn local(p: &mut Board, r: int, c: int, v: int) -> nothing {
    let cells = &mut p.cells;
    cells[r][c] = v;
    return nothing;
}
fn external(cells: &mut &mut int[2][2], r: int, c: int, v: int) -> nothing {
    cells[r][c] = v;
    return nothing;
}
`

const localReborrowDigest = "d66c39e07856f2b361aa88eb5ebcd0c1d1f6dc283ad99ba6a6592cb129bac4d7"

func TestAnalyzeLocalFieldReborrowIndex(t *testing.T) {
	checkOriginSource(t, localReborrowSource, localReborrowDigest, originSpan{0, len(localReborrowSource), localReborrowSource})
	f, analysis := analyzeOriginRoot(t, "local_field_reborrow", localReborrowSource, false, nil)
	local := patternFn(t, localReborrowSource, "fn local(")
	originExactPending(t, analysis, f.unit.SourceKey, local, nil)
	originNoEscape(t, analysis, f.owner.File.ID, local)
	requireOriginSummary(t, analysis, f.owner.File.ID, "local", false, []uint32{})

	external := patternFn(t, localReborrowSource, "fn external(")
	originExactPending(t, analysis, f.unit.SourceKey, external, []originRefusal{
		{originSpan{250, 258, "cells[r]"}, "index requires its selected container transfer"},
		{originSpan{250, 261, "cells[r][c]"}, originOutgoingRefusal},
		{originSpan{250, 265, "cells[r][c] = v"}, nestedStoreRefusal},
	})
}
