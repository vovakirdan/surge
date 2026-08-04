#include "rt_slot_control_internal.h"

static rt_slot_control_status rt_slot_validate_exclusive_begin(const rt_slot_control* source,
                                                               const rt_slot_control* destination,
                                                               rt_slot_claim_kind kind,
                                                               uint64_t source_generation,
                                                               uint64_t destination_generation) {
    if (source == NULL || !rt_slot_kind_is_exclusive(kind)) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    if (rt_slot_kind_has_destination(kind) != (destination != NULL)) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    if (source->generation != source_generation) {
        return RT_SLOT_CONTROL_STALE;
    }
    if (source->slot.state != RT_SLOT_INITIALIZED) {
        return RT_SLOT_CONTROL_INVALID_STATE;
    }
    if (source->reader_pins != 0 || source->readers != NULL || source->exclusive_epoch != 0) {
        return RT_SLOT_CONTROL_BUSY;
    }
    if (!rt_slot_epoch_available(source)) {
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

rt_slot_control_status rt_slot_claim_exclusive_locked(rt_slot_control* source,
                                                      rt_slot_control* destination,
                                                      rt_slot_claim_kind kind,
                                                      uint64_t source_generation,
                                                      uint64_t destination_generation,
                                                      rt_claim_token* out_token) {
    if (out_token == NULL) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    *out_token = (rt_claim_token){0};
    rt_slot_control_status status = rt_slot_validate_exclusive_begin(
        source, destination, kind, source_generation, destination_generation);
    if (status != RT_SLOT_CONTROL_OK) {
        return status;
    }

    uint64_t source_epoch = rt_slot_take_epoch(source);
    uint64_t destination_epoch = 0;
    source->slot.state = RT_SLOT_CLAIMED;
    source->exclusive_epoch = source_epoch;
    source->exclusive_kind = kind;
    if (destination != NULL) {
        destination_epoch = rt_slot_take_epoch(destination);
        rt_slot_reserve_destination(destination,
                                    source,
                                    kind,
                                    RT_SLOT_RESERVATION_IRREVOCABLE,
                                    source_epoch,
                                    destination_epoch);
    }
    rt_slot_fill_token(out_token, source, destination, kind, source_epoch, destination_epoch);
    return RT_SLOT_CONTROL_OK;
}

static int rt_slot_exclusive_source_matches(const rt_slot_control* source,
                                            const rt_claim_token* token,
                                            rt_slot_claim_kind kind) {
    return rt_slot_token_source_matches(source, token) && token->kind == kind &&
           source->slot.state == RT_SLOT_CLAIMED &&
           source->exclusive_epoch == token->source_epoch && source->exclusive_kind == kind &&
           source->reader_pins == 0 && source->readers == NULL;
}

static int rt_slot_exclusive_destination_matches(const rt_slot_control* destination,
                                                 const rt_claim_token* token,
                                                 rt_slot_claim_kind kind) {
    return rt_slot_token_destination_matches(destination, token) && token->kind == kind &&
           destination->slot.state == RT_SLOT_EMPTY &&
           destination->reservation_phase == RT_SLOT_RESERVATION_IRREVOCABLE;
}

static void rt_slot_clear_exclusive(rt_slot_control* source) {
    source->exclusive_epoch = 0;
    source->exclusive_kind = RT_SLOT_CLAIM_INVALID;
}

rt_slot_control_status rt_slot_commit_move_locked(rt_slot_control* source,
                                                  rt_slot_control* destination,
                                                  const rt_claim_token* token) {
    if (token == NULL ||
        (token->kind != RT_SLOT_CLAIM_MOVE && token->kind != RT_SLOT_CLAIM_CROSS_MOVE) ||
        !rt_slot_exclusive_source_matches(source, token, token->kind) ||
        !rt_slot_exclusive_destination_matches(destination, token, token->kind)) {
        return RT_SLOT_CONTROL_INVARIANT;
    }
    destination->slot.state = RT_SLOT_INITIALIZED;
    rt_slot_clear_reservation(destination);
    rt_slot_clear_exclusive(source);
    source->slot.state = RT_SLOT_MOVED;
    return RT_SLOT_CONTROL_OK;
}

rt_slot_control_status rt_slot_commit_drop_locked(rt_slot_control* source,
                                                  const rt_claim_token* token) {
    if (token == NULL || token->kind != RT_SLOT_CLAIM_DROP ||
        !rt_slot_token_has_no_destination(token) ||
        !rt_slot_exclusive_source_matches(source, token, RT_SLOT_CLAIM_DROP)) {
        return RT_SLOT_CONTROL_INVARIANT;
    }
    rt_slot_clear_exclusive(source);
    source->slot.state = RT_SLOT_DROPPED;
    return RT_SLOT_CONTROL_OK;
}

rt_slot_control_status rt_slot_cross_move_failed_locked(rt_slot_control* source,
                                                        rt_slot_control* destination,
                                                        const rt_claim_token* token,
                                                        int source_unchanged,
                                                        int destination_empty) {
    if (token == NULL || token->kind != RT_SLOT_CLAIM_CROSS_MOVE || source_unchanged == 0 ||
        destination_empty == 0 ||
        !rt_slot_exclusive_source_matches(source, token, RT_SLOT_CLAIM_CROSS_MOVE) ||
        !rt_slot_exclusive_destination_matches(destination, token, RT_SLOT_CLAIM_CROSS_MOVE)) {
        return RT_SLOT_CONTROL_INVARIANT;
    }
    rt_slot_clear_reservation(destination);
    rt_slot_clear_exclusive(source);
    source->slot.state = RT_SLOT_INITIALIZED;
    return RT_SLOT_CONTROL_OK;
}
