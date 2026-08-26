// nanosleep, which this stand uses to wait on task status from the outside.
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#define _POSIX_C_SOURCE 199309L

#include "rt_async_internal.h"

#include "rt_channel_lane.h"

#include <stdatomic.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

// A channel carrying an OWNED element, driven by more senders than the channel
// has park slots.
//
// What an owned element buys that an inert word cannot. Its move EMPTIES the
// source, so a value that arrives as zero is proof that the runtime read
// storage it had already moved out of -- a send that re-reads its caller's
// place after a park is the way that happens. Its drop is counted, so a value
// destroyed instead of delivered, or destroyed twice, shows up as a number at
// the end rather than as a wrong answer somewhere later.
//
// Why MORE senders than park slots. A channel stages a parked sender's value in
// a slot it owns, and that pool is a small constant. Once it is exhausted, a
// sender parks holding its own value and can only be WOKEN to retry -- never
// acked, because nothing of its was delivered. Ten senders against eight slots
// is what makes that path ordinary rather than exotic.

enum {
    POLL_OWNED_MAKER = 9301,
    POLL_OWNED_SENDER = 9302,
    POLL_OWNED_RECEIVER = 9303,
    POLL_OWNED_ONE_SEND = 9304,
};

#define OWNED_SENDERS 10u
#define OWNED_PER_SENDER 20u
#define OWNED_TOTAL (OWNED_SENDERS * OWNED_PER_SENDER)
#define OWNED_CAPACITY 2u

// Which buffer the maker builds. Buffered and unbuffered are different
// rendezvous paths through the same storage, and only one of them can be the
// default, so the mode says which.
static _Atomic uint64_t g_owned_capacity = OWNED_CAPACITY;

// The element owns a heap allocation, which is what makes "moved or dropped
// exactly once" a question a sanitizer can answer: a value delivered twice
// frees the same block twice, and one destroyed instead of delivered shows up
// in the drop census below.
typedef struct {
    uint64_t marker;
    char* text;
} owned_element;

#define OWNED_TEXT_BYTES 24

// The entry point's argv, which this stand has none of and rt_io.c requires.
int rt_argc = 0;
char** rt_argv_raw = NULL;

static _Atomic(void*) g_owned_chan;
static _Atomic uint32_t g_owned_drops;
static _Atomic uint32_t g_owned_received;
static _Atomic uint32_t g_owned_bad_markers;
static _Atomic uint32_t g_owned_closed;
static _Atomic uint32_t g_owned_seen[OWNED_TOTAL + 1u];
static _Atomic uint32_t g_owned_progress[OWNED_SENDERS];
static _Atomic uint64_t g_owned_sender_ids[OWNED_SENDERS];

static void owned_move(void* destination, void* source) {
    owned_element* to = (owned_element*)destination;
    owned_element* from = (owned_element*)source;
    to->marker = from->marker;
    to->text = from->text;
    // A move CONSUMES its source: the obligation is the destination's now, and
    // a second read of the source must not find one.
    from->marker = 0;
    from->text = NULL;
}

static void owned_drop(void* value) {
    owned_element* typed = (owned_element*)value;
    free(typed->text);
    typed->text = NULL;
    typed->marker = 0;
    atomic_fetch_add_explicit(&g_owned_drops, 1, memory_order_acq_rel);
}

// Builds one value with its obligation attached.
static int owned_make(owned_element* out, uint64_t marker) {
    out->marker = marker;
    out->text = (char*)malloc(OWNED_TEXT_BYTES);
    if (out->text == NULL) {
        return 0;
    }
    snprintf(out->text, OWNED_TEXT_BYTES, "%llu", (unsigned long long)marker);
    return 1;
}

static rt_carrier_status
owned_plan_cross(const void* source, rt_cross_mode mode, rt_cross_plan* out) {
    // A local channel never crosses; the slot refuses rather than inventing a
    // plan, which is what the mandatory-but-vacuous slot is for.
    (void)source;
    (void)mode;
    (void)out;
    return RT_CARRIER_STATUS_INVALID_STATE;
}

