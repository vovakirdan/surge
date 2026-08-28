#ifndef SURGE_RUNTIME_NATIVE_RT_CHANNEL_LANE_H
#define SURGE_RUNTIME_NATIVE_RT_CHANNEL_LANE_H

#include "rt_async_internal.h"

#include "rt_channel_refcount.h"
#include "rt_park_pool.h"
#include "rt_typed_fifo.h"
#include "rt_value_ops.h"

// Private channel-lane header shared by rt_async_channel.c (async fast
// lanes) and rt_channel_sync.c (try/compat/blocking/close lanes). The
// struct and helpers are static inline so each lane compiles the exact
// same owner-lock protocol without exporting channel internals.

struct rt_channel {
    uint64_t capacity;
    // Owner shard (D5 channel slice): fixed at creation to the creating
    // task's shard (shard 0 outside tasks); channel waiter keys live in the
    // owner's store and never move.
    uint32_t owner_shard_id;
    uint8_t closed;
    // Set under the owner lock by the reclaim, before it detaches anything.
    // Section 7 of docs/RUNTIME_V2.md names this the first step of teardown,
    // and what it buys is a place to fail closed: an operation that pins the
    // object after this is set found it through something that should already
    // have been gone, and a panic naming that is worth more than the storage
    // it would have written into.
    //
    // Atomic because the reader is not always under the lock the writer holds:
    // an operation pin is taken before any lock, which is the whole point --
    // the pin has to exist before the window it protects opens.
    _Atomic uint8_t dying;
    // The element's descriptor, and its type id.
    //
    // ONE descriptor for the whole channel, per the storage model: a slot
    // carries payload and a one-byte lifecycle state, never a copy of the
    // owner's callback metadata. The id rides along because it is the only
    // thing that survives a boundary -- a far channel is created from a
    // request whose payload type arrived as a number, and the local side
    // turns it back into this pointer.
    const rt_value_ops* ops;
    uint64_t element_type_id;
    // The buffer, at the element's own stride: no word padding, and nothing
    // for an element that occupies no bytes.
    rt_typed_fifo ring;
    // Staging the CHANNEL owns, not the tasks. A parked task's poll function
    // can leave by longjmp, so a value staged in the task itself would be
    // lost with nobody left to free it; the channel outlives every park on
    // it. A task carries a capability token naming its slot, which is what
    // keeps the scheduler mailbox carrying control only.
    rt_park_pool parks;
    // What still names this object, in the two kinds section 7 of
    // docs/RUNTIME_V2.md distinguishes: handle_refs counts the copies of the
    // handle a program holds, pins count the runtime's own holds -- a
    // registered waiter, a select subscription, a claimed detached operation.
    // Both keep the object alive, and reclamation needs both at zero; see
    // rt_channel_refcount.h. `reclaiming` names the release that performs the
    // reclaim when both kinds retire at once, so the object is handed over
    // once and not twice.
    _Atomic uint32_t handle_refs;
    _Atomic uint32_t pins;
    _Atomic uint8_t reclaiming;
};

// Channel lane (peel B2): a channel's buffer and its two waiter keys live
// under the channel owner's shard lock; entry APIs no longer take the
// control lane. Peer delivery is candidate/validate: pop one entry under
// the channel lock, deliver same-shard peers inline (the channel lock IS
// their owner lock), and go through control -> peer-owner for foreign
// peers so the owner read stays stable. A dead candidate never consumes a
// value: the caller retries with the next. Foreign parked SENDERS are
// woken to retry instead of having their value read remotely, so buffered
// refill never strands a value between locks.

// Sync-channel compat fallback: a peer that was RUNNING inside a blocking
// helper is not enqueued by the leaf wake; its OS worker sleeps on compat_cv
// under the control lock, so that broadcast is its only wake (the token is
// already set). Called with no shard lock held.
static inline void channel_compat_broadcast_if_needed(rt_executor* ex, int pushed) {
    if (pushed) {
        return;
    }
    const rt_channel_blocking_compat* compat = rt_executor_channel_blocking_compat_const(ex);
    if (compat == NULL ||
        atomic_load_explicit(&compat->channel_blocked_workers, memory_order_acquire) == 0) {
        return;
    }
    int need_control = !rt_lane_holds_control();
    if (need_control) {
        rt_control_lock(ex);
    }
    pthread_cond_broadcast(&ex->compat_cv);
    if (need_control) {
        rt_control_unlock(ex);
    }
}

