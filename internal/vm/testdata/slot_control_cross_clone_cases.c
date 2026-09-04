#include "slot_control_harness.h"

#include "rt_value_ops.h"

// The crossing-clone runtime DISPATCHER, rt_value_cross_clone_init_detached.
// It is the value-ops half of the barrier: given a descriptor that admits a
// crossing clone, it checks the flag/callback pairing and the lock, then calls
// the descriptor's own cross_clone_init and returns its status verbatim.
//
// This proves the dispatcher only. How the crossing SITE claims the source is a
// separate matter and a different path: a clone is a READ claim, not the
// exclusive one a move takes, because it reads an immutable pinned source and
// never empties it. That wiring belongs to the crossing site, not here.

static int cross_clone_calls;
static rt_carrier_status cross_clone_next_status;

// A cross-clone callback that records it ran and returns whatever the case
// asked for, so the case can prove the dispatcher passes a refusal and an OK
// through unchanged.
static rt_carrier_status cross_clone_record(void* destination,
                                            const void* source,
                                            const rt_cross_plan* plan,
                                            rt_cross_allocator* allocator) {
    (void)destination;
    (void)source;
    (void)plan;
    (void)allocator;
    cross_clone_calls++;
    return cross_clone_next_status;
}

// A descriptor that admits a crossing clone: the flag and the callback are one
// statement, so both are present.
static const rt_value_ops cross_clone_ops = {
    .layout =
        {
            .size = sizeof(mock_value),
            .align = _Alignof(mock_value),
            .stride = sizeof(mock_value),
            .flags = RT_VALUE_FLAG_COPY | RT_VALUE_FLAG_CROSS_CLONABLE,
        },
    .move_init = harness_noop_move,
    .copy_init = harness_noop_copy,
    .plan_cross = harness_noop_plan,
    .cross_clone_init = cross_clone_record,
};

int harness_case_cross_clone(void) {
    cross_clone_calls = 0;

    mock_value source_value = {.initialized = 1, .payload = 21};
    mock_value destination_value = {0};
    rt_cross_plan plan = {0};
    rt_cross_allocator allocator = {0};

    // A recoverable refusal from the descriptor passes through the dispatcher
    // unchanged, and the callback ran exactly once.
    cross_clone_next_status = RT_CARRIER_STATUS_CAPACITY;
    REQUIRE(rt_value_cross_clone_init_detached(
                &cross_clone_ops, &destination_value, &source_value, &plan, &allocator) ==
            RT_CARRIER_STATUS_CAPACITY);
    REQUIRE(cross_clone_calls == 1);

    // An OK likewise.
    cross_clone_next_status = RT_CARRIER_STATUS_OK;
    REQUIRE(rt_value_cross_clone_init_detached(
                &cross_clone_ops, &destination_value, &source_value, &plan, &allocator) ==
            RT_CARRIER_STATUS_OK);
    REQUIRE(cross_clone_calls == 2);

    // The dispatcher aborts on a descriptor whose flag/callback pairing does not
    // admit the mode -- a caller protocol violation rather than a recoverable
    // refusal -- so that arm is not exercised in-process here. It is asserted
    // structurally by the slot preflight (slot_control_descriptor_cases.c) and
    // the manifest's frozen biconditional. What THIS case adds is the live proof
    // that a well-formed clone descriptor dispatches to its own body and returns
    // its own status, a refusal and an OK alike.
    REQUIRE(source_value.payload == 21);
    return 0;
}
