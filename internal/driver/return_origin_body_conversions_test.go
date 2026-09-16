package driver

import (
	"testing"
)

// A conversion or operator SEMA resolved to a non-generic source body is
// certified when its result is erased and its checked summary names no origin:
// HIR lowers it as a call, and such a call can carry no operand origin or loan.
// The argument a call converts is then read as the conversion's value, so the
// site-level refusal at the call disappears with it.
const bodyConversionSource = `type MyInt = { value: int };
extern<MyInt> {
    fn __to(self: MyInt, _: int) -> int {
        return self.value;
    }
}
type Pt = { x: int, y: int };
extern<Pt> {
    fn __to(self: &Pt, _: int) -> int {
        return self.x + self.y;
    }
    fn __mul(self: &Pt, k: int) -> Pt {
        if k == 0 { return Pt { x: 0, y: 0 }; }
        return self * (k - 1);
    }
}
@allow_to
fn takes_int(x: int) -> int {
    return x;
}
fn implicit_let() -> int {
    let mi: MyInt = MyInt { value: 1 };
    let n: int = mi;
    return n;
}
fn implicit_arg() -> int {
    let mi: MyInt = MyInt { value: 2 };
    return takes_int(mi);
}
fn explicit_ref_self() -> int {
    let p: Pt = Pt { x: 1, y: 2 };
    return p to int;
}
fn recursive_mul(p: &Pt) -> Pt {
    return p * 2;
}
fn print_int() -> nothing {
    print(7);
    return nothing;
}
`

const bodyConversionDigest = "3714814c29e24a62be20d584f74fb42913208e9afd4f779593529c358226dc00"

const originImplicitToRefusal = "implicit conversion needs its selected __to origin contract"

const originCastRetainRefusal = "conversion retains its actual expression for origin finalization"

const originArgumentConversionRefusal = "implicit argument conversion needs its resolved callable origin contract"

const originBinaryCallableRefusal = "binary callable needs an exact origin contract"

const originUseOtherOperation = "generic use disagrees with its original typed operation"

func TestAnalyzeBodyConversions(t *testing.T) {
	checkOriginSource(t, bodyConversionSource, bodyConversionDigest)
	f, analysis := analyzeOriginRoot(t, "body_conversions", bodyConversionSource, false, nil)
	checkOriginBodyLeaves(t, analysis, f, bodyConversionSource, bodyConversionDigest, []originBodyLeaf{
		{name: "implicit_let", body: "implicit_let", clean: true,
			function: originSpan{426, 529, "fn implicit_let() -> int {\n    let mi: MyInt = MyInt { value: 1 };\n    let n: int = mi;\n    return n;\n}"},
			cleared: []originRefusal{
				{originSpan{510, 512, "mi"}, originImplicitToRefusal},
				{originSpan{510, 512, "mi"}, originUseOtherOperation},
			}},
		{name: "implicit_arg", body: "implicit_arg", clean: true,
			function: originSpan{530, 624, "fn implicit_arg() -> int {\n    let mi: MyInt = MyInt { value: 2 };\n    return takes_int(mi);\n}"},
			cleared: []originRefusal{
				{originSpan{608, 621, "takes_int(mi)"}, originArgumentConversionRefusal},
				{originSpan{618, 620, "mi"}, originImplicitToRefusal},
			}},
		{name: "explicit_cast_ref_self", body: "explicit_ref_self", clean: true,
			function: originSpan{625, 714, "fn explicit_ref_self() -> int {\n    let p: Pt = Pt { x: 1, y: 2 };\n    return p to int;\n}"},
			cleared: []originRefusal{
				{originSpan{703, 711, "p to int"}, originCastRetainRefusal},
				{originSpan{703, 711, "p to int"}, originUseOtherOperation},
			}},
		{name: "recursive_body_operator", body: "recursive_mul", clean: true,
			function: originSpan{715, 767, "fn recursive_mul(p: &Pt) -> Pt {\n    return p * 2;\n}"},
			cleared: []originRefusal{
				{originSpan{759, 764, "p * 2"}, originBinaryCallableRefusal},
				{originSpan{346, 360, "self * (k - 1)"}, originBinaryCallableRefusal},
			}},
		{name: "converted_call_argument", body: "print_int", clean: true,
			function: originSpan{768, 831, "fn print_int() -> nothing {\n    print(7);\n    return nothing;\n}"},
			cleared:  []originRefusal{{originSpan{800, 808, "print(7)"}, originArgumentConversionRefusal}}},
	})
}
