#include "slot_control_harness.h"

static int harness_publish(rt_slot_control* control, uint64_t generation) {
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    REQUIRE(rt_slot_publish_initial_locked(control, generation) == RT_SLOT_CONTROL_OK);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);
    return 0;
}

int harness_case_read(void) {
    mock_value source_value = {.initialized = 1, .payload = 41};
    mock_value copy_value = {0};
    mock_value clone_value = {0};
    mock_value empty_value = {0};
    rt_slot_control source;
    rt_slot_control copy_destination;
    rt_slot_control clone_destination;
    rt_slot_control empty_destination;
    REQUIRE(harness_init_slot(&source, 1, 1, &source_value) == RT_SLOT_CONTROL_OK);
    REQUIRE(harness_init_slot(&copy_destination, 2, 1, &copy_value) == RT_SLOT_CONTROL_OK);
    REQUIRE(harness_init_slot(&clone_destination, 3, 1, &clone_value) == RT_SLOT_CONTROL_OK);
    REQUIRE(harness_init_slot(&empty_destination, 4, 1, &empty_value) == RT_SLOT_CONTROL_OK);
    REQUIRE(harness_publish(&source, 1) == 0);
    harness_reset_callbacks();

    rt_slot_read_claim copy_registration;
    rt_claim_token copy_token;
    rt_slot_read_claim_init(&copy_registration);
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    REQUIRE(rt_slot_claim_read_locked(&source,
                                      &copy_destination,
                                      RT_SLOT_CLAIM_COPY,
                                      1,
                                      1,
                                      &copy_registration,
                                      &copy_token) == RT_SLOT_CONTROL_OK);
    REQUIRE(source.reader_pins == 1);
    REQUIRE(copy_destination.reservation_phase == RT_SLOT_RESERVATION_FALLIBLE);
    rt_claim_token retagged_copy = copy_token;
    retagged_copy.kind = RT_SLOT_CLAIM_TRACE;
    REQUIRE(rt_slot_retire_read_locked(&source, &retagged_copy) == RT_SLOT_CONTROL_INVALID_TOKEN);
    REQUIRE(source.reader_pins == 1);
    rt_claim_token redirected_copy = copy_token;
    redirected_copy.destination_identity++;
    REQUIRE(rt_slot_retire_read_locked(&source, &redirected_copy) == RT_SLOT_CONTROL_INVALID_TOKEN);
    REQUIRE(source.reader_pins == 1);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);
    harness_value_ops.copy_init(&copy_value, &source_value);
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    rt_claim_token forged_publication = copy_token;
    forged_publication.source_identity++;
    REQUIRE(rt_slot_publish_destination_locked(&copy_destination, &forged_publication) ==
            RT_SLOT_CONTROL_INVALID_TOKEN);
    REQUIRE(rt_slot_publish_destination_locked(&copy_destination, &copy_token) ==
            RT_SLOT_CONTROL_OK);
    REQUIRE(rt_slot_retire_read_locked(&source, &copy_token) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_slot_retire_read_locked(&source, &copy_token) == RT_SLOT_CONTROL_INVALID_TOKEN);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);
    REQUIRE(copy_destination.slot.state == RT_SLOT_INITIALIZED && copy_value.payload == 41);

    rt_slot_read_claim clone_registration;
    rt_claim_token clone_token;
    rt_slot_read_claim_init(&clone_registration);
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    REQUIRE(rt_slot_claim_read_locked(&source,
                                      &clone_destination,
                                      RT_SLOT_CLAIM_CLONE,
                                      1,
                                      1,
                                      &clone_registration,
                                      &clone_token) == RT_SLOT_CONTROL_OK);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);
    harness_value_ops.clone_init(&clone_value, &source_value);
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    REQUIRE(rt_slot_reject_initialized_destination_locked(&clone_destination, &clone_token) ==
            RT_SLOT_CONTROL_OK);
    REQUIRE(rt_slot_retire_read_locked(&source, &clone_token) == RT_SLOT_CONTROL_OK);
    REQUIRE(clone_destination.reservation_phase == RT_SLOT_RESERVATION_CLEANUP);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);
    harness_value_ops.drop_in_place(&clone_value);
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    REQUIRE(rt_slot_finish_destination_cleanup_locked(&clone_destination, &clone_token) ==
            RT_SLOT_CONTROL_OK);
    REQUIRE(rt_slot_finish_destination_cleanup_locked(&clone_destination, &clone_token) ==
            RT_SLOT_CONTROL_INVALID_TOKEN);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);

    rt_slot_read_claim empty_registration;
    rt_claim_token empty_token;
    rt_slot_read_claim_init(&empty_registration);
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    REQUIRE(rt_slot_claim_read_locked(&source,
                                      &empty_destination,
                                      RT_SLOT_CLAIM_COPY,
                                      1,
                                      1,
                                      &empty_registration,
                                      &empty_token) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_slot_release_empty_destination_locked(&empty_destination, &empty_token) ==
            RT_SLOT_CONTROL_OK);
    REQUIRE(rt_slot_retire_read_locked(&source, &empty_token) == RT_SLOT_CONTROL_OK);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);

    rt_slot_read_claim trace_registration_a;
    rt_slot_read_claim trace_registration_b;
    rt_claim_token trace_token_a;
    rt_claim_token trace_token_b;
    rt_slot_read_claim_init(&trace_registration_a);
    rt_slot_read_claim_init(&trace_registration_b);
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    REQUIRE(rt_slot_claim_read_locked(
                &source, NULL, RT_SLOT_CLAIM_TRACE, 1, 0, &trace_registration_a, &trace_token_a) ==
            RT_SLOT_CONTROL_OK);
    REQUIRE(rt_slot_claim_read_locked(
                &source, NULL, RT_SLOT_CLAIM_TRACE, 1, 0, &trace_registration_b, &trace_token_b) ==
            RT_SLOT_CONTROL_OK);
    REQUIRE(source.reader_pins == 2);
    REQUIRE(trace_token_b.source_epoch > trace_token_a.source_epoch);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);
    harness_value_ops.trace(&source_value, NULL);
    harness_value_ops.trace(&source_value, NULL);
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    REQUIRE(rt_slot_retire_read_locked(&source, &trace_token_a) == RT_SLOT_CONTROL_OK);
    REQUIRE(source.reader_pins == 1);
    REQUIRE(rt_slot_retire_read_locked(&source, &trace_token_b) == RT_SLOT_CONTROL_OK);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);

    REQUIRE(harness_callback_error == 0);
    REQUIRE(harness_callback_calls == 5);
    REQUIRE(harness_payload_accesses == 7);
    return 0;
}

