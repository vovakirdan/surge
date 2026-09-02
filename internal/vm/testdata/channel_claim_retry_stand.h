#ifndef SURGE_INTERNAL_VM_TESTDATA_CHANNEL_CLAIM_RETRY_STAND_H
#define SURGE_INTERNAL_VM_TESTDATA_CHANNEL_CLAIM_RETRY_STAND_H

// The RV2-DEBT-277 bounded claim-retry stand: one process, no worker threads,
// the runtime's channel entry points driven from a hand-made current task
// while a rival claim on the ring is HELD by the driver. Each mode prints one
// OK_* census line; a broken property prints FAIL: and exits non-zero.

#include "rt_async_internal.h"
#include "rt_channel_lane.h"

#include <stddef.h>
#include <stdint.h>

typedef struct {
    rt_executor* ex;
    rt_task* task;
    void* handle;
    rt_channel* channel;
    rt_shard* owner;
    rt_typed_fifo_ticket held;
} retry_fixture;

extern const rt_value_ops retry_word_ops;
extern const rt_value_ops retry_word_probe_ops;

rt_task* make_retry_task(rt_executor* ex);
_Noreturn void stand_fail(const char* message);
retry_fixture make_fixture_with_ops(const rt_value_ops* ops);
retry_fixture make_fixture(void);
void drive_direct_refusals(retry_fixture* f, uint64_t* value);
void commit_retry_park(retry_fixture* f);
void release_held_claim(retry_fixture* f);
void hold_ring_push(retry_fixture* f);
void clear_prepared_waiter(retry_fixture* f);
void add_dead_receiver(retry_fixture* f, int foreign_owner_hint);
void turn_held_push_into_pop(retry_fixture* f);
void release_held_pop(retry_fixture* f);
void require_woken(const retry_fixture* f);
void resume_for_poll(retry_fixture* f);
void finish_fixture(retry_fixture* f);
void arm_park_release_probe(retry_fixture* f, uint64_t value);
int park_release_probe_is_armed(void);
void disarm_park_release_probe(void);
size_t retry_waiter_count(retry_fixture* f, waker_key key);
uint32_t retry_pin_count(const retry_fixture* f);

void run_direct_mode(void);
void run_select_mode(void);
void run_select_identity_mode(void);
void run_select_prefix_mode(void);
void run_select_default_mode(void);
void run_recovery_reset_mode(int foreign_recovery);
void run_park_finish_release_mode(void);
void run_handoff_mode(const char* state);
void run_handoff_direct_first_mode(int second_direct);
void run_register_verify_mode(const char* action);
void run_recv_mode(void);
void run_close_mode(void);

#endif // SURGE_INTERNAL_VM_TESTDATA_CHANNEL_CLAIM_RETRY_STAND_H
