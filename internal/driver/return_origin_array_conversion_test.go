package driver

import "testing"

const arrayConversionSource = `type Foo = { x: int };
extern<Foo> {
    fn __to(self: &Foo, _: string) -> string {
        return "Foo";
    }
}
fn probe() -> nothing {
    let a: int[] = [];
    let f: int[2] = [1, 2];
    let xs: Foo[] = [];
    let sa: string = a to string;
    let sf: string = f to string;
    let sx: string = xs to string;
    let _ = sa;
    let _ = sf;
    let _ = sx;
    return nothing;
}
`

func TestAnalyzeExactArrayStringConversions(t *testing.T) {
	f, analysis := analyzeOriginRoot(t, "array_string_conversions", arrayConversionSource, false, nil)
	originExactPending(t, analysis, f.unit.SourceKey, originSpan{0, len(arrayConversionSource), arrayConversionSource}, nil)
}

const userGenericConversionSource = `type Wrap<T> = { value: T };
extern<Wrap<T>> {
    fn __to(self: &Wrap<T>, _: string) -> string {
        return "Wrap";
    }
}
fn probe() -> nothing {
    let w: Wrap<int> = Wrap::<int> { value = 1 };
    let s: string = w to string;
    let _ = s;
    return nothing;
}
`

func TestAnalyzeExactUserGenericConversion(t *testing.T) {
	f, analysis := analyzeOriginRoot(t, "user_generic_conversion", userGenericConversionSource, false, nil)
	originExactPending(t, analysis, f.unit.SourceKey, originSpan{0, len(userGenericConversionSource), userGenericConversionSource}, nil)
}
