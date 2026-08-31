#include "rt_async_internal.h"

// Waker keys: the identity a park is filed under, and the only place each kind
// is minted. A key is an opaque identity that can OUTLIVE the object it names,
// which is why the shard a key must be routed to is stamped into the key here,
// while the caller still holds the object live in hand, rather than resolved
// later by dereferencing an id. Everything downstream only ever copies the key
// (task->park_key, task->wait_keys[], the waiter store entries, the wake path's
// captured stale key), so a copy is always as routable as the original.
//
// Nothing here takes a lock, touches the waiter store, or reads a task. The
// store side and the fd-registry bridge live in rt_async_waiter.c; the task's
// wait-key set lives in rt_task_wait_keys.c. Declarations for all three are in
// rt_waiter.h.

waker_key waker_none(void) {
    waker_key key = {WAKER_NONE, 0, 0};
    return key;
}

int waker_valid(waker_key key) {
    return key.kind != WAKER_NONE && key.id != 0;
}

waker_key join_key(uint64_t id) {
    waker_key key = {WAKER_JOIN, id, 0};
    return key;
}

// A timer key carries the sleeping task's owner shard, for the same reason a
// channel key carries the channel's: the key OUTLIVES the object it names. The
// deferred stale-key removal in wake_task_with_policy captures a parked task's
// timer key, drops the owner shard lock, and only then removes the waiter --
// and in that window the timeout that was waiting on this sleep can resolve,
// release the last reference and free the task. Routing by looking the id up
// and reading the task's placement therefore reads freed memory. The shard is
// stamped here instead, while the caller holds the task live in hand.
//
// It is also the shard the park itself used: poll_sleep_task adds the deadline
// to that shard's sleep store, so recording it keeps the waiter entry and the
// deadline index on one shard even if the task is later re-placed. Resolving
// live would send the removal to the new owner's store, where the entry it
// wants has never been.
waker_key timer_key(uint64_t id, uint32_t owner_shard_id) {
    waker_key key = {WAKER_TIMER, id, owner_shard_id};
    return key;
}

waker_key scope_key(uint64_t id) {
    waker_key key = {WAKER_SCOPE, id, 0};
    return key;
}

waker_key blocking_key(uint64_t id) {
    waker_key key = {WAKER_BLOCKING, id, 0};
    return key;
}

waker_key remote_spawn_reply_key(uint64_t id, uint32_t owner_shard_id) {
    waker_key key = {WAKER_REMOTE_SPAWN_REPLY, id, owner_shard_id};
    return key;
}

// Channel keys carry the channel's owner shard, read HERE — the only place a
// channel key is ever built, and a place every caller reaches while holding the
// channel (send/recv/close/select all have it live in hand). Everything
// downstream copies the key: task->park_key, task->wait_keys[], the wake path's
// captured stale key, the store entries. That copy is what lets the routing
// path resolve a channel key without dereferencing it, which matters because a
// far channel IS freed under a carried key (RV2-DEBT-199). The shard is fixed
// before any task can park (rt_waiter.h: rt_channel_bind_owner_shard runs
// between rt_channel_new and the mint), so a key built at any later moment
// stamps the same value.
waker_key channel_send_key(const rt_channel* ch) {
    waker_key key = {WAKER_CHAN_SEND, (uint64_t)(uintptr_t)ch, rt_channel_owner_shard_id(ch)};
    return key;
}

waker_key channel_recv_key(const rt_channel* ch) {
    waker_key key = {WAKER_CHAN_RECV, (uint64_t)(uintptr_t)ch, rt_channel_owner_shard_id(ch)};
    return key;
}

waker_key net_accept_key(int fd) {
    waker_key key = {WAKER_NET_ACCEPT, (uint64_t)fd, 0};
    return key;
}

waker_key net_read_key(int fd) {
    waker_key key = {WAKER_NET_READ, (uint64_t)fd, 0};
    return key;
}

waker_key net_write_key(int fd) {
    waker_key key = {WAKER_NET_WRITE, (uint64_t)fd, 0};
    return key;
}

int waker_is_net(waker_key key) {
    waker_kind kind = (waker_kind)key.kind;
    return kind == WAKER_NET_ACCEPT || kind == WAKER_NET_READ || kind == WAKER_NET_WRITE;
}