static const rt_value_ops owned_ops = {
    .layout = {.size = sizeof(owned_element),
               .align = _Alignof(owned_element),
               .stride = sizeof(owned_element),
               .flags = RT_VALUE_FLAG_DROPPABLE},
    .move_init = owned_move,
    .copy_init = NULL,
    .clone_init = NULL,
    .drop_in_place = owned_drop,
    .trace = NULL,
    .plan_cross = owned_plan_cross,
    .cross_move_init = NULL,
    .cross_clone_init = NULL,
};

static void owned_sleep_us(unsigned long micros) {
    struct timespec ts;
    ts.tv_sec = (time_t)(micros / 1000000UL);
    ts.tv_nsec = (long)((micros % 1000000UL) * 1000UL);
    while (nanosleep(&ts, &ts) != 0) {
    }
}

// A sender learns which block of values is its own by claiming a row of the
// table on its first poll and finding the same row on every later one.
static uint32_t owned_sender_index(void) {
    uint64_t me = rt_current_task_id();
    for (uint32_t i = 0; i < OWNED_SENDERS; i++) {
        if (atomic_load_explicit(&g_owned_sender_ids[i], memory_order_acquire) == me) {
            return i;
        }
    }
    for (uint32_t i = 0; i < OWNED_SENDERS; i++) {
        uint64_t empty = 0;
        if (atomic_compare_exchange_strong_explicit(
                &g_owned_sender_ids[i], &empty, me, memory_order_acq_rel, memory_order_acquire)) {
            return i;
        }
    }
    return OWNED_SENDERS;
}

static void poll_owned_maker(void) {
    atomic_store_explicit(
        &g_owned_chan,
        rt_channel_new(
            atomic_load_explicit(&g_owned_capacity, memory_order_acquire), &owned_ops, 0),
        memory_order_release);
    rt_async_return(NULL, &(uint64_t){0});
}

static void poll_owned_sender(void) {
    void* channel = atomic_load_explicit(&g_owned_chan, memory_order_acquire);
    uint32_t index = owned_sender_index();
    if (index >= OWNED_SENDERS || channel == NULL) {
        rt_async_return(NULL, &(uint64_t){0});
    }
    for (;;) {
        uint32_t done = atomic_load_explicit(&g_owned_progress[index], memory_order_acquire);
        if (done >= OWNED_PER_SENDER) {
            rt_async_return(NULL, &(uint64_t){1});
        }
        // Rebuilt from the progress counter on every entry, exactly as a
        // compiled sender's value is re-presented from its async frame when a
        // parked send is re-polled.
        owned_element value;
        if (!owned_make(&value, (uint64_t)index * OWNED_PER_SENDER + done + 1u)) {
            rt_async_return(NULL, &(uint64_t){0});
        }
        if (!rt_channel_send(channel, &value)) {
            // The send did not complete: the value is still this task's, and
            // the next poll rebuilds it from the progress counter. Release the
            // obligation this attempt made so the retry's is the only one.
            free(value.text);
            rt_async_yield(NULL, 0);
        }
        atomic_fetch_add_explicit(&g_owned_progress[index], 1, memory_order_acq_rel);
    }
}

// Sends exactly one value and returns. With no receiver and no buffer the send
// parks, and the value it is holding is staged in a slot the channel owns --
// which is the state the cancellation row is about.
static void poll_owned_one_send(void) {
    void* channel = atomic_load_explicit(&g_owned_chan, memory_order_acquire);
    owned_element value;
    if (channel == NULL || !owned_make(&value, 1)) {
        rt_async_return(NULL, &(uint64_t){0});
    }
    if (!rt_channel_send(channel, &value)) {
        free(value.text);
        rt_async_yield(NULL, 0);
    }
    atomic_fetch_add_explicit(&g_owned_progress[0], 1, memory_order_acq_rel);
    rt_async_return(NULL, &(uint64_t){1});
}

