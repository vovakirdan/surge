package driver

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"surge/internal/diag"
)

// A Map may not hold a task in its key or its value (SEM3225, owner ruling 2026-09-28):
// directly, or through an Option, a tuple, an array, a struct field or a generic argument.
// Each program is a ROOT program over the real core. A refused row names every span that
// must carry the error, exactly once, and the one other error code it may also carry; a
// control row carries no error at all. Tasks kept in an array and drained, the tasks'
// results kept in a map, a far task handle, a reference to a task, a Channel<Task<T>> in a
// map and generic maps of ints stay accepted.
//
// residual_generic_body is a named residual, not a control: a generic body that builds
// Map<int, T> is checked once over T, so its instantiation with a task is not a site the
// rule sees. The row goes red when that form becomes refused.
// semaTaskInMap is SEM3225 (diag.SemaTaskInMap) by number, so these rows compile, and read red,
// on a tree that has no such rule.
const semaTaskInMap = diag.Code(3225)

type taskInMapRow struct {
	probe taskCheckProbe
	spans []string
	also  diag.Code // the one other error a refused row may carry
}

func taskInMapRows() []taskInMapRow {
	return []taskInMapRow{
		{probe: taskCheckProbe{name: "annotation", digest: "3129f8b59f3d80fb38721c5f263e2fe29343d447c115f6747b693be89d603c92",
			text: "fn f(m: Map<int, Task<int>>) -> int {\n    return 0;\n}\n"}, spans: []string{"Map<int, Task<int>>"}},
		{probe: taskCheckProbe{name: "turbofish_new", digest: "2a2cbbc1fb3bea056d48538093731ee5a9a5ea3795736d7f73cb575ac70bc828",
			text: "async fn work(n: int) -> int {\n    return n * 2;\n}\n\nfn f() -> uint {\n    let m = Map::<int, Task<int>>::new();\n    return m.length();\n}\n"}, spans: []string{"Map::<int, Task<int>>::new()"}},
		{probe: taskCheckProbe{name: "annotation_and_turbofish", digest: "a485464276138d2a02a0a38842ced2826631a645a6e0ef9d1d4bbf3a4e0ec5c1",
			text: "async fn work(n: int) -> int {\n    return n * 2;\n}\n\nfn f() -> uint {\n    let mut m: Map<int, Task<int>> = Map::<int, Task<int>>::new();\n    m.insert(1, work(1));\n    return m.length();\n}\n"}, spans: []string{"Map<int, Task<int>>", "Map::<int, Task<int>>::new()"}},
		{probe: taskCheckProbe{name: "literal", digest: "86c29ed0b9d56d3f004998ec3f5db543950f1d9dcea3af89ef1f5589244fbe32",
			text: "async fn work(n: int) -> int {\n    return n * 2;\n}\n\nfn f() -> uint {\n    let m = { 1 => work(1), 2 => work(2) };\n    return m.length();\n}\n"}, spans: []string{"{ 1 => work(1), 2 => work(2) }"}},
		{probe: taskCheckProbe{name: "option_value", digest: "3b0fa39f902bacac1cd3d69d7b1c4f6515b3a0e101891bdadfbd8438bc450fb6",
			text: "fn f(m: Map<int, Option<Task<int>>>) -> int {\n    return 0;\n}\n"}, spans: []string{"Map<int, Option<Task<int>>>"}},
		{probe: taskCheckProbe{name: "array_value", digest: "77d846a18cdea399b9122fa99cf9be31f2eb0010f497e0beb9ff2a11f6b8f7c2",
			text: "fn f(m: Map<int, Task<int>[]>) -> int {\n    return 0;\n}\n"}, spans: []string{"Map<int, Task<int>[]>"}},
		{probe: taskCheckProbe{name: "tuple_value", digest: "d45d5d4905d66919b05fae038118e5cc11d2b550e901b549b3edf4afe68176ca",
			text: "fn f(m: Map<int, (int, Task<int>)>) -> int {\n    return 0;\n}\n"}, spans: []string{"Map<int, (int, Task<int>)>"}},
		{probe: taskCheckProbe{name: "record_value", digest: "c60a8ae72a867f3ed83af10848557ac12f9de8460e1a9a3ba8431d151b2dfc50",
			text: "type Holder = { t: Task<int> }\n\nfn f(m: Map<string, Holder>) -> int {\n    return 0;\n}\n"}, spans: []string{"Map<string, Holder>"}},
		{probe: taskCheckProbe{name: "task_key", digest: "e5a43cc5c2158bc83e5d61cf2a9c7849bef5b1165bddfe617d41fa9fa728462e",
			text: "fn f(m: Map<Task<int>, int>) -> int {\n    return 0;\n}\n"}, spans: []string{"Map<Task<int>, int>"}},
		{probe: taskCheckProbe{name: "task_key_keeps_the_map_type", digest: "970ff5a5ec7a00e28cfd94839a3f1f5590fcd30a148beab3a7ad0d21f7b041c4",
			text: "async fn work(n: int) -> int {\n    return n * 2;\n}\n\nfn f() -> uint {\n    let mut m = Map::<Task<int>, int>::new();\n    m.insert(work(1), 1);\n    return m.length();\n}\n"}, spans: []string{"Map::<Task<int>, int>::new()"}},
		{probe: taskCheckProbe{name: "nested_map_reports_the_inner_once", digest: "299d199487fb7a87c89e1162874ee4fb7c7ef1b866a2672d709bea7c997f0ae0",
			text: "fn f(m: Map<int, Map<int, Task<int>>>) -> int {\n    return 0;\n}\n"}, spans: []string{"Map<int, Task<int>>"}},
		{probe: taskCheckProbe{name: "nested_literal_reports_the_inner_once", digest: "6f41135f1547fb0fee29bd9776c9103876fb047dcc30e0789a359b13213d3fa4",
			text: "async fn work(n: int) -> int {\n    return n * 2;\n}\n\nfn f() -> uint {\n    let m = { 1 => { 2 => work(1) } };\n    return m.length();\n}\n"}, spans: []string{"{ 2 => work(1) }"}},
		{probe: taskCheckProbe{name: "generic_fn_call", digest: "a3f355140ac5050dc8e82373af591c8f75ef9b83af5a9814732399eff57038f3",
			text: "async fn work(n: int) -> int {\n    return n * 2;\n}\n\nfn wrap<T>(x: T) -> Map<int, T> {\n    let mut m = Map::<int, T>::new();\n    m.insert(1, x);\n    return m;\n}\n\nfn f() -> uint {\n    let m = wrap(work(1));\n    return m.length();\n}\n"}, spans: []string{"wrap(work(1))"}},
		{probe: taskCheckProbe{name: "generic_fn_call_reports_every_site", digest: "513f56aec5b1550e5dd928516038ffa270a9cf75ab2882796cf9ad96bc042552",
			text: "async fn work(n: int) -> int {\n    return n * 2;\n}\n\nfn wrap<T>(x: T) -> Map<int, T> {\n    let mut m = Map::<int, T>::new();\n    m.insert(1, x);\n    return m;\n}\n\nfn f() -> uint {\n    let a = wrap(work(1));\n    let b = wrap(work(2));\n    return a.length() + b.length();\n}\n"}, spans: []string{"wrap(work(1))", "wrap(work(2))"}},
		{probe: taskCheckProbe{name: "generic_fn_turbofish", digest: "bde604376d62ecfce39cf2d81290edcf6c9b37600e26ef91ce6b10b7db6752de",
			text: "fn make<T>() -> Map<int, T> {\n    return Map::<int, T>::new();\n}\n\nfn f() -> uint {\n    let m = make::<Task<int>>();\n    return m.length();\n}\n"}, spans: []string{"make::<Task<int>>()"}},
		{probe: taskCheckProbe{name: "generic_struct", digest: "bb463ffc61750af9b95ea011ae230dc64014f9c8bbbb3df8934d3c6c6d8c7db0",
			text: "type Box<T> = { m: Map<int, T> }\n\nfn f(b: Box<Task<int>>) -> int {\n    return 0;\n}\n"}, spans: []string{"Box<Task<int>>"}},
		{probe: taskCheckProbe{name: "generic_struct_reports_the_inner_once", digest: "ba2919925c307b128036dbb6ee0dfe3ce55cab9894f406039708ed79860572fe",
			text: "type Box<T> = { m: Map<int, T> }\n\nfn f(b: Box<Box<Task<int>>>) -> int {\n    return 0;\n}\n"}, spans: []string{"Box<Task<int>>"}},
		{probe: taskCheckProbe{name: "generic_struct_literal", digest: "2504200fef1cb5a5781b00df7ee7845c0c58f7c6a0652e2a57c033a378191a3b",
			text: "async fn work(n: int) -> int {\n    return n * 2;\n}\n\ntype P<T> = { x: T, m: Map<int, T> }\n\nfn f() -> int {\n    let p = P { x: work(1), m: Map::new() };\n    return 0;\n}\n"}, spans: []string{"P { x: work(1), m: Map::new() }"}, also: diag.SemaTaskNotAwaited},
		{probe: taskCheckProbe{name: "static_turbofish", digest: "c6e11016ff25955bcb892506c31ebe8db72e89f6d298ce295e29c5fb97691c88",
			text: "type Box<T> = { m: Map<int, T> }\n\nextern<Box<T>> {\n    fn zero() -> int {\n        return 0;\n    }\n}\n\nfn f() -> int {\n    return Box::<Task<int>>::zero();\n}\n"}, spans: []string{"Box::<Task<int>>::zero()"}},
		{probe: taskCheckProbe{name: "static_turbofish_reports_every_site", digest: "7d97c05a114f9f8c130f8eed428fbcf1255f91779042dfaab87204e65573d37a",
			text: "type Box<T> = { m: Map<int, T> }\n\nextern<Box<T>> {\n    fn zero() -> int {\n        return 0;\n    }\n}\n\nfn f() -> int {\n    let a = Box::<Task<int>>::zero();\n    let b = Box::<Task<int>>::zero();\n    return a + b;\n}\n"}, spans: []string{"Box::<Task<int>>::zero()", "Box::<Task<int>>::zero()"}},
		{probe: taskCheckProbe{name: "generic_alias", digest: "dc9eeca6b178d90580f1fccf97cba85fc43e40c6d8afc6647fb4a3c1aeb97eac",
			text: "type TM<T> = Map<int, T>;\n\nfn f(m: TM<Task<int>>) -> int {\n    return 0;\n}\n"}, spans: []string{"TM<Task<int>>"}},
		{probe: taskCheckProbe{name: "alias_reports_its_declaration_once", digest: "3e9e2ae83bee9d79eb91b197a93c7235e50d32dfa09203c706baf5e21f04ab9a",
			text: "type TM = Map<int, Task<int>>;\n\nfn f(m: TM) -> int {\n    return 0;\n}\n\nfn g(m: TM) -> int {\n    return 0;\n}\n"}, spans: []string{"Map<int, Task<int>>"}},
		{probe: taskCheckProbe{name: "control_map_of_int", digest: "33bf968ccbac3021bab4b400d010eb8bcd6d06c94ff58e8873100200ef3bd1b5",
			text: "fn f() -> uint {\n    let mut m: Map<int, int> = Map::<int, int>::new();\n    m.insert(1, 2);\n    let l = { 1 => 2 };\n    return m.length() + l.length();\n}\n"}, spans: nil},
		{probe: taskCheckProbe{name: "control_array_of_tasks_drained", digest: "733359b6d1fef80e3da163615c0293e27ba7c8dfc1e2b6239d6979ea8b9c1d9e",
			text: "async fn work(n: int) -> int {\n    return n * 2;\n}\n\nasync fn f() -> int {\n    let mut q: Task<int>[] = [];\n    q.push(spawn work(1));\n    q.push(spawn work(2));\n    while q.__len() != 0:uint {\n        let t = q.pop().safe();\n        let _ = t.await();\n    }\n    return 0;\n}\n"}, spans: nil},
		{probe: taskCheckProbe{name: "control_map_of_results", digest: "098c1ee9e9bfe4983d973cd65e2325ac5a8355c9af6e3f4fd41c49a4d54e61af",
			text: "async fn work(n: int) -> int {\n    return n * 2;\n}\n\nasync fn f() -> uint {\n    let mut m: Map<int, int> = Map::<int, int>::new();\n    compare work(1).await() {\n        Success(v) => { m.insert(1, v); }\n        finally => {}\n    };\n    return m.length();\n}\n"}, spans: nil},
		{probe: taskCheckProbe{name: "control_generic_fn_of_int", digest: "6e6dca4adb9a166107101c4d0b6218c31fa3d9f3f4ea0329a2fe1785ebdfd4e0",
			text: "fn wrap<T>(x: T) -> Map<int, T> {\n    let mut m = Map::<int, T>::new();\n    m.insert(1, x);\n    return m;\n}\n\nfn f() -> uint {\n    let m = wrap(1);\n    return m.length();\n}\n"}, spans: nil},
		{probe: taskCheckProbe{name: "control_generic_static_of_int", digest: "a72432e173fd1954740114a18f24fb0f475a63ca021c7ea16d1fad2ebd83ec74",
			text: "type Box<T> = { m: Map<int, T> }\n\nextern<Box<T>> {\n    fn zero() -> int {\n        return 0;\n    }\n}\n\nfn f() -> int {\n    return Box::<int>::zero();\n}\n"}, spans: nil},
		{probe: taskCheckProbe{name: "control_channel_of_tasks", digest: "230ac03056318f20b8ffc5e219eca61d9cc3da8c166604a56910624bcf1bc3a6",
			text: "fn f(m: Map<int, Channel<Task<int>>>) -> int {\n    return 0;\n}\n"}, spans: nil},
		{probe: taskCheckProbe{name: "control_far_task", digest: "1d9192a01eb204df3c4699b5eed30bbf177e5f9e2d21278b1a753dd1eeef378d",
			text: "fn f(m: Map<int, far Task<int>>) -> int {\n    return 0;\n}\n"}, spans: nil},
		{probe: taskCheckProbe{name: "control_reference_to_task", digest: "46ff11bde9917e47be40abc66a99c086abc3db3e5d76571250cccacd629fa060",
			text: "fn f(m: Map<int, &Task<int>>) -> int {\n    return 0;\n}\n"}, spans: nil},
		{probe: taskCheckProbe{name: "residual_generic_body", digest: "4838ff47596c1958e0c209aed0750e2fef0a90645e7a01e169526194436841d0",
			text: "async fn work(n: int) -> int {\n    return n * 2;\n}\n\nfn keep<T>(x: T) -> uint {\n    let mut m = Map::<int, T>::new();\n    m.insert(1, x);\n    return m.length();\n}\n\nfn f() -> uint {\n    return keep(work(1));\n}\n"}, spans: nil},
	}
}

