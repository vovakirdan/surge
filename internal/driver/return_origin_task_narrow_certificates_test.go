package driver

import (
	"strings"
	"testing"
)

// Task-handle precision over ROOT programs with the real core. A row reads the
// Pending rows inside one function; clean does not mean source-free when the
// summary deliberately carries a parameter root.
//
//   - D1: a call through a function value whose result is a core Task carries the
//     callable promise's declared payload sources through await. The promise must not
//     match a body-less intrinsic; direct body-less values stay refused too.
//   - Task values use their exact current origins through assignments, parameters
//     and async captures. Running-task borrows belong to the task checker; its
//     negative probes reject a borrowed handle that could outlive its frame.
//   - The core Task clone preserves the handle's origins. The ordinary generic
//     core clone preserves its input parameter root and is therefore also exact.
//
// The identity rows break the core declaration used by D1 or the Task clone and
// require those controls to be refused again.
type taskNarrowCertificateRow struct {
	name, fn, text string
	clean          bool
}

func taskNarrowCertificateRows() []taskNarrowCertificateRow {
	return []taskNarrowCertificateRow{
		{name: "d2_null_default_control", fn: "slot_default", clean: true,
			text: "fn slot_default() -> Task<int> {\n    let mut slot: Task<int>;\n    return async {\n        ret compare slot.await() {\n            Success(v) => v;\n            Cancelled() => 0;\n        };\n    };\n}\n"},
		{name: "d2_assigned_on_one_branch", fn: "slot_branch", clean: true,
			text: "fn slot_branch(c: bool) -> Task<int> {\n    let mut slot: Task<int>;\n    if c {\n        slot = async { ret 1; };\n    }\n    return async {\n        ret compare slot.await() {\n            Success(v) => v;\n            Cancelled() => 0;\n        };\n    };\n}\n"},
		{name: "d2_assigned_in_a_loop", fn: "slot_loop", clean: true,
			text: "fn slot_loop(c: bool) -> Task<int> {\n    let mut slot: Task<int>;\n    while c {\n        slot = async { ret 2; };\n        break;\n    }\n    return async {\n        ret compare slot.await() {\n            Success(v) => v;\n            Cancelled() => 0;\n        };\n    };\n}\n"},
		{name: "d2_initialized_with_a_task", fn: "slot_init", clean: true,
			text: "fn slot_init() -> Task<int> {\n    let mut slot: Task<int> = async { ret 4; };\n    return async {\n        ret compare slot.await() {\n            Success(v) => v;\n            Cancelled() => 0;\n        };\n    };\n}\n"},
		{name: "d2_parameter_task", fn: "slot_param", clean: true,
			text: "fn slot_param(slot: Task<int>) -> Task<int> {\n    return async {\n        ret compare slot.await() {\n            Success(v) => v;\n            Cancelled() => 0;\n        };\n    };\n}\n"},
		{name: "d2_written_through_mut_reference", fn: "slot_mut_ref", clean: true,
			text: "fn fill(t: &mut Task<int>) {\n    *t = async { ret 3; };\n}\n\nfn slot_mut_ref() -> Task<int> {\n    let mut slot: Task<int>;\n    fill(&mut slot);\n    return async {\n        ret compare slot.await() {\n            Success(v) => v;\n            Cancelled() => 0;\n        };\n    };\n}\n"},
		{name: "d2_written_through_implicit_borrow", fn: "slot_implicit", clean: true,
			text: "fn fill(t: &mut Task<int>) {\n    *t = async { ret 3; };\n}\n\nfn slot_implicit() -> Task<int> {\n    let mut slot: Task<int>;\n    fill(slot);\n    return async {\n        ret compare slot.await() {\n            Success(v) => v;\n            Cancelled() => 0;\n        };\n    };\n}\n"},
		{name: "d2_written_through_stored_reference", fn: "slot_stored_ref", clean: true,
			text: "fn slot_stored_ref() -> Task<int> {\n    let mut slot: Task<int>;\n    {\n        let r = &mut slot;\n        *r = async { ret 3; };\n    }\n    return async {\n        ret compare slot.await() {\n            Success(v) => v;\n            Cancelled() => 0;\n        };\n    };\n}\n"},
		{name: "d2_written_by_a_mut_self_method", fn: "slot_method", clean: true,
			text: "extern<Task<T>> {\n    fn reset(self: &mut Task<T>) -> nothing {\n        return nothing;\n    }\n}\n\nfn slot_method() -> Task<int> {\n    let mut slot: Task<int>;\n    slot.reset();\n    return async {\n        ret compare slot.await() {\n            Success(v) => v;\n            Cancelled() => 0;\n        };\n    };\n}\n"},
		{name: "d1_inert_function_value_control", fn: "use_value", clean: true,
			text: "type Fn = async fn(int, int) -> int;\n\nasync fn add(a: int, b: int) -> int {\n    return a + b;\n}\n\nasync fn use_value(n: int) -> int {\n    let f: Fn = add;\n    let t = f(n, 2);\n    return compare t.await() {\n        Success(v) => v;\n        Cancelled() => 0;\n    };\n}\n"},
		{name: "d1_owned_string_payload_control", fn: "use_value", clean: true,
			text: "type Fn = async fn(int) -> string;\n\nasync fn name(n: int) -> string {\n    return \"x\";\n}\n\nasync fn use_value(n: int) -> int {\n    let f: Fn = name;\n    let t = f(n);\n    return compare t.await() {\n        Success(v) => 1;\n        Cancelled() => 0;\n    };\n}\n"},
		{name: "d1_reference_parameter", fn: "use_value", clean: true,
			text: "type Fn = async fn(&int) -> int;\n\nasync fn read(r: &int) -> int {\n    return *r;\n}\n\nasync fn use_value(n: int) -> int {\n    let f: Fn = read;\n    let t = f(&n);\n    return compare t.await() {\n        Success(v) => v;\n        Cancelled() => 0;\n    };\n}\n"},
		{name: "d1_loan_carrier_payload", fn: "use_value", clean: true,
			text: "type Fn = async fn(int) -> int[];\n\nasync fn make(n: int) -> int[] {\n    return [n];\n}\n\nasync fn use_value(n: int) -> int {\n    let f: Fn = make;\n    let t = f(n);\n    return compare t.await() {\n        Success(v) => 1;\n        Cancelled() => 0;\n    };\n}\n"},
		{name: "d1_reference_function_parameter_control", fn: "call_it", clean: true,
			text: "fn call_it(f: async fn(&int) -> int, n: &int) -> Task<int> {\n    return f(n);\n}\n"},
		{name: "d1_callable_parameter", fn: "use_value", clean: false,
			text: "fn twice(n: int) -> int {\n    return n * 2;\n}\n\ntype Fn = async fn(fn(int) -> int, int) -> int;\n\nasync fn apply(g: fn(int) -> int, n: int) -> int {\n    return g(n);\n}\n\nasync fn use_value(n: int) -> int {\n    let f: Fn = apply;\n    let t = f(twice, n);\n    return compare t.await() {\n        Success(v) => v;\n        Cancelled() => 0;\n    };\n}\n"},
		// A body-less core intrinsic through a value is not certified: neither backend can call it.
		{name: "d1_intrinsic_sleep_value_stays_refused", fn: "nap_value", clean: false,
			text: "fn nap_value(ms: uint) -> Task<nothing> {\n    let f = sleep;\n    return f(ms);\n}\n"},
		// The same intrinsic fixed to a declared promise, and a function-typed parameter a caller may
		// fill with an intrinsic: neither is certified.
		{name: "d1_intrinsic_sleep_annotated_value_stays_refused", fn: "nap_typed", clean: false,
			text: "fn nap_typed(ms: uint) -> Task<nothing> {\n    let f: fn(uint) -> Task<nothing> = sleep;\n    return f(ms);\n}\n"},
		{name: "d1_function_parameter_stays_refused", fn: "call_it", clean: false,
			text: "fn call_it(f: fn(uint) -> Task<nothing>, ms: uint) -> Task<nothing> {\n    return f(ms);\n}\n"},
		{name: "d3_generic_task_clone_control", fn: "duplicate", clean: true,
			text: "fn duplicate<T>(handle: &Task<T>) -> Task<T> {\n    return handle.clone();\n}\n"},
		{name: "d3_generic_core_clone_of_a_value", fn: "duplicate", clean: true,
			text: "fn duplicate<T>(value: &T) -> T {\n    return clone(value);\n}\n"},
	}
}