static void poll_owned_receiver(void) {
    void* channel = atomic_load_explicit(&g_owned_chan, memory_order_acquire);
    for (;;) {
        owned_element got = {.marker = 0, .text = NULL};
        uint8_t status = rt_channel_recv(channel, &got);
        if (status == 0) {
            rt_async_yield(NULL, 0);
        }
        if (status == 2) {
            atomic_store_explicit(&g_owned_closed, 1, memory_order_release);
            rt_async_return(NULL, &(uint64_t){1});
        }
        char expected[OWNED_TEXT_BYTES];
        snprintf(expected, sizeof(expected), "%llu", (unsigned long long)got.marker);
        if (got.marker == 0 || got.marker > OWNED_TOTAL || got.text == NULL ||
            strcmp(got.text, expected) != 0) {
            atomic_fetch_add_explicit(&g_owned_bad_markers, 1, memory_order_acq_rel);
        } else {
            atomic_fetch_add_explicit(&g_owned_seen[got.marker], 1, memory_order_acq_rel);
        }
        // The receiver owns what arrived, so it is the one that ends the
        // obligation; a value the RUNTIME destroys instead is counted by the
        // drop census and is a different outcome entirely.
        free(got.text);
        atomic_fetch_add_explicit(&g_owned_received, 1, memory_order_acq_rel);
    }
}

// The program-side symbols the runtime links against. Each is declared before
// it is defined because the stand is compiled with -Wmissing-prototypes, and
// each declaration is exempted by name because the emitter owns this spelling.
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_poll_call(uint64_t id);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_call(uint64_t id, void* state);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_result_call(uint64_t id, void* value);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_abandoned_state_call(uint64_t id, void* state);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_blocking_call(uint64_t id, void* state, void* out_dst);

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_poll_call(uint64_t id) {
    switch (id) {
        case POLL_OWNED_MAKER:
            poll_owned_maker();
            break;
        case POLL_OWNED_SENDER:
            poll_owned_sender();
            break;
        case POLL_OWNED_RECEIVER:
            poll_owned_receiver();
            break;
        case POLL_OWNED_ONE_SEND:
            poll_owned_one_send();
            break;
        default:
            break;
    }
    rt_async_return(NULL, &(uint64_t){0});
}

// No harness state carries a drop obligation, and no blocking work is
// submitted: reaching either of these is a defect in the stand, not a result.
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
}

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_result_call(uint64_t id, void* value) {
    (void)id;
    (void)value;
}

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_abandoned_state_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
}

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_blocking_call(uint64_t id, void* state, void* out_dst) {
    (void)id;
    (void)state;
    if (out_dst != NULL) {
        *(uint64_t*)out_dst = 0;
    }
    return;
}

static int owned_fail(const char* message) {
    if (message != NULL) {
        fputs(message, stderr);
        fputc('\n', stderr);
    }
    return 1;
}

static rt_task* owned_alloc_ready_task(rt_executor* ex, int64_t poll_fn_id) {
    uint64_t id = ex->next_id++;
    ensure_task_cap(ex, id);
    rt_task* task = (rt_task*)rt_alloc(sizeof(rt_task), _Alignof(rt_task));
    if (task == NULL) {
        return NULL;
    }
    memset(task, 0, sizeof(*task));
    // A stand's task answers with a machine word, which is exactly what the
    // opaque-word descriptor describes: the result slot carries it the same way
    // it carries a compiled type's value.
    (void)rt_value_cell_bind(&task->result, rt_channel_opaque_word_ops());
    task->id = id;
    task->poll_fn_id = poll_fn_id;
    task->kind = TASK_KIND_USER;
    task_status_store(task, TASK_READY);
    task_cancelled_store(task, 0);
    task_enqueued_store(task, 0);
    (void)task_wake_token_exchange(task, 0);
    atomic_store_explicit(&task->handle_refs, 1, memory_order_relaxed);
    rt_task_entitlements_init(&task->entitlements);
    rt_task_slot_store(ex, id, task);
    return task;
}

static rt_task* owned_spawn(rt_executor* ex, int64_t poll_fn_id, uint32_t wanted_shard) {
    rt_control_lock(ex);
    rt_task* task = owned_alloc_ready_task(ex, poll_fn_id);
    if (task != NULL) {
        size_t shards = rt_runtime_shard_count(rt_executor_runtime(ex));
        uint32_t shard = shards < 1 ? 0 : (uint32_t)(wanted_shard % (uint32_t)shards);
        rt_task_set_placement(task, shard, TASK_PLACEMENT_CONNECTION);
        ready_push(ex, task->id);
    }
    rt_control_unlock(ex);
    return task;
}

static int owned_wait_status(const rt_task* task, uint8_t want, uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        if (task_status_load(task) == want) {
            return 1;
        }
        owned_sleep_us(1000);
    }
    return 0;
}

