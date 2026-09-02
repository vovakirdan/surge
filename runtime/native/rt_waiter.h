#ifndef SURGE_RUNTIME_NATIVE_RT_WAITER_H
#define SURGE_RUNTIME_NATIVE_RT_WAITER_H

#include "rt_runtime_config.h"
#include <stddef.h>
#include <stdint.h>

// Waiter keys, entries, and stores. Stores are owned per the /9
// dependency map: net keys by the fd owner shard, join keys by the task's
// atomic join-owner route, timer/blocking keys by the parked-on task's owner
// shard, scope keys by the scope's pinned owner shard, and channel keys by the
// shard in rt_channel.owner_shard_id — the creating task's shard, or the bound target
// shard for a transport-minted channel, and 0 only when a channel is created
// outside task context.

typedef struct rt_executor rt_executor;
typedef struct rt_task rt_task;
typedef struct rt_channel rt_channel;
typedef struct rt_shard rt_shard;

typedef enum {
    WAKER_NONE = 0,
    WAKER_JOIN = 1,
    WAKER_TIMER = 2,
    WAKER_CHAN_SEND = 3,
    WAKER_CHAN_RECV = 4,
    WAKER_NET_ACCEPT = 5,
    WAKER_NET_READ = 6,
    WAKER_NET_WRITE = 7,
    WAKER_SCOPE = 8,
    WAKER_BLOCKING = 9,
    WAKER_REMOTE_SPAWN_REPLY = 10,
    WAKER_REMOTE_TASK_REPLY = 11,
    // Claim-budget exhaustion waits apart from ordinary channel readiness: a
    // released claim wakes one of these without popping, and so without
    // reordering, a sender or receiver waiting for capacity or a peer. Both
    // stay channel-owned keys -- same channel id, same owner shard, the same
    // internal pin -- and close drains them with the ordinary two.
    WAKER_CHAN_SEND_RETRY = 12,
    WAKER_CHAN_RECV_RETRY = 13,
} waker_kind;

// ABI order predates Runtime V2 and is shared with generated/test code.
// NOLINTNEXTLINE(clang-analyzer-optin.performance.Padding)
typedef struct {
    uint8_t kind;
    uint64_t id;
    uint32_t owner_shard_id;
} waker_key;

int waker_valid(waker_key key);

// A retry registration with no generation cannot be removed after its task is
// republished: remove_waiter_generation(seq=0) is an unqualified match-all and
// can sweep the fresh registration made by the next poll.  These keys have one
// terminal owner event that drains every entry, so retaining the stale entry is
// bounded until that self-cleaning drain. Timer/net completion is independently
// addressed, and channel parks carry a generation.
static inline int waker_seq0_retry_is_terminal_drained(waker_key key) {
    switch ((waker_kind)key.kind) {
#ifndef RV2_DEBT_046_NEGATIVE_CONTROL
        case WAKER_JOIN:
#endif
        case WAKER_SCOPE:
        case WAKER_BLOCKING:
        case WAKER_REMOTE_SPAWN_REPLY:
#ifndef RV2_SEQ0_RETRY_NEGATIVE_CONTROL
        case WAKER_REMOTE_TASK_REPLY:
#endif
            return 1;
        default:
            return 0;
    }
}

typedef struct {
    waker_key key;
    uint64_t task_id;
    // Owner shard of task_id at registration time (D3/D5): stable for
    // non-accept keys, refreshed by the control-lane accept transition.
    uint32_t owner_hint;
    // Channel-lane registration generation (candidate/validate): matches the
    // task's park_seq while the park is current. Zero marks wake-only
    // entries (select arms and other add_waiter registrations), which carry
    // no value mailbox and must never receive a resume write.
    uint32_t seq;
} waiter;

typedef struct {
    size_t removed;
    size_t woken;
} rt_waiter_completion;

