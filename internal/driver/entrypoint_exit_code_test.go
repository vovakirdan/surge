package driver

import (
	"strings"
	"testing"

	"surge/internal/diag"
)

// An @entrypoint result other than `nothing`/`int` must implement the ExitCode
// contract: exactly one `fn __exit_code(self: &T) -> int`.
func TestEntrypointExitCodeContractRefusals(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		notes []string
		help  string
	}{
		{
			name:  "no_contract",
			src:   "type MyType = { value: int };\n@entrypoint\nfn main() -> MyType { return MyType { value = 1 }; }\n",
			notes: []string{"required: fn __exit_code(self: &MyType) -> int"},
			help:  "add `fn __exit_code(self: &MyType) -> int`",
		},
		{
			name:  "legacy_to_int",
			src:   "type Status = { code: int };\nextern<Status> {\n    fn __to(self: Status, _t: int) -> int { return self.code; }\n}\n@entrypoint\nfn main() -> Status { return Status { code = 1 }; }\n",
			notes: []string{"is a conversion, not an exit code"},
		},
		{
			name:  "self_by_value",
			src:   "type Status = { code: int };\nextern<Status> {\n    pub fn __exit_code(self: Status) -> int { return self.code; }\n}\n@entrypoint\nfn main() -> Status { return Status { code = 1 }; }\n",
			notes: []string{"declare the receiver as `self: &Status`", "does not take `self: &T`"},
		},
		{
			name:  "wrong_result",
			src:   "type Status = { code: int };\nextern<Status> {\n    pub fn __exit_code(self: &Status) -> uint { return 1:uint; }\n}\n@entrypoint\nfn main() -> Status { return Status { code = 1 }; }\n",
			notes: []string{"does not return `int`"},
		},
		{
			name:  "float_has_no_exit_code",
			src:   "@entrypoint\nfn main() -> float { return 2.0; }\n",
			notes: []string{"required: fn __exit_code(self: &float) -> int"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, _ := diagnoseEntrypointContract(t, tc.src)
			diagnostic := requireEntrypointDiagnostic(t, result, diag.SemaEntrypointReturnNotConvertible)
			if !strings.Contains(diagnostic.Message, "implement the ExitCode contract") {
				t.Fatalf("message = %q", diagnostic.Message)
			}
			for _, note := range tc.notes {
				requireEntrypointNote(t, diagnostic, note)
			}
			if tc.help != "" {
				requireEntrypointHelp(t, diagnostic, tc.help)
			}
			if result.HIR != nil {
				t.Fatal("an entrypoint without ExitCode must stop before HIR")
			}
		})
	}
}

func TestEntrypointExitCodeContractAccepts(t *testing.T) {
	for name, src := range map[string]string{
		"option": "@entrypoint\nfn main() -> int? { return nothing; }\n",
		"erring": "@entrypoint\nfn main() -> int! { return Success(0); }\n",
		"uint":   "@entrypoint\nfn main() -> uint { return 0:uint; }\n",
		"user":   "type S = { c: int };\nextern<S> {\n    pub fn __exit_code(self: &S) -> int { return self.c; }\n}\n@entrypoint\nfn main() -> S { return S { c = 0 }; }\n",
	} {
		t.Run(name, func(t *testing.T) {
			result, _ := diagnoseEntrypointContract(t, src)
			for _, d := range result.Bag.Items() {
				if d.Severity == diag.SevError {
					t.Fatalf("unexpected error %s: %s", d.Code.ID(), d.Message)
				}
			}
			found := false
			for _, binding := range result.Sema.EntrypointCallableBindings {
				found = found || binding.CalleeKey != "" && strings.Contains(binding.CalleeKey, "__exit_code")
			}
			if !found {
				t.Fatalf("no __exit_code binding in %+v", result.Sema.EntrypointCallableBindings)
			}
		})
	}
}

// Option and Erring have no `__to(int)`: their exit code is not a conversion,
// so neither an implicit binding nor an explicit cast turns them into an int.
func TestOptionAndErringDoNotConvertToInt(t *testing.T) {
	for name, src := range map[string]string{
		"option_let":          "@entrypoint\nfn main() -> int { let o: Option<int> = Some(5); let x: int = o; return x; }\n",
		"option_cast":         "@entrypoint\nfn main() -> int { let o: Option<int> = Some(5); return o to int; }\n",
		"erring_let":          "@entrypoint\nfn main() -> int { let e: int! = Success(5); let x: int = e; return x; }\n",
		"erring_cast":         "@entrypoint\nfn main() -> int { let e: int! = Success(5); return e to int; }\n",
		"option_return":       "fn f(o: Option<int>) -> int { return o; }\n@entrypoint\nfn main() -> int { return f(Some(5)); }\n",
		"option_allow_to_arg": "@allow_to\nfn g(x: int) -> int { return x; }\n@entrypoint\nfn main() -> int { let o: Option<int> = Some(5); return g(o); }\n",
		"erring_field":        "type S = { v: int };\n@entrypoint\nfn main() -> int { let e: int! = Success(5); let s = S { v = e }; return s.v; }\n",
	} {
		t.Run(name, func(t *testing.T) {
			result, _ := diagnoseEntrypointContract(t, src)
			diagnostic := requireEntrypointDiagnostic(t, result, diag.SemaTypeMismatch)
			requireEntrypointHelp(t, diagnostic, "`.__exit_code()` gives its process exit code")
			for _, fixSuggestion := range diagnostic.Fixes {
				if strings.Contains(fixSuggestion.Title, "cast expression to int") {
					t.Fatalf("a cast that cannot exist is suggested: %+v", fixSuggestion)
				}
			}
		})
	}
}
