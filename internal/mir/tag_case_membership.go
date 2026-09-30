package mir

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"surge/internal/source"
	"surge/internal/types"
)

// A tag test asks one union a question about one of ITS cases. Sema lets a
// compare name `nothing` against a union that has no such case: a value built
// as `Some(x)` is typed by its constructor, `Some<T>`, whose only case is
// `Some`, and `compare e { Some(v) => ..; nothing => .. }` is still accepted.
// The VM answers such a test by comparing arm names and says false; the native
// backend asks the union for the case's discriminant and has none to give.
//
// The answer is known when the test is built: the static type of the value
// fixes the cases it can hold, so a test for a case outside them is false.
// Lowering says so as a constant, and validation refuses any test or switch
// that still names a case its value's union does not have, before a backend
// is asked to spell it.

// unionCaseNamesOf lists the direct cases of the union a tag test reads,
// named as union membership names them (see buildUnionCases). The value is
// read through aliases, `own`, references and pointers, as the backends read
// the discriminant. ok is false when the value is not a union whose every
// member has a name.
func unionCaseNamesOf(typesIn *types.Interner, id types.TypeID) (names []string, unionID types.TypeID, ok bool) {
	unionID = tagTestUnionType(typesIn, id)
	if typesIn == nil || typesIn.Strings == nil || unionID == types.NoTypeID {
		return nil, unionID, false
	}
	tt, found := typesIn.Lookup(unionID)
	if !found || tt.Kind != types.KindUnion {
		return nil, unionID, false
	}
	info, found := typesIn.UnionInfo(unionID)
	if !found || info == nil || len(info.Members) == 0 {
		return nil, unionID, false
	}
	names = make([]string, 0, len(info.Members))
	for i := range info.Members {
		member := &info.Members[i]
		switch member.Kind {
		case types.UnionMemberTag:
			names = append(names, typesIn.Strings.MustLookup(member.TagName))
		case types.UnionMemberNothing:
			names = append(names, "nothing")
		case types.UnionMemberType:
			memberType := canonicalType(typesIn, member.Type)
			if memberType == types.NoTypeID {
				return nil, unionID, false
			}
			names = append(names, fmt.Sprintf("type#%d", memberType))
		default:
			return nil, unionID, false
		}
	}
	return names, unionID, true
}

func tagTestUnionType(typesIn *types.Interner, id types.TypeID) types.TypeID {
	if typesIn == nil {
		return id
	}
	for range 32 {
		if id == types.NoTypeID {
			return id
		}
		tt, ok := typesIn.Lookup(id)
		if !ok {
			return id
		}
		switch tt.Kind {
		case types.KindAlias:
			target, ok := typesIn.AliasTarget(id)
			if !ok || target == types.NoTypeID || target == id {
				return id
			}
			id = target
		case types.KindOwn, types.KindReference, types.KindPointer:
			id = tt.Elem
		default:
			return id
		}
	}
	return id
}

// tagTestNeverMatches reports whether the value's own union has no case with
// this name, so a test for it is false for every value of that type.
func tagTestNeverMatches(typesIn *types.Interner, valueType types.TypeID, tagName string) bool {
	names, _, ok := unionCaseNamesOf(typesIn, valueType)
	if !ok || tagName == "" {
		return false
	}
	for _, name := range names {
		if name == tagName {
			return false
		}
	}
	return true
}

// validateTagCaseMembership refuses a tag test or a tag switch that names a
// case the tested value's union does not have.
func validateTagCaseMembership(f *Func, typesIn *types.Interner, files *source.FileSet) error {
	if f == nil || typesIn == nil {
		return nil
	}
	var errs []error
	check := func(where string, value *Operand, tagName string) {
		valueType := operandTagTestType(f, value)
		names, unionID, ok := unionCaseNamesOf(typesIn, valueType)
		if !ok {
			return
		}
		for _, name := range names {
			if name == tagName {
				return
			}
		}
		errs = append(errs, fmt.Errorf("%s: %s names case %q, which type#%d does not have (cases: %s)",
			where, describeTagSubject(f, value, files), tagName, unionID, strings.Join(names, ", ")))
	}
	for bi := range f.Blocks {
		bb := &f.Blocks[bi]
		for ii := range bb.Instrs {
			ins := &bb.Instrs[ii]
			if ins.Kind == InstrAssign && storesNothing(&ins.Assign) && len(ins.Assign.Dst.Proj) == 0 &&
				ins.Assign.Dst.Kind == PlaceLocal && int(ins.Assign.Dst.Local) < len(f.Locals) {
				dst := Operand{Kind: OperandCopy, Place: ins.Assign.Dst}
				if !holdsNothingAsAType(typesIn, operandTagTestType(f, &dst)) {
					check(fmt.Sprintf("bb%d instr %d stores nothing", bi, ii), &dst, "nothing")
				}
			}
			if ins.Kind != InstrAssign || ins.Assign.Src.Kind != RValueTagTest {
				continue
			}
			tt := &ins.Assign.Src.TagTest
			check(fmt.Sprintf("bb%d instr %d tag_test", bi, ii), &tt.Value, tt.TagName)
		}
		if bb.Term.Kind == TermSwitchTag {
			sw := &bb.Term.SwitchTag
			for ci := range sw.Cases {
				check(fmt.Sprintf("bb%d switch_tag case %d", bi, ci), &sw.Value, sw.Cases[ci].TagName)
			}
		}
	}
	return errors.Join(errs...)
}

// holdsNothingAsAType: the union admits `nothing` as a bare type member
// rather than as its `nothing` case.
func holdsNothingAsAType(typesIn *types.Interner, id types.TypeID) bool {
	names, _, ok := unionCaseNamesOf(typesIn, id)
	return ok && slices.Contains(names, fmt.Sprintf("type#%d", typesIn.Builtins().Nothing))
}

// storesNothing: the assignment writes the `nothing` constant, which only a
// union with a `nothing` case can hold.
func storesNothing(a *AssignInstr) bool {
	return a.Src.Kind == RValueUse && a.Src.Use.Kind == OperandConst && a.Src.Use.Const.Kind == ConstNothing
}

func operandTagTestType(f *Func, op *Operand) types.TypeID {
	if op == nil {
		return types.NoTypeID
	}
	if op.Type != types.NoTypeID || op.Kind == OperandConst {
		return op.Type
	}
	if len(op.Place.Proj) == 0 && op.Place.Kind == PlaceLocal && int(op.Place.Local) >= 0 && int(op.Place.Local) < len(f.Locals) {
		return f.Locals[op.Place.Local].Type
	}
	return types.NoTypeID
}

// describeTagSubject names the tested value and where the source wrote it.
func describeTagSubject(f *Func, op *Operand, files *source.FileSet) string {
	if op == nil || op.Kind == OperandConst || op.Place.Kind != PlaceLocal ||
		int(op.Place.Local) < 0 || int(op.Place.Local) >= len(f.Locals) {
		return "the value"
	}
	local := &f.Locals[op.Place.Local]
	name := local.Name
	if name == "" {
		name = fmt.Sprintf("L%d", op.Place.Local)
	}
	span := local.Span
	if span == (source.Span{}) {
		span = f.Span
	}
	if files != nil {
		return fmt.Sprintf("%s at %s", name, source.FormatSpan(span, files))
	}
	return fmt.Sprintf("%s at span %s", name, span)
}