// taskNarrowFunction is the byte range of `fn name(` (with a leading `async `) to its closing brace.
func taskNarrowFunction(t *testing.T, text, name string) (int, int) {
	t.Helper()
	start := strings.Index(text, "fn "+name+"(")
	if start < 0 {
		start = strings.Index(text, "fn "+name+"<")
	}
	if start < 0 || strings.Count(text, "fn "+name+"(")+strings.Count(text, "fn "+name+"<") != 1 {
		t.Fatalf("PRECONDITION: function %q is not unique", name)
	}
	if strings.HasSuffix(text[:start], "async ") {
		start -= len("async ")
	}
	end := strings.Index(text[start:], "\n}\n")
	if end < 0 {
		t.Fatalf("PRECONDITION: function %q has no closing brace", name)
	}
	return start, start + end + 2
}

func checkTaskNarrowCertificate(t *testing.T, row taskNarrowCertificateRow, prepare func(originalGenericFixture), wantClean bool) {
	t.Helper()
	start, end := taskNarrowFunction(t, row.text, row.fn)
	f, analysis := analyzeOriginRoot(t, "task_narrow_"+row.name, row.text, false, prepare)
	local := originPendingWithin(analysis, f.unit.SourceKey, start, end)
	originNoEscape(t, analysis, f.owner.File.ID, originSpan{start, end, row.text[start:end]})
	if wantClean && len(local) != 0 {
		t.Errorf("certified function %q is still unfinished: %+v", row.fn, local)
	}
	if !wantClean && len(local) == 0 {
		t.Errorf("witness %q lost every refusal: the certificate reached what it must not", row.fn)
	}
}