static inline rt_shard* channel_owner_shard(rt_executor* ex, const rt_channel* ch) {
    return rt_runtime_shard(rt_executor_runtime(ex), ch->owner_shard_id);
}

// Caller holds the channel owner's lock; FIFO per key.
//
// THE RULE FOR EVERY CALLER: a popped candidate is either delivered to or
// woken, never simply dropped. The pop CONSUMES the peer's registration, and a
// parked task whose registration is gone is a task the channel can no longer
// call on -- it sleeps until the process ends. The one exception is a
// candidate that fails channel_candidate_valid, which means the peer is no
// longer parked on this key and has a live registration elsewhere.
static inline int channel_pop_candidate_locked(rt_shard* owner_shard, waker_key key, waiter* out) {
    rt_waiter_store* store = &owner_shard->waiter_store;
    for (size_t i = 0; i < store->len; i++) {
        waiter w = store->entries[i];
        if (w.key.kind == key.kind && w.key.id == key.id) {
            memmove(
                &store->entries[i], &store->entries[i + 1], (store->len - i - 1) * sizeof(waiter));
            store->len--;
            // The entry held one of the channel's pins for as long as it named
            // it. Retiring the entry retires the pin, and if it was the last
            // hold the reclaim it sets off is deferred until this lane lets go
            // of the owner lock -- which is why the caller may keep using the
            // channel below, on the operation's own pin.
            rt_channel_key_retired(key, 1);
            *out = w;
            return 1;
        }
    }
    return 0;
}

// Candidate validation (spike candidate/validate): the popped entry is only
// deliverable while the peer's current park still targets this key. A live
// peer that consumed an earlier wake and re-registered elsewhere leaves its
// old entry semantically dead; delivering into it would overwrite resume
// state for an unrelated park (observable as a foreign value surfacing from
// a reused channel address). park_key holds the key from registration until
// the wake leaf clears it — through the park commit, which resets only the
// prepared flag — so key equality alone is the liveness test. The cross-lock
// read is benign, and is the last survivor of the stale-skip deref the
// now-deleted pop_waiter used: a mismatch drops the candidate, and an exact
// match on this key means the peer still parks (or re-parked) on this same
// channel, so delivery is due.
static inline int channel_candidate_valid(const rt_task* peer, const waiter* w) {
    return peer != NULL && task_status_load(peer) != TASK_DONE && task_cancelled_load(peer) == 0 &&
           peer->park_key.kind == w->key.kind && peer->park_key.id == w->key.id &&
           peer->park_seq == w->seq;
}

// Wake-only candidates (seq == 0) come from add_waiter registrations —
// select arms parked across several keys. They have no value mailbox: wake
// the task so it re-polls its arms and treat the pop as a non-event for the
// rendezvous (the caller keeps its value and keeps scanning or parks).
static inline void channel_wake_only(rt_executor* ex, rt_shard* ch_shard, const waiter* w) {
    rt_task* peer = get_task(ex, w->task_id);
    if (peer == NULL || task_status_load(peer) == TASK_DONE) {
        return;
    }
    if (w->owner_hint == ch_shard->shard_id) {
        (void)wake_task_on_shard_locked(ex, ch_shard, peer, 1, 0, 1, NULL);
        return;
    }
    rt_shard_unlock(ch_shard);
    int need_control = !rt_lane_holds_control();
    if (need_control) {
        rt_control_lock(ex);
    }
    peer = get_task(ex, w->task_id);
    if (peer != NULL && task_status_load(peer) != TASK_DONE) {
        rt_shard* peer_shard = rt_task_owner_shard(ex, peer);
        rt_shard_lock(peer_shard);
        if (task_status_load(peer) != TASK_DONE) {
            (void)wake_task_on_shard_locked(ex, peer_shard, peer, 1, 0, 1, NULL);
        }
        rt_shard_unlock(peer_shard);
    }
    if (need_control) {
        rt_control_unlock(ex);
    }
    rt_shard_lock(ch_shard);
}

