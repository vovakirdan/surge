#include "rt_far_channel.h"
#include "rt_placement.h"
#include "rt_remote_spawn_internal.h"
#include "rt_remote_task_internal.h"

#include <string.h>

// Far-channel handle registry: live object records for channels exported
// across shards. The token shape (owner_shard + kind + id + generation) is
// shared with far tasks; the lifetime model is not — entries are explicitly
// released (caller handle free, owner teardown), stay live while operations
// are in flight, and never couple with task-lease refcounts. Teardown order:
// stop accepting new crossings -> drain in-flight -> invalidate generations
// -> reclaim.
//
// The two crossings that reach this registry through the transport -- create
// and share, with their dispatchers -- live in rt_far_channel_crossing.c;
// they mint through the public entry points below and nothing private
// crosses the seam.

// One lease per holder. The generation is globally unique (the shared
// request allocator never repeats), so the generation IS the lease
// identity: sibling tokens carry the same entry id with distinct
// generations, and a double release of one sibling is exactly
// stale-detectable without touching the others. The entry lives while any
// lease is active or any anchored block holds a pin; a released lease
// stops new work for ITS token only.
typedef struct rt_far_channel_lease {
    uint64_t generation;
    uint8_t released;
    struct rt_far_channel_lease* next;
} rt_far_channel_lease;

typedef struct rt_far_channel_entry {
    void* channel;
    uint64_t id;
    uint32_t owner_shard_id;
    uint32_t active_leases;
    rt_far_channel_lease* leases;
    _Atomic uint32_t inflight;
    struct rt_far_channel_entry* next;
} rt_far_channel_entry;

static rt_far_channel_lease* lease_new(uint64_t generation) {
    rt_far_channel_lease* lease =
        (rt_far_channel_lease*)rt_alloc(sizeof(*lease), _Alignof(rt_far_channel_lease));
    if (lease == NULL) {
        return NULL;
    }
    memset(lease, 0, sizeof(*lease));
    lease->generation = generation;
    return lease;
}

struct rt_far_channel_state {
    rt_executor* executor;
    pthread_mutex_t lock;
    rt_far_channel_entry* head;
};

rt_far_channel_state* rt_far_channel_state_get(rt_executor* ex) {
    return ex != NULL ? ex->far_channels : NULL;
}

