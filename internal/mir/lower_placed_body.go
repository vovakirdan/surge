package mir

import (
	"surge/internal/hir"
	"surge/internal/source"
	"surge/internal/types"
)

func (l *funcLowerer) lowerPlacedBodyFunc(id FuncID, name string, body *hir.Block, result types.TypeID, captures []spawnOnCaptureInfo, span source.Span) (*Func, error) {
	params := make([]blockingCaptureInfo, len(captures))
	for i, capture := range captures {
		params[i] = blockingCaptureInfo{SymbolID: capture.SymbolID, Name: capture.Name, Type: capture.Type}
	}
	f, err := l.lowerSyntheticFunc(id, name, body, result, span, true, false, params)
	if err != nil {
		return nil, err
	}
	f.CapturesArriveOwned = true
	f.ResultCrossesThreads = true
	// Keep the same pre-transform relinquish proof as every crossing return.
	// Scope-join insertion later moves this terminal unshare onto its success
	// edge, after all local children have completed.
	for bi := range f.Blocks {
		term := f.Blocks[bi].Term
		if term.Kind != TermReturn || !term.Return.HasValue || term.Return.Cancelled {
			continue
		}
		f.Blocks[bi].Term = Terminator{}
		l.cur = BlockID(bi)
		value := term.Return.Value
		if value.Type == types.NoTypeID {
			value.Type = result
		}
		term.Return.Value = l.relinquishOperand(&value, span)
		f.Blocks[bi].Term = term
	}
	return f, nil
}