// Caller holds the channel owner's lock and the candidate's owner hint
// equals that shard: validate and deliver inline.
static inline int channel_deliver_same_shard_locked(rt_executor* ex,
                                                    rt_shard* owner_shard,
                                                    const waiter* w,
                                                    uint8_t resume_kind_value,
                                                    rt_park_token resume_slot,
                                                    int signal_ready,
                                                    int* out_pushed) {
    rt_task* peer = get_task(ex, w->task_id);
    if (!channel_candidate_valid(peer, w)) {
        return 0;
    }
    peer->resume_kind = resume_kind_value;
    // RESUME_NONE means "wake up and look again", not "here is a value", so it
    // must leave the peer's token alone. A parked SENDER keeps its staged value
    // in a slot named by that token; overwriting it here strands the value and
    // the sender waits for an ack that can never come. The word-shaped version
    // could clear the field safely because the value lived in the sender's own
    // frame -- it does not any more.
    if (resume_kind_value != RESUME_NONE) {
        peer->resume_slot = resume_slot;
    }
    int pushed = wake_task_on_shard_locked(
        ex, owner_shard, peer, channel_wake_force_inject_enabled(), 0, signal_ready, NULL);
    if (out_pushed != NULL) {
        *out_pushed = pushed;
    }
    return 1;
}

// Caller holds no lock. Validates and delivers to a foreign-owner peer
// under control -> peer owner (D5 collect-then-wake).
static inline int channel_deliver_foreign(rt_executor* ex,
                                          const waiter* w,
                                          uint8_t resume_kind_value,
                                          rt_park_token resume_slot) {
    int need_control = !rt_lane_holds_control();
    if (need_control) {
        rt_control_lock(ex);
    }
    int live = 0;
    rt_task* peer = get_task(ex, w->task_id);
    if (channel_candidate_valid(peer, w)) {
        rt_shard* peer_shard = rt_task_owner_shard(ex, peer);
        rt_shard_lock(peer_shard);
        int pushed = 0;
        if (channel_candidate_valid(peer, w)) {
            peer->resume_kind = resume_kind_value;
            if (resume_kind_value != RESUME_NONE) {
                peer->resume_slot = resume_slot;
            }
            pushed = wake_task_on_shard_locked(ex, peer_shard, peer, 1, 0, 1, NULL);
            live = 1;
        }
        rt_shard_unlock(peer_shard);
        if (live) {
            channel_compat_broadcast_if_needed(ex, pushed);
        }
    }
    if (need_control) {
        rt_control_unlock(ex);
    }
    return live;
}

// Claims a foreign parked sender's staged value for this channel's own use.
//
// Only the TOKEN crosses a lock boundary: the value itself sits in the
// channel's pool, so it never travels between owners. The token is read and
// the ack is written under the sender's own owner lock, which is the lock that
// answers for its mailbox; the caller then moves the value under the channel's
// lock, which is the lock that answers for the pool.
//
// Returns 0 when the sender is gone, or when it parked holding its own value
// because the pool was full. That sender is WOKEN rather than acked: an ack
// says "your send completed", and a send that never handed a value anywhere
// has not completed -- telling it otherwise loses the value silently.
//
// Enters and leaves with the channel shard locked; releases it in between,
// because control nests outside a shard lock and a peer's owner must be taken
// through it.
static inline int channel_claim_foreign_sender_locked(
    rt_executor* ex, rt_shard* ch_shard, rt_channel* ch, const waiter* w, rt_park_token* out_slot) {
    *out_slot = (rt_park_token){0};
    rt_shard_unlock(ch_shard);
    int need_control = !rt_lane_holds_control();
    if (need_control) {
        rt_control_lock(ex);
    }
    int claimed = 0;
    rt_task* sender = get_task(ex, w->task_id);
    if (channel_candidate_valid(sender, w)) {
        rt_shard* sender_shard = rt_task_owner_shard(ex, sender);
        rt_shard_lock(sender_shard);
        int live = 0;
        int pushed = 0;
        if (channel_candidate_valid(sender, w)) {
            live = 1;
            rt_park_token slot = sender->resume_slot;
            // Nobody else can be taking this sender's value: the waiter entry
            // that names it was popped by this caller, and a take starts from
            // that entry.
            if (rt_park_pool_token_is_live(&ch->parks, &slot)) {
                *out_slot = slot;
                sender->resume_slot = (rt_park_token){0};
                sender->resume_kind = RESUME_CHAN_SEND_ACK;
                claimed = 1;
            }
            pushed = wake_task_on_shard_locked(ex, sender_shard, sender, 1, 0, 1, NULL);
        }
        rt_shard_unlock(sender_shard);
        if (live) {
            channel_compat_broadcast_if_needed(ex, pushed);
        }
    }
    if (need_control) {
        rt_control_unlock(ex);
    }
    rt_shard_lock(ch_shard);
    return claimed;
}

