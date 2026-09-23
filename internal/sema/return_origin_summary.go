package sema

import (
	"cmp"
	"slices"

	"fortio.org/safecast"

	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/types"
)

// The summaries form a finite may-source lattice. A recursive component starts
// at NoNormalReturn and stays private until all of its body transfers stabilize.
// Reconsidering every body is deliberately simple; it does not publish a
// provisional empty source promise for a caller visited before its callee.
func (a *returnOriginAnalyzer) solveBodies() error {
	for _, fn := range a.declarations {
		body := &returnOriginBody{analyzer: a, function: fn}
		slots, valid := body.declaredFunctionSources(fn, fn.item.NameSpan)
		value := body.opaqueReturnSources(fn, fn.info, slots, valid, fn.item.NameSpan)
		a.summaries[fn.key] = returnOriginSummaryFact{value: projectReturnOriginSummary(value),
			conditions: body.conditions, required: body.required}
	}
	for {
		changed := false
		for _, fn := range a.functions {
			if err := a.ctx.Err(); err != nil {
				return err
			}
			body := &returnOriginBody{analyzer: a, function: fn}
			value, err := body.analyze()
			if err != nil {
				return err
			}
			next := a.summaries[fn.key].join(returnOriginSummaryFact{value: projectReturnOriginSummary(value),
				conditions: body.conditions, required: body.required, postCells: projectReturnOriginCellPosts(body.postCells),
				postBackings: projectReturnOriginCellPosts(body.postBackings)})
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
	for _, fn := range a.declarations {
		body := &returnOriginBody{analyzer: a, function: fn}
		sources, valid := body.declaredFunctionSources(fn, fn.item.NameSpan)
		body.opaqueReturnSources(fn, fn.info, sources, valid, fn.item.NameSpan)
	}
	for _, fn := range a.functions {
		body := &returnOriginBody{analyzer: a, function: fn}
		result, err := body.analyze()
		if err != nil {
			return err
		}
		value := a.summaries[fn.key].value
		refused := returnOriginRefusedResult(result)
		summary := ReturnOriginSummary{BodyKey: fn.key, Name: fn.name, Source: fn.item.NameSpan, NoNormalReturn: !value.normal}
		for _, root := range value.roots {
			if root.kind == returnOriginParam {
				summary.ParamSlots = append(summary.ParamSlots, root.param)
			} else {
				summary.Unknown = true
				if !refused {
					body.pending(fn.item.ReturnSpan, "function result contains an unproved source")
				}
			}
		}
		// V(i) and R(i) are two private facts about one public input slot.
		slices.Sort(summary.ParamSlots)
		summary.ParamSlots = slices.Compact(summary.ParamSlots)
		a.report.Summaries = append(a.report.Summaries, summary)
	}
	if err := a.checkGenericUses(); err != nil {
		return err
	}
	if err := a.checkDeferredClones(); err != nil {
		return err
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
	if len(value.callables) != 0 {
		// Function-result conversion is still an explicit pending boundary;
		// dropping callable facts must not publish a RefFree/empty summary.
		roots = append(roots, returnOrigin{kind: returnOriginUnknown})
	}
	for _, root := range value.roots {
		if root.kind == returnOriginParam && !root.expired {
			roots = append(roots, returnOrigin{kind: returnOriginParam, param: root.param, selector: root.selector})
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
	allowed, validPromise := b.declaredFunctionSources(fn, fn.item.NameSpan)
	env := newReturnOriginEnv()
	for i, param := range fn.params {
		if !param.IsValid() {
			continue
		}
		value := returnOriginValueOf()
		// Incoming callable or direct-T contents belong to the caller; the
		// parameter's own storage remains Local. Their operations still need
		// callable or type-dependent transfer. This does not prove a callable
		// has no captures or make its function type a borrowed source slot.
		if returnOriginFnInfo(fn.unit.Sema.TypeInterner, fn.info.Params[i]) != nil || fn.directTemplateParam(fn.info.Params[i]) {
			slot, err := safecast.Conv[uint32](i)
			if err != nil {
				return returnOriginValue{}, err
			}
			env = env.assign(param, fn.scope, returnOriginValueOf(returnOrigin{kind: returnOriginParam, param: slot}))
			continue
		}
		switch returnOriginView(fn).shape(fn.info.Params[i]) {
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
	env = fn.initBackings(fn.initExternalCells(env))
	flow, err := b.stmt(fn.item.Body, env, returnOriginTargets{scope: fn.scope})
	if err != nil {
		return returnOriginValue{}, err
	}
	flow = b.closeFlow(flow, fn.scope, fn.item.Span)
	b.collectCellExit(flow.normal, fn.item.ReturnSpan)
	b.collectBackingExit(flow.normal, fn.item.ReturnSpan)
	value := returnOriginValue{}
	if flow.normal.reachable {
		value = returnOriginValueOf()
		result := returnOriginFallthroughResult(fn)
		for range 64 {
			target, alias := fn.unit.Sema.TypeInterner.AliasTarget(result)
			if !alias {
				break
			}
			result = target
		}
		if result != fn.unit.Sema.TypeInterner.Builtins().Nothing {
			b.pending(fn.item.ReturnSpan, "reachable function fallthrough has no proven value for its declared result")
			value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
		}
	}
	for key, outcome := range flow.exits {
		if key.kind != returnOriginFunctionReturn || key.target != fn.scope {
			b.pending(key.site, "function has an unresolved control-flow target")
			continue
		}
		b.collectCellExit(outcome.env, key.site)
		b.collectBackingExit(outcome.env, key.site)
		// A symbolic result may become reference-free. Its conditional promise
		// is checked for every current concrete use after the fixed point.
		if validPromise && !fn.info.ReturnSources().IsAllInputs() && !types.ContainsGenericParam(fn.unit.Sema.TypeInterner, fn.info.Result) {
			b.checkDeclaredReturn(outcome.value, allowed, key.site)
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
