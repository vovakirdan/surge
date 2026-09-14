package symbols

import (
	"slices"
	"strings"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/types"
)

// ReturnSourceMarker keeps the source promise anchored to its formal slot.
type ReturnSourceMarker struct {
	Slot          uint32
	Span          source.Span
	ArgumentCount int
}

// ReturnSourceSyntax preserves declaration roots, before generic substitution.
// Its AST IDs belong to the builder owning Span, never an importing builder.
// Typed eligibility requests must additionally retain the original resolved
// types; the syntax alone cannot reconstruct a generic type environment.
type ReturnSourceSyntax struct {
	params  []ast.TypeID
	result  ast.TypeID
	span    source.Span
	markers []ReturnSourceMarker
}

// Sources returns the immutable declaration-ordered relation.
func (s ReturnSourceSyntax) Sources() types.ReturnSources {
	if len(s.markers) == 0 {
		return types.ReturnSources{}
	}
	slots := make([]uint32, len(s.markers))
	for i, marker := range s.markers {
		slots[i] = marker.Slot
	}
	return types.ExplicitReturnSources(slots...)
}

// Params returns a detached copy of the original parameter type roots.
func (s ReturnSourceSyntax) Params() []ast.TypeID { return slices.Clone(s.params) }

// Result returns the original result type root.
func (s ReturnSourceSyntax) Result() ast.TypeID { return s.result }

// Span identifies the source declaration that owns the AST roots.
func (s ReturnSourceSyntax) Span() source.Span { return s.span }

// Markers returns a detached copy of the marked parameter locations.
func (s ReturnSourceSyntax) Markers() []ReturnSourceMarker { return slices.Clone(s.markers) }

// FunctionReturnSourceSyntax captures named parameters, including self/defaults.
func FunctionReturnSourceSyntax(builder *ast.Builder, fn *ast.FnItem) ReturnSourceSyntax {
	if builder == nil || fn == nil {
		return ReturnSourceSyntax{}
	}
	syntax := ReturnSourceSyntax{result: fn.ReturnType, span: fn.Span}
	var slot uint32
	for _, id := range builder.Items.GetFnParamIDs(fn) {
		param := builder.Items.FnParam(id)
		if param == nil {
			syntax.params = append(syntax.params, ast.NoTypeID)
		} else {
			syntax.params = append(syntax.params, param.Type)
			syntax.appendMarkers(builder, slot, param.AttrStart, param.AttrCount)
		}
		slot++
	}
	return syntax
}

// FunctionTypeReturnSourceSyntax captures an unnamed callback signature.
func FunctionTypeReturnSourceSyntax(builder *ast.Builder, id ast.TypeID) ReturnSourceSyntax {
	if builder == nil {
		return ReturnSourceSyntax{}
	}
	fn, ok := builder.Types.Fn(id)
	if !ok || fn == nil {
		return ReturnSourceSyntax{}
	}
	syntax := ReturnSourceSyntax{result: fn.Return, span: builder.Types.Get(id).Span}
	var slot uint32
	for _, param := range fn.Params {
		syntax.params = append(syntax.params, param.Type)
		syntax.appendMarkers(builder, slot, param.AttrStart, param.AttrCount)
		slot++
	}
	return syntax
}

func (s *ReturnSourceSyntax) appendMarkers(builder *ast.Builder, slot uint32, start ast.AttrID, count uint32) {
	for _, attr := range builder.Items.CollectAttrs(start, count) {
		if strings.EqualFold(builder.StringsInterner.MustLookup(attr.Name), "return_source") {
			s.markers = append(s.markers, ReturnSourceMarker{Slot: slot, Span: attr.Span, ArgumentCount: len(attr.Args)})
		}
	}
}
