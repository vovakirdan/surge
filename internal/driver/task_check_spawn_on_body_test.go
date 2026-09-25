package driver

import "testing"

// N-TASK-27S. What a `spawn on` body's tasks borrow is the task check's (owner ruling 2026-09-15), and since TC-XB
// (RV2-DEBT-378) the body is a frame for it: a task over a body local or over the body's copy of a capture is refused
// at the body's `ret` or end. These rows pin those edges for the `spawn on` forms this packet makes reachable, and
// the SEM3139 wording at a crossing `ret`: what the task borrows is freed when the BODY finishes (the `async` body's
// sentence), not "when the function returns". Each row logs `CROSSING_FRAME codes <set> edge <messages>`.

// 6 RUN: 1 parent, 5 leaves.
func TestTaskCheckSpawnOnBodyEdges(t *testing.T) {
	rows := []crossingFrameRow{
		{name: "body_local_task_unjoined", text: spawnOnR06BodyTaskUnjoinedSource, digest: spawnOnR06BodyTaskUnjoinedSourceDigest, want: "SEM3021", edge: "a task still borrows 'bl' at this ret"},
		{name: "body_local_task_returned", text: spawnOnR07BodyTaskReturnedSource, digest: spawnOnR07BodyTaskReturnedSourceDigest, want: "SEM3139", edge: "cannot return this task: it borrows 'bl', which is freed when this body finishes while the task may still be running"},
		{name: "body_spawn_over_local_returned", text: spawnOnR20BodySpawnOverLocalReturnedSource, digest: spawnOnR20BodySpawnOverLocalReturnedSourceDigest, want: "SEM3107,SEM3139", edge: "cannot return this task: it borrows 'bl', which is freed when this body finishes while the task may still be running"},
		{name: "body_spawn_over_capture_returned", text: spawnOnR21BodySpawnOverCaptureReturnedSource, digest: spawnOnR21BodySpawnOverCaptureReturnedSourceDigest, want: "SEM3107,SEM3139", edge: "cannot return this task: it borrows 'k', which is freed when this body finishes while the task may still be running"},
		{name: "detached_spawn_in_body", text: spawnOnSonDetachSource, digest: spawnOnSonDetachSourceDigest, want: "SEM3107", edge: ""},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			got, errs := taskCheckErrorCodes(t, taskCheckProbe{name: row.name, digest: row.digest, want: row.want, text: row.text})
			edge := crossingFrameEdge(errs)
			t.Logf("CROSSING_FRAME codes %q edge %q", got, edge)
			if got != row.want {
				t.Fatalf("error codes %q, want %q: a task a `spawn on` body starts is not refused where the body leaves", got, row.want)
			}
			if edge != row.edge {
				t.Fatalf("refusal edge %q, want %q", edge, row.edge)
			}
		})
	}
}

// A crossing body closed by `panic` (or `exit`) has no `ret` and no reachable end, so only walkCrossingBody's restore
// drops its body tasks' pins. TC-XB made this program clean where the base refused it at the caller's `return`
// (TC-XB bundle review R-1). That is the `async` body's rule (its twin below) and sound because a native panic ends
// the process; the program is never run here (a ret-less `on` body is RV2-DEBT-379). CF-TCX-RESTORE reddens the
// `on` leaf and leaves the `async` one green.
//
// 3 RUN: 1 parent, 2 leaves.
func TestTaskCheckCrossingBodyPanicClosedFrame(t *testing.T) {
	for _, row := range []crossingFrameRow{
		{name: "on_panic_closed_body", text: spawnOnP1RestorePanicSource, digest: spawnOnP1RestorePanicSourceDigest},
		{name: "async_panic_closed_body", text: spawnOnP9AsyncPanicSource, digest: spawnOnP9AsyncPanicSourceDigest},
	} {
		t.Run(row.name, func(t *testing.T) {
			got, errs := taskCheckErrorCodes(t, taskCheckProbe{name: row.name, digest: row.digest, text: row.text})
			t.Logf("CROSSING_FRAME codes %q edge %q", got, crossingFrameEdge(errs))
			if got != "" {
				t.Fatalf("a panic-closed body is refused: %q", got)
			}
		})
	}
}
