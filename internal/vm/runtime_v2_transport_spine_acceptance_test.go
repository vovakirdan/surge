//go:build runtime_v2_transport_spine

package vm_test

import (
	"strings"
	"testing"
)

func TestRuntimeV2TransportSpineAcceptanceRows(t *testing.T) {
	rows := []struct {
		name       string
		mode       string
		flags      []string
		expectFail bool
	}{
		{name: "lost-wake/recheck", mode: "threaded_recheck"},
		{
			name:       "lost-wake negative skip recheck",
			mode:       "threaded_recheck",
			flags:      []string{"-DRT_TRANSPORT_NEG_SKIP_RECHECK"},
			expectFail: true,
		},
		{
			name:       "lost-wake negative relaxed park ordering",
			mode:       "worker_wait_wake",
			flags:      []string{"-DRT_TRANSPORT_NEG_RELAXED_PARK_ORDER"},
			expectFail: true,
		},
		{name: "wake elision RUNNING", mode: "running_elision"},
		{name: "PARKED wake exactly once", mode: "worker_wait_wake"},
		{
			name:       "wake elision negative parked wake skipped",
			mode:       "worker_wait_wake",
			flags:      []string{"-DRT_TRANSPORT_NEG_SKIP_PARKED_WAKE"},
			expectFail: true,
		},
		{
			name:       "wake elision negative running wake written",
			mode:       "running_elision",
			flags:      []string{"-DRT_TRANSPORT_NEG_WRITE_RUNNING_WAKE"},
			expectFail: true,
		},
		{name: "PARKED-with-inbound invariant", mode: "recheck"},
		{
			name:       "PARKED-with-inbound negative",
			mode:       "parked_inbound_negative",
			flags:      []string{"-DRT_TRANSPORT_NEG_SKIP_RECHECK"},
			expectFail: true,
		},
		{name: "shutdown wakes parked shards and reply waiters", mode: "shutdown_wake"},
		{
			name:       "shutdown negative no wake",
			mode:       "shutdown_wake",
			flags:      []string{"-DRT_TRANSPORT_NEG_SHUTDOWN_NO_WAKE"},
			expectFail: true,
		},
		{name: "empty-queue drain reads no wake pipe", mode: "empty_drain_no_read"},
		{
			name:       "empty-queue drain negative unconditional read",
			mode:       "empty_drain_no_read",
			flags:      []string{"-DRT_TRANSPORT_NEG_DRAIN_EMPTY_QUEUE"},
			expectFail: true,
		},
		{name: "reply wait suspends task instead of parking shard", mode: "reply_wait"},
		{
			name:       "reply wait negative shard park",
			mode:       "reply_wait",
			flags:      []string{"-DRT_TRANSPORT_NEG_REPLY_WAIT_PARKS_SHARD"},
			expectFail: true,
		},
	}
	for _, row := range rows {
		row := row
		t.Run(row.name, func(t *testing.T) {
			output, err := runTransportSpineAcceptanceProgram(t, row.mode, row.flags)
			if row.expectFail {
				if err == nil {
					t.Fatalf("negative control unexpectedly passed\n%s", output)
				}
				if !strings.Contains(output, "transport-spine-check:") {
					t.Fatalf("negative control failed without deterministic harness message: %v\n%s", err, output)
				}
				return
			}
			if err != nil {
				t.Fatalf("acceptance row failed: %v\n%s", err, output)
			}
		})
	}
}
