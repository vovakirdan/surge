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
// Nothing here runs generated code and nothing here takes a scheduler lock
// (section 8, P2). The last release hands the object to
// rt_channel_free_when_unlocked, which reclaims immediately on a lane that
// holds no lock and defers to the moment that lane lets go otherwise --
// because reclaiming drains the buffer, and a drain runs the element's own
// drop.

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
// C0 defines the counter and the pair; the registration sites are C3's. Until
// then the count is provably zero, which is what makes the fail-closed
// assertion below meaningful rather than aspirational.
void rt_channel_pin(void* channel);
void rt_channel_unpin(void* channel);

// Fail-closed, called by rt_channel_free before it destroys anything: nothing
// may still name the object. Panics when a handle or a pin is outstanding, and
// reports (without refusing) a waiter still registered on either of the
// channel's two keys -- that one is a debug invariant rather than the
// mechanism, because the mechanism is the pin a registered waiter will hold.
void rt_channel_assert_reclaimable(void* channel);

#endif
