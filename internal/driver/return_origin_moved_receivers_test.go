package driver

import (
	"testing"
)

// A `&T` formal accepts an `own` binding through the same admitted borrow as any
// other place, and a non-generic alias on the actual is matched through its
// target. `.await()` keeps its refusal: the moved-receiver rule waits for the
// task-borrow model, so these two leaves also pin that G1 has not landed.
const movedReceiverSource = `type Ints = int[];
async fn one() -> int {
    return 1;
}
async fn await_call() -> int {
    let r: TaskResult<int> = one().await();
    return 0;
}
async fn await_ident() -> int {
    let t: Task<int> = one();
    let r: TaskResult<int> = t.await();
    return 0;
}
fn channel_own() -> nothing {
    let ch: own Channel<int> = Channel::<int>::new(1:uint);
    ch.send(1);
    let v: int? = ch.recv();
    ch.close();
    return nothing;
}
fn alias_receiver() -> nothing {
    let mut xs: Ints = [];
    xs.push(1);
    return nothing;
}
`

const movedReceiverDigest = "3a9c69f371113c9c5220a30c74a2a765f2bd79689c8bae2197d406b95c7066d6"

const originSubstitutedRefusal = "generic original call argument disagrees with its substituted source signature"

func TestAnalyzeMovedReceivers(t *testing.T) {
	checkOriginSource(t, movedReceiverSource, movedReceiverDigest)
	f, analysis := analyzeOriginRoot(t, "moved_receivers", movedReceiverSource, false, nil)
	checkOriginBodyLeaves(t, analysis, f, movedReceiverSource, movedReceiverDigest, []originBodyLeaf{
		{name: "own_channel_receiver", body: "channel_own",
			function: originSpan{268, 440, "fn channel_own() -> nothing {\n    let ch: own Channel<int> = Channel::<int>::new(1:uint);\n    ch.send(1);\n    let v: int? = ch.recv();\n    ch.close();\n    return nothing;\n}"},
			cleared: []originRefusal{
				{originSpan{362, 372, "ch.send(1)"}, originSubstitutedRefusal},
				{originSpan{392, 401, "ch.recv()"}, originSubstitutedRefusal},
				{originSpan{407, 417, "ch.close()"}, originSubstitutedRefusal},
			}},
		{name: "alias_receiver", body: "alias_receiver",
			function: originSpan{441, 538, "fn alias_receiver() -> nothing {\n    let mut xs: Ints = [];\n    xs.push(1);\n    return nothing;\n}"},
			cleared:  []originRefusal{{originSpan{505, 515, "xs.push(1)"}, originSubstitutedRefusal}}},
		// G1 is held until the task-borrow model lands: both await forms keep the row.
		{name: "await_call_control", body: "await_call",
			function: originSpan{59, 149, "async fn await_call() -> int {\n    let r: TaskResult<int> = one().await();\n    return 0;\n}"},
			stays:    []originRefusal{{originSpan{119, 132, "one().await()"}, originSubstitutedRefusal}}},
		{name: "await_ident_control", body: "await_ident",
			function: originSpan{150, 267, "async fn await_ident() -> int {\n    let t: Task<int> = one();\n    let r: TaskResult<int> = t.await();\n    return 0;\n}"},
			stays:    []originRefusal{{originSpan{241, 250, "t.await()"}, originSubstitutedRefusal}}},
	})
}
