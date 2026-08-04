#include "slot_control_harness.h"

#include <limits.h>
#include <stdalign.h>

int harness_case_storage(void) {
    alignas(64) unsigned char storage[128] = {0};
    uintptr_t base = (uintptr_t)&storage[0];
    rt_slot_control source;
    rt_slot_control overlap;
    rt_slot_control adjacent;
    rt_slot_control same_identity;
    rt_slot_control other_operations;

    REQUIRE(
        rt_slot_control_init(&source, 31, &harness_value_ops, 1, base, sizeof(mock_value), 64) ==
        RT_SLOT_CONTROL_OK);
    REQUIRE(rt_slot_control_init(&overlap,
                                 32,
                                 &harness_value_ops,
                                 1,
                                 base + _Alignof(mock_value),
                                 sizeof(mock_value),
                                 _Alignof(mock_value)) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_slot_control_init(&adjacent,
                                 33,
                                 &harness_value_ops,
                                 1,
                                 base + sizeof(mock_value),
                                 sizeof(mock_value),
                                 _Alignof(mock_value)) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_slot_pair_preflight(&source, &overlap) == RT_SLOT_CONTROL_STORAGE_OVERLAP);
    REQUIRE(rt_slot_pair_preflight(&overlap, &source) == RT_SLOT_CONTROL_STORAGE_OVERLAP);
    REQUIRE(rt_slot_pair_preflight(&source, &adjacent) == RT_SLOT_CONTROL_OK);

    REQUIRE(rt_slot_control_init(&same_identity,
                                 31,
                                 &harness_value_ops,
                                 1,
                                 base + 32,
                                 sizeof(mock_value),
                                 _Alignof(mock_value)) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_slot_pair_preflight(&source, &same_identity) == RT_SLOT_CONTROL_LOGICAL_SELF);
    REQUIRE(rt_slot_control_init(&other_operations,
                                 34,
                                 &harness_other_ops,
                                 1,
                                 base + 32,
                                 sizeof(mock_value),
                                 _Alignof(mock_value)) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_slot_pair_preflight(&source, &other_operations) ==
            RT_SLOT_CONTROL_OPERATIONS_MISMATCH);

    rt_slot_control invalid;
    REQUIRE(rt_slot_control_init(&invalid,
                                 35,
                                 &harness_value_ops,
                                 1,
                                 base + 32,
                                 sizeof(mock_value) - 1,
                                 _Alignof(mock_value)) == RT_SLOT_CONTROL_LAYOUT_MISMATCH);
    const rt_value_ops aligned_operations = {
        .layout = {.size = 8, .align = 16, .stride = 16},
    };
    REQUIRE(rt_slot_control_init(&invalid, 36, &aligned_operations, 1, base + 32, 8, 8) ==
            RT_SLOT_CONTROL_LAYOUT_MISMATCH);
    REQUIRE(rt_slot_control_init(&invalid, 36, &aligned_operations, 1, base + 32, 8, 32) ==
            RT_SLOT_CONTROL_OK);
    const rt_value_ops invalid_descriptor = {
        .layout = {.size = 0, .align = 3, .stride = 0},
    };
    REQUIRE(rt_slot_control_init(&invalid, 37, &invalid_descriptor, 1, base, 0, 4) ==
            RT_SLOT_CONTROL_LAYOUT_MISMATCH);
    const rt_value_ops byte_operations = {
        .layout = {.size = 8, .align = 1, .stride = 8},
    };
    REQUIRE(rt_slot_control_init(&invalid, 38, &byte_operations, 1, base, 8, 3) ==
            RT_SLOT_CONTROL_STORAGE_MISALIGNED);
    REQUIRE(rt_slot_control_init(&invalid, 39, &byte_operations, 1, UINTPTR_MAX - 3, 8, 1) ==
            RT_SLOT_CONTROL_STORAGE_OVERFLOW);
    REQUIRE(rt_slot_control_init(&invalid, 0, &harness_value_ops, 1, base, 8, 4) ==
            RT_SLOT_CONTROL_INVALID_ARGUMENT);
    REQUIRE(rt_slot_control_init(&invalid, 40, NULL, 1, base, 8, 4) ==
            RT_SLOT_CONTROL_INVALID_ARGUMENT);

    source.storage_size++;
    REQUIRE(rt_slot_pair_preflight(&source, &adjacent) == RT_SLOT_CONTROL_LAYOUT_MISMATCH);
    source.storage_size--;
    source.storage_alignment = 2;
    REQUIRE(rt_slot_pair_preflight(&source, &adjacent) == RT_SLOT_CONTROL_LAYOUT_MISMATCH);
    return 0;
}

int harness_case_zst(void) {
    alignas(4096) static unsigned char owner_token[4096];
    const uintptr_t address = (uintptr_t)&owner_token[sizeof(owner_token)];
    const size_t alignments[] = {64, 256, 4096};
    const rt_value_ops operations[] = {
        {.layout = {.size = 0, .align = 64, .stride = 0, .flags = RT_VALUE_FLAG_COPY}},
        {.layout = {.size = 0, .align = 256, .stride = 0, .flags = RT_VALUE_FLAG_COPY}},
        {.layout = {.size = 0, .align = 4096, .stride = 0, .flags = RT_VALUE_FLAG_COPY}},
    };
    harness_reset_callbacks();
    for (size_t index = 0; index < sizeof(alignments) / sizeof(alignments[0]); index++) {
        rt_slot_control source;
        rt_slot_control destination;
        uint64_t source_identity = UINT64_C(100) + (uint64_t)(index * 2);
        uint64_t destination_identity = source_identity + 1;
        REQUIRE(
            rt_slot_control_init(
                &source, source_identity, &operations[index], 1, address, 0, alignments[index]) ==
            RT_SLOT_CONTROL_OK);
        REQUIRE(rt_slot_control_init(&destination,
                                     destination_identity,
                                     &operations[index],
                                     1,
                                     address,
                                     0,
                                     alignments[index]) == RT_SLOT_CONTROL_OK);
        REQUIRE(rt_slot_pair_preflight(&source, &destination) == RT_SLOT_CONTROL_OK);

        rt_slot_read_claim registration;
        rt_claim_token token;
        rt_slot_read_claim_init(&registration);
        REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
        REQUIRE(rt_slot_publish_initial_locked(&source, 1) == RT_SLOT_CONTROL_OK);
        REQUIRE(rt_slot_claim_read_locked(
                    &source, &destination, RT_SLOT_CLAIM_COPY, 1, 1, &registration, &token) ==
                RT_SLOT_CONTROL_OK);
        REQUIRE(token.operations == &operations[index]);
        REQUIRE(token.source_identity != token.destination_identity);
        REQUIRE(rt_slot_release_empty_destination_locked(&destination, &token) ==
                RT_SLOT_CONTROL_OK);
        REQUIRE(rt_slot_retire_read_locked(&source, &token) == RT_SLOT_CONTROL_OK);
        REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);
    }
    REQUIRE(harness_callback_calls == 0);
    REQUIRE(harness_payload_accesses == 0);
    return 0;
}
