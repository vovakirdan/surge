package driver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/diag"
)

// Exercise the same public finalization with and without a request for HIR.
// Rejecting only during lowering leaves ordinary diagnostics unsound.
func TestDiagnoseReturnOriginEscape(t *testing.T) {
	stdlibRoot := detectStdlibRootFrom(".")
	if stdlibRoot == "" {
		t.Fatal("failed to locate stdlib root")
	}
	t.Setenv("SURGE_STDLIB", stdlibRoot)
	for _, emitHIR := range []bool{false, true} {
		t.Run(fmt.Sprintf("emit_hir_%t", emitHIR), func(t *testing.T) {
			for _, tc := range returnOriginPublicCases() {
				t.Run(tc.name, func(t *testing.T) {
					path := filepath.Join(t.TempDir(), "origin.sg")
					if err := os.WriteFile(path, []byte(tc.src), 0o600); err != nil {
						t.Fatal(err)
					}
					opts := DiagnoseOptions{
						Stage: DiagnoseStageAll, MaxDiagnostics: 64,
						IgnoreWarnings: true, KeepArtifacts: true, EmitHIR: emitHIR,
					}
					res, err := DiagnoseWithOptions(t.Context(), path, &opts)
					if err != nil {
						t.Fatalf("public diagnosis failed: %v", err)
					}
					if res == nil || res.Bag == nil || res.Sema == nil || res.Builder == nil ||
						res.Symbols == nil || res.Sema.TypeInterner == nil || len(res.Sema.ExprTypes) == 0 {
						t.Fatal("source did not reach typed semantic analysis")
					}
					t.Logf("RETURN_ORIGIN_DIAGNOSTICS=%+v", res.Bag.Items())
					if !tc.escapes {
						if res.Bag.HasErrors() {
							t.Fatalf("external owner must remain legal: %+v", res.Bag.Items())
						}
						if emitHIR && res.HIR == nil {
							t.Fatal("accepted source did not produce requested HIR")
						}
						return
					}
					found := false
					for _, d := range res.Bag.Items() {
						if d.Severity != diag.SevError {
							continue
						}
						if d.Code != diag.SemaBorrowEscapesReturn {
							t.Fatalf("unrelated refusal cannot prove lifetime safety: %+v", d)
						}
						if d.Primary.Start >= d.Primary.End || int(d.Primary.End) > len(tc.src) || len(d.Notes) == 0 {
							t.Fatalf("escape needs a source span and owner explanation: %+v", d)
						}
						found = true
					}
					if !found {
						t.Fatal("expected SEM3139 for a borrow whose owner leaves scope")
					}
					if res.HIR != nil {
						t.Fatal("rejected source reached HIR")
					}
				})
			}
		})
	}
}

type returnOriginPublicCase struct {
	name    string
	src     string
	escapes bool
}

func returnOriginPublicCases() []returnOriginPublicCase {
	var cases []returnOriginPublicCase
	for _, family := range []string{"multi_root_alias", "multi_root_call", "sequential_rebind", "loop_backedge"} {
		for _, local := range []bool{true, false} {
			owner, suffix := "outside_b", "outer_owners"
			if local {
				owner, suffix = "owned", "local_owner"
			}
			header := "fn read(value: &string) -> int { return 1; }\n"
			var body string
			switch family {
			case "multi_root_alias", "multi_root_call":
				value := "alias"
				if family == "multi_root_call" {
					header += "fn identity(value: &string) -> &string { return value; }\n"
					value = "identity(alias)"
				}
				body = fmt.Sprintf(`        let alias: &string = {
            if flag { ret &%s; }
            ret &outside_a;
        };
        ret %s;`, owner, value)
			case "sequential_rebind":
				first, last := "owned", "outside_a"
				if local {
					first, last = "outside_a", "owned"
				}
				body = fmt.Sprintf(`        let mut alias: &string = &%s;
        alias = &%s;
        ret alias;`, first, last)
			case "loop_backedge":
				body = fmt.Sprintf(`        let mut alias: &string = &outside_a;
        let mut selected: &string = &outside_a;
        let mut i: int = 0;
        while i < 2 {
            selected = alias;
            alias = &%s;
            i = i + 1;
        }
        ret selected;`, owner)
			}
			src := header + fmt.Sprintf(`fn probe(flag: bool) -> int {
    let outside_a: string = "first external owner";
    let outside_b: string = "second external owner";
    let escaped: &string = {
        let owned: string = "outer value block owner";
%s
    };
    return read(escaped) + read(&outside_a) + read(&outside_b);
}
`, body)
			cases = append(cases, returnOriginPublicCase{family + "_" + suffix, src, local})
		}
	}
	const block = `fn read(value: &string) -> int { return 1; }
fn probe() -> int {
    let outside: string = "outside";
    let escaped: &string = {
        let owned: string = "owned";
        ret &SOURCE;
    };
    return read(escaped);
}
`
	const assigned = `fn read(value: &string) -> int { return 1; }
fn probe() -> int {
    let outside: string = "outside";
    let other: string = "other";
    let mut escaped: &string = &outside;
    let count: int = {
        let owned: string = "owned";
        escaped = &SOURCE;
        ret 1;
    };
    return read(escaped) + count;
}
`
	cases = append(cases,
		returnOriginPublicCase{"direct_local_owner", strings.ReplaceAll(block, "SOURCE", "owned"), true},
		returnOriginPublicCase{"direct_outer_owner", strings.ReplaceAll(block, "SOURCE", "outside"), false},
		returnOriginPublicCase{"outer_binding_local_owner", strings.ReplaceAll(assigned, "SOURCE", "owned"), true},
		returnOriginPublicCase{"outer_binding_outer_owner", strings.ReplaceAll(assigned, "SOURCE", "other"), false},
		returnOriginPublicCase{"owned_parameter_storage", "fn probe(owned: string) -> &string { return &owned; }\n", true},
		returnOriginPublicCase{"borrowed_parameter_content", "fn probe(value: &string) -> &string { return value; }\n", false},
		returnOriginPublicCase{"known_function_ignores_local_argument", `fn first(a: &string, b: &string) -> &string { return a; }
fn read(value: &string) -> int { return 1; }
fn probe() -> int {
    let outside: string = "outside";
    let escaped: &string = {
        let owned: string = "owned";
        ret first(&outside, &owned);
    };
    return read(escaped);
}
`, false},
	)
	return cases
}
