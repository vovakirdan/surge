package driver

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"testing"

	"surge/internal/diag"
)

// A reference may not be stored in an aggregate at any depth (SEM3138): `Option<&T>`, `Option<&mut T>` and any type holding a reference somewhere
// inside are refused wherever a bare `&T` is -- a struct field, a tuple or array element, a
// map key or value, a user tag's payload, a channel payload -- as a written type, a literal,
// or a generic user struct instantiated with such an argument. Each program is a ROOT program
// over the real core. A refused row names every span that must carry SEM3138, exactly once,
// and its message; a control row carries no error: `Option<&T>` (or a union of the user's
// own) as a function result, a local, a parameter or a compare subject stays legal, and so
// do owned Option fields, the core BytesView and the `(&T)[]` a variadic `...args: &T`
// parameter desugars to, and a reference TO an array. A core Map or Array written by name
// (`Map<K, &V>`, `Array<&T>`) holds its elements as `(&T)[]` does, so it is refused too;
// map_of_references_outlives_its_referent is the program that read a dying local through
// such a map and was refused only by an unfinished return-origin analysis. A type argument closed by a `>>` token spans both of its characters, so
// the inner nominal of `Box<Box<&int>>` and the payload of `Channel<Option<&int>>` read with
// one `>` too many.
const semaRefInAggregate = diag.Code(3138)

type refInAggregateRow struct {
	probe   taskCheckProbe
	spans   []string
	message string
}