// Registers the current task on a channel key while the channel owner's
// lock is held (add_waiter would self-deadlock on the same shard). The
// park itself commits later through the wake_token dance.
static inline void
channel_park_prepare_locked(rt_shard* owner_shard, rt_task* task, waker_key key) {
    rt_waiter_store* store = &owner_shard->waiter_store;
    // Dedupe against the store itself: absorbed wakes and compat_cv wakeups
    // re-enter this path without their entry having been popped; appending
    // again would strand a duplicate that outlives this park and, once the
    // guest reuses the channel address, misdelivers into an unrelated park.
    // A matched entry must be re-armed with a fresh generation: it may be a
    // leftover from a superseded park (same task, same key — e.g. a reused
    // channel address across park generations), and an entry whose seq lags
    // task->park_seq validates false at delivery, which would pop-and-drop
    // the registration and strand this park forever.
    for (size_t i = 0; i < store->len; i++) {
        waiter* w = &store->entries[i];
        if (w->task_id == task->id && w->key.kind == key.kind && w->key.id == key.id) {
            task->park_seq++;
            if (task->park_seq == 0) {
                task->park_seq = 1;
            }
            w->seq = task->park_seq;
            task->park_key = key;
            task->park_prepared = 1;
            return;
        }
    }
    if (rt_waiter_store_ensure_cap(store) != RT_RUNTIME_STATUS_OK) {
        panic_msg("async: waiter allocation failed");
        return;
    }
    uint32_t owner_hint = task->owner_shard_valid != 0 ? task->owner_shard_id : 0;
    task->park_seq++;
    if (task->park_seq == 0) {
        task->park_seq = 1;
    }
    store->entries[store->len++] = (waiter){key, task->id, owner_hint, task->park_seq};
    // A registered waiter is one of the three holds section 7 of
    // docs/RUNTIME_V2.md names. Only the APPEND takes it: the dedupe arm above
    // rewrites an entry that already holds one, and pinning there would leave a
    // hold nothing ever retires. The park it registers outlives this call, so
    // the entry -- not this frame -- is what owns the pin.
    rt_channel_key_registered(key);
    task->park_key = key;
    task->park_prepared = 1;
}

static inline uint64_t channel_align_up(uint64_t size, uint64_t align) {
    if (align == 0) {
        return size;
    }
    uint64_t rem = size % align;
    if (rem == 0) {
        return size;
    }
    uint64_t add = align - rem;
    if (size > UINT64_MAX - add) {
        panic_msg("async: channel allocation overflow");
        return 0;
    }
    return size + add;
}

// How many tasks may stage a value in this channel at once.
//
// A park slot is what a sender's value lives in while the sender is parked,
// and what a delivery is written into for a receiver that has not woken yet.
// Neither count is bounded by the language -- any number of tasks may pile up
// on one channel -- so the pool is a fast path with a defined fallback rather
// than a guarantee: a sender that finds no slot parks holding its own value
// and retries when woken, which is the loop's existing behaviour and costs
// only a retry.
static inline uint64_t channel_park_capacity(uint64_t capacity) {
    // CONSTANT, not a function of capacity, and that is a correction made
    // against a measurement rather than a guess. Sizing this pool from the
    // buffer made a Channel<nothing> cost eighteen bytes per cell -- worse
    // than the eight-byte word ring it replaces -- because every cell bought
    // a park slot no task was ever going to use. The storage model asks for
    // O(1) in capacity here for exactly this reason: Mutex, Condition and
    // Semaphore are Channel<nothing>, and they must not pay for typing.
    //
    // What this bounds is how many tasks can stage AT ONCE, which is a
    // property of the program's concurrency, not of the buffer's size. Running
    // out is a defined fallback, not a failure: a sender parks holding its own
    // value and retries when woken.
    (void)capacity;
    return 8;
}

static inline uint64_t channel_ring_offset(void) {
    return channel_align_up((uint64_t)sizeof(rt_channel), (uint64_t) _Alignof(uint64_t));
}