// WAITER-STORE MUTATION POINTS — the complete list, re-derived at f2641713.
//
// Every site that inserts, removes, re-arms or rewrites an entry is below.
// Keep it complete: a partial enumeration is what let a re-arm branch be
// mistaken for an append (double retain on every absorbed wake) and let a
// symbol with no callers be counted as a live mutator. If you add a site,
// add a row.
//
// Rows name a file and a function on purpose. Line numbers would be exact
// today and quietly wrong after the next edit to any of these five files,
// and no gate measures that rot; a function name a reader can grep for
// survives it.
//
// INSERT — append one entry, always after rt_waiter_store_ensure_cap:
//   rt_async_waiter.c  add_waiter, net arm: appends (key, task, hint,
//       seq=0) to the fd owner's store, bumps net_len, attaches fd interest.
//   rt_async_waiter.c  add_waiter, generic arm: appends (key, task, hint,
//       seq=0) to the store rt_waiter_store_for_key routes to. A channel key
//       here is a select subscription; the append registers its pin.
//   rt_waiter_join_route.c  rt_waiter_add_join_waiter: appends a join entry
//       under the route lock, after revalidating the route.
//   rt_channel_lane.h  channel_park_prepare_locked, append arm: appends a
//       channel entry stamped with a freshly bumped task->park_seq, and
//       registers the pin that entry holds for as long as it exists.
//
// RE-ARM — rewrite an entry in place, no length change:
//   rt_channel_lane.h  channel_park_prepare_locked, dedupe arm: an absorbed
//       wake or compat_cv wakeup re-enters the park with its entry never
//       popped. This bumps task->park_seq and writes w->seq through the
//       existing entry INSTEAD of appending. Reading this as an append is
//       what would double-retain the registration on every absorbed wake.
//
// REMOVE — drop entries and compact:
//   rt_async_waiter.c  remove_waiter_from_store_seq, the shared compactor
//       behind remove_waiter and remove_waiter_generation: drops (key, task)
//       entries, generation-qualified when seq!=0, counts same-key survivors,
//       decrements net_len, and retires one channel pin per entry it dropped.
//   rt_async_waiter.c  rt_executor_wake_net_waiters_for_key_on_owner: drains
//       every entry of a net key into a wake batch, then detaches the fd
//       interest under the same lock.
//   rt_task_park.c  wake_key_all_with_policy, non-join arm: drains every
//       entry of a scope/timer/blocking key into a wake batch, and retires one
//       channel pin per drained entry -- inert today, because no caller passes
//       this a channel key, and present so the retire side stays complete.
//   rt_waiter_join_route.c  rt_waiter_remove_join_waiter_generation: drops
//       join entries matching (key, task) and, when seq!=0, that seq.
//   rt_waiter_join_route.c  rt_waiter_collect_join_waiters: drains every
//       entry of a join key into a wake batch.
//   rt_channel_lane.h  channel_pop_candidate_locked: memmoves out the first
//       FIFO entry for a channel key and retires that entry's pin; the caller
//       validates the candidate afterwards and drops it if the peer moved on.
//
// MOVE — remove from one shard's store and append to another's:
//   rt_waiter_route.c  rt_waiter_migrate_join_waiters: drains join entries
//       from the old owner and appends them to the new one, 16 at a time.
//       Old publish order, kept for the RV2-DEBT-020 negative control.
//   rt_waiter_route.c  rt_waiter_publish_join_owner_and_migrate: same
//       drain/append, but publishes the join route under the source lock
//       first. Only JOIN keys ever migrate.
//
// REALLOCATE — no entry semantics, but invalidates every waiter* into the
// store, which is why the re-arm above writes through w only under the lock
// and never across an ensure_cap:
//   rt_async_waiter.c  rt_waiter_store_ensure_cap — the only one left. The
//       wrapper ensure_waiter_cap was deleted with this list: it grew shard
//       0's store on its own, took no lock, and had no callers. Every insert
//       grows the store it is about to append to, under that store's lock.
//
// Channel keys never move. rt_channel.owner_shard_id is written only in
// rt_channel_new (rt_async_channel.c) and by rt_channel_bind_owner_shard
// (same file), whose one production caller is rt_far_channel_dispatch_create
// (rt_far_channel.c) — it binds BETWEEN rt_channel_new and rt_far_channel_mint,
// so the shard is fixed before the handle is minted and published and before
// any task can park on it. Nothing rebinds it afterwards, and both migrators
// above filter on join_key, so a channel entry never changes stores.
//
// A channel key CARRIED by a caller outlives its channel, and nothing counts
// that copy, so resolving it by casting the id back to a pointer is a read of
// storage that may already be gone. A far
// channel is freed at rt_far_channel.c's release_entry, and the deferred
// stale-key removal in rt_task_park.c's wake_task_with_policy still holds the
// parked task's channel key across the window in which the woken task
// completes, releases the registry entry and retires the object. Because the
// shard is fixed before any park (the paragraph above), channel_send_key /
// channel_recv_key stamp it into the key's owner_shard_id field and
// rt_waiter_route.c routes on that copy — a channel key must therefore be
// treated as an OPAQUE identity, never as a pointer to dereference. Entry
// MATCHING is unaffected: every comparison in this file's mutation points tests
// kind and id only.
//
// A channel key IN THE STORE is the one exception, and it is not a weakening of
// the rule above: the entry holds an internal pin, so while it exists the
// object it names is provably alive. That is why the two hooks below may act on
// the key as a pointer where nothing else may. The carried key is the dangerous
// one precisely because it is a copy nothing counts.
typedef struct {
    waiter* entries;
    size_t len;
    size_t cap;
    size_t net_len;
} rt_waiter_store;

