package sema

import (
	"cmp"
	"slices"

	"surge/internal/source"
	"surge/internal/types"
)

type returnOriginCondition struct {
	body    string
	site    source.Span
	kind    returnOriginConditionKind
	subject types.TypeID
	view    returnOriginTypeView
}

type returnOriginSummaryFact struct {
	value      returnOriginValue
	conditions []*returnOriginCondition
	required   returnOriginRequirements
	// postCells holds what each mutable external cell may contain after a
	// normal exit, in the callee's own V/R vocabulary.
	postCells map[uint32]returnOriginValue
	// postBackings is the same for mutable container formals' backings.
	postBackings map[uint32]returnOriginValue
}

func compareReturnOriginCondition(a, b *returnOriginCondition) int {
	if a == nil || b == nil {
		if a == b {
			return 0
		}
		if a == nil {
			return -1
		}
		return 1
	}
	if n := cmp.Compare(a.body, b.body); n != 0 {
		return n
	}
	if n := compareReturnOriginSpans(a.site, b.site); n != 0 {
		return n
	}
	if n := cmp.Compare(a.kind, b.kind); n != 0 {
		return n
	}
	if n := cmp.Compare(a.subject, b.subject); n != 0 {
		return n
	}
	return compareReturnOriginView(a.view, b.view)
}

func (f *returnOriginSummaryFact) join(other *returnOriginSummaryFact) returnOriginSummaryFact {
	if f == nil {
		f = &returnOriginSummaryFact{}
	}
	if other == nil {
		other = &returnOriginSummaryFact{}
	}
	conditions := make([]*returnOriginCondition, 0, len(f.conditions)+len(other.conditions))
	for _, source := range [][]*returnOriginCondition{f.conditions, other.conditions} {
		for _, condition := range source {
			if condition == nil {
				continue
			}
			cloned := *condition
			cloned.view = condition.view.clone()
			conditions = append(conditions, &cloned)
		}
	}
	slices.SortFunc(conditions, compareReturnOriginCondition)
	conditions = slices.CompactFunc(conditions, func(a, b *returnOriginCondition) bool { return compareReturnOriginCondition(a, b) == 0 })
	return returnOriginSummaryFact{value: f.value.join(other.value), conditions: conditions, required: f.required.join(other.required),
		postCells: joinReturnOriginCellPosts(f, other), postBackings: joinReturnOriginBackingPosts(f, other)}
}

func (f *returnOriginSummaryFact) equal(other *returnOriginSummaryFact) bool {
	if f == nil || other == nil {
		return f == other
	}
	return f.value.equal(other.value) && f.required.equal(other.required) && equalReturnOriginCellPosts(f.postCells, other.postCells) &&
		equalReturnOriginCellPosts(f.postBackings, other.postBackings) &&
		slices.EqualFunc(f.conditions, other.conditions, func(a, b *returnOriginCondition) bool { return compareReturnOriginCondition(a, b) == 0 })
}

func (b *returnOriginBody) requireOpaqueState(view returnOriginTypeView, result types.TypeID, span source.Span) returnOriginValue {
	condition := &returnOriginCondition{body: b.function.key, site: span, kind: returnOriginNoBorrowedState, subject: result, view: view.clone()}
	b.conditions = append(b.conditions, condition)
	required := view.requirement(condition.kind, result)
	b.required = b.required.join(required)
	b.reportRequirements(required, span)
	if required.failed() {
		return returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	}
	return returnOriginValueOf()
}

func (b *returnOriginBody) reportRequirements(required returnOriginRequirements, span source.Span) {
	if required.refuted {
		b.pending(span, "opaque result type may carry borrowed state")
	}
	if required.unsupported {
		b.pending(span, "opaque result borrowed-state classification is unsupported")
	}
}

// The caller has already checked the actual source root/edge and signature.
// Provisional bottom contributes TRUE, never a sticky unsupported condition.
func (b *returnOriginBody) inheritRequirements(fn *returnOriginFunction, view returnOriginTypeView, span source.Span) returnOriginRequirements {
	required := b.analyzer.summaries[fn.key].required
	if len(fn.candidate.TemplateParams) != 0 {
		required = required.rebase(view)
	}
	b.required = b.required.join(required)
	b.reportRequirements(required, span)
	return required
}