// One allocation for the whole owner: header, then the ring at the element's
// stride, then the park pool. A zero-sized element contributes no ring bytes
// at all, which is the point -- Mutex, Condition and Semaphore are all built
// on Channel<nothing>.
static inline uint64_t channel_alloc_size(const rt_value_ops* ops, uint64_t capacity) {
    uint64_t offset = channel_ring_offset();
    size_t ring = rt_typed_fifo_alloc_size(ops, capacity);
    size_t parks = rt_park_pool_alloc_size(ops, channel_park_capacity(capacity));
    if (ops != NULL && (ring == 0 || parks == 0)) {
        panic_msg("async: channel allocation overflow");
        return 0;
    }
    uint64_t total = offset;
    uint64_t align = (uint64_t) _Alignof(uint64_t);
    if (ops != NULL && ops->layout.align > align) {
        align = (uint64_t)ops->layout.align;
    }
    total = channel_align_up(total, align);
    if ((uint64_t)ring > UINT64_MAX - total) {
        panic_msg("async: channel allocation overflow");
        return 0;
    }
    total += (uint64_t)ring;
    total = channel_align_up(total, align);
    if ((uint64_t)parks > UINT64_MAX - total) {
        panic_msg("async: channel allocation overflow");
        return 0;
    }
    return total + (uint64_t)parks;
}

static inline rt_channel* channel_from_handle(void* handle) {
    if (handle == NULL) {
        panic_msg("async: null channel handle");
        return NULL;
    }
    return (rt_channel*)handle;
}

// The element descriptor a caller outside this header needs to size storage.
static inline const rt_value_ops* rt_channel_element_ops(const void* handle) {
    const rt_channel* ch = handle == NULL ? NULL : (const rt_channel*)handle;
    return ch == NULL ? NULL : ch->ops;
}

static inline uint64_t channel_buffered(const rt_channel* ch) {
    return ch == NULL ? 0 : rt_typed_fifo_len(&ch->ring);
}

// The admission test for a rendezvous. A receiver may be handed a value
// directly only while the buffer has nothing older to give it first, and
// "older" includes a value whose transfer into the buffer is still in flight.
static inline int channel_nothing_queued(const rt_channel* ch) {
    return ch == NULL || rt_typed_fifo_nothing_queued_locked(&ch->ring);
}

// A value has just entered the buffer, so a receiver that parked while the
// buffer was empty has to be told to come back for it. Nothing else will tell
// it: the value went into a cell rather than into that receiver's resume slot,
// so no delivery consumed its registration. It is WOKEN rather than handed the
// value, because the value it must take is the queue's head and this one may
// not be it.
static inline void
channel_wake_parked_receiver_locked(rt_executor* ex, rt_shard* ch_shard, const rt_channel* ch) {
    waiter cand;
    while (channel_pop_candidate_locked(ch_shard, channel_recv_key(ch), &cand)) {
        const rt_task* peer = get_task(ex, cand.task_id);
        if (peer == NULL || task_status_load(peer) == TASK_DONE) {
            // A registration whose task is gone wakes nobody; keep looking, or
            // the value sits in the buffer with a live receiver still asleep.
            continue;
        }
        channel_wake_only(ex, ch_shard, &cand);
        return;
    }
}

// What a receive found, described rather than performed.
//
// The owner-locked cores cannot move the value themselves: a move runs the
// element's generated move_init, and no generated operation may run under a
// runtime owner lock. So they CLAIM under the lock and hand back where the
// value is; the caller releases the lock, moves, and then commits. That split
// is the whole reason these functions changed shape rather than just changing
// types.
// How wide an element a select can stage in its own frame. A channel of a
// wider element refuses rather than silently truncating: the operation owns
// this storage, and growing it is a decision with a stack cost, not an
// accident.
#define RT_SELECT_STAGING_BYTES 128

typedef enum {
    RT_CHANNEL_TAKE_NONE = 0,
    RT_CHANNEL_TAKE_FROM_RING = 1,
    RT_CHANNEL_TAKE_FROM_SENDER = 2,
} rt_channel_take_kind;

typedef struct {
    rt_channel_take_kind kind;
    // Where the value is right now. Valid for both take kinds.
    void* address;
    rt_typed_fifo_ticket ticket;
    rt_park_token slot;
    // A sender whose value this claim took, to be acked once the move is done.
    uint64_t sender_task_id;
} rt_channel_take;

