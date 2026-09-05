#ifndef SURGE_RUNTIME_NATIVE_RT_REMOTE_ADMIT_H
#define SURGE_RUNTIME_NATIVE_RT_REMOTE_ADMIT_H

#include "rt_async_internal.h"
#include "rt_remote_spawn.h"

// One request's way onto the transport, kept on its pending so a refused
// admission can be retried by the same task on its next poll.
//
// Two lanes decide it, in this order. First the request's OWN shard, where
// the reply will land: a request that expects a data-lane reply reserves one
// slot there before it is admitted anywhere, so a committed reply can never
// find the lane full (rt_transport.h, reply_reserved). Then the target's data
// lane, where the request itself goes. Either lane may be exhausted, and in
// both cases the producer PARKS -- the task registers on that shard's slot
// key (WAKER_TRANSPORT_SLOT), the caller answers PENDING, and a data slot
// freed there by a drain or a released reservation wakes it to try again.
// A caller never sees QUEUE_FULL for a request; the refusal is the
// transport's, and the transport's answer to it is a wait.
//
// The reservation is spent by the reply's enqueue or released by the
// pending's terminal transition, whichever comes first, and exactly once:
// `reserved` is exchanged to 0 by the one that gets there.
typedef struct rt_remote_admission {
    rt_transport_msg msg;
    uint32_t source_shard_id;
    uint32_t target_shard_id;
    uint8_t wants_reply;
    _Atomic uint8_t reserved;
    // Exchanged to 0 by whoever ends the park -- the producer's own retry, or
    // a teardown that abandons it -- so the registration is removed and the
    // never-enqueued message reference released exactly once between them.
    _Atomic uint8_t parked;
    uint8_t parked_on_source;
    // Set when the refusal rt_remote_admit answered was the executor's or
    // the target's shutdown, observed by a producer that woke from its park:
    // the caller reports DESTINATION_SHUTDOWN, not a generic refusal.
    uint8_t refused_by_shutdown;
} rt_remote_admission;

void rt_remote_admission_init(rt_remote_admission* adm,
                              const rt_transport_msg* msg,
                              int wants_reply);

// OK: the request is on the target's lane. UNAVAILABLE: the task is
// registered on a slot key and the caller answers PENDING. Anything else is a
// refusal the caller maps and reports. A parked admission is retried by
// calling this again from the same task.
rt_transport_status rt_remote_admit(rt_executor* ex, rt_task* current, rt_remote_admission* adm);

// A caller that gives up on a parked admission (cancelled, torn down):
// unregisters `task_id`'s slot wait and releases the reservation it may
// hold. Safe from the caller's own poll and from a teardown sweep alike.
// Answers 1 when it ended a park -- the caller then releases the message
// reference the submission took for an envelope nothing will enqueue.
int rt_remote_admission_abandon(rt_executor* ex, uint64_t task_id, rt_remote_admission* adm);

// Claims the reservation for spending or releasing; 1 for the one caller
// that now owns that step, 0 for everyone after it.
int rt_remote_admission_take_reservation(rt_remote_admission* adm);
// Gives an unspent reservation back to the source lane (after a successful
// take) and wakes producers parked there. Must not run under a shard lock.
void rt_remote_admission_release_reservation(rt_executor* ex, const rt_remote_admission* adm);

// The belt for a reservation no terminal path spent or released, run from
// the pending's final free: releases it if no shard lock forbids the wake,
// releases it without the wake if the source's own lock is held (the next
// data pop on that lane wakes its parked producers anyway), and otherwise
// counts an orphan rather than deadlock. Every stand asserts the count is 0.
void rt_remote_admission_release_reservation_belt(rt_executor* ex, const rt_remote_admission* adm);
uint64_t rt_remote_admission_orphan_count(void);

// Wakes every producer parked on every shard's data lane: shutdown's way of
// letting them observe the shutdown and fail their requests.
void rt_remote_admission_wake_all_parked(rt_executor* ex);

// The spawn family's reading of a refused admission (rt_remote_spawn.h's
// status set): shutdown observed from a park is DESTINATION_SHUTDOWN.
rt_remote_spawn_status rt_remote_spawn_admission_refusal(const rt_remote_admission* adm,
                                                         rt_transport_status status);

#endif // SURGE_RUNTIME_NATIVE_RT_REMOTE_ADMIT_H
