#include "slot_control_harness.h"

#include <stdalign.h>
#include <stdint.h>
#include <string.h>

static rt_value_ops harness_minimal_ops(void) {
    return (rt_value_ops){
        .layout =
            {
                .size = sizeof(int),
                .align = _Alignof(int),
                .stride = sizeof(int),
            },
        .move_init = harness_noop_move,
        .plan_cross = harness_noop_plan,
    };
}

static int harness_token_is_zero(const rt_claim_token* token) {
    return token->source_control == NULL && token->destination_control == NULL &&
           token->operations == NULL && token->source_identity == 0 &&
           token->source_generation == 0 && token->source_epoch == 0 &&
           token->destination_identity == 0 && token->destination_generation == 0 &&
           token->destination_epoch == 0 && token->kind == RT_SLOT_CLAIM_INVALID &&
           token->has_destination == 0;
}

static int harness_expect_invalid_ops(const rt_value_ops* operations, uint64_t identity) {
    alignas(64) static unsigned char storage[64];
    rt_slot_control control;
    unsigned char before[sizeof(control)];
    memset(&control, 0xa5, sizeof(control));
    memcpy(before, &control, sizeof(control));
    size_t alignment = operations->layout.align;
    if (alignment == 0 || (alignment & (alignment - 1)) != 0) {
        alignment = 1;
    }
    REQUIRE(rt_slot_control_init(&control,
                                 identity,
                                 operations,
                                 1,
                                 (uintptr_t)storage,
                                 operations->layout.size,
                                 alignment) == RT_SLOT_CONTROL_INVALID_OPERATIONS);
    REQUIRE(memcmp(before, &control, sizeof(control)) == 0);
    return 0;
}

static int harness_expect_unsupported_read(rt_slot_control* source,
                                           rt_slot_control* destination,
                                           rt_slot_claim_kind kind) {
    unsigned char source_before[sizeof(*source)];
    unsigned char destination_before[sizeof(*destination)];
    rt_slot_read_claim registration;
    rt_claim_token token;
    rt_slot_read_claim_init(&registration);
    memset(&token, 0xa5, sizeof(token));
    memcpy(source_before, source, sizeof(*source));
    if (destination != NULL) {
        memcpy(destination_before, destination, sizeof(*destination));
    }
    REQUIRE(rt_slot_claim_read_locked(
                source, destination, kind, 1, destination != NULL ? 1 : 0, &registration, &token) ==
            RT_SLOT_CONTROL_UNSUPPORTED_OPERATION);
    REQUIRE(memcmp(source_before, source, sizeof(*source)) == 0);
    REQUIRE(destination == NULL ||
            memcmp(destination_before, destination, sizeof(*destination)) == 0);
    REQUIRE(registration.active == 0 && registration.source_control == NULL &&
            registration.destination_control == NULL);
    REQUIRE(harness_token_is_zero(&token));
    return 0;
}

static int harness_expect_unsupported_exclusive(rt_slot_control* source,
                                                rt_slot_control* destination,
                                                rt_slot_claim_kind kind) {
    unsigned char source_before[sizeof(*source)];
    unsigned char destination_before[sizeof(*destination)];
    rt_claim_token token;
    memset(&token, 0xa5, sizeof(token));
    memcpy(source_before, source, sizeof(*source));
    memcpy(destination_before, destination, sizeof(*destination));
    REQUIRE(rt_slot_claim_exclusive_locked(source, destination, kind, 1, 1, &token) ==
            RT_SLOT_CONTROL_UNSUPPORTED_OPERATION);
    REQUIRE(memcmp(source_before, source, sizeof(*source)) == 0);
    REQUIRE(memcmp(destination_before, destination, sizeof(*destination)) == 0);
    REQUIRE(harness_token_is_zero(&token));
    return 0;
}

