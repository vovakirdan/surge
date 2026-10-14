package driver

import (
	"strings"
	"testing"

	"surge/internal/diag"
)

const taskStatementTemporaryRuntimeSource = `async fn worker(s: &string) -> int {
    checkpoint().await();
    return len(s) to int;
}
fn make(t: string) -> Task<int> {
    return worker("abc" + t);
}
@entrypoint
fn main() -> int {
    let task = make("x");
    return compare task.await() {
        Success(n) => n;
        Cancelled() => 99;
    };
}
`

const taskStatementTemporaryBoundControl = `async fn worker(s: &string) -> int {
    checkpoint().await();
    return len(s) to int;
}
async fn f(t: string) -> int {
    let tmp = "abc" + t;
    return compare worker(tmp).await() {
        Success(n) => n;
        Cancelled() => 0;
    };
}
`

const taskStatementTemporaryExplicitReturn = `async fn worker(s: &string) -> int { return len(s) to int; }
fn f() -> Task<int> { return worker(&"g"); }
`

const taskStatementTemporaryImmediateAwait = `async fn worker(s: &string) -> int { return len(s) to int; }
async fn f() -> nothing { let _ = worker(&"h").await(); return nothing; }
`

const taskStatementTemporarySpawn = `async fn worker(s: &string) -> int { return len(s) to int; }
async fn f() -> nothing { let t = spawn worker(&"i"); let _ = t.await(); return nothing; }
`

const taskStatementTemporaryNestedAsync = `async fn worker(s: &string) -> int { return len(s) to int; }
async fn fire(s: &string) -> nothing { let _ = worker(s).await(); return nothing; }
async fn f(t: string) -> nothing { let _ = fire("q" + t).await(); return nothing; }
`

func TestTaskCheckRefusesStatementTemporaryTaskBorrow(t *testing.T) {
	for _, row := range []struct {
		probe         taskCheckProbe
		primary, note string
	}{
		{taskCheckProbe{name: "runtime_uaf", digest: "96869f0dc7540672aea77b87fc5e8076c8000e73433538aea68bfb2a9d725c7e", text: taskStatementTemporaryRuntimeSource}, `"abc" + t`, `"abc" + t`},
		{taskCheckProbe{name: "explicit_return", digest: "e1b01b4e0931c00b35e95c10436e5661e3a9522f62bf718928c584fbe7b32f59", text: taskStatementTemporaryExplicitReturn}, `&"g"`, `"g"`},
		{taskCheckProbe{name: "immediate_await", digest: "91238afea40faf0de252c5739eea658c0fdec8524314147d58f9457d00b43a07", text: taskStatementTemporaryImmediateAwait}, `&"h"`, `"h"`},
		{taskCheckProbe{name: "spawn", digest: "9a4c9dbbbac22372ad7611265a2e9e54c781080024cd19ceae76337c2a7214e5", text: taskStatementTemporarySpawn}, `&"i"`, `"i"`},
		{taskCheckProbe{name: "spawn_return", digest: originSpawnTemporarySourceDigest, text: originSpawnTemporarySource}, `"a" + b`, `"a" + b`},
		{taskCheckProbe{name: "spawn_implicit_join", digest: originSpawnImplicitJoinTemporarySourceDigest, text: originSpawnImplicitJoinTemporarySource}, `"a" + b`, `"a" + b`},
		{taskCheckProbe{name: "nested_async", digest: "7a60bdc88e61c37d91e299716fcf167e129139f1a8ffcfb592538dd35979fde4", text: taskStatementTemporaryNestedAsync}, `"q" + t`, `"q" + t`},
		{taskCheckProbe{name: "spawn_on_body", digest: spawnOnR09BodyStringTempSourceDigest, text: spawnOnR09BodyStringTempSource}, `"abc"`, `"abc"`},
		{taskCheckProbe{name: "select_head", digest: originSelectTemporarySourceDigest, text: originSelectTemporarySource}, `"a" + b`, `"a" + b`},
	} {
		t.Run(row.probe.name, func(t *testing.T) {
			assertTaskStatementTemporaryRefusal(t, row.probe, row.primary, row.note)
		})
	}
}

func assertTaskStatementTemporaryRefusal(t *testing.T, probe taskCheckProbe, primary, note string) {
	t.Helper()
	codes, errs := taskCheckErrorCodes(t, probe)
	wantCodes := diag.SemaBorrowNonAddressable.ID()
	if codes != wantCodes {
		t.Fatalf("errors %q, want exactly %s: %+v", codes, wantCodes, errs)
	}
	var d *diag.Diagnostic
	for _, candidate := range errs {
		if candidate.Code == diag.SemaBorrowNonAddressable {
			d = candidate
			break
		}
	}
	if d == nil {
		t.Fatalf("missing %s in %+v", diag.SemaBorrowNonAddressable.ID(), errs)
	}
	if got := probe.text[d.Primary.Start:d.Primary.End]; got != primary {
		t.Fatalf("primary span is %q, want %q", got, primary)
	}
	hasHelp := false
	for _, help := range d.Help {
		hasHelp = hasHelp || strings.Contains(help.Msg, "bind the value first")
	}
	if d.Message != "a task cannot borrow a temporary value; bind it to a variable first" ||
		len(d.Notes) != 1 || probe.text[d.Notes[0].Span.Start:d.Notes[0].Span.End] != note ||
		!hasHelp {
		t.Fatalf("refusal lost its task-temporary explanation: %+v", d)
	}
}

func TestTaskCheckAllowsBoundTemporaryUntilAwait(t *testing.T) {
	probe := taskCheckProbe{
		name:   "task_statement_temporary_bound_control",
		digest: "d09d9a7412886f04123b5804bc93f5c6e52537b0e2cdd99531d3f44abfa1ee11",
		text:   taskStatementTemporaryBoundControl,
	}
	if codes, errs := taskCheckErrorCodes(t, probe); codes != "" || len(errs) != 0 {
		t.Fatalf("bound temporary control was refused: %q %+v", codes, errs)
	}
}
