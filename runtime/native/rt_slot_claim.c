#include "rt_slot_control_internal.h"

static rt_slot_control_status rt_slot_validate_read_begin(const rt_slot_control* source,
                                                          const rt_slot_control* destination,
                                                          rt_slot_claim_kind kind,
                                                          uint64_t source_generation,
                                                          uint64_t destination_generation,
                                                          const rt_slot_read_claim* registration) {
    if (source == NULL || registration == NULL || registration->active != 0 ||
        !rt_slot_kind_is_read(kind)) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    if (rt_slot_kind_has_destination(kind) != (destination != NULL)) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    if (source->generation != source_generation) {
        return RT_SLOT_CONTROL_STALE;
    }
    if (source->slot.state != RT_SLOT_INITIALIZED || source->exclusive_epoch != 0) {
        return RT_SLOT_CONTROL_INVALID_STATE;
    }
    if (source->reader_pins == UINT32_MAX || !rt_slot_epoch_available(source)) {
        return RT_SLOT_CONTROL_EPOCH_EXHAUSTED;
    }
    if (destination == NULL) {
        return RT_SLOT_CONTROL_OK;
    }
    if (destination->generation != destination_generation) {
        return RT_SLOT_CONTROL_STALE;
    }
    rt_slot_control_status status = rt_slot_pair_preflight(source, destination);
    if (status != RT_SLOT_CONTROL_OK) {
        return status;
    }
    if (!rt_slot_destination_available(destination)) {
        return RT_SLOT_CONTROL_BUSY;
    }
    if (!rt_slot_epoch_available(destination)) {
        return RT_SLOT_CONTROL_EPOCH_EXHAUSTED;
    }
    return RT_SLOT_CONTROL_OK;
}

rt_slot_control_status rt_slot_claim_read_locked(rt_slot_control* source,
                                                 rt_slot_control* destination,
                                                 rt_slot_claim_kind kind,
                                                 uint64_t source_generation,
                                                 uint64_t destination_generation,
                                                 rt_slot_read_claim* registration,
                                                 rt_claim_token* out_token) {
    if (out_token == NULL) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    *out_token = (rt_claim_token){0};
    rt_slot_control_status status = rt_slot_validate_read_begin(
        source, destination, kind, source_generation, destination_generation, registration);
    if (status != RT_SLOT_CONTROL_OK) {
        return status;
    }

    uint64_t source_epoch = rt_slot_take_epoch(source);
    uint64_t destination_epoch = 0;
    registration->active = 1;
    registration->epoch = source_epoch;
    registration->next = source->readers;
    source->readers = registration;
    source->reader_pins++;

    if (destination != NULL) {
        destination_epoch = rt_slot_take_epoch(destination);
        rt_slot_reserve_destination(destination,
                                    source,
                                    kind,
                                    RT_SLOT_RESERVATION_FALLIBLE,
                                    source_epoch,
                                    destination_epoch);
    }
    registration->kind = kind;
    registration->has_destination = destination != NULL ? 1 : 0;
    registration->destination_identity = destination != NULL ? destination->logical_identity : 0;
    registration->destination_generation = destination != NULL ? destination->generation : 0;
    registration->destination_epoch = destination_epoch;
    rt_slot_fill_token(out_token, source, destination, kind, source_epoch, destination_epoch);
    return RT_SLOT_CONTROL_OK;
}

rt_slot_control_status rt_slot_retire_read_locked(rt_slot_control* source,
                                                  const rt_claim_token* token) {
    if (source == NULL || token == NULL || !rt_slot_kind_is_read(token->kind)) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    if (token->source_identity != source->logical_identity ||
        token->operations != source->operations) {
        return RT_SLOT_CONTROL_INVALID_TOKEN;
    }
    if (token->source_generation != source->generation) {
        return RT_SLOT_CONTROL_STALE;
    }
    if (source->slot.state != RT_SLOT_INITIALIZED) {
        return RT_SLOT_CONTROL_INVALID_STATE;
    }

    rt_slot_read_claim** cursor = &source->readers;
    while (*cursor != NULL && (*cursor)->epoch != token->source_epoch) {
        cursor = &(*cursor)->next;
    }
    if (*cursor == NULL) {
        return RT_SLOT_CONTROL_INVALID_TOKEN;
    }
    rt_slot_read_claim* registration = *cursor;
    if (registration->active == 0 || source->reader_pins == 0) {
        return RT_SLOT_CONTROL_INVARIANT;
    }
    if (registration->kind != token->kind ||
        registration->has_destination != token->has_destination ||
        registration->destination_identity != token->destination_identity ||
        registration->destination_generation != token->destination_generation ||
        registration->destination_epoch != token->destination_epoch) {
        return RT_SLOT_CONTROL_INVALID_TOKEN;
    }
    *cursor = registration->next;
    *registration = (rt_slot_read_claim){0};
    source->reader_pins--;
    return RT_SLOT_CONTROL_OK;
}

static rt_slot_control_status
rt_slot_validate_fallible_destination(const rt_slot_control* destination,
                                      const rt_claim_token* token) {
    if (!rt_slot_token_destination_matches(destination, token)) {
        return RT_SLOT_CONTROL_INVALID_TOKEN;
    }
    if (destination->slot.state != RT_SLOT_EMPTY) {
        return RT_SLOT_CONTROL_INVARIANT;
    }
    if (destination->reservation_phase != RT_SLOT_RESERVATION_FALLIBLE) {
        return RT_SLOT_CONTROL_INVALID_STATE;
    }
    return RT_SLOT_CONTROL_OK;
}

rt_slot_control_status rt_slot_publish_destination_locked(rt_slot_control* destination,
                                                          const rt_claim_token* token) {
    rt_slot_control_status status = rt_slot_validate_fallible_destination(destination, token);
    if (status != RT_SLOT_CONTROL_OK) {
        return status;
    }
    destination->slot.state = RT_SLOT_INITIALIZED;
    rt_slot_clear_reservation(destination);
    return RT_SLOT_CONTROL_OK;
}

rt_slot_control_status rt_slot_reject_initialized_destination_locked(rt_slot_control* destination,
                                                                     const rt_claim_token* token) {
    rt_slot_control_status status = rt_slot_validate_fallible_destination(destination, token);
    if (status != RT_SLOT_CONTROL_OK) {
        return status;
    }
    destination->reservation_phase = RT_SLOT_RESERVATION_CLEANUP;
    return RT_SLOT_CONTROL_OK;
}

rt_slot_control_status rt_slot_finish_destination_cleanup_locked(rt_slot_control* destination,
                                                                 const rt_claim_token* token) {
    if (!rt_slot_token_destination_matches(destination, token)) {
        return RT_SLOT_CONTROL_INVALID_TOKEN;
    }
    if (destination->slot.state != RT_SLOT_EMPTY ||
        destination->reservation_phase != RT_SLOT_RESERVATION_CLEANUP) {
        return RT_SLOT_CONTROL_INVALID_STATE;
    }
    rt_slot_clear_reservation(destination);
    return RT_SLOT_CONTROL_OK;
}

rt_slot_control_status rt_slot_release_empty_destination_locked(rt_slot_control* destination,
                                                                const rt_claim_token* token) {
    rt_slot_control_status status = rt_slot_validate_fallible_destination(destination, token);
    if (status != RT_SLOT_CONTROL_OK) {
        return status;
    }
    rt_slot_clear_reservation(destination);
    return RT_SLOT_CONTROL_OK;
}
