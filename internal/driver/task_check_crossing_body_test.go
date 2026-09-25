package driver

import (
	"strings"
	"testing"

	"surge/internal/diag"
)

// TC-XB (RV2-DEBT-378). The body of an `on dst { ... }` or `spawn on dst { ... }` crossing runs on its own copies
// of what it captured, and frees them, with its own locals, when it ends: it is a frame for task borrows, as an
// `async { }` body is. A task the body starts over one of those places is refused at the body's `ret` or at its
// end. Before, the pin was filed under the caller's binding of the same name and judged only at the caller's
// exits, and a caller that ends in `panic` holding a parameter has none: `tcxb-on-uaf` (the review's q18)
// diagnosed clean, built and crashed natively. The `spawn on` rows are the review's probes; until N-TASK-27S the
// return-origin analysis leaves them unfinished (kind 27), which taskCheckErrorCodes reads as no error code.
// The programs are in task_check_crossing_body_{on,spawn_on}_sources_test.go.

// crossingFrameRow is one leaf: the program must answer exactly want, and every SEM3021 and SEM3139 message it
// prints, in emission order and joined by "; ", must be exactly edge: the exit that refused it.
type crossingFrameRow struct {
	name, text, digest, want, edge string
}

func crossingFrameRefusals() []crossingFrameRow {
	return []crossingFrameRow{
		{name: "on_hot_task_over_parameter_copy", text: crossingFrameOnHotTaskSource, digest: crossingFrameOnHotTaskSourceDigest, want: "SEM3021", edge: "a task still borrows 's' at this ret"},
		{name: "on_discarded_body_end", text: crossingFrameOnDiscardedSource, digest: crossingFrameOnDiscardedSourceDigest, want: "SEM3021", edge: "a task still borrows 's' at this on body end"},
		{name: "on_kept_task_awaited_in_body", text: crossingFrameOnKeptAwaitedSource, digest: crossingFrameOnKeptAwaitedSourceDigest, want: "SEM3021", edge: "a task still borrows 's' at this ret"},
		{name: "on_let_capture", text: crossingFrameOnLetCaptureSource, digest: crossingFrameOnLetCaptureSourceDigest, want: "SEM3021", edge: "a task still borrows 's' at this ret"},
		{name: "on_body_local", text: crossingFrameOnBodyLocalSource, digest: crossingFrameOnBodyLocalSourceDigest, want: "SEM3021", edge: "a task still borrows 'bl' at this ret"},
		{name: "on_async_host", text: crossingFrameOnAsyncHostSource, digest: crossingFrameOnAsyncHostSourceDigest, want: "SEM3021", edge: "a task still borrows 's' at this ret"},
		{name: "spawn_on_hot_task_over_parameter_copy", text: crossingFrameSpawnOnHotTaskSource, digest: crossingFrameSpawnOnHotTaskSourceDigest, want: "SEM3021", edge: "a task still borrows 's' at this ret"},
		{name: "spawn_on_cold_task_over_parameter", text: crossingFrameSpawnOnColdTaskSource, digest: crossingFrameSpawnOnColdTaskSourceDigest, want: "SEM3021", edge: "a task still borrows 'n' at this ret"},
		{name: "spawn_on_caller_awaits", text: crossingFrameSpawnOnCallerAwaitsSource, digest: crossingFrameSpawnOnCallerAwaitsSourceDigest, want: "SEM3021", edge: "a task still borrows 'k' at this ret"},
		{name: "spawn_on_kept_task_awaited_in_body", text: crossingFrameSpawnOnKeptAwaitedSource, digest: crossingFrameSpawnOnKeptAwaitedSourceDigest, want: "SEM3021", edge: "a task still borrows 's' at this ret"},
		{name: "spawn_on_task_payload_over_capture", text: crossingFrameSpawnOnTaskPayloadSource, digest: crossingFrameSpawnOnTaskPayloadSourceDigest, want: "SEM3139", edge: "cannot return this task: it borrows 'k', which is freed when the function returns while the task may still be running"},
	}
}

func crossingFrameControls() []crossingFrameRow {
	return []crossingFrameRow{
		{name: "on_by_value_twin", text: crossingFrameOnByValueSource, digest: crossingFrameOnByValueSourceDigest},
		{name: "on_task_joined_in_body", text: crossingFrameOnJoinedSource, digest: crossingFrameOnJoinedSourceDigest},
		{name: "on_discarded_by_value", text: crossingFrameOnDiscardedByValueSource, digest: crossingFrameOnDiscardedByValueSourceDigest},
		{name: "on_await_in_body", text: crossingFrameOnAwaitInBodySource, digest: crossingFrameOnAwaitInBodySourceDigest},
		{name: "spawn_on_by_value_twin", text: crossingFrameSpawnOnByValueSource, digest: crossingFrameSpawnOnByValueSourceDigest},
		{name: "spawn_on_body_local_joined", text: crossingFrameSpawnOnLocalJoinedSource, digest: crossingFrameSpawnOnLocalJoinedSourceDigest},
		{name: "spawn_on_capture_spawn_joined", text: crossingFrameSpawnOnCaptureJoinedSource, digest: crossingFrameSpawnOnCaptureJoinedSourceDigest},
	}
}

func crossingFrameEdge(errs []*diag.Diagnostic) string {
	var out []string
	for _, d := range errs {
		if id := d.Code.ID(); id == "SEM3021" || id == "SEM3139" {
			out = append(out, d.Message)
		}
	}
	return strings.Join(out, "; ")
}

// 12 RUN: 1 parent, 11 leaves.
func TestTaskCheckCrossingBodyIsAFrame(t *testing.T) {
	rows := crossingFrameRefusals()
	if len(rows) != 11 {
		t.Fatalf("PRECONDITION: frozen roster changed: rows=%d", len(rows))
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			got, errs := taskCheckErrorCodes(t, taskCheckProbe{name: row.name, digest: row.digest, want: row.want, text: row.text})
			edge := crossingFrameEdge(errs)
			t.Logf("CROSSING_FRAME codes %q edge %q", got, edge)
			if got != row.want {
				t.Fatalf("error codes %q, want %q: a task over the crossing body's own copy is not refused where the body leaves (TC-XB)", got, row.want)
			}
			if edge != row.edge {
				t.Fatalf("refusal edge %q, want %q: the task is refused, but not at the crossing body's own exit (TC-XB)", edge, row.edge)
			}
		})
	}
}

// 8 RUN: 1 parent, 7 leaves. The twins by value, a task joined in the body, and the review's controls.
func TestTaskCheckCrossingBodyKeepsSoundPrograms(t *testing.T) {
	rows := crossingFrameControls()
	if len(rows) != 7 {
		t.Fatalf("PRECONDITION: frozen roster changed: rows=%d", len(rows))
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			if got, _ := taskCheckErrorCodes(t, taskCheckProbe{name: row.name, digest: row.digest, text: row.text}); got != "" {
				t.Fatalf("a sound program is refused: %q", got)
			}
		})
	}
}
