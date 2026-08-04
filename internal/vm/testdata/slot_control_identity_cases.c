#include "slot_control_harness.h"

#include <string.h>

static int harness_tokens_have_same_scalar_identity(const rt_claim_token* left,
                                                    const rt_claim_token* right) {
    return left->operations == right->operations &&
           left->source_identity == right->source_identity &&
           left->source_generation == right->source_generation &&
           left->source_epoch == right->source_epoch &&
           left->destination_identity == right->destination_identity &&
           left->destination_generation == right->destination_generation &&
           left->destination_epoch == right->destination_epoch && left->kind == right->kind &&
           left->has_destination == right->has_destination;
}

static int harness_case_read_identity(void) {
    mock_value source_values[2] = {
        {.initialized = 1, .payload = 81},
        {.initialized = 1, .payload = 82},
    };
    mock_value destination_values[2] = {0};
    rt_slot_control sources[2];
    rt_slot_control destinations[2];
    rt_slot_read_claim registrations[2];
    rt_claim_token tokens[2];
    for (size_t index = 0; index < 2; index++) {
        REQUIRE(harness_init_slot(&sources[index], 800, 1, &source_values[index]) ==
                RT_SLOT_CONTROL_OK);
        REQUIRE(harness_init_slot(&destinations[index], 801, 1, &destination_values[index]) ==
                RT_SLOT_CONTROL_OK);
        rt_slot_read_claim_init(&registrations[index]);
    }

    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    for (size_t index = 0; index < 2; index++) {
        REQUIRE(rt_slot_publish_initial_locked(&sources[index], 1) == RT_SLOT_CONTROL_OK);
        REQUIRE(rt_slot_claim_read_locked(&sources[index],
                                          &destinations[index],
                                          RT_SLOT_CLAIM_COPY,
                                          1,
                                          1,
                                          &registrations[index],
                                          &tokens[index]) == RT_SLOT_CONTROL_OK);
    }
    REQUIRE(harness_tokens_have_same_scalar_identity(&tokens[0], &tokens[1]));
    REQUIRE(tokens[0].source_control != tokens[1].source_control);
    REQUIRE(tokens[0].destination_control != tokens[1].destination_control);

    unsigned char source_before[sizeof(sources[1])];
    unsigned char destination_before[sizeof(destinations[1])];
    memcpy(source_before, &sources[1], sizeof(sources[1]));
    memcpy(destination_before, &destinations[1], sizeof(destinations[1]));
    REQUIRE(rt_slot_publish_destination_locked(&destinations[1], &tokens[0]) ==
            RT_SLOT_CONTROL_INVALID_TOKEN);
    REQUIRE(rt_slot_retire_read_locked(&sources[1], &tokens[0]) == RT_SLOT_CONTROL_INVALID_TOKEN);
    REQUIRE(memcmp(source_before, &sources[1], sizeof(sources[1])) == 0);
    REQUIRE(memcmp(destination_before, &destinations[1], sizeof(destinations[1])) == 0);

    for (size_t index = 0; index < 2; index++) {
        REQUIRE(rt_slot_release_empty_destination_locked(&destinations[index], &tokens[index]) ==
                RT_SLOT_CONTROL_OK);
        REQUIRE(rt_slot_retire_read_locked(&sources[index], &tokens[index]) == RT_SLOT_CONTROL_OK);
        REQUIRE(registrations[index].source_control == NULL &&
                registrations[index].destination_control == NULL);
        REQUIRE(rt_slot_release_empty_destination_locked(&destinations[index], &tokens[index]) ==
                RT_SLOT_CONTROL_INVALID_TOKEN);
        REQUIRE(rt_slot_retire_read_locked(&sources[index], &tokens[index]) ==
                RT_SLOT_CONTROL_INVALID_TOKEN);
    }
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);
    return 0;
}

static int harness_case_exclusive_identity(void) {
    mock_value source_values[2] = {
        {.initialized = 1, .payload = 91},
        {.initialized = 1, .payload = 92},
    };
    mock_value destination_values[2] = {0};
    rt_slot_control sources[2];
    rt_slot_control destinations[2];
    rt_claim_token tokens[2];
    for (size_t index = 0; index < 2; index++) {
        REQUIRE(harness_init_slot(&sources[index], 810, 1, &source_values[index]) ==
                RT_SLOT_CONTROL_OK);
        REQUIRE(harness_init_slot(&destinations[index], 811, 1, &destination_values[index]) ==
                RT_SLOT_CONTROL_OK);
    }

    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    for (size_t index = 0; index < 2; index++) {
        REQUIRE(rt_slot_publish_initial_locked(&sources[index], 1) == RT_SLOT_CONTROL_OK);
        REQUIRE(
            rt_slot_claim_exclusive_locked(
                &sources[index], &destinations[index], RT_SLOT_CLAIM_MOVE, 1, 1, &tokens[index]) ==
            RT_SLOT_CONTROL_OK);
    }
    REQUIRE(harness_tokens_have_same_scalar_identity(&tokens[0], &tokens[1]));
    REQUIRE(tokens[0].source_control != tokens[1].source_control);
    REQUIRE(tokens[0].destination_control != tokens[1].destination_control);

    unsigned char source_before[sizeof(sources[1])];
    unsigned char destination_before[sizeof(destinations[1])];
    memcpy(source_before, &sources[1], sizeof(sources[1]));
    memcpy(destination_before, &destinations[1], sizeof(destinations[1]));
    REQUIRE(rt_slot_commit_move_locked(&sources[1], &destinations[1], &tokens[0]) ==
            RT_SLOT_CONTROL_INVARIANT);
    REQUIRE(memcmp(source_before, &sources[1], sizeof(sources[1])) == 0);
    REQUIRE(memcmp(destination_before, &destinations[1], sizeof(destinations[1])) == 0);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);

    harness_reset_callbacks();
    for (size_t index = 0; index < 2; index++) {
        harness_value_ops.move_init(&destination_values[index], &source_values[index]);
    }
    REQUIRE(pthread_mutex_lock(&harness_owner_lock) == 0);
    for (size_t index = 0; index < 2; index++) {
        REQUIRE(rt_slot_commit_move_locked(&sources[index], &destinations[index], &tokens[index]) ==
                RT_SLOT_CONTROL_OK);
        REQUIRE(destinations[index].reservation_source_control == NULL &&
                destinations[index].reservation_destination_control == NULL);
        REQUIRE(rt_slot_commit_move_locked(&sources[index], &destinations[index], &tokens[index]) ==
                RT_SLOT_CONTROL_INVARIANT);
    }
    REQUIRE(rt_slot_begin_generation_locked(&sources[0],
                                            2,
                                            (uintptr_t)&source_values[0],
                                            sizeof(source_values[0]),
                                            _Alignof(mock_value)) == RT_SLOT_CONTROL_OK);
    REQUIRE(sources[0].reservation_source_control == NULL &&
            sources[0].reservation_destination_control == NULL);
    REQUIRE(pthread_mutex_unlock(&harness_owner_lock) == 0);
    REQUIRE(harness_callback_error == 0 && harness_callback_calls == 2);
    REQUIRE(destination_values[0].payload == 91 && destination_values[1].payload == 92);
    return 0;
}

int harness_case_identity(void) {
    REQUIRE(harness_case_read_identity() == 0);
    REQUIRE(harness_case_exclusive_identity() == 0);
    return 0;
}
