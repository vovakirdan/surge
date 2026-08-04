#ifndef SURGE_RUNTIME_NATIVE_RT_SLOT_CONTROL_INTERNAL_H
#define SURGE_RUNTIME_NATIVE_RT_SLOT_CONTROL_INTERNAL_H

#include "rt_slot_control.h"

#include <limits.h>

static inline int rt_slot_kind_is_read(rt_slot_claim_kind kind) {
    return kind == RT_SLOT_CLAIM_COPY || kind == RT_SLOT_CLAIM_CLONE ||
           kind == RT_SLOT_CLAIM_TRACE || kind == RT_SLOT_CLAIM_CROSS_CLONE;
}

static inline int rt_slot_kind_is_exclusive(rt_slot_claim_kind kind) {
    return kind == RT_SLOT_CLAIM_MOVE || kind == RT_SLOT_CLAIM_DROP ||
           kind == RT_SLOT_CLAIM_CROSS_MOVE;
}

static inline int rt_slot_kind_has_destination(rt_slot_claim_kind kind) {
    return kind == RT_SLOT_CLAIM_COPY || kind == RT_SLOT_CLAIM_CLONE ||
           kind == RT_SLOT_CLAIM_MOVE || kind == RT_SLOT_CLAIM_CROSS_MOVE ||
           kind == RT_SLOT_CLAIM_CROSS_CLONE;
}

static inline int rt_slot_epoch_available(const rt_slot_control* control) {
    return control != NULL && control->next_epoch != UINT64_MAX;
}

static inline uint64_t rt_slot_take_epoch(rt_slot_control* control) {
    control->next_epoch++;
    return control->next_epoch;
}

static inline void rt_slot_fill_token(rt_claim_token* token,
                                      const rt_slot_control* source,
                                      const rt_slot_control* destination,
                                      rt_slot_claim_kind kind,
                                      uint64_t source_epoch,
                                      uint64_t destination_epoch) {
    *token = (rt_claim_token){
        .operations = source->operations,
        .source_identity = source->logical_identity,
        .source_generation = source->generation,
        .source_epoch = source_epoch,
        .destination_identity = destination != NULL ? destination->logical_identity : 0,
        .destination_generation = destination != NULL ? destination->generation : 0,
        .destination_epoch = destination_epoch,
        .kind = kind,
        .has_destination = destination != NULL ? 1 : 0,
    };
}

static inline int rt_slot_destination_available(const rt_slot_control* destination) {
    return destination != NULL && destination->slot.state == RT_SLOT_EMPTY &&
           destination->reader_pins == 0 && destination->readers == NULL &&
           destination->exclusive_epoch == 0 &&
           destination->reservation_phase == RT_SLOT_RESERVATION_NONE;
}

static inline void rt_slot_reserve_destination(rt_slot_control* destination,
                                               const rt_slot_control* source,
                                               rt_slot_claim_kind kind,
                                               rt_slot_reservation_phase phase,
                                               uint64_t source_epoch,
                                               uint64_t destination_epoch) {
    destination->reservation_kind = kind;
    destination->reservation_phase = phase;
    destination->reservation_source_identity = source->logical_identity;
    destination->reservation_source_generation = source->generation;
    destination->reservation_source_epoch = source_epoch;
    destination->reservation_epoch = destination_epoch;
}

static inline void rt_slot_clear_reservation(rt_slot_control* destination) {
    destination->reservation_kind = RT_SLOT_CLAIM_INVALID;
    destination->reservation_phase = RT_SLOT_RESERVATION_NONE;
    destination->reservation_source_identity = 0;
    destination->reservation_source_generation = 0;
    destination->reservation_source_epoch = 0;
    destination->reservation_epoch = 0;
}

static inline int rt_slot_token_destination_matches(const rt_slot_control* destination,
                                                    const rt_claim_token* token) {
    return destination != NULL && token != NULL && token->has_destination == 1 &&
           token->source_identity == destination->reservation_source_identity &&
           token->source_generation == destination->reservation_source_generation &&
           token->source_epoch == destination->reservation_source_epoch &&
           token->destination_identity == destination->logical_identity &&
           token->destination_generation == destination->generation &&
           token->destination_epoch != 0 &&
           token->destination_epoch == destination->reservation_epoch &&
           token->operations == destination->operations &&
           token->kind == destination->reservation_kind;
}

static inline int rt_slot_token_has_no_destination(const rt_claim_token* token) {
    return token != NULL && token->has_destination == 0 && token->destination_identity == 0 &&
           token->destination_generation == 0 && token->destination_epoch == 0;
}

static inline int rt_slot_token_source_matches(const rt_slot_control* source,
                                               const rt_claim_token* token) {
    return source != NULL && token != NULL && token->source_identity == source->logical_identity &&
           token->source_generation == source->generation && token->source_epoch != 0 &&
           token->operations == source->operations;
}

#endif