// What a stall looks like from outside: who is still parked, how far each got,
// and what the channel is holding. A timeout with none of that says only that
// something did not happen.
static void
owned_report_stall(rt_executor* ex, rt_task** senders, const rt_task* receiver, uint32_t first) {
    const rt_channel* ch =
        (const rt_channel*)atomic_load_explicit(&g_owned_chan, memory_order_acquire);
    fprintf(stderr,
            "stall first=%u received=%u drops=%u buffered=%llu parks_live=%llu\n",
            first,
            (unsigned)atomic_load_explicit(&g_owned_received, memory_order_acquire),
            (unsigned)atomic_load_explicit(&g_owned_drops, memory_order_acquire),
            (unsigned long long)channel_buffered(ch),
            (unsigned long long)rt_park_pool_live(&ch->parks));
    for (uint32_t i = 0; i < OWNED_SENDERS; i++) {
        fprintf(stderr,
                "  sender=%u status=%u progress=%u resume_kind=%u park_key=%u/%llu seq=%llu\n",
                i,
                (unsigned)task_status_load(senders[i]),
                (unsigned)atomic_load_explicit(&g_owned_progress[i], memory_order_acquire),
                (unsigned)senders[i]->resume_kind,
                (unsigned)senders[i]->park_key.kind,
                (unsigned long long)senders[i]->park_key.id,
                (unsigned long long)senders[i]->park_seq);
    }
    fprintf(stderr,
            "  receiver status=%u resume_kind=%u park_key=%u/%llu seq=%llu\n",
            (unsigned)task_status_load(receiver),
            (unsigned)receiver->resume_kind,
            (unsigned)receiver->park_key.kind,
            (unsigned long long)receiver->park_key.id,
            (unsigned long long)receiver->park_seq);
    rt_shard* ch_shard = channel_owner_shard(ex, ch);
    rt_shard_lock(ch_shard);
    const rt_waiter_store* store = &ch_shard->waiter_store;
    for (size_t i = 0; i < store->len; i++) {
        fprintf(stderr,
                "  waiter kind=%u id=%llu task=%llu hint=%u seq=%llu\n",
                (unsigned)store->entries[i].key.kind,
                (unsigned long long)store->entries[i].key.id,
                (unsigned long long)store->entries[i].task_id,
                (unsigned)store->entries[i].owner_hint,
                (unsigned long long)store->entries[i].seq);
    }
    rt_shard_unlock(ch_shard);
}

static int mode_owned_element(rt_executor* ex) {
    const rt_task* maker = owned_spawn(ex, POLL_OWNED_MAKER, 2);
    if (maker == NULL || !owned_wait_status(maker, TASK_DONE, 4000)) {
        return owned_fail("channel maker failed");
    }
    if (atomic_load_explicit(&g_owned_chan, memory_order_acquire) == NULL) {
        return owned_fail("channel was not created");
    }
    const rt_task* receiver = owned_spawn(ex, POLL_OWNED_RECEIVER, 0);
    if (receiver == NULL) {
        return owned_fail("receiver allocation failed");
    }
    rt_task* senders[OWNED_SENDERS];
    for (uint32_t i = 0; i < OWNED_SENDERS; i++) {
        senders[i] = owned_spawn(ex, POLL_OWNED_SENDER, i);
        if (senders[i] == NULL) {
            return owned_fail("sender allocation failed");
        }
    }
    for (uint32_t i = 0; i < OWNED_SENDERS; i++) {
        if (!owned_wait_status(senders[i], TASK_DONE, 20000)) {
            owned_report_stall(ex, senders, receiver, i);
            (void)rt_executor_request_shutdown(ex);
            return owned_fail("a sender never finished");
        }
    }
    rt_channel_close(atomic_load_explicit(&g_owned_chan, memory_order_acquire));
    if (!owned_wait_status(receiver, TASK_DONE, 20000)) {
        (void)rt_executor_request_shutdown(ex);
        return owned_fail("the receiver never finished");
    }
    (void)rt_executor_request_shutdown(ex);
    // Every task that could touch it has finished, so the stand gives back the
    // one handle rt_channel_new minted for it, and that last release is the
    // reclaim. The census is taken AFTER that drain: a value stranded in a cell
    // or in a park slot is destroyed there or nowhere, and counting before it
    // would call a stranded value delivered.
    rt_channel_handle_drop(atomic_load_explicit(&g_owned_chan, memory_order_acquire));
    atomic_store_explicit(&g_owned_chan, NULL, memory_order_release);

    uint32_t received = atomic_load_explicit(&g_owned_received, memory_order_acquire);
    uint32_t bad = atomic_load_explicit(&g_owned_bad_markers, memory_order_acquire);
    uint32_t drops = atomic_load_explicit(&g_owned_drops, memory_order_acquire);
    uint32_t missing = 0;
    uint32_t duplicated = 0;
    for (uint32_t value = 1; value <= OWNED_TOTAL; value++) {
        uint32_t times = atomic_load_explicit(&g_owned_seen[value], memory_order_acquire);
        if (times == 0) {
            missing++;
        } else if (times > 1) {
            duplicated++;
        }
    }
    printf("received=%u bad=%u drops=%u missing=%u duplicated=%u closed=%u\n",
           received,
           bad,
           drops,
           missing,
           duplicated,
           (unsigned)atomic_load_explicit(&g_owned_closed, memory_order_acquire));
    if (bad != 0) {
        return owned_fail("a value arrived out of storage that had been moved from");
    }
    if (missing != 0 || duplicated != 0 || received != OWNED_TOTAL) {
        return owned_fail("values did not arrive exactly once each");
    }
    if (drops != 0) {
        return owned_fail("a value was destroyed instead of delivered");
    }
    if (atomic_load_explicit(&g_owned_closed, memory_order_acquire) != 1) {
        return owned_fail("the receiver missed the close");
    }
    return 0;
}