func TestTaskInMap(t *testing.T) {
	for _, row := range taskInMapRows() {
		t.Run(row.probe.name, func(t *testing.T) {
			_, errs := taskCheckErrorCodes(t, row.probe)
			requireTaskInMapSpans(t, row.probe.text, errs, row.spans, row.also)
		})
	}
}

// requireTaskInMapSpans: SEM3225 at exactly spans, each with its help line, and no other error
// but also; with no spans, no error at all.
func requireTaskInMapSpans(t *testing.T, text string, errs []*diag.Diagnostic, spans []string, also diag.Code) {
	t.Helper()
	var got []string
	for _, d := range errs {
		if d.Code != semaTaskInMap {
			if spans == nil || d.Code != also {
				t.Fatalf("error %s besides SEM3225: %+v", d.Code.ID(), *d)
			}
			continue
		}
		got = append(got, text[d.Primary.Start:d.Primary.End])
		if len(d.Help) != 1 || d.Help[0].Msg == "" {
			t.Fatalf("SEM3225 without its help line: %+v", d.Help)
		}
	}
	if !slices.Equal(got, spans) {
		t.Fatalf("SEM3225 at %q, want exactly %q", got, spans)
	}
}

// An imported generic nominal named by a static call is refused at the call, however many
// times it is named. imported_annotation_unsubstituted pins today's behaviour of an imported
// generic nominal written as a type: its field types stay unsubstituted (inserting an int into
// an imported `P<int>`'s `Map<int, T>` field reports `expected T, got int`), and the rule does
// not see it. Re-check this row when that substitution is fixed.
const taskInMapImportedLib = `pub type Box<T> = { m: Map<int, T> }

extern<Box<T>> {
    pub fn zero() -> int {
        return 0;
    }
}
`

