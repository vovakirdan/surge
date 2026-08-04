#include "slot_control_harness.h"

static int harness_publish_for_order(rt_slot_control* control) {
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    REQUIRE(rt_slot_publish_initial_locked(control, 1) == RT_SLOT_CONTROL_OK);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);
    return 0;
}

int harness_case_ordering(void) {
    harness_reset_callbacks();

    mock_value publish_source_value = {.initialized = 1, .payload = 51};
    mock_value publish_destination_value = {0};
    rt_slot_control publish_source;
    rt_slot_control publish_destination;
    REQUIRE(harness_init_slot(&publish_source, 41, 1, &publish_source_value) == RT_SLOT_CONTROL_OK);
    REQUIRE(harness_init_slot(&publish_destination, 42, 1, &publish_destination_value) ==
            RT_SLOT_CONTROL_OK);
    REQUIRE(harness_publish_for_order(&publish_source) == 0);
    rt_slot_read_claim publish_registration;
    rt_claim_token publish_token;
    rt_slot_read_claim_init(&publish_registration);
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    REQUIRE(rt_slot_claim_read_locked(&publish_source,
                                      &publish_destination,
                                      RT_SLOT_CLAIM_COPY,
                                      1,
                                      1,
                                      &publish_registration,
                                      &publish_token) == RT_SLOT_CONTROL_OK);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);
    harness_value_ops.copy_init(&publish_destination_value, &publish_source_value);
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    REQUIRE(rt_slot_retire_read_locked(&publish_source, &publish_token) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_slot_publish_destination_locked(&publish_destination, &publish_token) ==
            RT_SLOT_CONTROL_OK);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);

    mock_value reject_source_value = {.initialized = 1, .payload = 52};
    mock_value reject_destination_value = {0};
    rt_slot_control reject_source;
    rt_slot_control reject_destination;
    REQUIRE(harness_init_slot(&reject_source, 43, 1, &reject_source_value) == RT_SLOT_CONTROL_OK);
    REQUIRE(harness_init_slot(&reject_destination, 44, 1, &reject_destination_value) ==
            RT_SLOT_CONTROL_OK);
    REQUIRE(harness_publish_for_order(&reject_source) == 0);
    rt_slot_read_claim reject_registration;
    rt_claim_token reject_token;
    rt_slot_read_claim_init(&reject_registration);
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    REQUIRE(rt_slot_claim_read_locked(&reject_source,
                                      &reject_destination,
                                      RT_SLOT_CLAIM_CLONE,
                                      1,
                                      1,
                                      &reject_registration,
                                      &reject_token) == RT_SLOT_CONTROL_OK);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);
    harness_value_ops.clone_init(&reject_destination_value, &reject_source_value);
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    REQUIRE(rt_slot_retire_read_locked(&reject_source, &reject_token) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_slot_reject_initialized_destination_locked(&reject_destination, &reject_token) ==
            RT_SLOT_CONTROL_OK);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);
    harness_value_ops.drop_in_place(&reject_destination_value);
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    REQUIRE(rt_slot_finish_destination_cleanup_locked(&reject_destination, &reject_token) ==
            RT_SLOT_CONTROL_OK);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);

    mock_value reuse_source_value = {.initialized = 1, .payload = 53};
    mock_value pending_destination_value = {0};
    mock_value move_destination_value = {0};
    rt_slot_control reuse_source;
    rt_slot_control pending_destination;
    rt_slot_control move_destination;
    REQUIRE(harness_init_slot(&reuse_source, 45, 1, &reuse_source_value) == RT_SLOT_CONTROL_OK);
    REQUIRE(harness_init_slot(&pending_destination, 46, 1, &pending_destination_value) ==
            RT_SLOT_CONTROL_OK);
    REQUIRE(harness_init_slot(&move_destination, 47, 1, &move_destination_value) ==
            RT_SLOT_CONTROL_OK);
    REQUIRE(harness_publish_for_order(&reuse_source) == 0);
    rt_slot_read_claim pending_registration;
    rt_claim_token pending_token;
    rt_slot_read_claim_init(&pending_registration);
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    REQUIRE(rt_slot_claim_read_locked(&reuse_source,
                                      &pending_destination,
                                      RT_SLOT_CLAIM_COPY,
                                      1,
                                      1,
                                      &pending_registration,
                                      &pending_token) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_slot_retire_read_locked(&reuse_source, &pending_token) == RT_SLOT_CONTROL_OK);
    rt_claim_token move_token;
    REQUIRE(rt_slot_claim_exclusive_locked(
                &reuse_source, &move_destination, RT_SLOT_CLAIM_MOVE, 1, 1, &move_token) ==
            RT_SLOT_CONTROL_OK);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);
    harness_value_ops.move_init(&move_destination_value, &reuse_source_value);
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    REQUIRE(rt_slot_commit_move_locked(&reuse_source, &move_destination, &move_token) ==
            RT_SLOT_CONTROL_OK);
    REQUIRE(rt_slot_begin_generation_locked(&reuse_source,
                                            2,
                                            (uintptr_t)&reuse_source_value,
                                            sizeof(reuse_source_value),
                                            _Alignof(mock_value)) == RT_SLOT_CONTROL_OK);
    rt_claim_token forged_pending = pending_token;
    forged_pending.source_generation++;
    REQUIRE(rt_slot_release_empty_destination_locked(&pending_destination, &forged_pending) ==
            RT_SLOT_CONTROL_INVALID_TOKEN);
    REQUIRE(rt_slot_release_empty_destination_locked(&pending_destination, &pending_token) ==
            RT_SLOT_CONTROL_OK);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);

    REQUIRE(harness_callback_error == 0);
    REQUIRE(harness_callback_calls == 4);
    return 0;
}
