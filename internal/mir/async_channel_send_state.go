package mir

import (
	"surge/internal/sema"
	"surge/internal/types"
)

// A consuming send has two distinct suspended states. Before transfer its
// source must survive a refused reservation; after transfer only the channel
// operation survives. Packing live-in in both cases would resurrect the moved
// source and make cancellation destroy it twice.
func prepareAsyncConsumingSends(f *Func, typesIn *types.Interner, semaRes *sema.Result) map[BlockID]BlockID {
	resumes := make(map[BlockID]BlockID)
	for bi := range len(f.Blocks) {
		for ii := range f.Blocks[bi].Instrs {
			ins := f.Blocks[bi].Instrs[ii]
			if ins.Kind != InstrChanSend || ins.ChanSend.Value.Kind != OperandMove {
				continue
			}
			consumed := addLocal(f, "__send_consumed", typesIn.Builtins().Bool,
				localFlagsFor(typesIn, semaRes, typesIn.Builtins().Bool))
			resume := ins
			resume.ChanSend.Resume = true
			// A legal inert operand keeps generic MIR walkers independent of
			// this retry protocol. It carries no source place or ownership.
			resume.ChanSend.Value = Operand{Kind: OperandConst, Type: typesIn.Builtins().Nothing,
				Const: Const{Kind: ConstNothing, Type: typesIn.Builtins().Nothing}}
			resumeBB := newBlock(f)
			f.Blocks[resumeBB].Instrs = []Instr{resume}
			f.Blocks[resumeBB].Term = Terminator{Kind: TermUnreachable}
			ins.ChanSend.TrackConsumed = true
			ins.ChanSend.Consumed = Place{Local: consumed}
			f.Blocks[bi].Instrs[ii] = ins
			resumes[BlockID(bi)] = resumeBB
		}
	}
	return resumes
}

// Pending blocks are built after their payload variants, so wire the dynamic
// choice only now. Each successor packs exactly the state its retry reads.
func finishAsyncConsumingSends(f *Func, sites []awaitSite, resumes map[BlockID]BlockID) {
	pending := make(map[BlockID]BlockID, len(sites))
	for _, site := range sites {
		pending[site.pollBB] = site.pendingBB
	}
	for _, site := range sites {
		resumeBB, ok := resumes[site.pollBB]
		if !ok {
			continue
		}
		consumed := f.Blocks[site.pollBB].Instrs[site.pollInstr].ChanSend.Consumed.Local
		dispatch := newBlock(f)
		setBlockTerm(f, dispatch, Terminator{Kind: TermIf, If: IfTerm{
			Cond: operandForLocal(f, consumed), Then: pending[resumeBB], Else: site.pendingBB,
		}})
		f.Blocks[site.pollBB].Instrs[site.pollInstr].ChanSend.PendBB = dispatch
	}
}
