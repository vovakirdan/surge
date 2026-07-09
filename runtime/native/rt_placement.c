#include "rt_placement.h"
#include "rt_async_internal.h"

#include <limits.h>
#include <stdatomic.h>

static const uint32_t rt_placement_no_shard = UINT32_MAX;

static rt_placement rt_placement_encode(uint8_t kind, uint64_t payload) {
    return (payload << RT_PLACEMENT_KIND_BITS) | (uint64_t)kind;
}

rt_placement rt_placement_pool(void) {
    return rt_placement_encode((uint8_t)RT_PLACEMENT_KIND_POOL, 0);
}

rt_placement rt_placement_distributed(void) {
    return rt_placement_encode((uint8_t)RT_PLACEMENT_KIND_DISTRIBUTED, 0);
}

rt_placement rt_placement_shard(uint32_t shard_id) {
    return rt_placement_encode((uint8_t)RT_PLACEMENT_KIND_SHARD, shard_id);
}

uint8_t rt_placement_kind_of(rt_placement placement) {
    return (uint8_t)(placement & RT_PLACEMENT_KIND_MASK);
}

uint64_t rt_placement_payload(rt_placement placement) {
    return placement >> RT_PLACEMENT_KIND_BITS;
}

static void rt_placement_counter_add(_Atomic uint64_t* counter) {
    (void)atomic_fetch_add_explicit(counter, UINT64_C(1), memory_order_relaxed);
}

static rt_placement_resolution rt_placement_resolution_make(rt_placement_status status,
                                                            uint8_t kind,
                                                            uint64_t payload,
                                                            uint32_t shard_id) {
    rt_placement_resolution out = {
        .status = status,
        .shard_id = shard_id,
        .kind = kind,
        ._pad = {0, 0, 0},
        .payload = payload,
    };
    return out;
}

static rt_placement_resolution
rt_placement_unsupported(rt_runtime* runtime, uint8_t kind, uint64_t payload) {
    rt_placement_counter_add(&runtime->placement_debug.unsupported_resolutions);
    return rt_placement_resolution_make(
        RT_PLACEMENT_STATUS_UNSUPPORTED, kind, payload, rt_placement_no_shard);
}

rt_placement_resolution
rt_placement_resolve(rt_runtime* runtime, rt_placement placement, uint32_t current_shard_id) {
    if (runtime == NULL || runtime->shard_count < 1 || runtime->shard_count > UINT32_MAX) {
        return rt_placement_resolution_make(RT_PLACEMENT_STATUS_INVALID_ARGUMENT,
                                            rt_placement_kind_of(placement),
                                            rt_placement_payload(placement),
                                            rt_placement_no_shard);
    }

    uint32_t shard_count = (uint32_t)runtime->shard_count;
    uint8_t kind = rt_placement_kind_of(placement);
    uint64_t payload = rt_placement_payload(placement);
    rt_placement_counter_add(&runtime->placement_debug.resolve_attempts);

    switch ((rt_placement_kind)kind) {
        case RT_PLACEMENT_KIND_SHARD:
            if (payload >= (uint64_t)shard_count) {
                rt_placement_counter_add(&runtime->placement_debug.invalid_shard_resolutions);
                return rt_placement_resolution_make(
                    RT_PLACEMENT_STATUS_INVALID_SHARD, kind, payload, rt_placement_no_shard);
            }
            rt_placement_counter_add(&runtime->placement_debug.exact_shard_resolutions);
            return rt_placement_resolution_make(
                RT_PLACEMENT_STATUS_OK, kind, payload, (uint32_t)payload);

        case RT_PLACEMENT_KIND_DISTRIBUTED: {
            if (current_shard_id >= shard_count) {
                return rt_placement_resolution_make(
                    RT_PLACEMENT_STATUS_INVALID_ARGUMENT, kind, payload, rt_placement_no_shard);
            }
            uint64_t ticket = atomic_fetch_add_explicit(
                &runtime->placement_rr_next, UINT64_C(1), memory_order_relaxed);
            uint32_t selected = (uint32_t)(ticket % (uint64_t)shard_count);
            if (shard_count > 1 && selected == current_shard_id) {
                selected = (uint32_t)((current_shard_id + UINT32_C(1)) % shard_count);
            }
            rt_placement_counter_add(&runtime->placement_debug.distributed_resolutions);
            if (selected != current_shard_id) {
                rt_placement_counter_add(
                    &runtime->placement_debug.distributed_non_caller_resolutions);
            }
            return rt_placement_resolution_make(RT_PLACEMENT_STATUS_OK, kind, payload, selected);
        }

        case RT_PLACEMENT_KIND_POOL:
        default:
            return rt_placement_unsupported(runtime, kind, payload);
    }
}

struct rt_placement_debug_snapshot rt_placement_debug_snapshot(const rt_runtime* runtime) {
    struct rt_placement_debug_snapshot snapshot = {0};
    if (runtime == NULL) {
        return snapshot;
    }
    snapshot.resolve_attempts =
        atomic_load_explicit(&runtime->placement_debug.resolve_attempts, memory_order_relaxed);
    snapshot.exact_shard_resolutions = atomic_load_explicit(
        &runtime->placement_debug.exact_shard_resolutions, memory_order_relaxed);
    snapshot.distributed_resolutions = atomic_load_explicit(
        &runtime->placement_debug.distributed_resolutions, memory_order_relaxed);
    snapshot.distributed_non_caller_resolutions = atomic_load_explicit(
        &runtime->placement_debug.distributed_non_caller_resolutions, memory_order_relaxed);
    snapshot.invalid_shard_resolutions = atomic_load_explicit(
        &runtime->placement_debug.invalid_shard_resolutions, memory_order_relaxed);
    snapshot.unsupported_resolutions = atomic_load_explicit(
        &runtime->placement_debug.unsupported_resolutions, memory_order_relaxed);
    return snapshot;
}

void rt_placement_debug_reset(rt_runtime* runtime) {
    if (runtime == NULL) {
        return;
    }
    atomic_store_explicit(&runtime->placement_rr_next, UINT64_C(0), memory_order_relaxed);
    atomic_store_explicit(
        &runtime->placement_debug.resolve_attempts, UINT64_C(0), memory_order_relaxed);
    atomic_store_explicit(
        &runtime->placement_debug.exact_shard_resolutions, UINT64_C(0), memory_order_relaxed);
    atomic_store_explicit(
        &runtime->placement_debug.distributed_resolutions, UINT64_C(0), memory_order_relaxed);
    atomic_store_explicit(&runtime->placement_debug.distributed_non_caller_resolutions,
                          UINT64_C(0),
                          memory_order_relaxed);
    atomic_store_explicit(
        &runtime->placement_debug.invalid_shard_resolutions, UINT64_C(0), memory_order_relaxed);
    atomic_store_explicit(
        &runtime->placement_debug.unsupported_resolutions, UINT64_C(0), memory_order_relaxed);
}
