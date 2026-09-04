#ifndef SURGE_RUNTIME_NATIVE_RT_CHANNEL_RETRY_H
#define SURGE_RUNTIME_NATIVE_RT_CHANNEL_RETRY_H

#include <stddef.h>
#include <stdint.h>

typedef struct rt_executor rt_executor;
typedef struct rt_task rt_task;
typedef struct rt_channel rt_channel;
typedef struct rt_shard rt_shard;

// The channel's single-transfer claim is refused, never queued (RUNTIME_V2.md,
// "A claim is refused, never queued"): a refused operation goes back to
// selection as a publication like any other. What bounds that is a budget: a
// logical operation -- one direct send, one direct receive, or one select
// with its whole arm set -- may be republished after at most this many actual
// claim refusals. The eighth refusal EXHAUSTS the budget and the operation
// parks on the channel's own retry key, to be woken by the release of the
// claim that refused it; close terminates the retry. RV2-DEBT-277.
//
// RV2_DEBT_277_RETRY_NEGATIVE_CONTROL widens the budget by one, which the
// budget rows read as a refusal count of nine -- the Rule 13 mutant.
#ifdef RV2_DEBT_277_RETRY_NEGATIVE_CONTROL
#define RT_CHANNEL_RETRY_BUDGET ((uint8_t)9)
#else
#define RT_CHANNEL_RETRY_BUDGET ((uint8_t)8)
#endif

// How many earlier refusals of the same logical operation are remembered
// beside the one that exhausts it: the budget less one. A select is refused
// on whichever arm's claim is busy at each poll, and at exhaustion it must
// park on EVERY channel that refused it, not only the eighth -- a release on
// an earlier arm is as much its wake as one on the last.
#define RT_CHANNEL_RETRY_PREFIX ((uint8_t)7)

typedef enum {
    RT_CHANNEL_RETRY_NONE = 0,
    RT_CHANNEL_RETRY_SEND = 1,
    RT_CHANNEL_RETRY_RECV = 2,
    RT_CHANNEL_RETRY_SELECT = 3,
} rt_channel_retry_operation;

typedef enum {
    RT_CHANNEL_CLAIM_REFUSAL_RING_PUSH = 0,
    RT_CHANNEL_CLAIM_REFUSAL_RING_POP = 1,
    RT_CHANNEL_CLAIM_REFUSAL_PARK_TAKE = 2,
    // A rendezvous claim is out on the channel's oldest receiver
    // (rt_channel_claim.h): no send is admitted until it is retired.
    RT_CHANNEL_CLAIM_REFUSAL_RENDEZVOUS = 3,
    RT_CHANNEL_CLAIM_REFUSAL_COUNT = 4,
} rt_channel_claim_refusal_cause;

// One remembered refusal: the channel, the direction the arm was claiming in,
// and what refused it.
typedef struct {
    uint64_t channel;
    uint8_t operation;
    uint8_t cause;
} rt_channel_retry_refusal;

// Per-task retry state. It belongs to the one poll operation currently
// executing the task -- only that poller reads or writes it -- survives
// public-queue republication and an ordinary readiness park, and is reset
// only when the operation makes progress or completes.
typedef struct {
    uint8_t operation;
    uint8_t count;
    uint8_t prefix_len;
    uint64_t key_id;
    rt_channel_retry_refusal prefix[RT_CHANNEL_RETRY_PREFIX];
} rt_channel_retry_state;

// Records one refusal against the task's current logical operation and
// answers non-zero once the budget is exhausted. key_id is the channel for a
// direct send/receive and zero for a select, whose arm set is one operation;
// `channel` and `arm` name the arm actually refused, remembered in the prefix.
int rt_channel_retry_refused(rt_task* task,
                             rt_channel_retry_operation operation,
                             uint64_t key_id,
                             const rt_channel* channel,
                             rt_channel_retry_operation arm,
                             rt_channel_claim_refusal_cause cause);
void rt_channel_retry_republished(void);
// The reset itself; rt_channel_retry_reset (rt_channel_lane.h) is the inline
// fast path that skips it for a task that was never refused.
void rt_channel_retry_reset_slow(rt_task* task);

// Direct send/receive: the refusal is counted, and the operation either
// republishes (owner lane released, no key) or, exhausted, parks on the
// channel's retry key (owner lane released, pending_key set).
void rt_channel_wait_after_claim_refusal_locked(rt_shard* channel_shard,
                                                rt_channel* channel,
                                                rt_task* task,
                                                rt_channel_retry_operation operation,
                                                rt_channel_claim_refusal_cause cause);

// A claim was released on this channel: wake at most one exhausted retrier
// per direction, without touching the ordinary sender/receiver FIFOs. Called
// with the channel owner lane held. rt_channel_claim_released_locked
// (rt_channel_lane.h) is the inline entry that comes here only when
// somebody stands on a retry key.
void rt_channel_claim_released_slow(rt_executor* ex, rt_shard* ch_shard, const rt_channel* ch);

void rt_channel_trace_claim_released(void);
size_t rt_channel_trace_append(char* buf, size_t* pos, size_t cap);

#endif // SURGE_RUNTIME_NATIVE_RT_CHANNEL_RETRY_H