func TestTaskInMapImported(t *testing.T) {
	for _, row := range []struct {
		name, text string
		spans      []string
	}{
		{name: "imported_static_turbofish", spans: []string{"Box::<Task<int>>::zero()", "Box::<Task<int>>::zero()"},
			text: "import ./lib::*;\n\nfn f() -> int {\n    let a = Box::<Task<int>>::zero();\n    let b = Box::<Task<int>>::zero();\n    return a + b;\n}\n"},
		{name: "imported_static_of_int", spans: nil,
			text: "import ./lib::*;\n\nfn f() -> int {\n    return Box::<int>::zero();\n}\n"},
		{name: "imported_annotation_unsubstituted", spans: nil,
			text: "import ./lib::*;\n\nfn f(b: Box<Task<int>>) -> int {\n    return 0;\n}\n"},
	} {
		t.Run(row.name, func(t *testing.T) {
			requireTaskInMapSpans(t, row.text, taskInMapImportedErrors(t, row.text), row.spans, 0)
		})
	}
}

// taskInMapImportedErrors diagnoses main beside taskInMapImportedLib and answers its errors.
func taskInMapImportedErrors(t *testing.T, main string) []*diag.Diagnostic {
	t.Helper()
	t.Setenv("SURGE_STDLIB", repoRootFromDriverTest(t))
	dir := t.TempDir()
	for name, text := range map[string]string{"lib.sg": taskInMapImportedLib, "main.sg": main} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	opts := DiagnoseOptions{Stage: DiagnoseStageSema, BaseDir: dir, MaxDiagnostics: 64, IgnoreWarnings: true}
	res, err := DiagnoseWithOptions(t.Context(), filepath.Join(dir, "main.sg"), &opts)
	var unfinished *returnOriginUnfinishedError
	if err != nil && !errors.As(err, &unfinished) {
		t.Fatalf("PRECONDITION: the probe did not reach the checker: %v", err)
	}
	if res == nil || res.Bag == nil {
		if unfinished == nil {
			t.Fatal("PRECONDITION: no diagnostics bag")
		}
		return nil // sema accepted it; return origins left rows that are not this rule's question
	}
	var errs []*diag.Diagnostic
	for _, d := range res.Bag.Items() {
		if d.Severity < diag.SevError {
			continue
		}
		if d.Primary.File != res.File.ID {
			t.Fatalf("PRECONDITION: error outside the root file: %+v", *d)
		}
		errs = append(errs, d)
	}
	return errs
}