// A sender parked with its value staged, then cancelled. Nothing delivers that
// value and nothing else owns it, so the CHANNEL's own drain is what destroys
// it -- exactly once, when the channel is reclaimed. Under a sanitizer this
// row is also where a second destruction would surface.
static int mode_owned_cancelled_sender(rt_executor* ex) {
    atomic_store_explicit(&g_owned_capacity, 0, memory_order_release);
    const rt_task* maker = owned_spawn(ex, POLL_OWNED_MAKER, 2);
    if (maker == NULL || !owned_wait_status(maker, TASK_DONE, 4000)) {
        return owned_fail("channel maker failed");
    }
    void* channel = atomic_load_explicit(&g_owned_chan, memory_order_acquire);
    if (channel == NULL) {
        return owned_fail("channel was not created");
    }
    rt_task* sender = owned_spawn(ex, POLL_OWNED_ONE_SEND, 1);
    if (sender == NULL) {
        return owned_fail("sender allocation failed");
    }
    if (!owned_wait_status(sender, TASK_WAITING, 8000)) {
        (void)rt_executor_request_shutdown(ex);
        return owned_fail("the sender never parked on the channel");
    }
    rt_task_cancel(sender);
    if (!owned_wait_status(sender, TASK_DONE, 8000)) {
        (void)rt_executor_request_shutdown(ex);
        return owned_fail("the cancelled sender never finished");
    }
    (void)rt_executor_request_shutdown(ex);
    // The channel outlives every park on it, so the last release is what ends
    // the value.
    rt_channel_handle_drop(channel);
    atomic_store_explicit(&g_owned_chan, NULL, memory_order_release);
    unsigned drops = (unsigned)atomic_load_explicit(&g_owned_drops, memory_order_acquire);
    unsigned received = (unsigned)atomic_load_explicit(&g_owned_received, memory_order_acquire);
    printf("cancelled sender: drops=%u received=%u\n", drops, received);
    if (received != 0) {
        return owned_fail("a cancelled send delivered its value anyway");
    }
    if (drops != 1) {
        return owned_fail("a cancelled sender's staged value was not destroyed exactly once");
    }
    return 0;
}

int main(int argc, char** argv) {
    if (argc != 2) {
        return owned_fail("usage: channel_owned_element <mode>");
    }
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return owned_fail("missing executor");
    }
    if (strcmp(argv[1], "owned-element") == 0) {
        return mode_owned_element(ex);
    }
    if (strcmp(argv[1], "owned-unbuffered") == 0) {
        atomic_store_explicit(&g_owned_capacity, 0, memory_order_release);
        return mode_owned_element(ex);
    }
    if (strcmp(argv[1], "owned-cancelled-sender") == 0) {
        return mode_owned_cancelled_sender(ex);
    }
    return owned_fail("unknown mode");
}
