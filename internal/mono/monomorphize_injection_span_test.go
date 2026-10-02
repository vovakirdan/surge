package mono

import "testing"

func TestMonoGroupedImplicitTagInjection(t *testing.T) {
	for _, tag := range []string{"Some", "Success"} {
		declaration, target := "type Option<T> = Some(T) | nothing;", "Option<int>"
		if tag == "Success" {
			declaration, target = "type Erring<T, E> = Success(T) | E;", "Erring<int, nothing>"
		}
		for _, tc := range []struct {
			expression string
			functions  int
		}{
			{"7", 2},
			{"(7)", 2},
			{"(((7)))", 2},
			{"((1 + 6))", 2},
			// The payload's real generic call must keep its own use-site span.
			{"((id(7)))", 3},
		} {
			t.Run(tag+"/"+tc.expression, func(t *testing.T) {
				src := "tag " + tag + "<T>(T);\n" + declaration + "\n" +
					"fn id<T>(x: T) -> T { return x; }\n" +
					"fn main() { let value: " + target + " = " + tc.expression + "; }\n"
				mm, typesIn, err := compileAndMonomorphize(t, src)
				if err != nil {
					t.Fatalf("grouped implicit %s injection failed: %v", tag, err)
				}
				if err := validateMonoModuleNoTypeParams(mm, typesIn); err != nil {
					t.Fatalf("implicit injection left generic types: %v", err)
				}
				if got := len(mm.Funcs); got != tc.functions {
					t.Fatalf("mono funcs = %d, want %d (main, %s<int>, and id<int> when used)", got, tc.functions, tag)
				}
			})
		}
	}
}
