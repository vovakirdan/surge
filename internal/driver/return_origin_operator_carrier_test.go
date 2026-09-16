package driver

import (
	"testing"
)

// An operator whose result can keep a storage loan proves nothing on the borrow-free
// path. Three shapes reach that path with all-RefFree operands -- a body operator, a
// body unary operator, and a body-less OPAQUE declaration whose implementation is
// elsewhere -- and each must leave its operands' origins alone rather than erase them.
//
// The two concat controls are the exemption: core's `Array<T> + Array<T>` and
// `ArrayFixed<T, N> + ArrayFixed<T, N>` are certified by identity, so they stay on the
// fast path. They are asserted BY REASON, because both already carry an unrelated
// refusal at this tip ("generic use disagrees with its original typed operation"),
// which this packet neither adds nor removes.
const operatorCarrierSource = `type Arr = { items: uint64[4] };
extern<Arr> {
    fn __add(self: &Arr, other: &Arr) -> uint64[] {
        return self.items[[0..2]];
    }
    fn __neg(self: &Arr) -> uint64[] {
        return self.items[[1..3]];
    }
    fn __sub(self: &Arr, other: &Arr) -> uint64 {
        return self.items[0] + other.items[0];
    }
    fn __mul(self: &Arr, other: &Arr) -> uint64[];
}
fn stash(out: &mut uint64[][]) -> nothing {
    let a: Arr = Arr { items = [1:uint64, 2:uint64, 3:uint64, 4:uint64] };
    let b: Arr = Arr { items = [5:uint64, 6:uint64, 7:uint64, 8:uint64] };
    out.push(a + b);
    return nothing;
}
fn stash_unary(out: &mut uint64[][]) -> nothing {
    let a: Arr = Arr { items = [1:uint64, 2:uint64, 3:uint64, 4:uint64] };
    out.push(-a);
    return nothing;
}
fn stash_opaque(out: &mut uint64[][]) -> nothing {
    let a: Arr = Arr { items = [1:uint64, 2:uint64, 3:uint64, 4:uint64] };
    let b: Arr = Arr { items = [5:uint64, 6:uint64, 7:uint64, 8:uint64] };
    out.push(a * b);
    return nothing;
}
fn erased_control() -> uint64 {
    let a: Arr = Arr { items = [1:uint64, 2:uint64, 3:uint64, 4:uint64] };
    let b: Arr = Arr { items = [5:uint64, 6:uint64, 7:uint64, 8:uint64] };
    return a - b;
}
fn concat_control() -> uint64[] {
    let xs: uint64[] = [1:uint64, 2:uint64];
    let ys: uint64[] = [3:uint64, 4:uint64];
    return xs + ys;
}
fn fixed_concat_control() -> uint64[4] {
    let p: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let q: uint64[4] = [5:uint64, 6:uint64, 7:uint64, 8:uint64];
    return p + q;
}
`

const operatorCarrierDigest = "a6c04b80b881c73b085df4122b3c6b13b6623bf31045d8d0485f490a4de26f2b"

const (
	carrierBinaryRefusal     = "binary callable needs an exact origin contract"
	carrierUnaryRefusal      = "unary callable needs an exact origin contract"
	carrierGenericUseRefusal = "generic use disagrees with its original typed operation"
)

func TestAnalyzeOperatorCarrierResult(t *testing.T) {
	leaves := []struct {
		name    string
		site    originSpan
		reason  string
		refused bool
		// concat sites carry an unrelated row at this tip; it is a PRECONDITION, not
		// an assertion about this packet, and S-Y2''' fires when it is gone.
		unrelated bool
	}{
		{name: "stash", site: originSpan{583, 588, "a + b"}, reason: carrierBinaryRefusal, refused: true},
		{name: "stash_unary", site: originSpan{751, 753, "-a"}, reason: carrierUnaryRefusal, refused: true},
		{name: "stash_opaque", site: originSpan{992, 997, "a * b"}, reason: carrierBinaryRefusal, refused: true},
		{name: "erased_control", site: originSpan{1215, 1220, "a - b"}, reason: carrierBinaryRefusal, refused: false},
		{name: "concat_control", site: originSpan{1359, 1366, "xs + ys"}, reason: carrierBinaryRefusal, refused: false, unrelated: true},
		{name: "fixed_concat_control", site: originSpan{1552, 1557, "p + q"}, reason: carrierBinaryRefusal, refused: false, unrelated: true},
	}
	spans := make([]originSpan, 0, len(leaves))
	for _, leaf := range leaves {
		spans = append(spans, leaf.site)
	}
	checkOriginSource(t, operatorCarrierSource, operatorCarrierDigest, spans...)
	f, analysis := analyzeOriginRoot(t, "operator_carrier", operatorCarrierSource, false, nil)
	logReturnOriginCallEvidence(t, map[string]any{"stage": "operator_carrier",
		"pending": originPendingWithin(analysis, f.unit.SourceKey, 0, len(operatorCarrierSource))})
	for _, leaf := range leaves {
		t.Run(leaf.name, func(t *testing.T) {
			if leaf.unrelated && !originPendingAt(analysis, f.unit.SourceKey, leaf.site, carrierGenericUseRefusal) {
				t.Fatalf("PRECONDITION: %q no longer carries %q; the leaf must become a plain-absence assertion (S-Y2''')",
					leaf.site.snippet, carrierGenericUseRefusal)
			}
			got := originPendingAt(analysis, f.unit.SourceKey, leaf.site, leaf.reason)
			if got != leaf.refused {
				t.Errorf("%q carries %q = %v, want %v", leaf.site.snippet, leaf.reason, got, leaf.refused)
			}
		})
	}
}