func refInAggregateRows() []refInAggregateRow {
	return []refInAggregateRow{
		{probe: taskCheckProbe{name: "field_option_ref", digest: "8aaa4de7520cb618d52ad59828ee7d50eff70124fc7218a17b0fe10a8dd58a48",
			text: "type S = { a: Option<&int> }\n"}, spans: []string{"Option<&int>"}, message: "a struct field cannot hold a reference (Option<&int> holds &int)"},
		{probe: taskCheckProbe{name: "field_option_mut_ref", digest: "d8ca482c014995ef725230be48ac09c3edfeef6a64739d7e0e766fa4d2fd109d",
			text: "type S = { a: Option<&mut int> }\n"}, spans: []string{"Option<&mut int>"}, message: "a struct field cannot hold a reference (Option<&mut int> holds &mut int)"},
		{probe: taskCheckProbe{name: "field_nested_option_array", digest: "f8e8320b101493b5829f802d37ade2e8b92a3b4e69f6373669e2c725f6c231e1",
			text: "type S = { a: Option<Option<&int>>[] }\n"}, spans: []string{"Option<Option<&int>>"}, message: "an array element cannot hold a reference (Option<Option<&int>> holds &int)"},
		{probe: taskCheckProbe{name: "tuple_type", digest: "fa4fac37bfb265dd6a90a0dd90bd57147c6001a0f689b6eef7a0c4a1b102fc79",
			text: "fn f(t: (Option<&int>, int)) -> int {\n    return 0;\n}\n"}, spans: []string{"Option<&int>"}, message: "a tuple element cannot hold a reference (Option<&int> holds &int)"},
		{probe: taskCheckProbe{name: "tuple_literal_some_mut", digest: "629a2ce7f70ff540aef42eb41bac8b4a93ff8f7b67587ec3710f4a0ceb932f18",
			text: "fn f() -> int {\n    let mut x: int = 1;\n    let t = (Some::<&mut int>(&mut x), 1);\n    return 0;\n}\n"}, spans: []string{"Some::<&mut int>(&mut x)"}, message: "a tuple element cannot hold a reference (Some<&mut int> holds &mut int)"},
		{probe: taskCheckProbe{name: "array_type", digest: "b61daad76d82a7fa41a360970d8bdd268dccfefa0555a75686b786b339bbd07e",
			text: "fn f(xs: Option<&int>[]) -> int {\n    return 0;\n}\n"}, spans: []string{"Option<&int>"}, message: "an array element cannot hold a reference (Option<&int> holds &int)"},
		{probe: taskCheckProbe{name: "array_literal", digest: "5dee1e3098f2520c520a5055e90ab6211bb7dd12a3a513b0383788fe9981a8d9",
			text: "fn f() -> int {\n    let x: int = 1;\n    let xs = [Some::<&int>(&x)];\n    return 0;\n}\n"}, spans: []string{"Some::<&int>(&x)"}, message: "an array element cannot hold a reference (Some<&int> holds &int)"},
		{probe: taskCheckProbe{name: "user_tag_payload", digest: "b59cb7f5d3909b12d6c46f8f5302f99158db0e34771d5afca56874eff85adf71",
			text: "tag W(Option<&int>);\n"}, spans: []string{"Option<&int>"}, message: "a tag payload cannot hold a reference (Option<&int> holds &int)"},
		{probe: taskCheckProbe{name: "generic_struct_option", digest: "d6576bdcc8ca14eb9bb363021e090b212974f8627bd538780e36eed163e13c80",
			text: "type Box<T> = { v: T }\n\nfn f(b: Box<Option<&int>>) -> int {\n    return 0;\n}\n"}, spans: []string{"Box<Option<&int>>"}, message: "a struct field cannot hold a reference (Box<Option<&int>> holds &int)"},
		{probe: taskCheckProbe{name: "generic_struct_ref", digest: "2cd9c2f2649003b5f37a76d0f2ffc9c006cb7708058c1fd982e4b2f11c148b1e",
			text: "type Box<T> = { v: T }\n\nfn f(b: Box<&int>) -> int {\n    return 0;\n}\n"}, spans: []string{"Box<&int>"}, message: "a struct field cannot hold a reference (Box<&int> holds &int)"},
		{probe: taskCheckProbe{name: "generic_struct_reports_the_inner_once", digest: "13fb5b73752ea561bd4c488311cdf30d7b593ab06aff11bc6b3550a5d0ae6f4e",
			text: "type Box<T> = { v: T }\n\ntype S = { b: Box<Box<&int>> }\n"}, spans: []string{"Box<&int>>"}, message: "a struct field cannot hold a reference (Box<&int> holds &int)"},
		{probe: taskCheckProbe{name: "generic_struct_literal", digest: "eed524b668efa81a35f8934f8a7030ef452195a43765789ec77dfa0b688e942b",
			text: "type Box<T> = { v: T }\n\nfn f() -> int {\n    let x: int = 1;\n    let b = Box { v: Some::<&int>(&x) };\n    return 0;\n}\n"}, spans: []string{"Box { v: Some::<&int>(&x) }"}, message: "a struct field cannot hold a reference (Box<Some<&int>> holds &int)"},
		{probe: taskCheckProbe{name: "static_turbofish", digest: "b9711112f73fcf8d4f7970c74e9bc30a73254cf1a2dd3f086c8efd95431daf38",
			text: "type Box<T> = { v: T }\n\nextern<Box<T>> {\n    fn zero() -> int {\n        return 0;\n    }\n}\n\nfn f() -> int {\n    return Box::<Option<&int>>::zero();\n}\n"}, spans: []string{"Box::<Option<&int>>::zero()"}, message: "a struct field cannot hold a reference (Box<Option<&int>> holds &int)"},
		{probe: taskCheckProbe{name: "alias_field", digest: "68e63cf5f01667d6e28218e978ad3615cdeef3aa577a57833c9498c3e89cf99d",
			text: "type A = Option<&int>;\n\ntype S = { a: A }\n"}, spans: []string{"A"}, message: "a struct field cannot hold a reference (A holds &int)"},
		{probe: taskCheckProbe{name: "channel_payload", digest: "f0d06d7b2c7d4c1072a2a32083dba88003c7b4e9ad0ed0fa22b029a488425650",
			text: "fn f(c: Channel<Option<&int>>) -> int {\n    return 0;\n}\n"}, spans: []string{"Option<&int>>"}, message: "a channel payload cannot hold a reference (Option<&int> holds &int)"},
		{probe: taskCheckProbe{name: "user_union_in_field", digest: "e126c4197a132cb3d2e5ee41fedf3be8d3b91c2a529029ef8c25ff48c89eeb91",
			text: "tag Wrap<T>(T);\n\ntype V<T> = Wrap(T) | nothing;\n\ntype S = { v: V<&int> }\n"}, spans: []string{"V<&int>"}, message: "a struct field cannot hold a reference (V<&int> holds &int)"},
		{probe: taskCheckProbe{name: "map_value_in_field", digest: "16d08984c58fa047de91c145831ae6642c95d3a98835b0348d0ff6051c3175de",
			text: "type S = { m: Map<string, Option<&int>> }\n"}, spans: []string{"Map<string, Option<&int>>"}, message: "a map value cannot hold a reference (Option<&int> holds &int)"},
		{probe: taskCheckProbe{name: "map_value_bare_by_name", digest: "6d7a16f238de4561080f83fa6702857500ec2087eaeb6d9ebab68605446a82ee",
			text: "fn f(m: &Map<string, &int>) -> int {\n    return 0;\n}\n"}, spans: []string{"Map<string, &int>"}, message: "a map value cannot hold a reference (&int)"},
		{probe: taskCheckProbe{name: "array_bare_by_name", digest: "ea9966fe04e1330e08e7e4dce420d9b167c5e3e0179f4f2ccf1334b5af4b8c1e",
			text: "fn f(xs: &Array<&int>) -> int {\n    return 0;\n}\n"}, spans: []string{"Array<&int>"}, message: "an array element cannot hold a reference (&int)"},
		{probe: taskCheckProbe{name: "map_turbofish_bare", digest: "1f94e0f52bd1d09b89d7aa082ff50bc7119a9263aeed3ba36cc03c5a7d71a956",
			text: "fn f() -> uint {\n    let m = Map::<string, &int>::new();\n    return m.length();\n}\n"}, spans: []string{"Map::<string, &int>::new()"}, message: "a map value cannot hold a reference (&int)"},
		{probe: taskCheckProbe{name: "map_of_references_outlives_its_referent", digest: "7c74cf1ebd48a4d3cae9fa9524ac5854f081e29e4eb03a51bedbec1374eac3a1",
			text: "fn main() -> int { let mut m: Map<string, &string> = Map::<string, &string>::new(); { let x = \"hello\" + \"!\"; m.insert(\"a\", &x); } let k = \"a\"; compare m.get_ref(&k) { Some(v) => { print(**v); } nothing => {} }; return 0; }\n"}, spans: []string{"Map<string, &string>", "Map::<string, &string>::new()"}, message: "a map value cannot hold a reference (&string)"},
		{probe: taskCheckProbe{name: "control_fn_result", digest: "7382823e90c8d3c3a9cfab496397ba252d1e7d38f8a4d184de5d23090ed1be59",
			text: "fn look(m: &Map<string, int>, k: &string) -> Option<&int> {\n    return m.get_ref(k);\n}\n"}},
		{probe: taskCheckProbe{name: "control_local_and_compare", digest: "168a62c152b30d986088c09de8c348f203784e908e004e1bdcea877a0f9986a1",
			text: "fn f(m: &Map<string, int>) -> int {\n    let k: string = \"a\";\n    let o = m.get_ref(&k);\n    return compare o {\n        Some(v) => *v;\n        nothing => 0;\n    };\n}\n"}},
		{probe: taskCheckProbe{name: "control_parameter", digest: "c5be541806343a67b670a70d18f283d2fde1452ecc262f2d61a8cd2cb699b5f8",
			text: "fn f(o: Option<&mut int>) -> int {\n    return compare o {\n        Some(v) => *v;\n        nothing => 0;\n    };\n}\n"}},
		{probe: taskCheckProbe{name: "control_owned_fields", digest: "09131f6bb6b95de6d6f473a122c1a54810c50f101320c3438cd4a8654dbd0a6e",
			text: "type Box<T> = { v: T }\n\ntype S = { a: Option<int>, b: BytesView, c: (int, string)[], d: Box<Option<string>>, e: Map<string, Option<int>> }\n"}},
		{probe: taskCheckProbe{name: "control_user_union_parameter", digest: "3e4c02967d983b58ebf801613885b17f2600951489b39ffd8b4aa3137dc11e21",
			text: "tag Wrap<T>(T);\n\ntype V<T> = Wrap(T) | nothing;\n\nfn f(v: V<&int>) -> int {\n    return 0;\n}\n"}},
		{probe: taskCheckProbe{name: "control_variadic_references", digest: "7bbf557ef3a65ba7e2b07ec229f811fe393c026fdf909cae7865a8dc38f87d75",
			text: "fn count(...xs: &int) -> uint {\n    return xs.__len();\n}\n\nfn f() -> uint {\n    let a: int = 1;\n    let b: int = 2;\n    return count(&a, &b);\n}\n"}},
		{probe: taskCheckProbe{name: "control_references_to_arrays", digest: "f7e2908181236059ecf7a4a3531ab59b0f277b0e08b2da0ae4fa3c5e3f86ee5a",
			text: "fn f(xs: &int[], ys: &mut int[]) -> int {\n    let r: &int[] = xs;\n    return 0;\n}\n"}},
	}
}

