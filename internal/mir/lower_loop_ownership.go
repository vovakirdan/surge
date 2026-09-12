package mir

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/hir"
	"surge/internal/types"
)

// numericLoopPost specializes only the generated latch's overwrite obligation.
// Alias/own wrappers preserve ownership; a reference still names someone else's
// value. Copy the node so lowering never mutates another instantiation's HIR.
func (l *funcLowerer) numericLoopPost(post *hir.Expr) *hir.Expr {
	if post == nil || post.Kind != hir.ExprBinaryOp {
		return post
	}
	data, ok := post.Data.(hir.BinaryOpData)
	if !ok || data.Op != ast.ExprBinaryAssign {
		return post
	}
	resolved := resolveAliasAndOwn(l.types, l.exprType(data.Left))
	data.DropOverwritten = l.isRefCountedScalar(resolved)
	copyPost := *post
	copyPost.Data = data
	return &copyPost
}

// An owned temporary still lends its value to the initializer. Acquire the
// generated scalar's reference before statement cleanup releases that temporary.
func (l *funcLowerer) acquireGeneratedLoopValue(op Operand, kind hir.GeneratedDropKind) Operand {
	if kind == hir.GeneratedDropCountedScalar && op.Kind == OperandCopy && l.isRefCountedScalar(op.Type) {
		op.Kind = OperandRetain
	}
	return op
}

// registerGeneratedLoopLocal gives a synthesized binding its lexical owner.
// Its statement frame is still open, so initializer temporaries release only
// after the assignment has handed their value to this binding.
func (l *funcLowerer) registerGeneratedLoopLocal(local LocalID, kind hir.GeneratedDropKind) error {
	if kind == hir.GeneratedDropNone || l == nil || l.f == nil ||
		local < 0 || int(local) >= len(l.f.Locals) {
		return nil
	}
	ty := l.f.Locals[local].Type
	resource := kind == hir.GeneratedDropNumericIterableResource && l.isNumericIterableResource(ty)
	if !resource && (kind != hir.GeneratedDropCountedScalar || !l.isRefCountedScalar(ty)) {
		return nil
	}
	// lowerBlock owns the lexical frame immediately below this statement.
	// Generated declarations never lower through a separate scope mechanism.
	if len(l.tempDropFrames) < 2 {
		return fmt.Errorf("mir: generated loop local %s has no lexical drop frame", l.f.Locals[local].Name)
	}
	lexical := len(l.tempDropFrames) - 2
	l.tempDropFrames[lexical] = append(l.tempDropFrames[lexical], tempDropEntry{
		local: local, numericResource: resource,
	})
	return nil
}

func (l *funcLowerer) isNumericIterableResource(ty types.TypeID) bool {
	if l == nil || l.types == nil {
		return false
	}
	ty = resolveAlias(l.types, ty)
	tt, ok := l.types.Lookup(ty)
	if !ok || tt.Kind == types.KindReference || tt.Kind == types.KindPointer || tt.Kind == types.KindOwn {
		return false
	}
	if elem, ok := l.types.ArrayInfo(ty); ok {
		return l.isRefCountedScalar(elem)
	}
	if elem, _, ok := l.types.ArrayFixedInfo(ty); ok {
		return l.isRefCountedScalar(elem)
	}
	if tt.Kind == types.KindArray {
		return l.isRefCountedScalar(tt.Elem)
	}
	info, ok := l.types.StructInfo(ty)
	return ok && info != nil && l.typeNameMatches(info.Name, "Range") &&
		len(info.TypeArgs) == 1 && l.isRefCountedScalar(info.TypeArgs[0])
}

// Only the legacy release of this exact generated resource is replaced.
// A projected drop, scalar, caller binding or already-unwound frame keeps its
// existing cleanup; the ordinary lexical entry is the replacement owner.
func (l *funcLowerer) registeredNumericResource(place Place) bool {
	if place.Kind != PlaceLocal || len(place.Proj) != 0 {
		return false
	}
	for _, frame := range l.tempDropFrames {
		for _, entry := range frame {
			if entry.local == place.Local && entry.numericResource {
				return true
			}
		}
	}
	return false
}
