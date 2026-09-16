package driver

import (
	"slices"
	"testing"

	"surge/internal/ast"
	"surge/internal/sema"
	"surge/internal/symbols"
)

// STAGE A MEASUREMENT. Not a regression test: it asserts only that the source is the
// frozen one and that each site has a binary node. Everything else is logged, because
// the exemption for core concatenation has to be keyed on the identity the certified
// reader actually reports, and that identity is not decidable by reading.
//
// The source is inline and verbatim, and the digest gate is the first statement: a
// measurement file that carries a placeholder measures nothing.
const operatorSelectionProbeSource = `type Arr = { items: uint64[4] };
extern<Arr> {
    fn __add(self: &Arr, other: &Arr) -> uint64[] {
        return self.items[[0..2]];
    }
    fn __mul(self: &Arr, other: &Arr) -> uint64[];
}
fn selections(a: &Arr, b: &Arr) -> uint64 {
    let body_op = a + b;
    let opaque_op = a * b;
    return body_op[0] + opaque_op[0];
}
fn dynamic_concat() -> uint64 {
    let xs: uint64[] = [1:uint64, 2:uint64];
    let ys: uint64[] = [3:uint64, 4:uint64];
    let joined = xs + ys;
    return joined[0];
}
fn fixed_concat() -> uint64 {
    let p: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let q: uint64[4] = [5:uint64, 6:uint64, 7:uint64, 8:uint64];
    let both = p + q;
    return both[0];
}
`

const operatorSelectionProbeDigest = "ff32b7d95d57959b0707c4cfff4236cd25d7f3e041112d6832c844ebaa68d200"

func TestProbeOperatorSelectionAuthority(t *testing.T) {
	sites := []struct {
		name string
		span originSpan
	}{
		{"body_add", originSpan{255, 260, "a + b"}},
		{"opaque_mul", originSpan{282, 287, "a * b"}},
		{"dynamic_concat", originSpan{468, 475, "xs + ys"}},
		{"fixed_concat", originSpan{676, 681, "p + q"}},
	}
	spans := make([]originSpan, 0, len(sites))
	for _, site := range sites {
		spans = append(spans, site.span)
	}
	checkOriginSource(t, operatorSelectionProbeSource, operatorSelectionProbeDigest, spans...)
	f, analysis := analyzeOriginRoot(t, "operator_selection_probe", operatorSelectionProbeSource, false, nil)
	file := f.owner.File.ID
	for _, site := range sites {
		id := originExprAt(t, f.unit, file, site.span, ast.ExprBinary)
		selected, present := f.unit.Sema.MagicBinarySymbols[id]
		row := map[string]any{"site": site.name, "snippet": site.span.snippet,
			"selection_present": present, "selection_valid": selected.IsValid(),
			"result_type": f.unit.Sema.ExprTypes[id]}
		var mapped []sema.CallableCandidate
		for _, c := range f.authority.CallableCandidates {
			hit := c.Symbol == selected
			if len(f.unit.Publication.RootToLocalSymbols) > 0 {
				hit = slices.Contains(f.unit.Publication.LocalSymbols(c.Symbol), selected)
			}
			if hit {
				mapped = append(mapped, c)
			}
		}
		row["mapped_candidates"] = len(mapped)
		if sym := f.unit.Symbols.Table.Symbols.Get(selected); sym != nil {
			row["symbol_kind"] = sym.Kind
			row["symbol_builtin"] = sym.Flags&symbols.SymbolFlagBuiltin != 0
			if info, ok := f.unit.Sema.TypeInterner.FnInfo(sym.Type); ok && info != nil && len(mapped) == 1 {
				c := mapped[0]
				row["agrees_params"] = slices.Equal(info.Params, c.ParamTypes)
				row["agrees_result"] = info.Result == c.ResultType
				row["agrees_sources"] = info.ReturnSources().Equal(c.ReturnSources)
				row["agrees_span"] = sym.Span == c.Source
			}
		}
		for i, c := range mapped {
			row["candidate"] = map[string]any{"i": i, "name": c.Name, "has_body": c.HasBody,
				"builtin": c.Builtin, "intrinsic": c.Intrinsic, "module": c.ModulePath,
				"source_key": c.SourceKey, "body_key": c.BodyKey, "has_self": c.HasSelf,
				"template_params": len(c.TemplateParams), "receiver_arity": c.ReceiverTemplateArity,
				"defaults": c.Defaults, "variadic": c.Variadic}
			for _, owner := range f.inputs.units {
				if owner.Builder.Files.Get(owner.FileID).Span.File != c.Source.File {
					continue
				}
				for _, identity := range owner.Publication.LocalCallables {
					if identity.BodyKey == c.BodyKey && identity.SourceKey == c.SourceKey {
						row["owning_unit"] = owner.SourceKey
					}
				}
			}
		}
		logReturnOriginCallEvidence(t, row)
	}
	logReturnOriginCallEvidence(t, map[string]any{"stage": "operator_selection_probe",
		"pending": originPendingWithin(analysis, f.unit.SourceKey, 0, len(operatorSelectionProbeSource))})
}