int harness_case_exclusive(void) {
    mock_value source_value = {.initialized = 1, .payload = 7};
    mock_value destination_value = {0};
    rt_slot_control source;
    rt_slot_control destination;
    REQUIRE(harness_init_slot(&source, 11, 1, &source_value) == RT_SLOT_CONTROL_OK);
    REQUIRE(harness_init_slot(&destination, 12, 1, &destination_value) == RT_SLOT_CONTROL_OK);
    REQUIRE(harness_publish(&source, 1) == 0);
    harness_reset_callbacks();

    rt_slot_read_claim pin;
    rt_claim_token pin_token;
    rt_claim_token blocked_token;
    rt_slot_read_claim_init(&pin);
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    REQUIRE(rt_slot_claim_read_locked(&source, NULL, RT_SLOT_CLAIM_TRACE, 1, 0, &pin, &pin_token) ==
            RT_SLOT_CONTROL_OK);
    REQUIRE(rt_slot_claim_exclusive_locked(
                &source, &destination, RT_SLOT_CLAIM_MOVE, 1, 1, &blocked_token) ==
            RT_SLOT_CONTROL_BUSY);
    REQUIRE(blocked_token.source_epoch == 0);
    REQUIRE(rt_slot_claim_exclusive_locked(
                &source, NULL, RT_SLOT_CLAIM_DROP, 1, 0, &blocked_token) == RT_SLOT_CONTROL_BUSY);
    REQUIRE(rt_slot_begin_generation_locked(
                &source, 2, (uintptr_t)&source_value, sizeof(source_value), _Alignof(mock_value)) ==
            RT_SLOT_CONTROL_BUSY);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);
    harness_value_ops.trace(&source_value, NULL);
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    REQUIRE(rt_slot_retire_read_locked(&source, &pin_token) == RT_SLOT_CONTROL_OK);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);

    rt_claim_token move_token;
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    REQUIRE(rt_slot_claim_exclusive_locked(
                &source, &destination, RT_SLOT_CLAIM_MOVE, 1, 1, &move_token) ==
            RT_SLOT_CONTROL_OK);
    REQUIRE(source.slot.state == RT_SLOT_CLAIMED);
    REQUIRE(destination.reservation_phase == RT_SLOT_RESERVATION_IRREVOCABLE);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);
    harness_value_ops.move_init(&destination_value, &source_value);
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    REQUIRE(rt_slot_commit_move_locked(&source, &destination, &move_token) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_slot_commit_move_locked(&source, &destination, &move_token) ==
            RT_SLOT_CONTROL_INVARIANT);
    REQUIRE(source.slot.state == RT_SLOT_MOVED && destination.slot.state == RT_SLOT_INITIALIZED);
    REQUIRE(rt_slot_begin_generation_locked(
                &source, 1, (uintptr_t)&source_value, sizeof(source_value), _Alignof(mock_value)) ==
            RT_SLOT_CONTROL_STALE);
    REQUIRE(rt_slot_begin_generation_locked(
                &source, 2, (uintptr_t)&source_value, sizeof(source_value), _Alignof(mock_value)) ==
            RT_SLOT_CONTROL_OK);
    REQUIRE(rt_slot_retire_read_locked(&source, &pin_token) == RT_SLOT_CONTROL_STALE);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);

    mock_value drop_value = {.initialized = 1, .payload = 9};
    rt_slot_control drop_source;
    REQUIRE(harness_init_slot(&drop_source, 13, 1, &drop_value) == RT_SLOT_CONTROL_OK);
    REQUIRE(harness_publish(&drop_source, 1) == 0);
    rt_claim_token drop_token;
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    REQUIRE(rt_slot_claim_exclusive_locked(
                &drop_source, NULL, RT_SLOT_CLAIM_DROP, 1, 0, &drop_token) == RT_SLOT_CONTROL_OK);
    rt_claim_token forged_drop = drop_token;
    forged_drop.destination_identity = 1;
    REQUIRE(rt_slot_commit_drop_locked(&drop_source, &forged_drop) == RT_SLOT_CONTROL_INVARIANT);
    REQUIRE(drop_source.slot.state == RT_SLOT_CLAIMED);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);
    harness_value_ops.drop_in_place(&drop_value);
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    REQUIRE(rt_slot_commit_drop_locked(&drop_source, &drop_token) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_slot_commit_drop_locked(&drop_source, &drop_token) == RT_SLOT_CONTROL_INVARIANT);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);

    mock_value cross_source_value = {.initialized = 1, .payload = 12};
    mock_value cross_destination_value = {0};
    rt_slot_control cross_source;
    rt_slot_control cross_destination;
    REQUIRE(harness_init_slot(&cross_source, 14, 1, &cross_source_value) == RT_SLOT_CONTROL_OK);
    REQUIRE(harness_init_slot(&cross_destination, 15, 1, &cross_destination_value) ==
            RT_SLOT_CONTROL_OK);
    REQUIRE(harness_publish(&cross_source, 1) == 0);
    rt_claim_token cross_token;
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    REQUIRE(rt_slot_claim_exclusive_locked(
                &cross_source, &cross_destination, RT_SLOT_CLAIM_CROSS_MOVE, 1, 1, &cross_token) ==
            RT_SLOT_CONTROL_OK);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);
    REQUIRE(harness_value_ops.cross_move_init(
                &cross_destination_value, &cross_source_value, NULL, NULL) ==
            RT_CARRIER_STATUS_CAPACITY);
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    REQUIRE(
        rt_slot_cross_move_failed_locked(&cross_source, &cross_destination, &cross_token, 0, 1) ==
        RT_SLOT_CONTROL_INVARIANT);
    REQUIRE(cross_source.slot.state == RT_SLOT_CLAIMED);
    REQUIRE(rt_slot_cross_move_failed_locked(
                &cross_source, &cross_destination, &cross_token, 1, 1) == RT_SLOT_CONTROL_OK);
    REQUIRE(cross_source.slot.state == RT_SLOT_INITIALIZED &&
            cross_destination.slot.state == RT_SLOT_EMPTY);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);
    REQUIRE(harness_callback_error == 0);
    REQUIRE(harness_callback_calls == 4);
    return 0;
}