rt_runtime_status rt_far_channel_state_init(rt_executor* ex) {
    if (ex == NULL || ex->far_channels != NULL) {
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    rt_far_channel_state* state =
        (rt_far_channel_state*)rt_alloc(sizeof(*state), _Alignof(rt_far_channel_state));
    if (state == NULL) {
        return RT_RUNTIME_STATUS_ALLOCATION_FAILED;
    }
    memset(state, 0, sizeof(*state));
    state->executor = ex;
    if (pthread_mutex_init(&state->lock, NULL) != 0) {
        rt_free((uint8_t*)state, sizeof(*state), _Alignof(rt_far_channel_state));
        return RT_RUNTIME_STATUS_ALLOCATION_FAILED;
    }
    ex->far_channels = state;
    return RT_RUNTIME_STATUS_OK;
}

rt_runtime_status rt_far_channel_state_destroy(rt_executor* ex) {
    if (ex == NULL) {
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    rt_far_channel_state* state = ex->far_channels;
    if (state == NULL) {
        return RT_RUNTIME_STATUS_OK;
    }
    pthread_mutex_lock(&state->lock);
    if (state->head != NULL) {
        pthread_mutex_unlock(&state->lock);
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    ex->far_channels = NULL;
    pthread_mutex_unlock(&state->lock);
    pthread_mutex_destroy(&state->lock);
    rt_free((uint8_t*)state, sizeof(*state), _Alignof(rt_far_channel_state));
    return RT_RUNTIME_STATUS_OK;
}

// Registers an owner-side channel and fills the caller-visible token. Ids
// come from the shared remote-request allocator so no two live far handles
// of any kind share an id; the kind tag still guards cross-registry lookups.
rt_remote_task_status rt_far_channel_mint(rt_executor* ex,
                                          void* channel,
                                          uint32_t owner_shard_id,
                                          rt_far_task_handle* out) {
    rt_far_channel_state* state = rt_far_channel_state_get(ex);
    rt_remote_task_state* tokens = rt_remote_task_state_get(ex);
    if (state == NULL || tokens == NULL || channel == NULL || out == NULL) {
        return RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT;
    }
    rt_far_channel_entry* entry =
        (rt_far_channel_entry*)rt_alloc(sizeof(*entry), _Alignof(rt_far_channel_entry));
    if (entry == NULL) {
        return RT_REMOTE_TASK_STATUS_REFUSED;
    }
    memset(entry, 0, sizeof(*entry));
    entry->channel = channel;
    entry->id = atomic_fetch_add_explicit(&tokens->next_request_id, 1, memory_order_relaxed);
    entry->owner_shard_id = owner_shard_id;
    // The creating holder is the first lease; every later holder mints a
    // sibling through the same allocator, so all lease validation shares
    // one code path from the first token on.
    rt_far_channel_lease* first = lease_new(entry->id);
    if (first == NULL) {
        rt_free((uint8_t*)entry, sizeof(*entry), _Alignof(rt_far_channel_entry));
        return RT_REMOTE_TASK_STATUS_REFUSED;
    }
    entry->leases = first;
    entry->active_leases = 1;
    pthread_mutex_lock(&state->lock);
    entry->next = state->head;
    state->head = entry;
    pthread_mutex_unlock(&state->lock);
    *out = (rt_far_task_handle){.task_id = entry->id,
                                .generation = first->generation,
                                .owner_shard_id = owner_shard_id,
                                .kind = RT_FAR_HANDLE_KIND_CHANNEL};
    return RT_REMOTE_TASK_STATUS_OK;
}

static rt_far_channel_entry* find_locked(rt_far_channel_state* state,
                                         const rt_far_task_handle* handle) {
    for (rt_far_channel_entry* it = state->head; it != NULL; it = it->next) {
        if (it->id == handle->task_id) {
            return it;
        }
    }
    return NULL;
}

// Token validation happens in layers shared by every token-facing
// operation: the id reaches the entry, the generation reaches this
// token's lease row; the live layer additionally requires the owner-shard
// match and an ACTIVE lease (the resolve/pin contract — a released lease
// stops new work for its holder while pinned blocks keep their cached
// pointers and other holders keep working).
static rt_far_channel_entry* lease_checked_locked(rt_far_channel_state* state,
                                                  const rt_far_task_handle* handle,
                                                  rt_far_channel_lease** out_lease) {
    rt_far_channel_entry* entry = find_locked(state, handle);
    if (entry == NULL) {
        return NULL;
    }
    for (rt_far_channel_lease* lease = entry->leases; lease != NULL; lease = lease->next) {
        if (lease->generation == handle->generation) {
            if (out_lease != NULL) {
                *out_lease = lease;
            }
            return entry;
        }
    }
    return NULL;
}

static rt_far_channel_entry* live_lease_locked(rt_far_channel_state* state,
                                               const rt_far_task_handle* handle) {
    rt_far_channel_lease* lease = NULL;
    rt_far_channel_entry* entry = lease_checked_locked(state, handle, &lease);
    if (entry == NULL || lease->released != 0 || entry->owner_shard_id != handle->owner_shard_id) {
        return NULL;
    }
    return entry;
}

// Resolves a token to the live owner-side channel. Kind is validated before
// anything else: a task token can never resolve against a channel record.
// A stale generation (owner teardown/entry release) is distinct from the
// channel's own closed state, which local channel ops report themselves.
void* rt_far_channel_resolve(rt_executor* ex, const rt_far_task_handle* handle) {
    rt_far_channel_state* state = rt_far_channel_state_get(ex);
    if (state == NULL || handle == NULL || handle->kind != RT_FAR_HANDLE_KIND_CHANNEL) {
        return NULL;
    }
    pthread_mutex_lock(&state->lock);
    rt_far_channel_entry* entry = live_lease_locked(state, handle);
    void* channel = NULL;
    if (entry != NULL) {
        channel = entry->channel;
    }
    pthread_mutex_unlock(&state->lock);
    return channel;
}

// The reclaim contract in one place: only an entry with no active leases
// and no in-flight pins may be freed, and the free happens after the
// registry lock is dropped (release_entry retakes it to unlink), so no
// site frees under the lock and no quiescent entry is freed twice — every
// decision is serialized by the same lock that made it.
static int reclaim_ready_locked(const rt_far_channel_entry* entry) {
    return entry->active_leases == 0 &&
           atomic_load_explicit(&entry->inflight, memory_order_acquire) == 0;
}

static void release_entry(rt_far_channel_state* state, rt_far_channel_entry* entry);

static void unlock_then_reclaim(rt_far_channel_state* state, rt_far_channel_entry* target) {
    pthread_mutex_unlock(&state->lock);
    if (target != NULL) {
        release_entry(state, target);
    }
}

static void release_entry(rt_far_channel_state* state, rt_far_channel_entry* entry) {
    pthread_mutex_lock(&state->lock);
    rt_far_channel_entry** cursor = &state->head;
    while (*cursor != NULL && *cursor != entry) {
        cursor = &(*cursor)->next;
    }
    if (*cursor == entry) {
        *cursor = entry->next;
    }
    pthread_mutex_unlock(&state->lock);
    rt_far_channel_lease* lease = entry->leases;
    while (lease != NULL) {
        rt_far_channel_lease* next = lease->next;
        rt_free((uint8_t*)lease, sizeof(*lease), _Alignof(rt_far_channel_lease));
        lease = next;
    }
    // This is the single choke point every reclaim path (release,
    // release_all's shutdown sweep, unpin-drops-to-zero) already routes
    // through, and by the time an
    // entry gets here no lease or pin can resolve it anymore (this
    // function unlinked it above, under the same lock every resolve/pin
    // takes) -- so the owner-side channel object has exactly one path
    // left to it, this one, and frees exactly once.
    // Deferred when a scheduler lock is held: the drain inside runs an
    // element's drop, which must not execute with control or a shard held.
    // Released through the handle count rather than freed outright, because
    // rt_far_channel_dispatch_create minted this object with rt_channel_new
    // and that is one handle: giving it back here is what takes the count to
    // zero, which is what the reclaim is now allowed to happen on.
    rt_channel_handle_drop(entry->channel);
    rt_free((uint8_t*)entry, sizeof(*entry), _Alignof(rt_far_channel_entry));
}

// Invalidates the token (generation can never be reissued: ids are
// allocator-unique) and reclaims the entry, AND the owner-side channel
// object it points at (release_entry), once no operation is in flight
// and no lease remains (this registry is the channel's only owner from
// the mint site on, per rt_far_channel_dispatch_create, so freeing here
// is exactly-once).
rt_remote_task_status rt_far_channel_release(rt_executor* ex, const rt_far_task_handle* handle) {
    rt_far_channel_state* state = rt_far_channel_state_get(ex);
    if (state == NULL || handle == NULL || handle->kind != RT_FAR_HANDLE_KIND_CHANNEL) {
        return RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT;
    }
    pthread_mutex_lock(&state->lock);
    rt_far_channel_lease* lease = NULL;
    rt_far_channel_entry* entry = lease_checked_locked(state, handle, &lease);
    if (entry == NULL || lease->released != 0) {
        pthread_mutex_unlock(&state->lock);
        return RT_REMOTE_TASK_STATUS_STALE_TOKEN;
    }
    lease->released = 1;
    entry->active_leases--;
    unlock_then_reclaim(state, reclaim_ready_locked(entry) ? entry : NULL);
    return RT_REMOTE_TASK_STATUS_OK;
}

// Shutdown sweep: invalidate every live entry, then reclaim the quiescent
// ones. Entries pinned by in-flight operations are reclaimed by the owning
// operation's completion path.
void rt_far_channel_release_all(rt_executor* ex) {
    rt_far_channel_state* state = rt_far_channel_state_get(ex);
    if (state == NULL) {
        return;
    }
    for (;;) {
        pthread_mutex_lock(&state->lock);
        rt_far_channel_entry* target = NULL;
        for (rt_far_channel_entry* it = state->head; it != NULL; it = it->next) {
            for (rt_far_channel_lease* lease = it->leases; lease != NULL; lease = lease->next) {
                lease->released = 1;
            }
            it->active_leases = 0;
            if (reclaim_ready_locked(it)) {
                target = it;
                break;
            }
        }
        unlock_then_reclaim(state, target);
        if (target == NULL) {
            return;
        }
    }
}

// Pins a live entry for an in-flight anchored block (validating kind,
// generation, and owner) so release/teardown cannot reclaim the channel
// under the body's feet. Returns nonzero on success; the pin is dropped by
// rt_far_channel_unpin when the block's reply edge resolves.
// Pin and resolve are one atomic step: the OPEN check, the inflight
// increment, and the channel-pointer read happen under the registry lock,
// so a release racing the pin can never yield "pinned but unresolvable".
int rt_far_channel_pin(rt_executor* ex, const rt_far_task_handle* handle, void** out_channel) {
    rt_far_channel_state* state = rt_far_channel_state_get(ex);
    if (state == NULL || handle == NULL || handle->kind != RT_FAR_HANDLE_KIND_CHANNEL) {
        return 0;
    }
    pthread_mutex_lock(&state->lock);
    rt_far_channel_entry* entry = live_lease_locked(state, handle);
    int pinned = 0;
    if (entry != NULL) {
        (void)atomic_fetch_add_explicit(&entry->inflight, 1, memory_order_acq_rel);
        if (out_channel != NULL) {
            *out_channel = entry->channel;
        }
        pinned = 1;
    }
    pthread_mutex_unlock(&state->lock);
    return pinned;
}

// Test-support census: live (non-reclaimed) registry entries. Harness rows
// use it to prove teardown leaves nothing behind.
size_t rt_far_channel_debug_live_count(rt_executor* ex) {
    rt_far_channel_state* state = rt_far_channel_state_get(ex);
    if (state == NULL) {
        return 0;
    }
    size_t count = 0;
    pthread_mutex_lock(&state->lock);
    for (const rt_far_channel_entry* it = state->head; it != NULL; it = it->next) {
        count++;
    }
    pthread_mutex_unlock(&state->lock);
    return count;
}

// Drops one pin. The pin was validated when it was taken, so this needs
// only the entry -- but it refuses what a pin could never have produced: a
// token of another kind (a share request rides the same pending slot with a
// channel token; a far TASK token never pins), and an entry with no pin
// left. An unpin without a pin used to subtract from an unsigned counter
// and park the entry at UINT32_MAX in-flight, which no release could ever
// reclaim; it is a caller defect and is told as one.
void rt_far_channel_unpin(rt_executor* ex, const rt_far_task_handle* handle) {
    rt_far_channel_state* state = rt_far_channel_state_get(ex);
    if (state == NULL || handle == NULL || handle->kind != RT_FAR_HANDLE_KIND_CHANNEL) {
        return;
    }
    pthread_mutex_lock(&state->lock);
    rt_far_channel_entry* entry = find_locked(state, handle);
    rt_far_channel_entry* reclaim = NULL;
    if (entry != NULL) {
        if (atomic_load_explicit(&entry->inflight, memory_order_acquire) == 0) {
            pthread_mutex_unlock(&state->lock);
            panic_msg("async: far channel unpinned more times than it was pinned");
            return;
        }
        (void)atomic_fetch_sub_explicit(&entry->inflight, 1, memory_order_acq_rel);
        if (reclaim_ready_locked(entry)) {
            reclaim = entry;
        }
    }
    unlock_then_reclaim(state, reclaim);
}

// Caller-side handle storage: a bare token struct owned by the caller task's
// scope. Unlike far-task handles (pointers into a lease), a channel handle
// is a pure value token; the struct exists only so the LLVM far-handle
// representation (pointer) has something to point at.
rt_remote_task_status rt_far_channel_handle_alloc(rt_far_task_handle** out) {
    if (out == NULL) {
        return RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT;
    }
    rt_far_task_handle* handle =
        (rt_far_task_handle*)rt_alloc(sizeof(*handle), _Alignof(rt_far_task_handle));
    if (handle == NULL) {
        return RT_REMOTE_TASK_STATUS_REFUSED;
    }
    memset(handle, 0, sizeof(*handle));
    handle->kind = RT_FAR_HANDLE_KIND_CHANNEL;
    *out = handle;
    return RT_REMOTE_TASK_STATUS_OK;
}

// Drop hook for a caller-side channel handle: releases the owner-side
// registry entry (dropping the only handle makes the channel unreachable)
// and frees the token. The LLVM backend has no scope-exit drop emission yet
// (heap reclamation is task/heap-cell level); this is the single call the
// drop machinery wires to when it lands, and teardown paths may call it
// directly. Stale entries (already released/teardown) are tolerated.
void rt_far_channel_handle_drop(rt_far_task_handle* handle) {
    if (handle == NULL || handle->kind != RT_FAR_HANDLE_KIND_CHANNEL) {
        return;
    }
    rt_executor* ex = ensure_exec();
    if (ex != NULL) {
        (void)rt_far_channel_release(ex, handle);
    }
    rt_far_channel_handle_free(handle);
}

// Takes a MUTABLE handle: freeing is not a const operation, and the cast that
// used to launder the const away here went pointer -> void* -> uintptr_t, which
// is the shape clang-tidy flags as bugprone-casting-through-void. The rest of
// the far-channel API keeps its const handles, because resolve/release/pin/unpin
// genuinely do not modify the token; only alloc and free own it, and alloc
// already hands one back mutable.
void rt_far_channel_handle_free(rt_far_task_handle* handle) {
    if (handle == NULL) {
        return;
    }
    rt_free((uint8_t*)handle, sizeof(*handle), _Alignof(rt_far_task_handle));
}

// Active-lease count for the entry a token addresses (0 when the entry is
// gone). The deadlock diagnostic uses it to name the lease topology: a
// parked block plus global quiescence means every one of these holders is
// idle, so none of them can ever drain the channel.
size_t rt_far_channel_active_lease_count(rt_executor* ex, const rt_far_task_handle* handle) {
    rt_far_channel_state* state = rt_far_channel_state_get(ex);
    if (state == NULL || handle == NULL || handle->kind != RT_FAR_HANDLE_KIND_CHANNEL) {
        return 0;
    }
    pthread_mutex_lock(&state->lock);
    rt_far_channel_entry* entry = find_locked(state, handle);
    size_t count = entry != NULL ? entry->active_leases : 0;
    pthread_mutex_unlock(&state->lock);
    return count;
}

// Test-support census of lease rows across all live entries: the leak
// audit proves churn leaves neither entries nor lease rows behind.
size_t rt_far_channel_debug_lease_count(rt_executor* ex) {
    rt_far_channel_state* state = rt_far_channel_state_get(ex);
    if (state == NULL) {
        return 0;
    }
    size_t count = 0;
    pthread_mutex_lock(&state->lock);
    for (const rt_far_channel_entry* it = state->head; it != NULL; it = it->next) {
        for (const rt_far_channel_lease* lease = it->leases; lease != NULL; lease = lease->next) {
            count++;
        }
    }
    pthread_mutex_unlock(&state->lock);
    return count;
}

// Mints a sibling lease on the source token's entry: the source lease must
// be alive (a released holder cannot propagate access), the sibling gets
// its own allocator-unique generation, and the new token addresses the
// same entry. One registry lock step — a release racing the share either
// lands before (share answers stale) or after (the sibling is already
// active and unaffected).
rt_remote_task_status rt_far_channel_mint_sibling(rt_executor* ex,
                                                  const rt_far_task_handle* source,
                                                  rt_far_task_handle* out) {
    rt_far_channel_state* state = rt_far_channel_state_get(ex);
    rt_remote_task_state* tokens = rt_remote_task_state_get(ex);
    if (state == NULL || tokens == NULL || source == NULL || out == NULL ||
        source->kind != RT_FAR_HANDLE_KIND_CHANNEL) {
        return RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT;
    }
    rt_far_channel_lease* sibling =
        lease_new(atomic_fetch_add_explicit(&tokens->next_request_id, 1, memory_order_relaxed));
    if (sibling == NULL) {
        return RT_REMOTE_TASK_STATUS_REFUSED;
    }
    pthread_mutex_lock(&state->lock);
    rt_far_channel_entry* entry = live_lease_locked(state, source);
    if (entry == NULL) {
        pthread_mutex_unlock(&state->lock);
        rt_free((uint8_t*)sibling, sizeof(*sibling), _Alignof(rt_far_channel_lease));
        return RT_REMOTE_TASK_STATUS_STALE_TOKEN;
    }
    sibling->next = entry->leases;
    entry->leases = sibling;
    entry->active_leases++;
    pthread_mutex_unlock(&state->lock);
    *out = (rt_far_task_handle){.task_id = entry->id,
                                .generation = sibling->generation,
                                .owner_shard_id = entry->owner_shard_id,
                                .kind = RT_FAR_HANDLE_KIND_CHANNEL};
    return RT_REMOTE_TASK_STATUS_OK;
}
