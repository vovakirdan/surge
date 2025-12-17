package mir

// RecognizeSwitchTag converts chains of tag_test + if into switch_tag terminators.
// Pattern recognized:
//
//	bb0:
//	  L_tmp = tag_test copy L_val is TagName
//	  if copy L_tmp then bbTrue else bbElse
//	bbElse:
//	  L_tmp2 = tag_test copy L_val is TagName2  (same L_val!)
//	  if copy L_tmp2 then bbTrue2 else bbElse2
//	...
//
// Becomes:
//
//	bb0:
//	  switch_tag copy L_val { TagName -> bbTrue; TagName2 -> bbTrue2; default -> bbElseN; }
func RecognizeSwitchTag(f *Func) {
	if f == nil || len(f.Blocks) == 0 {
		return
	}

	for i := range f.Blocks {
		bb := &f.Blocks[i]
		if chain := detectTagTestChain(f, bb); chain != nil {
			convertToSwitchTag(bb, chain)
		}
	}
}

// tagTestChain holds the information extracted from a chain of tag_test + if patterns.
type tagTestChain struct {
	value    Operand         // The value being tested (e.g., copy L2)
	cases    []SwitchTagCase // Collected cases: tag name -> target block
	defBlock BlockID         // Default block (final else or unreachable)
}

// detectTagTestChain checks if a block starts a tag_test chain and returns
// the extracted chain info, or nil if the pattern doesn't match.
func detectTagTestChain(f *Func, bb *Block) *tagTestChain {
	// Pattern requirements:
	// 1. Block ends with if terminator
	// 2. Last instruction is: L_tmp = tag_test copy L_val is TagName
	// 3. If condition uses L_tmp (the tag_test result)

	if bb.Term.Kind != TermIf {
		return nil
	}

	tagTest, testLocal := extractTagTest(bb)
	if tagTest == nil {
		return nil
	}

	// Check if the if condition references the tag_test result local
	if !isOperandForLocal(&bb.Term.If.Cond, testLocal) {
		return nil
	}

	// Start building the chain
	chain := &tagTestChain{
		value: tagTest.Value,
		cases: []SwitchTagCase{
			{TagName: tagTest.TagName, Target: bb.Term.If.Then},
		},
	}

	// Follow the else branch to find more tag_tests on the same value
	elseBlock := bb.Term.If.Else
	visited := make(map[BlockID]bool)
	visited[bb.ID] = true

	for {
		if visited[elseBlock] {
			// Cycle detected, stop
			chain.defBlock = elseBlock
			break
		}
		visited[elseBlock] = true

		if elseBlock < 0 || int(elseBlock) >= len(f.Blocks) {
			// Invalid block, use as default
			chain.defBlock = elseBlock
			break
		}

		nextBB := &f.Blocks[elseBlock]

		// Check if this block continues the chain:
		// - Has exactly one instruction (the tag_test)
		// - Ends with if terminator
		// - tag_test is on the same value
		nextTagTest, nextTestLocal := extractTagTest(nextBB)
		if nextTagTest == nil || nextBB.Term.Kind != TermIf {
			// Chain broken, this block becomes default
			chain.defBlock = elseBlock
			break
		}

		// Check if testing the same value
		if !operandsEqual(&nextTagTest.Value, &chain.value) {
			// Different value, chain broken
			chain.defBlock = elseBlock
			break
		}

		// Check if the if condition uses the tag_test result
		if !isOperandForLocal(&nextBB.Term.If.Cond, nextTestLocal) {
			chain.defBlock = elseBlock
			break
		}

		// Add this case to the chain
		chain.cases = append(chain.cases, SwitchTagCase{
			TagName: nextTagTest.TagName,
			Target:  nextBB.Term.If.Then,
		})

		// Continue following the else branch
		elseBlock = nextBB.Term.If.Else
	}

	// Only convert if we have at least 2 cases (otherwise if is simpler)
	if len(chain.cases) < 2 {
		return nil
	}

	return chain
}

// extractTagTest checks if the block's last instruction is a tag_test assignment
// and returns the TagTest data and the destination local ID.
func extractTagTest(bb *Block) (*TagTest, LocalID) {
	if len(bb.Instrs) == 0 {
		return nil, NoLocalID
	}

	// For the first block in the chain, tag_test might not be the last instruction
	// But for continuation blocks, we expect exactly tag_test + if
	// Let's search for the tag_test instruction that produces the if condition

	// Simple approach: check if last instruction is tag_test
	lastInstr := &bb.Instrs[len(bb.Instrs)-1]
	if lastInstr.Kind != InstrAssign {
		return nil, NoLocalID
	}
	if lastInstr.Assign.Src.Kind != RValueTagTest {
		return nil, NoLocalID
	}

	return &lastInstr.Assign.Src.TagTest, lastInstr.Assign.Dst.Local
}

// isOperandForLocal checks if an operand references the given local.
func isOperandForLocal(op *Operand, local LocalID) bool {
	if op == nil {
		return false
	}
	if op.Kind != OperandCopy && op.Kind != OperandMove {
		return false
	}
	if len(op.Place.Proj) != 0 {
		return false
	}
	return op.Place.Local == local
}

// operandsEqual checks if two operands refer to the same value.
// For our purposes, we check if they refer to the same local.
func operandsEqual(a, b *Operand) bool {
	if a == nil || b == nil {
		return false
	}
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case OperandCopy, OperandMove:
		return placesEqual(a.Place, b.Place)
	default:
		return false
	}
}

func placesEqual(a, b Place) bool {
	if a.Local != b.Local {
		return false
	}
	if len(a.Proj) != len(b.Proj) {
		return false
	}
	for i := range a.Proj {
		ap := a.Proj[i]
		bp := b.Proj[i]
		if ap.Kind != bp.Kind {
			return false
		}
		if ap.FieldName != bp.FieldName || ap.FieldIdx != bp.FieldIdx || ap.IndexLocal != bp.IndexLocal {
			return false
		}
	}
	return true
}

// convertToSwitchTag replaces the block's terminator with switch_tag.
func convertToSwitchTag(bb *Block, chain *tagTestChain) {
	// Remove the tag_test instruction from the first block
	if len(bb.Instrs) > 0 {
		bb.Instrs = bb.Instrs[:len(bb.Instrs)-1]
	}

	// Replace if terminator with switch_tag
	bb.Term = Terminator{
		Kind: TermSwitchTag,
		SwitchTag: SwitchTagTerm{
			Value:   chain.value,
			Cases:   chain.cases,
			Default: chain.defBlock,
		},
	}
}