func TestTaskNarrowCertificates(t *testing.T) {
	for _, row := range taskNarrowCertificateRows() {
		t.Run(row.name, func(t *testing.T) {
			checkTaskNarrowCertificate(t, row, nil, row.clean)
		})
	}
}

// Breaking the module identity of the core declaration a certificate stands on
// refuses its control again. Nothing is recognized by a spelling.
func TestTaskNarrowCertificateIdentity(t *testing.T) {
	for _, row := range []struct{ control, candidate string }{
		{"d1_inert_function_value_control", "checkpoint"},
		{"d3_generic_task_clone_control", "clone"},
	} {
		t.Run(row.control+"_without_core_"+row.candidate, func(t *testing.T) {
			var control taskNarrowCertificateRow
			for _, r := range taskNarrowCertificateRows() {
				if r.name == row.control {
					control = r
				}
			}
			if !control.clean {
				t.Fatalf("PRECONDITION: %q is not a clean control", row.control)
			}
			mutate := func(f originalGenericFixture) {
				matched := 0
				for i := range f.authority.CallableCandidates {
					candidate := &f.authority.CallableCandidates[i]
					if candidate.Name == row.candidate && candidate.SourceKey == "builtin" && candidate.ModulePath == "core/intrinsics" {
						candidate.ModulePath = "core/intrinsics_shadow"
						matched++
					}
				}
				if matched != 1 {
					t.Fatalf("PRECONDITION: core %s candidate is not unique: %d", row.candidate, matched)
				}
			}
			checkTaskNarrowCertificate(t, control, mutate, false)
		})
	}
}
