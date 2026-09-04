//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
)

func TestRuntimeV2RemoteTaskBehavior(t *testing.T) {
	bin := buildRemoteTaskBehaviorHarness(t)
	rows := []struct {
		name string
		mode string
		env  []string
	}{
		{
			name: "already-done-immediate-reply",
			mode: "already-done",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "stale-request-and-reply",
			mode: "stale",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "completion-before-owner-register",
			mode: "race-before",
			env: remotePublicationEnv(
				"SURGE_SHARDS=1", "SURGE_THREADS=2",
				"SURGE_SYNC_POINT=SP_REMOTE_TASK_BEFORE_OWNER_REGISTER:block",
			),
		},
		{
			name: "completion-after-owner-register",
			mode: "race-after",
			env: remotePublicationEnv(
				"SURGE_SHARDS=1", "SURGE_THREADS=2",
				"SURGE_SYNC_POINT=SP_REMOTE_TASK_AFTER_OWNER_REGISTER:block",
			),
		},
		{
			name: "unconsumed-handle-teardown",
			mode: "teardown",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "cancel-before-publication-ack",
			mode: "pre-ack-cancel",
			env: remotePublicationEnv(
				"SURGE_SHARDS=2", "SURGE_THREADS=2",
				"SURGE_SYNC_POINT=SP_REMOTE_SPAWN_BEFORE_ACK:block",
			),
		},
		{
			// Two carriers: the driver waits for the park count while the
			// caller parks from its own carrier, so the caller needs one.
			name: "queue-failure-parks-then-shutdown-resolves-it",
			mode: "queue-failure",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "shutdown-wakes-reply-waiters-on-all-shards",
			mode: "shutdown-waiters",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "immediate-on-trace-equivalence-and-owner-proof",
			mode: "immediate-basic",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "immediate-on-distributed-non-caller",
			mode: "immediate-distributed",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "immediate-on-invalid-shard-cancelled-resume",
			mode: "immediate-invalid-shard",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "immediate-on-stale-request-rejected",
			mode: "immediate-stale",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "immediate-on-caller-cancel-exactly-one-resume",
			mode: "immediate-cancel-race",
			env: remotePublicationEnv(
				"SURGE_SHARDS=2", "SURGE_THREADS=2",
				"SURGE_SYNC_POINT=SP_IMMEDIATE_ON_BEFORE_PUBLISH:block",
			),
		},
		{
			name: "immediate-on-shutdown-wakes-reply-waiter",
			mode: "immediate-shutdown",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "immediate-on-self-crossing-uses-transport-at-one-shard",
			mode: "immediate-self-crossing",
			env:  remotePublicationEnv("SURGE_SHARDS=1", "SURGE_THREADS=1"),
		},
		{
			name: "far-channel-mint-resolve-release-round-trip",
			mode: "channel-create",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "far-channel-kind-tag-blocks-registry-aliasing",
			mode: "channel-kind-aliasing",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "far-channel-shutdown-releases-live-entries",
			mode: "channel-shutdown-release",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "far-channel-self-crossing-create-uses-transport",
			mode: "channel-create-self",
			env:  remotePublicationEnv("SURGE_SHARDS=1", "SURGE_THREADS=1"),
		},
		{
			name: "anchored-send-round-trip-with-trace-proof",
			mode: "anchored-send-round-trip",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "anchored-stale-and-wrong-kind-answer-without-a-body",
			mode: "anchored-stale-anchor",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "anchored-full-channel-parks-body-not-dispatcher",
			mode: "anchored-full-channel",
			env: remotePublicationEnv(
				"SURGE_SHARDS=2", "SURGE_THREADS=2",
				// The channel counterparty here is the harness main thread — an
				// external actor the quiescence scan cannot see.
				"SURGE_REMOTE_DEADLOCK_DETECT=0",
			),
		},
		{
			name: "anchored-close-wakes-parked-recv-with-closed-outcome",
			mode: "anchored-close-vs-recv",
			env: remotePublicationEnv(
				"SURGE_SHARDS=2", "SURGE_THREADS=2",
				// The channel counterparty here is the harness main thread — an
				// external actor the quiescence scan cannot see.
				"SURGE_REMOTE_DEADLOCK_DETECT=0",
			),
		},
		{
			name: "anchored-caller-cancel-cannot-resurrect-parked-body",
			mode: "anchored-cancel-parked-body",
			env: remotePublicationEnv(
				"SURGE_SHARDS=2", "SURGE_THREADS=2",
				// The channel counterparty here is the harness main thread — an
				// external actor the quiescence scan cannot see.
				"SURGE_REMOTE_DEADLOCK_DETECT=0",
			),
		},
		{
			name: "anchored-owner-teardown-fails-caller-deterministically",
			mode: "anchored-owner-teardown",
			env: remotePublicationEnv(
				"SURGE_SHARDS=2", "SURGE_THREADS=2",
				// The channel counterparty here is the harness main thread — an
				// external actor the quiescence scan cannot see.
				"SURGE_REMOTE_DEADLOCK_DETECT=0",
			),
		},
		{
			name: "anchored-release-during-block-keeps-pinned-channel-alive",
			mode: "anchored-pin-vs-release",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "anchored-helper-protocol-park-retry-close",
			mode: "anchored-helper-protocol",
			env: remotePublicationEnv(
				"SURGE_SHARDS=2", "SURGE_THREADS=2",
				// The parked send's counterparty is the harness main thread.
				"SURGE_REMOTE_DEADLOCK_DETECT=0",
			),
		},
		{
			name: "anchored-saturation-parks-the-producer-and-a-freed-slot-wakes-it",
			mode: "anchored-saturation-parks",
			env: remotePublicationEnv(
				"SURGE_SHARDS=2", "SURGE_THREADS=2",
				// The parked block's counterparty is the harness main thread.
				"SURGE_REMOTE_DEADLOCK_DETECT=0",
			),
		},
		{
			name: "anchored-lifecycle-churn-leaves-no-residue",
			mode: "anchored-leak-audit",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "anchored-cross-producer-order-is-lane-order-not-start-order",
			mode: "anchored-cross-producer-order",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "share-sibling-lease-round-trip-with-trace-proof",
			mode: "share-round-trip",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "share-per-lease-release-is-independent-and-exact",
			mode: "share-release-independence",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "share-through-released-lease-answers-stale",
			mode: "share-from-released-lease",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "share-pin-outlives-all-leases-to-the-reply-edge",
			mode: "share-pin-outlives-leases",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "share-owner-teardown-goes-stale-for-every-lease",
			mode: "share-teardown",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "share-runnable-holder-suppresses-the-deadlock-panic",
			mode: "share-no-deadlock-when-runnable",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "select-ready-arm-wins-without-parking",
			mode: "select-ready-first",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "select-parked-selector-wakes-on-send-exactly-once",
			mode: "select-park-then-send",
			env: remotePublicationEnv(
				"SURGE_SHARDS=2", "SURGE_THREADS=2",
				// The row parks the selector and then acts from the main
				// thread, which is invisible to the quiescence scan — the
				// documented external-feeder blind spot, so the detector's
				// documented opt-out applies.
				"SURGE_REMOTE_DEADLOCK_DETECT=0"),
		},
		{
			name: "select-tie-break-matches-the-local-scan-order",
			mode: "select-tie-break",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "select-closed-arm-wins-the-registration-race",
			mode: "select-close-before",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "select-close-wakes-the-parked-selector-exactly-once",
			mode: "select-park-then-close",
			env: remotePublicationEnv(
				"SURGE_SHARDS=2", "SURGE_THREADS=2",
				// The row parks the selector and then acts from the main
				// thread, which is invisible to the quiescence scan — the
				// documented external-feeder blind spot, so the detector's
				// documented opt-out applies.
				"SURGE_REMOTE_DEADLOCK_DETECT=0"),
		},
		{
			name: "select-cancel-before-binding-resumes-cancelled",
			mode: "select-cancel-unbound",
			env: remotePublicationEnv(
				"SURGE_SHARDS=2", "SURGE_THREADS=2",
				// The row parks the selector and then acts from the main
				// thread, which is invisible to the quiescence scan — the
				// documented external-feeder blind spot, so the detector's
				// documented opt-out applies. Its two neighbours below have
				// carried this since they were written; this row is the same
				// shape and was missing it, which is why it panicked with
				// "remote channel deadlock: an anchored select over 2 arms is
				// parked ... while every shard is idle" in roughly half of runs.
				"SURGE_REMOTE_DEADLOCK_DETECT=0"),
		},
		{
			name: "select-cancel-of-a-parked-selector-resumes-cancelled",
			mode: "select-cancel-parked",
			env: remotePublicationEnv(
				"SURGE_SHARDS=2", "SURGE_THREADS=2",
				// The row parks the selector and then acts from the main
				// thread, which is invisible to the quiescence scan — the
				// documented external-feeder blind spot, so the detector's
				// documented opt-out applies.
				"SURGE_REMOTE_DEADLOCK_DETECT=0"),
		},
		{
			name: "select-cancel-vs-send-yields-exactly-one-terminal",
			mode: "select-cancel-vs-send",
			env: remotePublicationEnv(
				"SURGE_SHARDS=2", "SURGE_THREADS=2",
				// The row parks the selector and then acts from the main
				// thread, which is invisible to the quiescence scan — the
				// documented external-feeder blind spot, so the detector's
				// documented opt-out applies.
				"SURGE_REMOTE_DEADLOCK_DETECT=0"),
		},
		{
			name: "select-spurious-caller-wake-mints-no-second-request",
			mode: "select-retry-single-body",
			env: remotePublicationEnv(
				"SURGE_SHARDS=2", "SURGE_THREADS=2",
				// The row parks the selector and then acts from the main
				// thread, which is invisible to the quiescence scan — the
				// documented external-feeder blind spot, so the detector's
				// documented opt-out applies.
				"SURGE_REMOTE_DEADLOCK_DETECT=0"),
		},
		{
			name: "select-spurious-body-wake-is-absorbed-and-rearmed",
			mode: "select-stale-wake",
			env: remotePublicationEnv(
				"SURGE_SHARDS=2", "SURGE_THREADS=2",
				// The row parks the selector and then acts from the main
				// thread, which is invisible to the quiescence scan — the
				// documented external-feeder blind spot, so the detector's
				// documented opt-out applies.
				"SURGE_REMOTE_DEADLOCK_DETECT=0"),
		},
		{
			name: "select-pins-outlive-every-released-lease-to-the-reply",
			mode: "select-release-while-parked",
			env: remotePublicationEnv(
				"SURGE_SHARDS=2", "SURGE_THREADS=2",
				// The row parks the selector and then acts from the main
				// thread, which is invisible to the quiescence scan — the
				// documented external-feeder blind spot, so the detector's
				// documented opt-out applies.
				"SURGE_REMOTE_DEADLOCK_DETECT=0"),
		},
		{
			name: "select-sibling-lease-send-wakes-exactly-one-selector",
			mode: "select-sibling-isolation",
			env: remotePublicationEnv(
				"SURGE_SHARDS=2", "SURGE_THREADS=2",
				// The row parks the selector and then acts from the main
				// thread, which is invisible to the quiescence scan — the
				// documented external-feeder blind spot, so the detector's
				// documented opt-out applies.
				"SURGE_REMOTE_DEADLOCK_DETECT=0"),
		},
		{
			name: "select-orphaned-reply-after-caller-teardown-is-consumed",
			mode: "select-caller-teardown",
			env: remotePublicationEnv(
				"SURGE_SHARDS=2", "SURGE_THREADS=2",
				// The row parks the selector and then acts from the main
				// thread, which is invisible to the quiescence scan — the
				// documented external-feeder blind spot, so the detector's
				// documented opt-out applies.
				"SURGE_REMOTE_DEADLOCK_DETECT=0"),
		},
		{
			name: "select-owner-teardown-fails-the-parked-caller-deterministically",
			mode: "select-owner-teardown",
			env: remotePublicationEnv(
				"SURGE_SHARDS=2", "SURGE_THREADS=2",
				// The row parks the selector and then acts from the main
				// thread, which is invisible to the quiescence scan — the
				// documented external-feeder blind spot, so the detector's
				// documented opt-out applies.
				"SURGE_REMOTE_DEADLOCK_DETECT=0"),
		},
		{
			name: "select-runnable-spinner-suppresses-the-deadlock-panic",
			mode: "select-no-deadlock-when-runnable",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "drop-invalid-placement-drops-exactly-once",
			mode: "drop-invalid-placement",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "drop-stale-anchor-drops-exactly-once",
			mode: "drop-stale-anchor",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			// One carrier: the caller requests shutdown from inside its own
			// poll once it parks, and the driver's await pumps the poll that
			// observes the resolution.
			name: "drop-parked-request-shut-down-drops-exactly-once",
			mode: "drop-queue-full",
			env:  remotePublicationEnv("SURGE_SHARDS=1", "SURGE_THREADS=1"),
		},
		{
			name: "drop-mixed-owner-select-refusal-drops-exactly-once",
			mode: "drop-select-mixed-owners",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "drop-handoff-to-a-published-body-never-drops-via-pending",
			mode: "drop-handoff-not-dropped",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "resident-bytes-of-one-crossing-are-exact-and-given-back",
			mode: "resident-handoff-balance",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "drop-zero-id-states-never-reach-the-dispatch",
			mode: "drop-zero-id-never-dispatches",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			name: "drop-bound-cancel-never-drops-through-the-pending",
			mode: "drop-bound-cancel-no-pending-drop",
			env: remotePublicationEnv(
				"SURGE_SHARDS=2", "SURGE_THREADS=2",
				// The parked body's counterparty is the harness main thread.
				"SURGE_REMOTE_DEADLOCK_DETECT=0"),
		},
		{
			// RV2-DEBT-053a: a DONE owner task whose heap RESULT nobody
			// consumed is reclaimed by free_task exactly once.
			name: "result-owner-release-drops-heap-result-exactly-once",
			mode: "result-owner-release",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			// Negative control: a Copy result (id 0) never reaches the
			// result-drop dispatch.
			name: "result-copy-inert-never-reaches-result-drop",
			mode: "result-copy-inert",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			// Negative control: a consumed result cleared the obligation, so
			// free_task must not double-drop.
			name: "result-consumed-does-not-double-drop",
			mode: "result-consumed-no-double-drop",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			// A landed AWAIT reply nobody consumed is reclaimed exactly
			// once when the caller-teardown sweep
			// (rt_remote_task_release_owned) releases the caller's last
			// reference.
			name: "caller-abandon-drops-landed-result-exactly-once",
			mode: "caller-abandon-drops-landed-result",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			// Negative control: a Copy result (id 0) never reaches the
			// result-drop dispatch, even when abandoned.
			name: "caller-abandon-copy-inert-never-reaches-result-drop",
			mode: "caller-abandon-copy-inert",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			// Negative control: a result already consumed before the sweep
			// runs must not be double-dropped.
			name: "caller-abandon-consumed-does-not-double-drop",
			mode: "caller-abandon-consumed-no-double-drop",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			// The sweep's op+caller_task_id filter is exact: an EXECUTE-op
			// pending and a different caller's AWAIT pending are both left
			// untouched.
			name: "caller-abandon-filters-by-op-and-caller",
			mode: "caller-abandon-filters-by-op-and-caller",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
		{
			// When the reply hasn't landed yet, the sweep releases only the
			// caller's own reference and leaves the pending listed to
			// resolve normally later.
			name: "caller-abandon-in-flight-pending-survives",
			mode: "caller-abandon-in-flight-survives",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			stdout, stderr, code := runRemotePublicationHarness(t, bin, row.mode, row.env)
			if code != 0 {
				t.Fatalf("remote task mode %q failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
					row.mode, code, stdout, stderr)
			}
		})
	}
}

