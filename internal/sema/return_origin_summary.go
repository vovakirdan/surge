package sema

import (
	"cmp"
	"slices"

	"fortio.org/safecast"

	"surge/internal/diag"
	"surge/internal/source"
)

// The summaries form a finite may-source lattice. A recursive component starts
// at NoNormalReturn and stays private until all of its body transfers stabilize.
// Reconsidering every body is deliberately simple; it does not publish a
// provisional empty source promise for a caller visited before its callee.
func (a *returnOriginAnalyzer) solveBodies() error {
	for {
		changed := false
		for _, fn := range a.functions {
			if err := a.ctx.Err(); err != nil {
				return err
			}
			value, err := (&returnOriginBody{analyzer: a, function: fn}).analyze()
			if err != nil {
				return err
			}
			next := a.summaries[fn.key].join(projectReturnOriginSummary(value))
			if !next.equal(a.summaries[fn.key]) {
				a.summaries[fn.key] = next
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	a.collect = true
	for _, fn := range a.functions {
		body := &returnOriginBody{analyzer: a, function: fn}
		if _, err := body.analyze(); err != nil {
			return err
		}
		value := a.summaries[fn.key]
		summary := ReturnOriginSummary{BodyKey: fn.key, Name: fn.name, Source: fn.item.NameSpan, NoNormalReturn: !value.normal}
		for _, root := range value.roots {
			if root.kind == returnOriginParam {
				summary.ParamSlots = append(summary.ParamSlots, root.param)
			} else {
				summary.Unknown = true
				body.pending(fn.item.ReturnSpan, "function result contains an unproved source")
			}
		}
		a.report.Summaries = append(a.report.Summaries, summary)
	}
	slices.SortFunc(a.report.Diagnostics, func(a, b diag.Diagnostic) int {
		if order := compareReturnOriginSpans(a.Primary, b.Primary); order != 0 {
			return order
		}
		return cmp.Compare(a.Message, b.Message)
	})
	slices.SortFunc(a.report.Pending, func(a, b ReturnOriginPending) int {
		if order := cmp.Compare(a.SourceKey, b.SourceKey); order != 0 {
			return order
		}
		if order := compareReturnOriginSpans(a.Span, b.Span); order != 0 {
			return order
		}
		return cmp.Compare(a.Reason, b.Reason)
	})
	return nil
}

func projectReturnOriginSummary(value returnOriginValue) returnOriginValue {
	if !value.normal {
		return returnOriginValue{}
	}
	roots := make([]returnOrigin, 0, len(value.roots))
	for _, root := range value.roots {
		if root.kind == returnOriginParam && !root.expired {
			roots = append(roots, returnOrigin{kind: returnOriginParam, param: root.param})
		} else {
			// Forbidden origins never disappear merely because they are not
			// formal inputs. The source check retains their owning locations.
			roots = append(roots, returnOrigin{kind: returnOriginUnknown})
		}
	}
	return returnOriginValueOf(roots...)
}

func (b *returnOriginBody) analyze() (returnOriginValue, error) {
	fn := b.function
	if !fn.info.ReturnSources().IsAllInputs() {
		b.pending(fn.item.NameSpan, "declared return sources require original-declaration validation")
	}
	env := newReturnOriginEnv()
	for i, param := range fn.params {
		if !param.IsValid() {
			continue
		}
		value := returnOriginValueOf()
		switch returnOriginTypeShape(fn.unit.Sema.TypeInterner, fn.info.Params[i], nil) {
		case returnOriginCarriesRef:
			slot, err := safecast.Conv[uint32](i)
			if err != nil {
				return returnOriginValue{}, err
			}
			value = returnOriginValueOf(returnOrigin{kind: returnOriginParam, param: slot})
		case returnOriginShapeUnknown:
			b.pending(fn.item.ParamsSpan, "parameter requires concrete type or callable provenance")
			value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
		}
		env = env.assign(param, fn.scope, value)
	}
	flow, err := b.stmt(fn.item.Body, env, returnOriginTargets{scope: fn.scope})
	if err != nil {
		return returnOriginValue{}, err
	}
	flow = b.closeFlow(flow, fn.scope, fn.item.Span)
	value := returnOriginValue{}
	if flow.normal.reachable {
		value = returnOriginValueOf()
	}
	for key, outcome := range flow.exits {
		if key.kind != returnOriginFunctionReturn || key.target != fn.scope {
			b.pending(key.site, "function has an unresolved control-flow target")
			continue
		}
		value = value.join(outcome.value)
	}
	return value, nil
}

func compareReturnOriginSpans(a, b source.Span) int {
	if order := cmp.Compare(a.File, b.File); order != 0 {
		return order
	}
	if order := cmp.Compare(a.Start, b.Start); order != 0 {
		return order
	}
	return cmp.Compare(a.End, b.End)
}
