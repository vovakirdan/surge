package driver

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"surge/internal/diag"
)

func asyncChannelCapturePrograms() []struct{ name, source string } {
	rows := make([]struct{ name, source string }, 0, 10)
	for _, value := range []struct{ name, typ, expr, send string }{
		{"int", "int", "7", "value"},
		{"int64", "int64", "7:int64", "value"},
		{"float", "float", "1.5", "value"},
		{"string", "string", `"hello"`, "own value"},
	} {
		rows = append(rows, struct{ name, source string }{value.name, fmt.Sprintf(`@entrypoint
fn main() -> int {
    let ch = Channel::<%s>::new(1:uint);
    let child = async { let value = %s; ch.send(%s); ret 0; };
    compare child.await() {
        Success(_) => {}
        Cancelled() => { return 1; }
    };
    let result = ch.recv();
    compare result {
        Some(v) => print(v to string);
        nothing => { return 2; }
    };
    return 0;
}
`, value.typ, value.expr, value.send)})
	}
	rows = append(rows, struct{ name, source string }{"alias_mixed", `type Pipe = Channel<int64>;
@entrypoint
fn main() -> int {
    let ch: Pipe = Channel::<int64>::new(1:uint);
    let value: int64 = 7:int64;
    let child = async { ch.send(value); ret 0; };
    compare child.await() { Success(_) => {} Cancelled() => { return 1; } };
    compare ch.recv() { Some(v) => print(v to string); nothing => { return 2; } };
    return 0;
}
`})
	rows = append(rows, struct{ name, source string }{"same_handle_twice", `@entrypoint
fn main() -> int {
    let ch = Channel::<int64>::new(2:uint);
    let alias = ch;
    let child = async { ch.send(7:int64); alias.send(8:int64); ret 0; };
    compare child.await() { Success(_) => {} Cancelled() => { return 1; } };
    compare ch.recv() { Some(v) => print(v to string); nothing => { return 2; } };
    compare alias.recv() { Some(v) => print(v to string); nothing => { return 3; } };
    return 0;
}
`})
	rows = append(rows, struct{ name, source string }{"nested_async", `@entrypoint
fn main() -> int {
    let ch = Channel::<int64>::new(1:uint);
    let outer = async {
        let inner = async { ch.send(7:int64); ret 0; };
        compare inner.await() { Success(_) => {} Cancelled() => { ret 1; } };
        ret 0;
    };
    compare outer.await() { Success(_) => {} Cancelled() => { return 1; } };
    compare ch.recv() { Some(v) => print(v to string); nothing => { return 2; } };
    return 0;
}
`})
	rows = append(rows, struct{ name, source string }{"captured_heap_int", `@entrypoint
fn main() -> int {
    let ch = Channel::<int>::new(1:uint);
    let large: int = 9223372036854775808;
    let child = async { ch.send(large); ret 0; };
    compare child.await() { Success(_) => {} Cancelled() => { return 1; } };
    compare ch.recv() { Some(v) => print(v to string); nothing => { return 2; } };
    return 0;
}
`})
	for _, scalar := range []struct{ name, typ, expr string }{
		{"captured_float", "float", "1.5"},
		{"captured_heap_uint", "uint", "18446744073709551616:uint"},
	} {
		rows = append(rows, struct{ name, source string }{scalar.name, fmt.Sprintf(`@entrypoint
fn main() -> int {
    let ch = Channel::<%s>::new(1:uint);
    let value: %s = %s;
    let child = async { ch.send(value); ret 0; };
    compare child.await() { Success(_) => {} Cancelled() => { return 1; } };
    compare ch.recv() { Some(v) => print(v to string); nothing => { return 2; } };
    return 0;
}
`, scalar.typ, scalar.typ, scalar.expr)})
	}
	return rows
}

func TestAsyncChannelCaptureHasIndependentOrigin(t *testing.T) {
	for _, row := range asyncChannelCapturePrograms() {
		t.Run(row.name, func(t *testing.T) {
			t.Logf("source_sha256=%x", sha256.Sum256([]byte(row.source)))
			f, analysis := analyzeOriginRoot(t, "async_channel_"+row.name, row.source, false, nil)
			span := originSpan{0, len(row.source), row.source}
			originExactPending(t, analysis, f.unit.SourceKey, span, nil)
			originNoEscape(t, analysis, f.owner.File.ID, span)
			requireOriginSummary(t, analysis, f.owner.File.ID, "main", false, nil)
		})
	}
}