// A self-deadlocked anchored block — the channel's only consumer is the
// caller suspended on the block's own reply — must abort the process with
// the actionable remote-channel-deadlock panic instead of hanging. The
// single-worker configuration is excluded deliberately: it starts no worker
// threads, so the park-edge check never runs there and quiescence is the
// driver-side "async deadlock" panic's territory.
func TestRuntimeV2RemoteChannelSelfDeadlockPanics(t *testing.T) {
	bin := buildRemoteTaskBehaviorHarness(t)
	rows := []struct {
		name string
		mode string
		env  []string
		want string
	}{
		{
			name: "one-shard-two-workers",
			mode: "anchored-self-deadlock",
			env:  remotePublicationEnv("SURGE_SHARDS=1", "SURGE_THREADS=2"),
			want: "parked on channel send",
		},
		{
			name: "two-shards",
			mode: "anchored-self-deadlock",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
			want: "parked on channel send",
		},
		{
			name: "two-holders-panic-names-the-lease-topology",
			mode: "share-deadlock-two-holders",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
			want: "has 2 leases but every holder is idle",
		},
		{
			name: "deadlock-still-fires-after-the-peer-released",
			mode: "share-deadlock-after-peer-release",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
			want: "consumer is the suspended caller",
		},
		{
			name: "parked-selector-deadlock-names-the-select-shape",
			mode: "select-self-deadlock",
			env:  remotePublicationEnv("SURGE_SHARDS=2", "SURGE_THREADS=2"),
			want: "an anchored select over 2 arms is parked",
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			stdout, stderr, code := runRemotePublicationHarness(t, bin, row.mode, row.env)
			if code == 0 {
				t.Fatalf("deadlock mode exited cleanly\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
			}
			combined := stdout + stderr
			if !strings.Contains(combined, "remote channel deadlock") {
				t.Fatalf("missing deadlock panic (code=%d)\nstdout:\n%s\nstderr:\n%s",
					code, stdout, stderr)
			}
			if !strings.Contains(combined, row.want) {
				t.Fatalf("panic missing %q\nstdout:\n%s\nstderr:\n%s", row.want, stdout, stderr)
			}
		})
	}
}
