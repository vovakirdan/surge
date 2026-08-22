#include "slot_control_harness.h"

#include "rt_value_ops.h"

#include <stdalign.h>
#include <stdint.h>
#include <string.h>

// The copy mode covers one decision: RT_VALUE_FLAG_COPY's callback slot is
// filled by a named runtime symbol that is a trap rather than a copy, and the
// bytes are moved by rt_value_copy_init, which still holds the descriptor whose
// rt_value_layout.size is the width.
//
// It deliberately does NOT re-test the flag/callback biconditional. That is
// covered, in both directions and for every flag, by
// EXPECT_FLAG_CALLBACK_MISMATCH in slot_control_descriptor_cases.c, which
// predates this mode; repeating it here would add no discriminating power.

typedef struct {
    uint32_t low;
    uint32_t high;
} copy_payload;

// guarded_payload puts a byte immediately after the copied value so a copy of
// the wrong width is visible rather than merely suspected.
typedef struct {
    copy_payload value;
    unsigned char guard;
} guarded_payload;

static int harness_specialized_copies = 0;

static void harness_specialized_copy(void* destination, const void* source) {
    harness_specialized_copies++;
    copy_payload* dst = destination;
    const copy_payload* src = source;
    dst->low = src->low;
    dst->high = src->high;
}

static rt_value_ops harness_copy_ops(rt_value_copy_init_fn filler) {
    return (rt_value_ops){
        .layout =
            {
                .size = sizeof(copy_payload),
                .align = _Alignof(copy_payload),
                .stride = sizeof(copy_payload),
                .flags = RT_VALUE_FLAG_COPY,
            },
        .move_init = harness_noop_move,
        .copy_init = filler,
        .plan_cross = harness_noop_plan,
    };
}

// The positive half: a descriptor whose copy_init holds the trap is one the
// owner-private preflight accepts, and the claim it unlocks is the COPY claim.
static int harness_copy_trap_bound_descriptor_is_accepted(void) {
    copy_payload source_value = {.low = 0x11223344u, .high = 0x55667788u};
    copy_payload destination_value = {0};
    rt_value_ops operations = harness_copy_ops(rt_value_copy_init_unbound_trap);
    rt_slot_control source;
    rt_slot_control destination;
    rt_slot_read_claim registration;
    rt_claim_token token;

    REQUIRE(rt_value_copy_uses_runtime_width(&operations) == 1);
    REQUIRE(rt_slot_control_init(&source,
                                 800,
                                 &operations,
                                 1,
                                 (uintptr_t)&source_value,
                                 sizeof(source_value),
                                 _Alignof(copy_payload)) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_slot_control_init(&destination,
                                 801,
                                 &operations,
                                 1,
                                 (uintptr_t)&destination_value,
                                 sizeof(destination_value),
                                 _Alignof(copy_payload)) == RT_SLOT_CONTROL_OK);
    rt_slot_read_claim_init(&registration);
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    REQUIRE(rt_slot_publish_initial_locked(&source, 1) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_slot_claim_read_locked(
                &source, &destination, RT_SLOT_CLAIM_COPY, 1, 1, &registration, &token) ==
            RT_SLOT_CONTROL_OK);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);

    // The copy itself runs outside the owner lock, exactly like every other
    // rt_value_ops operation.
    rt_value_copy_init(&operations, &destination_value, &source_value);

    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    REQUIRE(rt_slot_publish_destination_locked(&destination, &token) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_slot_retire_read_locked(&source, &token) == RT_SLOT_CONTROL_OK);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);
    REQUIRE(destination.slot.state == RT_SLOT_INITIALIZED);
    REQUIRE(destination_value.low == 0x11223344u && destination_value.high == 0x55667788u);
    return 0;
}