func TestAsyncChannelCaptureKeepsOriginFences(t *testing.T) {
	for _, row := range []struct{ name, typ, block, body, refusal string }{
		{"borrowed_handle", "&Channel<int64>", "async", "let _ = ch; ret 0;", originTaskBlockCaptureRefusal},
		{"array_payload", "Channel<int[]>", "async", "let _ = ch; ret 0;", originTaskBlockCaptureRefusal},
		{"task_payload", "Channel<Task<int>>", "async", "let _ = ch; ret 0;", originTaskBlockCaptureRefusal},
		{"channel_payload", "Channel<Channel<int>>", "async", "let _ = ch; ret 0;", originTaskBlockCaptureRefusal},
		{"blocking_handle", "Channel<int64>", "blocking", "ch.send(1:int64); ret 0;", originTaskBlockCaptureRefusal},
		{"result_origin", "Channel<int64>", "async", "ch.send(1:int64); let values: int[] = [1, 2]; ret values;", originTaskBlockPayloadRefusal},
	} {
		t.Run(row.name, func(t *testing.T) {
			result := "int"
			if row.name == "result_origin" {
				result = "int[]"
			}
			src := fmt.Sprintf("fn tag_len(label: &string) -> int { return 1; }\nfn subject(ch: %s) -> Task<%s> { return %s { %s }; }\n", row.typ, result, row.block, row.body)
			t.Logf("source_sha256=%x", sha256.Sum256([]byte(src)))
			f, analysis := analyzeOriginRoot(t, "async_channel_fence_"+row.name, src, false, nil)
			start := strings.Index(src, row.block+" {")
			found := false
			for _, pending := range originPendingWithin(analysis, f.unit.SourceKey, start, len(src)) {
				if pending.Reason == row.refusal {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing %q inside task block", row.refusal)
			}
		})
	}
}

func TestAsyncChannelCaptureRejectsNonCanonicalOwners(t *testing.T) {
	for _, row := range []struct{ name, prefix, typ string }{
		{"wrapped_handle", "type Holder = { ch: Channel<int64>, };", "Holder"},
		{"mutable_handle", "", "&mut Channel<int64>"},
	} {
		t.Run(row.name, func(t *testing.T) {
			src := row.prefix + "\nfn subject(ch: " + row.typ + ") -> Task<int> { return async { let _ = ch; ret 0; }; }\n"
			f, analysis := analyzeOriginRoot(t, "async_channel_owner_"+row.name, src, false, nil)
			start := strings.Index(src, "async {")
			found := false
			for _, pending := range originPendingWithin(analysis, f.unit.SourceKey, start, len(src)) {
				if pending.Reason == originTaskBlockCaptureRefusal {
					found = true
				}
			}
			if !found {
				t.Fatal("capture must retain its exact origin refusal")
			}
		})
	}
}

// These payloads are rejected before return-origin analysis can certify a capture.
func TestAsyncChannelCaptureRejectsInvalidPayloadTypes(t *testing.T) {
	for _, row := range []struct{ name, payload, code string }{
		{"reference", "&int", "SEM3138"},
		{"wrapped_reference", "Option<&int>", "SEM3138"},
		{"pointer", "*int", "SEM3129"},
	} {
		t.Run(row.name, func(t *testing.T) {
			src := fmt.Sprintf("fn subject(ch: Channel<%s>) -> Task<int> { return async { let _ = ch; ret 0; }; }\n", row.payload)
			result := returnOriginStdlibFixtureAllowing(t, src, false, func(d *diag.Diagnostic) bool { return d.Code.ID() == row.code })
			found := 0
			for _, d := range result.Bag.Items() {
				if d.Code.ID() == row.code {
					found++
				}
			}
			if found != 1 {
				t.Fatalf("%s diagnostics=%d, want 1", row.code, found)
			}
		})
	}
}