// Ends a park and destroys whatever its slot still holds.
//
// Enter and leave with the channel's lock held; it is released only across the
// element's drop, which is the one part that may not run under it. The slot's
// bookkeeping -- the free list, the live count, the generation -- stays under
// the lock throughout, which is what the split in rt_park_pool exists for.
//
// The ordinary case has nothing to destroy: a park whose value was delivered
// leaves an empty slot, and this just hands it back.
static inline void
channel_end_park_locked(rt_shard* ch_shard, rt_channel* ch, const rt_park_token* token) {
    void* value = NULL;
    if (rt_park_pool_begin_release_locked(&ch->parks, token, &value) != RT_SLOT_CONTROL_OK) {
        return;
    }
    if (value != NULL) {
        rt_shard_unlock(ch_shard);
        rt_value_drop_in_place_detached(ch->ops, value);
        rt_shard_lock(ch_shard);
    }
    (void)rt_park_pool_finish_release_locked(&ch->parks, token);
}

// Moves a staged value out of its park slot and into the ring, again with the
// lock released across the move. Used when a send finds room in the buffer
// rather than a waiting receiver.
static inline int channel_stage_into_ring_locked(rt_executor* ex,
                                                 rt_shard* ch_shard,
                                                 rt_channel* ch,
                                                 const rt_park_token* staged) {
    rt_typed_fifo_ticket ticket;
    if (rt_typed_fifo_reserve_push_locked(&ch->ring, &ticket) != RT_SLOT_CONTROL_OK) {
        return 0;
    }
    void* from = NULL;
    if (rt_park_pool_reserve_take_locked(&ch->parks, staged, &from) != RT_SLOT_CONTROL_OK) {
        (void)rt_typed_fifo_abandon_push_locked(&ch->ring, &ticket);
        return 0;
    }
    rt_shard_unlock(ch_shard);
    rt_value_move_init_detached(ch->ops, ticket.address, from);
    rt_shard_lock(ch_shard);
    (void)rt_park_pool_commit_take_locked(&ch->parks, staged);
    if (rt_typed_fifo_commit_push_locked(&ch->ring, &ticket) != RT_SLOT_CONTROL_OK) {
        panic_msg("async: buffered channel value could not be published");
        return 0;
    }
    channel_wake_parked_receiver_locked(ex, ch_shard, ch);
    return 1;
}

// The mirror image, for a send: where to put the value, claimed under the lock
// and filled by the caller once the lock is released.
typedef enum {
    RT_CHANNEL_PUT_NONE = 0,
    RT_CHANNEL_PUT_INTO_RING = 1,
    RT_CHANNEL_PUT_INTO_PARK = 2,
} rt_channel_put_kind;

typedef struct {
    rt_channel_put_kind kind;
    void* address;
    rt_typed_fifo_ticket ticket;
    rt_park_token slot;
    // The receiver this put is destined for, woken once the value is in place.
    waiter candidate;
    int has_candidate;
} rt_channel_put;

// The owner-locked cores: they CLAIM under the caller's lock and describe what
// they found, because moving the value runs generated code that must not run
// under a runtime owner lock. The caller releases, moves, reacquires, and
// finishes.
uint8_t rt_channel_try_recv_status_owner_locked(rt_executor* ex,
                                                rt_shard* ch_shard,
                                                rt_channel* ch,
                                                rt_channel_take* out_take);
void rt_channel_finish_take_owner_locked(rt_executor* ex,
                                         rt_shard* ch_shard,
                                         rt_channel* ch,
                                         const rt_channel_take* take);
uint8_t rt_channel_try_send_status_owner_locked(rt_executor* ex,
                                                rt_shard* ch_shard,
                                                rt_channel* ch,
                                                rt_channel_put* out_put);

// The control-lane pair: claim with control held, move with it released,
// finish with it held again.
uint8_t rt_channel_claim_recv_locked(rt_executor* ex, void* channel, rt_channel_take* out_take);
void rt_channel_finish_recv_locked(rt_executor* ex, void* channel, const rt_channel_take* take);
uint8_t rt_channel_claim_send_locked(rt_executor* ex, void* channel, rt_channel_put* out_put);
void rt_channel_finish_send_locked(rt_executor* ex, void* channel, rt_channel_put* put);
void rt_channel_finish_put_owner_locked(rt_executor* ex,
                                        rt_shard* ch_shard,
                                        rt_channel* ch,
                                        rt_channel_put* put);

#endif