// The map literal's task row in return origins ("may hold a task", return_origin_map_literal.go)
// stays as defence in depth behind SEM3225: the program it was written for is now refused by
// SEM3225 alone, and the analysis run on it with SEM3225 tolerated still leaves exactly that row.
const taskInMapLiteralFenceSource = `async fn work(v: int) -> int {
    return v + 1;
}

fn task_values() -> uint {
    let m = { 1 => work(1), 2 => work(2) };
    return m.length();
}
`

const taskInMapLiteralFenceDigest = "f15b32793f0f304d6ad5410aa1eac9af55539d3bc45b7b576d2bf1fbf9eeb1e5"

func TestTaskInMapFenceKeepsTheMapLiteralTaskRow(t *testing.T) {
	text := taskInMapLiteralFenceSource
	fn := tupleFn(t, text, "fn task_values(")
	literal := tupleIn(t, text, fn, "{ 1 => work(1), 2 => work(2) }")
	checkOriginSource(t, text, taskInMapLiteralFenceDigest, fn, literal)
	f, analysis := analyzeOriginRootUnderRule(t, "task_in_map_literal_fence", text, semaTaskInMap)
	originExactPending(t, analysis, f.unit.SourceKey, fn, []originRefusal{{span: literal, reason: originMapLiteralTask}})
	originNoEscape(t, analysis, f.owner.File.ID, fn)
}
