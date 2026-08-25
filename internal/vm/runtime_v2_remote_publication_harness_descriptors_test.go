//go:build runtime_v2_pending

package vm_test

// The stand's own DESCRIPTORS. A select arm's payload is moved and destroyed
// through the descriptor its type names, so a stand that wants to observe
// either has to supply one -- these are what `__surge_value_ops_for` answers
// with, in place of the numeric drop ids the arm table used to carry.
const remotePublicationHarnessDescriptors = `
// What a crossing hands the runtime as its shipped STATE, and what the runtime
// frees at this exact width when the crossing is abandoned. Every row that
// ships a droppable state allocates one, because a state box is a real
// allocation in every compiled program and the runtime gives it back.
static void remote_state_move(void* destination, void* source) {
    memcpy(destination, source, sizeof(remote_child_state));
    memset(source, 0, sizeof(remote_child_state));
}

static void remote_state_drop(void* value) {
    if (value != drop_expected_state) {
        fputs("a shipped state was destroyed at an address no row published\n", stderr);
        exit(97);
    }
    atomic_fetch_add_explicit(&drop_calls, 1, memory_order_acq_rel);
}

// The stand's own payload descriptor: a word whose destruction is COUNTED.
//
// A select arm's payload is destroyed through the descriptor its type names,
// so a stand that wants to observe that destruction has to supply one. The
// drop below is what payload_drop_calls counts, in place of the numeric drop
// id the arm table used to carry beside the value.
static void select_payload_move(void* destination, void* source) {
    *(uint64_t*)destination = *(uint64_t*)source;
    *(uint64_t*)source = 0;
}

static void select_payload_drop(void* value) {
    (void)value;
    atomic_fetch_add_explicit(&payload_drop_calls, 1, memory_order_acq_rel);
}

// The same descriptor one word WIDER, for the rows that prove an arm carries a
// value at its own width rather than at a pointer's.
static void wide_payload_move(void* destination, void* source) {
    uint64_t* dst = (uint64_t*)destination;
    uint64_t* src = (uint64_t*)source;
    dst[0] = src[0];
    dst[1] = src[1];
    src[0] = 0;
    src[1] = 0;
}

static void wide_payload_drop(void* value) {
    (void)value;
    atomic_fetch_add_explicit(&payload_drop_calls, 1, memory_order_acq_rel);
}

static rt_carrier_status
select_payload_plan(const void* source, rt_cross_mode mode, rt_cross_plan* out) {
    (void)source;
    (void)mode;
    (void)out;
    return RT_CARRIER_STATUS_INVALID_STATE;
}

// The compiler emits this lookup for real programs; a stand defines it to give
// its own types descriptors. Only the select payload has one -- every other id
// answers NULL, which the runtime reads as "carried as an opaque word".
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
const rt_value_ops* __surge_value_ops_for(uint64_t type_id);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
const rt_value_ops* __surge_value_ops_for(uint64_t type_id) {
    static const rt_value_ops payload_ops = {
        .layout = {.size = sizeof(uint64_t),
                   .align = _Alignof(uint64_t),
                   .stride = sizeof(uint64_t),
                   .flags = RT_VALUE_FLAG_DROPPABLE},
        .move_init = select_payload_move,
        .copy_init = NULL,
        .clone_init = NULL,
        .drop_in_place = select_payload_drop,
        .trace = NULL,
        .plan_cross = select_payload_plan,
        .cross_move_init = NULL,
        .cross_clone_init = NULL,
    };
    static const rt_value_ops wide_ops = {
        .layout = {.size = 2 * sizeof(uint64_t),
                   .align = _Alignof(uint64_t),
                   .stride = 2 * sizeof(uint64_t),
                   .flags = RT_VALUE_FLAG_DROPPABLE},
        .move_init = wide_payload_move,
        .copy_init = NULL,
        .clone_init = NULL,
        .drop_in_place = wide_payload_drop,
        .trace = NULL,
        .plan_cross = select_payload_plan,
        .cross_move_init = NULL,
        .cross_clone_init = NULL,
    };
    // A SHIPPED STATE's descriptor. The runtime destroys an abandoned state
    // box through the type it names -- members by drop_in_place, then the
    // block itself -- so the stand supplies a type whose drop is COUNTED and
    // whose size matches the block it hands over.
    static const rt_value_ops state_ops = {
        .layout = {.size = sizeof(remote_child_state),
                   .align = _Alignof(remote_child_state),
                   .stride = sizeof(remote_child_state),
                   .flags = RT_VALUE_FLAG_DROPPABLE},
        .move_init = remote_state_move,
        .copy_init = NULL,
        .clone_init = NULL,
        .drop_in_place = remote_state_drop,
        .trace = NULL,
        .plan_cross = select_payload_plan,
        .cross_move_init = NULL,
        .cross_clone_init = NULL,
    };
    if (type_id == DROP_SELECT_PAYLOAD) {
        return &payload_ops;
    }
    if (type_id == DROP_REMOTE_STATE) {
        return &state_ops;
    }
    return type_id == WIDE_SELECT_PAYLOAD ? &wide_ops : NULL;
}

`
