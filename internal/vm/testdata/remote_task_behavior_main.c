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
    if (strcmp(argv[1], "anchored-queue-full") == 0)
        return rtb_mode_anchored_queue_full();
    if (strcmp(argv[1], "anchored-leak-audit") == 0)
        return rtb_mode_anchored_leak_audit();
    if (strcmp(argv[1], "anchored-cross-producer-order") == 0)
        return rtb_mode_anchored_cross_producer_order();
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
    return rtb_fail("unknown mode");
}
