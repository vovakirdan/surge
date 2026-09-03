#ifndef SURGE_RUNTIME_NATIVE_RT_TRANSPORT_INTERNAL_H
#define SURGE_RUNTIME_NATIVE_RT_TRANSPORT_INTERNAL_H

#include "rt_async_internal.h"

// The seam between the transport's two files, private to them. rt_transport.c
// owns the envelope queues and the wake pipe -- what a message IS and where it
// sits; rt_transport_park.c owns admission, the park/wake protocol against the
// shard's worker, shutdown and the drains -- what a message DOES to a shard.
// Nothing here is for anyone else: the public surface is rt_transport.h.

rt_transport_msg_class rt_transport_msg_class_of(rt_transport_msg_kind kind);
void rt_transport_wake_write(rt_transport_state* state);
void rt_transport_wake_drain(rt_transport_state* state);
rt_transport_status
rt_transport_push_locked(rt_transport_state* state, const rt_transport_msg* msg, int control);
rt_transport_status
rt_transport_pop_locked(rt_transport_state* state, rt_transport_msg* out, int control);
// A reply that holds a reservation on this lane: spends it, never refused.
rt_transport_status rt_transport_push_reserved_reply_locked(rt_transport_state* state,
                                                            const rt_transport_msg* msg);
rt_transport_status rt_transport_reserve_reply_slot_locked(rt_transport_state* state);
void rt_transport_release_reply_slot_locked(rt_transport_state* state);

#endif // SURGE_RUNTIME_NATIVE_RT_TRANSPORT_INTERNAL_H
