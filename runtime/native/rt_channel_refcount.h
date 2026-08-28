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
// THE THREE HOLDERS, AND WHERE EACH TAKES ITS PIN. This list is a MAP: a
// reader closing a coverage hole looks here to see whether their path already
// holds one, so a site named here that takes no pin sends them away satisfied.
// Every entry below was checked against the call sites of rt_channel_pin.
//
//   A registered waiter -- channel_park_prepare_locked's append arm
//     (rt_channel_lane.h), retired by channel_pop_candidate_locked and by
//     remove_waiter_from_store_seq (rt_async_waiter.c).
//   A select subscription -- add_waiter's generic arm (rt_async_waiter.c),
//     retired by the same removal.
//   A claimed detached operation -- every function that calls rt_channel_pin,
//     each pinning for its own duration: channel_send_pinned, behind
//     rt_channel_send and rt_channel_send_yield, and rt_channel_recv
//     (rt_async_channel.c); rt_channel_try_send, rt_channel_try_recv,
//     rt_channel_send_blocking, rt_channel_recv_blocking and rt_channel_close
//     (rt_channel_sync.c); and rt_select_poll's two channel arms
//     (rt_async_select.c), which pin before rt_channel_claim_recv_locked /
//     claim_send_locked and unpin after the finish -- for a winning recv arm,
//     after rt_channel_release_payload has destroyed the value it took.
//
//   NOT here, and deliberately: the four *_locked claim/finish helpers
//     (rt_channel_sync.c) and rt_channel_release_payload (rt_async_channel.c)
//     take no pin of their own. They are the INSIDE of somebody else's claimed
//     operation -- their caller releases the lock between them, so the pin has
//     to span the bracket and cannot be taken per call. They fail closed on the
//     caller's pin instead; see rt_channel_assert_pinned below.
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
//
// ADMISSION IS THE SAME INSTRUCTION AS THE COUNT. A pin is taken outside every
// lock, so it shares none with the reclaim, and a flag read followed by a count
// written is a load-then-store that no memory ordering linearises against the
// reclaim's store-then-load: both sides could believe they were first. Pinning
// is therefore ONE read-modify-write on ONE word that carries the count and the
// teardown seal together, and so is sealing it; rt_channel_refcount.c states
// the argument, and rt_scope_membership.h states it at length for the word that
// taught the repository to ask.
void rt_channel_pin(void* channel);
void rt_channel_unpin(void* channel);

// Fail-closed for the surface named above as NOT taking a pin of its own: the
// four *_locked claim/finish helpers and rt_channel_release_payload. Each is a
// step inside a bracket whose caller releases the lock in the middle, so the
// hold that covers it belongs to the caller -- and an unpinned caller there is
// one whose channel may be reclaimed between the claim and the move. Panics
// rather than continuing, because the alternative evidence is the use-after-
// free that follows. Inert when the pin negative control has asked pins not to
// count, since that run has no hold to find by construction.
void rt_channel_assert_pinned(const void* channel);

// Called by rt_channel_free under the owner lock before it detaches anything.
// SEALS the object against any further pin and refuses the reclaim when
// something still names it: a handle outstanding, a pin outstanding, or a
// waiter still registered on either of the channel's two keys. The seal and the
// pin refusal are one read-modify-write for the reason rt_channel_pin gives
// above -- checking the count and then marking the object dying as two steps is
// exactly the window this closes. The registration leg used to report and
// continue, which refused nothing; now that a registration IS a pin, an entry
// here means the counting disagrees with the store it summarises.
//
// `owner` is the channel's owner shard, locked by the caller, or NULL when the
// runtime has no shard for it -- then there is no store to read and the two
// counts answer alone.
//
// rt_channel_free, rt_channel_free_when_unlocked and rt_channel_reclaim_drain
// are declared with the rest of the async surface in rt_async_internal.h and
// defined in this module, next to the counts that decide when they run.
struct rt_shard;
void rt_channel_seal_reclaimable_locked(const struct rt_shard* owner, void* channel);

#endif