// rt_value_copy_uses_runtime_width answers a question, so it answers it for the
// descriptors a caller may hold before it knows: none at all, one that is not
// Copy, and one whose slot the preflight would refuse.
static int harness_copy_width_question_answers_without_refusing(void) {
    rt_value_ops trap_bound = harness_copy_ops(rt_value_copy_init_unbound_trap);
    rt_value_ops unbound = harness_copy_ops(NULL);
    rt_value_ops not_copy = harness_copy_ops(rt_value_copy_init_unbound_trap);
    not_copy.layout.flags = 0;

    REQUIRE(rt_value_copy_uses_runtime_width(NULL) == 0);
    REQUIRE(rt_value_copy_uses_runtime_width(&unbound) == 0);
    REQUIRE(rt_value_copy_uses_runtime_width(&not_copy) == 0);
    REQUIRE(rt_value_copy_uses_runtime_width(&trap_bound) == 1);
    return 0;
}

// A trap-bound descriptor copies the descriptor's exact width, taken from
// rt_value_layout.size, and stops there.
static int harness_copy_copies_exactly_layout_size(void) {
    guarded_payload source = {.value = {.low = 0xdeadbeefu, .high = 0xfeedface}, .guard = 0x5au};
    guarded_payload destination = {.value = {0}, .guard = 0xa5u};
    rt_value_ops operations = harness_copy_ops(rt_value_copy_init_unbound_trap);

    rt_value_copy_init(&operations, &destination.value, &source.value);

    REQUIRE(destination.value.low == 0xdeadbeefu);
    REQUIRE(destination.value.high == 0xfeedface);
    REQUIRE(destination.guard == 0xa5u);
    REQUIRE(source.value.low == 0xdeadbeefu && source.value.high == 0xfeedface);
    REQUIRE(source.guard == 0x5au);
    return 0;
}

// A backend that emits its own copy for one exact type still owns the slot: the
// trap is the default, not the only legal value, and the pointer is dispatched
// as a pointer.
static int harness_copy_specialized_filler_is_dispatched(void) {
    copy_payload source = {.low = 7u, .high = 9u};
    copy_payload destination = {0};
    rt_value_ops operations = harness_copy_ops(harness_specialized_copy);

    REQUIRE(rt_value_copy_uses_runtime_width(&operations) == 0);
    harness_specialized_copies = 0;
    rt_value_copy_init(&operations, &destination, &source);
    REQUIRE(harness_specialized_copies == 1);
    REQUIRE(destination.low == 7u && destination.high == 9u);
    return 0;
}

// A zero-sized Copy value has an exact storage address and nothing to move.
static int harness_copy_accepts_a_zero_sized_value(void) {
    unsigned char source = 0x33u;
    unsigned char destination = 0xccu;
    rt_value_ops operations = harness_copy_ops(rt_value_copy_init_unbound_trap);
    operations.layout.size = 0;
    operations.layout.align = 1;
    operations.layout.stride = 0;

    rt_value_copy_init(&operations, &destination, &source);

    REQUIRE(destination == 0xccu && source == 0x33u);
    return 0;
}

int harness_case_copy(void) {
    REQUIRE(harness_copy_trap_bound_descriptor_is_accepted() == 0);
    REQUIRE(harness_copy_width_question_answers_without_refusing() == 0);
    REQUIRE(harness_copy_copies_exactly_layout_size() == 0);
    REQUIRE(harness_copy_specialized_filler_is_dispatched() == 0);
    REQUIRE(harness_copy_accepts_a_zero_sized_value() == 0);
    return 0;
}

// The descriptorless dispatch the runtime must never perform. It is a separate
// mode because it terminates the process: the point of the case is that the trap
// is LOUD when it is reached without the width it needs, rather than quietly
// copying nothing into storage the owner then publishes.
int harness_case_copy_direct(void) {
    copy_payload source_value = {.low = 1u, .high = 2u};
    copy_payload destination_value = {0};
    rt_value_ops operations = harness_copy_ops(rt_value_copy_init_unbound_trap);

    operations.copy_init(&destination_value, &source_value);

    return harness_fail(
        "the unbound-dispatch trap returned from a descriptorless dispatch", __FILE__, __LINE__);
}