func TestRefInAggregateAtAnyDepth(t *testing.T) {
	for _, row := range refInAggregateRows() {
		t.Run(row.probe.name, func(t *testing.T) {
			_, errs := taskCheckErrorCodes(t, row.probe)
			var got []string
			for _, d := range errs {
				if d.Code != semaRefInAggregate {
					t.Fatalf("error %s besides SEM3138: %+v", d.Code.ID(), *d)
				}
				got = append(got, row.probe.text[d.Primary.Start:d.Primary.End])
				if d.Message != row.message {
					t.Fatalf("SEM3138 message %q, want %q", d.Message, row.message)
				}
				if len(d.Help) == 0 {
					t.Fatalf("SEM3138 without its help: %+v", *d)
				}
			}
			if !slices.Equal(got, row.spans) {
				t.Fatalf("SEM3138 at %q, want exactly %q", got, row.spans)
			}
		})
	}
}

// storedReferenceRow is a program another rule's test used to hold a reference in an aggregate.
// Since the containment rule went deep the program is refused by SEM3138 before that rule is
// asked, so the row now pins the refusal: want is the sorted set of error codes, SEM3138 among
// them.
type storedReferenceRow struct {
	name, want, text string
}

// runStoredReferenceRows diagnoses each program as a root over the real core and requires
// exactly its error codes.
func runStoredReferenceRows(t *testing.T, rows []storedReferenceRow) {
	t.Helper()
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			probe := taskCheckProbe{name: row.name, digest: fmt.Sprintf("%x", sha256.Sum256([]byte(row.text))), text: row.text}
			if codes, _ := taskCheckErrorCodes(t, probe); codes != row.want {
				t.Fatalf("error codes %q, want %q", codes, row.want)
			}
		})
	}
}
