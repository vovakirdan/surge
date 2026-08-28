#ifndef SURGE_RUNTIME_NATIVE_RT_CHANNEL_REFCOUNT_H
#define SURGE_RUNTIME_NATIVE_RT_CHANNEL_REFCOUNT_H

// What still names a channel, counted by the channel.
//
// A Channel<T> handle is one machine word naming the object, so the handle
// itself carries no state; what makes the object reclaimable is that nothing
// is left that could reach it. The counts live in the channel for the same
// reason the task's handle count lives in the task (rt_task_lifetime.c): a
// handle copied into another shard's frame is a holder the first frame cannot
// see, so the two can only agree through the object.
//
// TWO KINDS, counted apart, per docs/RUNTIME_V2.md section 7 "Channel
// lifetime": "User handle references are the copies a program holds. Internal
// pins are held by a registered waiter, a select subscription, or a claimed
// detached operation. Both keep the object alive, which is what makes it
// impossible to free a channel while a generated callback is still running
// outside the owner lock."
//
// Keeping them apart is not bookkeeping vanity. A handle count answers the
// language's question -- may the program still send on this? -- and a pin
// count answers the runtime's -- is anyone inside an operation on it? A single
// number could not tell a channel with no holders that is quiescent from one
// with no holders that is mid-delivery, and only the first may be destroyed.
//
// No RELEASE here runs generated code or takes a scheduler lock (section 8,
// P2): the last one hands the object to rt_channel_free_when_unlocked, which
// reclaims immediately on a lane that holds no lock and defers to the moment
// that lane lets go otherwise -- because reclaiming drains the buffer, and a
// drain runs the element's own drop.
//
// The RECLAIM itself does both, in the order section 7 prescribes and in that
// order only: it takes the owner lock, marks the object dying, detaches every
// initialized slot and invalidates the generations; releases the lock; runs
// drop_in_place on what it detached; frees the storage. Nothing user-written
// runs while the lock is held, and nothing is half-detached while it runs.

// Zeroes a fresh channel header and gives it the one handle its creator holds.
// Called by rt_channel_new instead of the bare memset it replaces, so that no
// channel exists for an instant with a count of zero.
void rt_channel_handle_refs_init(void* channel);

// One more copy of the handle exists. NULL is a no-op: a container slot the
// handle was moved out of holds NULL and the container's glue still visits it.
void rt_channel_handle_retain(void* channel);

// A copy of the handle the program will never send or receive through again.
// When it was the last thing naming the object -- no handle and no pin left --
// the object is reclaimed, which drops every payload it still owns.
void rt_channel_handle_drop(void* channel);

// The runtime's own hold, for the span of one operation: a registered waiter,
// a select subscription, a claimed detached operation. A pin is NOT a handle:
// it says "an operation is inside this object", never "a program may still
// use it", and the two are counted apart for that reason.
//
// THE THREE HOLDERS, AND WHERE EACH TAKES ITS PIN.
//
//   A registered waiter -- channel_park_prepare_locked's append arm
//     (rt_channel_lane.h), retired by channel_pop_candidate_locked and by
//     remove_waiter_from_store_seq (rt_async_waiter.c).
//   A select subscription -- add_waiter's generic arm (rt_async_waiter.c),
//     retired by the same removal.
//   A claimed detached operation -- rt_channel_send / rt_channel_send_yield /
//     rt_channel_recv (rt_async_channel.c), rt_channel_close and the
//     try/blocking/claim lanes (rt_channel_sync.c), rt_channel_release_payload.
//
// WHY THE THIRD ONE IS AN OPERATION AND NOT A LINE. A channel operation
// RELEASES the owner shard lock across every step that runs generated code,
// because no element move or drop may run under a scheduler lock. There are
// nine such windows and the operation is inside the object across all of them:
// channel_stage_locked's move into a park slot and the buffered push beside it;
// the four takes in rt_channel_recv (a delivered value, the ring's head, a
// same-shard parked sender's slot, a foreign one's); channel_end_park_locked's
// drop and channel_stage_into_ring_locked's move (rt_channel_lane.h); and
// channel_wake_only / channel_deliver_foreign / channel_claim_foreign_sender_locked,
// which release the owner lock to take a peer's. A pin per window would have to
// be paired down every early return in all of them; one pin for the operation
// covers each window by construction, and is the reading section 7 asks for --
// the operation is what is CLAIMED, and the moves inside it are what is
// DETACHED.
//
// The two holders compose, and the composition is load-bearing: popping a
// candidate retires a waiter's pin, and the operation that popped it keeps
// using the channel afterwards on its own.
void rt_channel_pin(void* channel);
void rt_channel_unpin(void* channel);

// Fail-closed, called by rt_channel_free under the owner lock before it detaches
// anything: nothing may still name the object. Panics when a handle is
// outstanding, when a pin is, or when a waiter is still registered on either of
// the channel's two keys. That last one used to report and continue, because
// nothing took a pin for a registration; now that the registration IS a pin, an
// entry here means the counting is wrong, which is exactly what a fail-closed
// assertion is for.
//
// `owner` is the channel's owner shard, locked by the caller, or NULL when the
// runtime has no shard for it -- then there is no store to read and the two
// counts answer alone.
//
// rt_channel_free, rt_channel_free_when_unlocked and rt_channel_reclaim_drain
// are declared with the rest of the async surface in rt_async_internal.h and
// defined in this module, next to the counts that decide when they run.
struct rt_shard;
void rt_channel_assert_reclaimable_locked(const struct rt_shard* owner, void* channel);

#endif