static int harness_case_invalid_descriptors(void) {
    uint64_t identity = 600;
    rt_value_ops operations = harness_minimal_ops();
    operations.layout.flags = UINT64_C(64);
    REQUIRE(harness_expect_invalid_ops(&operations, identity++) == 0);
    operations = harness_minimal_ops();
    operations.layout.align = 0;
    REQUIRE(harness_expect_invalid_ops(&operations, identity++) == 0);
    operations = harness_minimal_ops();
    operations.layout.align = 3;
    REQUIRE(harness_expect_invalid_ops(&operations, identity++) == 0);
    operations = harness_minimal_ops();
    operations.layout.size = 0;
    operations.layout.stride = 1;
    REQUIRE(harness_expect_invalid_ops(&operations, identity++) == 0);
    operations = harness_minimal_ops();
    operations.layout.stride = 0;
    REQUIRE(harness_expect_invalid_ops(&operations, identity++) == 0);
    operations = harness_minimal_ops();
    operations.layout.stride += operations.layout.align;
    REQUIRE(harness_expect_invalid_ops(&operations, identity++) == 0);
    operations = harness_minimal_ops();
    operations.layout.size = SIZE_MAX - 3;
    operations.layout.align = 8;
    operations.layout.stride = 0;
    REQUIRE(harness_expect_invalid_ops(&operations, identity++) == 0);
    operations = harness_minimal_ops();
    operations.move_init = NULL;
    REQUIRE(harness_expect_invalid_ops(&operations, identity++) == 0);
    operations = harness_minimal_ops();
    operations.plan_cross = NULL;
    REQUIRE(harness_expect_invalid_ops(&operations, identity++) == 0);

#define EXPECT_FLAG_CALLBACK_MISMATCH(flag, field, callback)                                       \
    do {                                                                                           \
        operations = harness_minimal_ops();                                                        \
        operations.layout.flags = (flag);                                                          \
        REQUIRE(harness_expect_invalid_ops(&operations, identity++) == 0);                         \
        operations = harness_minimal_ops();                                                        \
        operations.field = (callback);                                                             \
        REQUIRE(harness_expect_invalid_ops(&operations, identity++) == 0);                         \
    } while (0)

    EXPECT_FLAG_CALLBACK_MISMATCH(RT_VALUE_FLAG_COPY, copy_init, harness_noop_copy);
    EXPECT_FLAG_CALLBACK_MISMATCH(RT_VALUE_FLAG_CLONABLE, clone_init, harness_noop_copy);
    EXPECT_FLAG_CALLBACK_MISMATCH(RT_VALUE_FLAG_DROPPABLE, drop_in_place, harness_noop_drop);
    EXPECT_FLAG_CALLBACK_MISMATCH(RT_VALUE_FLAG_TRACEABLE, trace, harness_noop_trace);
    EXPECT_FLAG_CALLBACK_MISMATCH(
        RT_VALUE_FLAG_SHARD_MOVABLE, cross_move_init, harness_noop_cross_move);
    EXPECT_FLAG_CALLBACK_MISMATCH(
        RT_VALUE_FLAG_CROSS_CLONABLE, cross_clone_init, harness_noop_cross_clone);
#undef EXPECT_FLAG_CALLBACK_MISMATCH

    alignas(64) unsigned char storage[64] = {0};
    rt_slot_control control;
    operations = harness_minimal_ops();
    operations.layout.flags = RT_VALUE_FLAG_COPY | RT_VALUE_FLAG_CLONABLE |
                              RT_VALUE_FLAG_DROPPABLE | RT_VALUE_FLAG_TRACEABLE |
                              RT_VALUE_FLAG_SHARD_MOVABLE | RT_VALUE_FLAG_CROSS_CLONABLE;
    operations.copy_init = harness_noop_copy;
    operations.clone_init = harness_noop_copy;
    operations.drop_in_place = harness_noop_drop;
    operations.trace = harness_noop_trace;
    operations.cross_move_init = harness_noop_cross_move;
    operations.cross_clone_init = harness_noop_cross_clone;
    REQUIRE(
        rt_slot_control_init(
            &control, identity, &operations, 1, (uintptr_t)storage, sizeof(int), _Alignof(int)) ==
        RT_SLOT_CONTROL_OK);
    return 0;
}

static int harness_case_capabilities(void) {
    int source_value = 7;
    int destination_value = 0;
    rt_value_ops operations = harness_minimal_ops();
    rt_slot_control source;
    rt_slot_control destination;
    REQUIRE(rt_slot_control_init(&source,
                                 700,
                                 &operations,
                                 1,
                                 (uintptr_t)&source_value,
                                 sizeof(source_value),
                                 _Alignof(int)) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_slot_control_init(&destination,
                                 701,
                                 &operations,
                                 1,
                                 (uintptr_t)&destination_value,
                                 sizeof(destination_value),
                                 _Alignof(int)) == RT_SLOT_CONTROL_OK);
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    REQUIRE(rt_slot_publish_initial_locked(&source, 1) == RT_SLOT_CONTROL_OK);
    REQUIRE(harness_expect_unsupported_read(&source, &destination, RT_SLOT_CLAIM_COPY) == 0);
    REQUIRE(harness_expect_unsupported_read(&source, &destination, RT_SLOT_CLAIM_CLONE) == 0);
    REQUIRE(harness_expect_unsupported_read(&source, NULL, RT_SLOT_CLAIM_TRACE) == 0);
    REQUIRE(harness_expect_unsupported_read(&source, &destination, RT_SLOT_CLAIM_CROSS_CLONE) == 0);
    REQUIRE(harness_expect_unsupported_exclusive(&source, &destination, RT_SLOT_CLAIM_CROSS_MOVE) ==
            0);

    rt_claim_token drop_token;
    harness_reset_callbacks();
    REQUIRE(rt_slot_claim_exclusive_locked(&source, NULL, RT_SLOT_CLAIM_DROP, 1, 0, &drop_token) ==
            RT_SLOT_CONTROL_OK);
    REQUIRE(drop_token.source_control == &source && drop_token.destination_control == NULL);
    REQUIRE(rt_slot_commit_drop_locked(&source, &drop_token) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_slot_commit_drop_locked(&source, &drop_token) == RT_SLOT_CONTROL_INVARIANT);
    REQUIRE(source.slot.state == RT_SLOT_DROPPED);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);
    REQUIRE(harness_callback_calls == 0 && harness_payload_accesses == 0);
    return 0;
}

int harness_case_descriptor(void) {
    REQUIRE(harness_case_invalid_descriptors() == 0);
    REQUIRE(harness_case_capabilities() == 0);
    return 0;
}
