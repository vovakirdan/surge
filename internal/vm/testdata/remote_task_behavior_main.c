#include "remote_task_behavior.h"

#include <string.h>

int main(int argc, char** argv) {
    if (argc != 2)
        return rtb_fail("usage: remote_task_behavior <mode>");
    if (strcmp(argv[1], "already-done") == 0)
        return rtb_mode_already_done();
    if (strcmp(argv[1], "stale") == 0)
        return rtb_mode_immediate_stale();
    if (strcmp(argv[1], "race-before") == 0) {
        return rtb_mode_registration_race(RT_SYNC_POINT_SP_REMOTE_TASK_BEFORE_OWNER_REGISTER);
    }
    if (strcmp(argv[1], "race-after") == 0) {
        return rtb_mode_registration_race(RT_SYNC_POINT_SP_REMOTE_TASK_AFTER_OWNER_REGISTER);
    }
    if (strcmp(argv[1], "teardown") == 0)
        return rtb_mode_teardown();
    if (strcmp(argv[1], "pre-ack-cancel") == 0)
        return rtb_mode_pre_ack_cancel();
    if (strcmp(argv[1], "queue-failure") == 0)
        return rtb_mode_queue_failure();
    if (strcmp(argv[1], "shutdown-waiters") == 0)
        return rtb_mode_shutdown_waiters();
    if (strcmp(argv[1], "immediate-basic") == 0)
        return rtb_mode_basic();
    if (strcmp(argv[1], "immediate-distributed") == 0)
        return rtb_mode_distributed();
    if (strcmp(argv[1], "immediate-invalid-shard") == 0)
        return rtb_mode_invalid_shard();
    if (strcmp(argv[1], "immediate-stale") == 0)
        return rtb_mode_stale();
    if (strcmp(argv[1], "immediate-cancel-race") == 0)
        return rtb_mode_cancel_race();
    if (strcmp(argv[1], "immediate-shutdown") == 0)
        return rtb_mode_shutdown();
    if (strcmp(argv[1], "immediate-self-crossing") == 0)
        return rtb_mode_immediate_self_crossing();
    if (strcmp(argv[1], "channel-create") == 0)
        return rtb_mode_channel_create();
    if (strcmp(argv[1], "channel-kind-aliasing") == 0)
        return rtb_mode_channel_kind_aliasing();
    if (strcmp(argv[1], "channel-shutdown-release") == 0)
        return rtb_mode_channel_shutdown_release();
    if (strcmp(argv[1], "channel-create-self") == 0)
        return rtb_mode_channel_create_self();
    if (strcmp(argv[1], "anchored-send-round-trip") == 0)
        return rtb_mode_anchored_send_round_trip();
    if (strcmp(argv[1], "anchored-stale-anchor") == 0)
        return rtb_mode_anchored_stale_anchor();
    if (strcmp(argv[1], "anchored-full-channel") == 0)
        return rtb_mode_anchored_full_channel();
    if (strcmp(argv[1], "anchored-close-vs-recv") == 0)
        return rtb_mode_anchored_close_vs_recv();
    if (strcmp(argv[1], "anchored-cancel-parked-body") == 0)
        return rtb_mode_anchored_cancel_parked_body();
    if (strcmp(argv[1], "anchored-owner-teardown") == 0)
        return rtb_mode_anchored_owner_teardown();
    if (strcmp(argv[1], "anchored-self-deadlock") == 0)
        return rtb_mode_anchored_self_deadlock();
    if (strcmp(argv[1], "anchored-pin-vs-release") == 0)
        return rtb_mode_anchored_pin_vs_release();
    if (strcmp(argv[1], "anchored-helper-protocol") == 0)
        return rtb_mode_anchored_helper_protocol();
    if (strcmp(argv[1], "anchored-saturation-parks") == 0)
        return rtb_mode_anchored_saturation_parks();
    if (strcmp(argv[1], "anchored-leak-audit") == 0)
        return rtb_mode_anchored_leak_audit();
    if (strcmp(argv[1], "anchored-cross-producer-order") == 0)
        return rtb_mode_anchored_cross_producer_order();
    if (strcmp(argv[1], "anchored-freed-channel-waiter") == 0)
        return rtb_mode_anchored_freed_channel_waiter();
    if (strcmp(argv[1], "share-round-trip") == 0)
        return rtb_mode_share_round_trip();
    if (strcmp(argv[1], "share-release-independence") == 0)
        return rtb_mode_share_release_independence();
    if (strcmp(argv[1], "share-from-released-lease") == 0)
        return rtb_mode_share_from_released_lease();
    if (strcmp(argv[1], "share-pin-outlives-leases") == 0)
        return rtb_mode_share_pin_outlives_leases();
    if (strcmp(argv[1], "share-teardown") == 0)
        return rtb_mode_share_teardown();
    if (strcmp(argv[1], "share-deadlock-two-holders") == 0)
        return rtb_mode_share_deadlock_two_holders();
    if (strcmp(argv[1], "share-no-deadlock-when-runnable") == 0)
        return rtb_mode_share_no_deadlock_when_runnable();
    if (strcmp(argv[1], "share-deadlock-after-peer-release") == 0)
        return rtb_mode_share_deadlock_after_peer_release();
    if (strcmp(argv[1], "select-ready-first") == 0)
        return rtb_mode_select_ready_first();
    if (strcmp(argv[1], "select-park-then-send") == 0)
        return rtb_mode_select_park_then_send();
    if (strcmp(argv[1], "select-tie-break") == 0)
        return rtb_mode_select_tie_break();
    if (strcmp(argv[1], "select-close-before") == 0)
        return rtb_mode_select_close_before();
    if (strcmp(argv[1], "select-park-then-close") == 0)
        return rtb_mode_select_park_then_close();
    if (strcmp(argv[1], "select-cancel-unbound") == 0)
        return rtb_mode_select_cancel_unbound();
    if (strcmp(argv[1], "select-cancel-parked") == 0)
        return rtb_mode_select_cancel_parked();
    if (strcmp(argv[1], "select-cancel-vs-send") == 0)
        return rtb_mode_select_cancel_vs_send();
    if (strcmp(argv[1], "select-retry-single-body") == 0)
        return rtb_mode_select_retry_single_body();
    if (strcmp(argv[1], "seq0-retry-classification") == 0)
        return rtb_mode_seq0_retry_classification();
    if (strcmp(argv[1], "select-seq0-retry-terminal-drain") == 0)
        return rtb_mode_select_seq0_retry_terminal_drain();
    if (strcmp(argv[1], "seq0-blocking-cancel-drain") == 0)
        return rtb_mode_seq0_blocking_cancel_drain();
    if (strcmp(argv[1], "seq0-spawn-abandon-drain") == 0)
        return rtb_mode_seq0_spawn_abandon_drain();
    if (strcmp(argv[1], "seq0-remote-teardown-drain") == 0)
        return rtb_mode_seq0_remote_teardown_drain();
    if (strcmp(argv[1], "seq0-remote-shutdown-drain") == 0)
        return rtb_mode_seq0_remote_shutdown_drain();
    if (strcmp(argv[1], "select-stale-wake") == 0)
        return rtb_mode_select_stale_wake();
    if (strcmp(argv[1], "select-release-while-parked") == 0)
        return rtb_mode_select_release_while_parked();
    if (strcmp(argv[1], "select-sibling-isolation") == 0)
        return rtb_mode_select_sibling_isolation();
    if (strcmp(argv[1], "select-caller-teardown") == 0)
        return rtb_mode_select_caller_teardown();
    if (strcmp(argv[1], "select-owner-teardown") == 0)
        return rtb_mode_select_owner_teardown();
    if (strcmp(argv[1], "select-no-deadlock-when-runnable") == 0)
        return rtb_mode_select_no_deadlock_when_runnable();
    if (strcmp(argv[1], "select-self-deadlock") == 0)
        return rtb_mode_select_self_deadlock();
    if (strcmp(argv[1], "drop-invalid-placement") == 0)
        return rtb_mode_drop_invalid_placement();
    if (strcmp(argv[1], "drop-stale-anchor") == 0)
        return rtb_mode_drop_stale_anchor();
    if (strcmp(argv[1], "drop-queue-full") == 0)
        return rtb_mode_drop_queue_full();
    if (strcmp(argv[1], "drop-select-mixed-owners") == 0)
        return rtb_mode_drop_select_mixed_owners();
    if (strcmp(argv[1], "drop-handoff-not-dropped") == 0)
        return rtb_mode_drop_handoff_not_dropped();
    if (strcmp(argv[1], "resident-handoff-balance") == 0)
        return rtb_mode_resident_handoff_balance();
    if (strcmp(argv[1], "drop-zero-id-never-dispatches") == 0)
        return rtb_mode_drop_zero_id_never_dispatches();
    if (strcmp(argv[1], "drop-bound-cancel-no-pending-drop") == 0)
        return rtb_mode_drop_bound_cancel_no_pending_drop();
    if (strcmp(argv[1], "result-owner-release") == 0)
        return rtb_mode_result_owner_release();
    if (strcmp(argv[1], "result-copy-inert") == 0)
        return rtb_mode_result_copy_inert();
    if (strcmp(argv[1], "result-consumed-no-double-drop") == 0)
        return rtb_mode_result_consumed_no_double_drop();
    if (strcmp(argv[1], "caller-abandon-drops-landed-result") == 0)
        return rtb_mode_caller_abandon_drops_landed_result();
    if (strcmp(argv[1], "caller-abandon-copy-inert") == 0)
        return rtb_mode_caller_abandon_copy_inert();
    if (strcmp(argv[1], "caller-abandon-consumed-no-double-drop") == 0)
        return rtb_mode_caller_abandon_consumed_no_double_drop();
    if (strcmp(argv[1], "caller-abandon-filters-by-op-and-caller") == 0)
        return rtb_mode_caller_abandon_filters_by_op_and_caller();
    if (strcmp(argv[1], "caller-abandon-in-flight-survives") == 0)
        return rtb_mode_caller_abandon_in_flight_survives();
    return rtb_fail("unknown mode");
}