typedef struct {
    uint64_t total;
    uint64_t join;
    uint64_t timer;
    uint64_t chan_send;
    uint64_t chan_recv;
    uint64_t net;
    uint64_t other;
} rt_waiter_trace_counts;

waker_key waker_none(void);
waker_key join_key(uint64_t id);
waker_key timer_key(uint64_t id, uint32_t owner_shard_id);
waker_key scope_key(uint64_t id, uint32_t owner_shard_id);
waker_key channel_send_key(const rt_channel* ch);
waker_key channel_recv_key(const rt_channel* ch);
waker_key channel_send_retry_key(const rt_channel* ch);
waker_key channel_recv_retry_key(const rt_channel* ch);
// Any of the four channel-owned kinds: the ordinary two and the retry two.
int waker_is_channel(waker_key key);
waker_key net_accept_key(int fd);
waker_key net_read_key(int fd);
waker_key net_write_key(int fd);
int waker_is_net(waker_key key);
waker_key blocking_key(uint64_t id);
waker_key remote_spawn_reply_key(uint64_t id, uint32_t owner_shard_id);
uint32_t rt_channel_owner_shard_id(const rt_channel* ch);
void rt_channel_bind_owner_shard(void* channel, uint32_t shard_id);

// A store entry on a channel key is one of the internal pins section 7 of
// docs/RUNTIME_V2.md counts, so the object cannot be reclaimed while the entry
// still names it. These two are the hooks the mutation points above call: every
// INSERT of a channel entry registers one, and every REMOVE of `count` of them
// retires that many. Both ignore a key of any other kind, so a caller that
// handles all kinds calls them unconditionally.
//
// This is what makes the key safe to act on. A key is otherwise an opaque
// identity precisely because it can outlive its object; while an entry holds
// it, the object is provably alive, so the pin is taken and released through
// the key itself rather than through a pointer a caller had to keep.
void rt_channel_key_registered(waker_key key);
void rt_channel_key_retired(waker_key key, size_t count);

rt_runtime_status rt_waiter_store_ensure_cap(rt_waiter_store* store);
rt_waiter_store* rt_waiter_store_for_key(rt_executor* ex, waker_key key);
rt_shard* rt_waiter_key_shard(rt_executor* ex, waker_key key);
void rt_waiter_migrate_join_waiters(rt_executor* ex,
                                    uint64_t task_id,
                                    uint32_t from_shard_id,
                                    uint32_t to_shard_id);
void rt_waiter_publish_join_owner_and_migrate(rt_executor* ex,
                                              rt_task* task,
                                              uint32_t from_shard_id,
                                              uint32_t to_shard_id);
rt_runtime_status
rt_waiter_add_join_waiter(rt_executor* ex, waker_key key, uint64_t task_id, uint32_t owner_hint);
void rt_waiter_remove_join_waiter_generation(rt_executor* ex,
                                             waker_key key,
                                             uint64_t task_id,
                                             uint32_t seq);
size_t rt_waiter_collect_join_waiters(rt_executor* ex,
                                      waker_key key,
                                      uint64_t** batch,
                                      size_t* batch_cap,
                                      const uint64_t* inline_batch);
rt_waiter_completion rt_executor_wake_net_waiters_for_key_on_owner(rt_executor* ex,
                                                                   waker_key key,
                                                                   uint32_t owner_shard_id);
uint32_t rt_net_owner_shard_for_key(rt_executor* ex, waker_key key, uint32_t fallback_shard_id);
uint32_t rt_net_owner_shard_probe_locked(rt_executor* ex, int fd, uint32_t hint_shard_id);
void remove_waiter(rt_executor* ex, waker_key key, uint64_t task_id);
void remove_waiter_generation(rt_executor* ex, waker_key key, uint64_t task_id, uint32_t seq);
void add_waiter(rt_executor* ex, waker_key key, uint64_t task_id);
void clear_wait_keys(rt_executor* ex, rt_task* task);
void add_wait_key(rt_executor* ex, rt_task* task, waker_key key);
void prepare_park(rt_executor* ex, rt_task* task, waker_key key, int already_added);
void rt_trace_collect_waiter_counts(const rt_executor* ex, rt_waiter_trace_counts* out);

#endif
