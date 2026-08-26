package driver

import (
	"strings"
	"testing"

	"surge/internal/diag"
)

func adviceProject(body string) map[string]string {
	return map[string]string{"main.sg": "\npragma module::app;\n" + body}
}

func adviceHelpText(bag *diag.Bag, code diag.Code) string {
	var out strings.Builder
	for _, item := range bag.Items() {
		if item.Code != code {
			continue
		}
		for _, entry := range item.Help {
			out.WriteString(entry.Msg)
			out.WriteString("\n")
		}
	}
	return out.String()
}

// TestCloneAdviceFollowsTheTypeIsThePointOfTheTable.
//
// Every clone hint used to be unconditional: the same `.__clone()` sentence
// went out whether the type had a clone, was Copy, or was a type parameter
// nobody had substituted yet. Following it on a non-clonable type produced a
// second error, which is the worst thing a hint can do.
func TestCloneAdviceFollowsTheType(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		code       diag.Code
		wantHelp   []string
		refuseHelp []string
	}{
		{
			// `string` carries `@intrinsic fn __clone(self: &string) -> string`,
			// so the clone route is real and may be named.
			name: "clonable value is offered the free function",
			body: `
fn sink(value: string) -> int { return 1; }

@entrypoint
fn main() {
    let s: string = "gone";
    let a = sink(s);
    let b = sink(s);
}
`,
			code:     diag.SemaUseAfterMove,
			wantHelp: []string{"clone(s)"},
		},
		{
			name: "non-clonable value is offered the borrow only",
			body: `
type Widget = { name: string }

fn sink(value: Widget) -> int { return 1; }

@entrypoint
fn main() {
    let w = Widget { name = "w" };
    let a = sink(w);
    let b = sink(w);
}
`,
			code:       diag.SemaUseAfterMove,
			wantHelp:   []string{"borrow"},
			refuseHelp: []string{"clone("},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := diagnoseCloneProject(t, adviceProject(testCase.body))
			help := adviceHelpText(result.Bag, testCase.code)
			if help == "" {
				t.Fatalf("no help was offered at all: %s", formatCloneDiagnostics(result.Bag))
			}
			for _, want := range testCase.wantHelp {
				if !strings.Contains(help, want) {
					t.Fatalf("help does not offer %q:\n%s", want, help)
				}
			}
			for _, refused := range testCase.refuseHelp {
				if strings.Contains(help, refused) {
					t.Fatalf("help names %q for a type that has no clone:\n%s", refused, help)
				}
			}
		})
	}
}

// TestAdviceOnlyGenericStaysUnconstrained is the mandatory negative control
// from the diagnostic contract.
//
// A generic instantiated with a non-clonable type, at a site whose diagnostic
// merely WOULD LIKE to mention cloning, must keep its original ownership
// diagnostic and nothing else. Optional advice is not a semantic obligation: it
// may not constrain the type parameter, reject the instantiation, or add
// SEM3116 — the only operation that records a clone obligation is one whose
// validity actually requires a clone.
func TestAdviceOnlyGenericStaysUnconstrained(t *testing.T) {
	result := diagnoseCloneProject(t, adviceProject(`
type Widget = { name: string }

fn sink<T>(value: T) -> int { return 1; }

@entrypoint
fn main() {
    let w = Widget { name = "w" };
    let a = sink(w);
    let b = sink(w);
}
`))
	moves := cloneDiagnosticsWithCode(result.Bag, diag.SemaUseAfterMove)
	if len(moves) == 0 {
		t.Fatalf("the original ownership diagnostic disappeared: %s", formatCloneDiagnostics(result.Bag))
	}
	if refusals := cloneDiagnosticsWithCode(result.Bag, diag.SemaTypeNotClonable); len(refusals) != 0 {
		t.Fatalf("an advice-only site turned into a clonability refusal: %s", formatCloneDiagnostics(result.Bag))
	}
	if refusals := cloneDiagnosticsWithCode(result.Bag, diag.SemaOperationNeedsClonable); len(refusals) != 0 {
		t.Fatalf("an advice-only site made the operation unavailable: %s", formatCloneDiagnostics(result.Bag))
	}
	for _, item := range moves {
		for _, entry := range item.Help {
			if strings.Contains(entry.Msg, "clone(") {
				t.Fatalf("an advice-only site advised a clone for a non-clonable substitution: %q", entry.Msg)
			}
		}
	}
}
