package driver

import (
	"strings"
	"testing"

	"surge/internal/diag"
)

// taskCloneProject wraps one body in a program that spawns a task and is
// otherwise the smallest thing the driver will compile.
func taskCloneProject(decls, body string) map[string]string {
	return map[string]string{
		"main.sg": `
pragma module::app;
` + decls + `
@entrypoint
fn main() {
` + body + `}
`,
	}
}

const nonClonablePayload = `
type Widget = { name: string }

async fn make_widget() -> Widget {
    return Widget { name = "w" };
}
`

// TestTaskCloneOnNonClonablePayloadIsRefused is the concrete half of the
// clonability obligation.
//
// Before this obligation existed the compiler accepted the call and handed out
// a second handle to one result, which is the divergence RV2-DEBT-133 records:
// `Task<T>.clone()` is valid exactly when an independent `T` can be produced.
func TestTaskCloneOnNonClonablePayloadIsRefused(t *testing.T) {
	result := diagnoseCloneProject(t, taskCloneProject(nonClonablePayload, `
    let first = spawn make_widget();
    let second = first.clone();
    let a = first.await();
    let b = second.await();
`))
	items := cloneDiagnosticsWithCode(result.Bag, diag.SemaTypeNotClonable)
	if len(items) != 1 {
		t.Fatalf("SEM3116 diagnostics = %d, want one: %s", len(items), formatCloneDiagnostics(result.Bag))
	}
	item := items[0]
	if !strings.Contains(item.Message, "owes another independent `Widget`") {
		t.Fatalf("headline does not explain the entitlement: %q", item.Message)
	}
	if !strings.Contains(item.Message, "neither Copy nor validly `__clone`-able") {
		t.Fatalf("headline does not name why the payload refuses: %q", item.Message)
	}
	// No automatic edit, ever: the compiler cannot invent a clone body, and
	// `@copy` would change what every other use of Widget means.
	if len(item.Fixes) != 0 {
		t.Fatalf("a Task clone refusal carried %d fix(es): %+v", len(item.Fixes), item.Fixes)
	}
	requireNoteContaining(t, item, "no `__clone` declaration claims this type")
	requireNoteContaining(t, item, "consume this one by awaiting it")
	requireNoteContaining(t, item, "extern<Widget> { fn __clone(self: &Widget) -> Widget }")
}

// TestTaskCloneKeepsClonablePayloadsAccepted is the positive twin. A refusal
// that also refused these would be a regression wearing the shape of a fix.
func TestTaskCloneKeepsClonablePayloadsAccepted(t *testing.T) {
	cases := []struct {
		name  string
		decls string
		body  string
	}{
		{
			name: "copy payload",
			decls: `
async fn make_int() -> int {
    return 7;
}
`,
			body: `
    let first = spawn make_int();
    let second = first.clone();
    let a = first.await();
    let b = second.await();
`,
		},
		{
			name: "string payload duplicates through its intrinsic",
			decls: `
async fn make_text() -> string {
    return "hello";
}
`,
			body: `
    let first = spawn make_text();
    let second = first.clone();
    let a = first.await();
    let b = second.await();
`,
		},
		{
			name: "payload with a user __clone",
			decls: `
type Boxed = { name: string }

extern<Boxed> {
    pub fn __clone(self: &Boxed) -> Boxed {
        return Boxed { name = clone(self.name) };
    }
}

async fn make_boxed() -> Boxed {
    return Boxed { name = "b" };
}
`,
			body: `
    let first = spawn make_boxed();
    let second = first.clone();
    let a = first.await();
    let b = second.await();
`,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := diagnoseCloneProject(t, taskCloneProject(testCase.decls, testCase.body))
			if items := cloneDiagnosticsWithCode(result.Bag, diag.SemaTypeNotClonable); len(items) != 0 {
				t.Fatalf("a clonable payload was refused: %s", formatCloneDiagnostics(result.Bag))
			}
		})
	}
}

// TestTaskCloneThroughGenericInstantiationReportsTheSameCode is the other half
// of RV2-DEBT-133: a generic clone failure used to escape as a raw
// monomorphization error, with no span an author could act on.
//
// The two paths must agree on the CODE and on the headline. They differ only in
// the two notes that name the instantiation, because that is the one fact the
// concrete call does not have.
func TestTaskCloneThroughGenericInstantiationReportsTheSameCode(t *testing.T) {
	result := diagnoseCloneProject(t, taskCloneProject(nonClonablePayload+`
fn duplicate<T>(handle: &Task<T>) -> Task<T> {
    return handle.clone();
}
`, `
    let first = spawn make_widget();
    let second = duplicate(&first);
    let a = first.await();
    let b = second.await();
`))
	items := cloneDiagnosticsWithCode(result.Bag, diag.SemaTypeNotClonable)
	if len(items) != 1 {
		t.Fatalf("SEM3116 diagnostics = %d, want one: %s", len(items), formatCloneDiagnostics(result.Bag))
	}
	item := items[0]
	if len(item.Fixes) != 0 {
		t.Fatalf("an instantiated Task clone refusal carried a fix: %+v", item.Fixes)
	}

	// The acceptance criterion is not "both report SEM3116" but "both report
	// THE SAME SEM3116": one code, one headline, one set of facts, so an author
	// who has seen the concrete refusal recognises the instantiated one.
	concrete := diagnoseCloneProject(t, taskCloneProject(nonClonablePayload, `
    let first = spawn make_widget();
    let second = first.clone();
    let a = first.await();
    let b = second.await();
`))
	concreteItems := cloneDiagnosticsWithCode(concrete.Bag, diag.SemaTypeNotClonable)
	if len(concreteItems) != 1 {
		t.Fatalf("concrete comparison run reported %d diagnostics", len(concreteItems))
	}
	if item.Message != concreteItems[0].Message {
		t.Fatalf("instantiated headline\n  %q\ndiffers from the concrete one\n  %q",
			item.Message, concreteItems[0].Message)
	}
	requireNoteContaining(t, item, "is legal on its own; instantiations with a clonable type still compile")
	requireNoteContaining(t, item, "`Widget` arrives from this instantiation")
}

// TestUninstantiatedGenericTaskCloneStaysDeferred is the deferral proof.
//
// A generic that nobody instantiates has not been decided, and refusing it here
// would put a clonability constraint on a template whose only crime is being
// unused. The obligation rides the instantiation graph precisely so that this
// program is never asked the question.
func TestUninstantiatedGenericTaskCloneStaysDeferred(t *testing.T) {
	result := diagnoseCloneProject(t, taskCloneProject(nonClonablePayload+`
fn duplicate<T>(handle: &Task<T>) -> Task<T> {
    return handle.clone();
}
`, `
    let first = spawn make_widget();
    let a = first.await();
`))
	if items := cloneDiagnosticsWithCode(result.Bag, diag.SemaTypeNotClonable); len(items) != 0 {
		t.Fatalf("an uninstantiated generic was refused: %s", formatCloneDiagnostics(result.Bag))
	}
}

func requireNoteContaining(t *testing.T, item *diag.Diagnostic, want string) {
	t.Helper()
	for _, note := range item.Notes {
		if strings.Contains(note.Msg, want) {
			return
		}
	}
	var got strings.Builder
	for _, note := range item.Notes {
		got.WriteString("\n  ")
		got.WriteString(note.Msg)
	}
	t.Fatalf("no note contains %q; notes were:%s", want, got.String())
}
