package driver

import (
	"slices"
	"testing"

	"surge/internal/diag"
)

// A task's result may not contain a task (SEM3223, owner ruling 2026-09-26): directly, or
// through an Option, a tuple, an array, a map, a struct field or a generic argument. Each
// program is a ROOT program over the real core, so Task, Option and Channel are the core's
// own declarations. A refused row names every span that must carry the error, exactly once;
// a control row carries none. Channel<Task<T>> is not a task result and stays accepted.
type taskPayloadIsTaskRow struct {
	probe taskCheckProbe
	spans []string
}

func taskPayloadIsTaskRows() []taskPayloadIsTaskRow {
	return []taskPayloadIsTaskRow{
		{probe: taskCheckProbe{name: "explicit_type", digest: "78fc49b6337fb1f3f2219bafd662972d064969559b583d3fa9cf2ed5cfe31431",
			text: "fn f(t: Task<Task<int>>) -> int {\n    return 0;\n}\n"}, spans: []string{"Task<Task<int>>"}},
		{probe: taskCheckProbe{name: "async_fn_result", digest: "60c1b48f1fcaf4e7fc3580ee8faf2a6f387be571d34b370d74e693f94ca9f839",
			text: "async fn f() -> Task<int> {\n    return async { ret 7; };\n}\n"}, spans: []string{"-> Task<int>"}},
		{probe: taskCheckProbe{name: "async_block_payload", digest: "16160cc8b492feed905839ec065d500d8ed9ad7db915b4d9c983a61cf40522a5",
			text: "fn f() -> int {\n    let t = async { ret 7; };\n    let outer = async {\n        ret t;\n    };\n    let _ = outer;\n    return 0;\n}\n"}, spans: []string{"async {\n        ret t;\n    }"}},
		{probe: taskCheckProbe{name: "generic_fn_call", digest: "99801532603b690c82c7852a75c518a8a2af97178146c72841414d5b5b3a53f7",
			text: "fn g<T>(x: T) -> Task<T> {\n    return async { ret x; };\n}\n\nfn use_g() -> int {\n    let inner = async { ret 1; };\n    let outer = g(inner);\n    let _ = outer;\n    return 0;\n}\n"}, spans: []string{"g(inner)"}},
		{probe: taskCheckProbe{name: "generic_async_fn_call", digest: "1665b226f669cfce19d8d0c25975d84b3394c713dd36d60930a5ad2d72dba6f7",
			text: "async fn g<T>(x: T) -> T {\n    return x;\n}\n\nfn use_g() -> int {\n    let inner = async { ret 1; };\n    let outer = g(inner);\n    let _ = outer;\n    return 0;\n}\n"}, spans: []string{"g(inner)"}},
		{probe: taskCheckProbe{name: "option", digest: "ff8d7113746e1f060c6adb56c2a6a2113b1aa1dd7f57deda39750ab71773a9f6",
			text: "fn f(t: Task<Option<Task<int>>>) -> int {\n    return 0;\n}\n"}, spans: []string{"Task<Option<Task<int>>>"}},
		{probe: taskCheckProbe{name: "tuple", digest: "c7a5015f5f68e68ec6222a890202dbb893f58a7f2cf138b7541f2f16c0972009",
			text: "fn f(t: Task<(int, Task<int>)>) -> int {\n    return 0;\n}\n"}, spans: []string{"Task<(int, Task<int>)>"}},
		{probe: taskCheckProbe{name: "array", digest: "32779a23454968575365f34bca5178a8947fd4e28cab7f30626f1700a6f41758",
			text: "fn f(t: Task<Task<int>[]>) -> int {\n    return 0;\n}\n"}, spans: []string{"Task<Task<int>[]>"}},
		{probe: taskCheckProbe{name: "map", digest: "bf25a6adb0980d01d829be6bad0efad66d5b17046b21803a8f983024d96d2cf9",
			text: "fn f(t: Task<Map<string, Task<int>>>) -> int {\n    return 0;\n}\n"}, spans: []string{"Task<Map<string, Task<int>>>"}},
		{probe: taskCheckProbe{name: "struct_field", digest: "fb29a0440b05cde5b6e332e9d89a55a9c7881687b748da43c452b465779eb6c8",
			text: "type Box = { child: Task<int> }\n\nfn f(t: Task<Box>) -> int {\n    return 0;\n}\n"}, spans: []string{"Task<Box>"}},
		{probe: taskCheckProbe{name: "generic_struct_argument", digest: "93edc2544d693f3b35312360b28667a33c8914494cc33f0b4d28419518e588d2",
			text: "type Box<T> = { child: T }\n\nfn f(t: Task<Box<Task<int>>>) -> int {\n    return 0;\n}\n"}, spans: []string{"Task<Box<Task<int>>>"}},
		{probe: taskCheckProbe{name: "generic_struct_field", digest: "eef3284bc5bf9b9abb78440386b2a21de48581b1ad3784ae667677099f54990a",
			text: "type W<T> = { t: Task<T> }\n\nfn f(w: W<Task<int>>) -> int {\n    return 0;\n}\n"}, spans: []string{"W<Task<int>>"}},
		{probe: taskCheckProbe{name: "alias", digest: "275bb81364d92a4decc4b031114e2601db97326ae26d5bd1e0caf2757e60e67a",
			text: "type Inner = Task<int>;\n\nfn f(t: Task<Inner>) -> int {\n    return 0;\n}\n"}, spans: []string{"Task<Inner>"}},
		{probe: taskCheckProbe{name: "triple_reports_the_inner_once", digest: "f1095244809453d5e412b4336d83713c1aa3b1bd4783aef117c0a94370629008",
			text: "fn f(t: Task<Task<Task<int>>>) -> int {\n    return 0;\n}\n"}, spans: []string{"Task<Task<int>>"}},
		{probe: taskCheckProbe{name: "control_channel_of_tasks", digest: "b4c7ae2a9281db0303986d07536861d5d696110e23f5668384be86f81b35e7d3",
			text: "fn f(ch: Channel<Task<int>>) -> int {\n    return 0;\n}\n"}, spans: nil},
		{probe: taskCheckProbe{name: "control_task_of_int", digest: "219aa6c53ddc36932d8f30971decf440661db61b98ea7f1b30448c564689e569",
			text: "fn f(t: Task<int>) -> int {\n    return 0;\n}\n"}, spans: nil},
		{probe: taskCheckProbe{name: "control_generic_int", digest: "84970847c988a34fae6d0d2b55d97349214411ac8517c632b8f0191fc09c5375",
			text: "fn g<T>(x: T) -> Task<T> {\n    return async { ret x; };\n}\n\nfn use_g() -> int {\n    let outer = g(1);\n    let _ = outer;\n    return 0;\n}\n"}, spans: nil},
		{probe: taskCheckProbe{name: "control_struct_of_int", digest: "ba71d8a9a7d3bd4c078cd2f678c52121334898462dcf5858674e59a8d7e25cd1",
			text: "type Box = { value: int }\n\nfn f(t: Task<Box>) -> int {\n    return 0;\n}\n"}, spans: nil},
	}
}

func TestTaskPayloadIsTask(t *testing.T) {
	for _, row := range taskPayloadIsTaskRows() {
		t.Run(row.probe.name, func(t *testing.T) {
			_, errs := taskCheckErrorCodes(t, row.probe)
			var got []string
			for _, d := range errs {
				if d.Code != diag.SemaTaskPayloadIsTask {
					continue
				}
				got = append(got, row.probe.text[d.Primary.Start:d.Primary.End])
			}
			if row.spans == nil && len(errs) != 0 {
				t.Fatalf("control has errors %+v: it must check cleanly for the row to mean anything", errs)
			}
			if !slices.Equal(got, row.spans) {
				t.Fatalf("SEM3223 at %q, want exactly %q", got, row.spans)
			}
		})
	}
}