int harness_case_stale(void) {
    mock_value source_value = {.initialized = 1, .payload = 17};
    mock_value destination_value = {0};
    rt_slot_control source;
    rt_slot_control destination;
    REQUIRE(harness_init_slot(&source, 21, 1, &source_value) == RT_SLOT_CONTROL_OK);
    REQUIRE(harness_init_slot(&destination, 22, 1, &destination_value) == RT_SLOT_CONTROL_OK);
    REQUIRE(harness_publish(&source, 1) == 0);
    harness_reset_callbacks();

    rt_slot_read_claim stale_registration;
    rt_claim_token stale_token;
    rt_slot_read_claim_init(&stale_registration);
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    rt_slot_control_status status = rt_slot_claim_read_locked(
        &source, &destination, RT_SLOT_CLAIM_COPY, 2, 1, &stale_registration, &stale_token);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);
    if (status == RT_SLOT_CONTROL_OK) {
        harness_value_ops.copy_init(&destination_value, &source_value);
    }
    REQUIRE(status == RT_SLOT_CONTROL_STALE);
    REQUIRE(harness_callback_calls == 0 && destination_value.initialized == 0);

    rt_slot_read_claim registration;
    rt_claim_token token;
    rt_slot_read_claim_init(&registration);
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    REQUIRE(rt_slot_claim_read_locked(
                &source, NULL, RT_SLOT_CLAIM_TRACE, 1, 0, &registration, &token) ==
            RT_SLOT_CONTROL_OK);
    rt_claim_token tampered = token;
    tampered.source_epoch++;
    REQUIRE(rt_slot_retire_read_locked(&source, &tampered) == RT_SLOT_CONTROL_INVALID_TOKEN);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);
    harness_value_ops.trace(&source_value, NULL);
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    REQUIRE(rt_slot_retire_read_locked(&source, &token) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_slot_retire_read_locked(&source, &token) == RT_SLOT_CONTROL_INVALID_TOKEN);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);
    REQUIRE(harness_callback_error == 0 && harness_callback_calls == 1);
    return 0;
}
