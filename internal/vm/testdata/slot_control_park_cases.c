#include "slot_control_harness.h"

#include "rt_park_pool.h"

#include <stdlib.h>
#include <string.h>

// These cases pin what a park slot must guarantee once the channel owns it
// rather than the task: a token stops working the instant its park ends, a
// value left in a slot is destroyed exactly once, and a transfer in flight
// cannot be pulled out from under either side.

typedef struct {
    uint64_t marker;
    uint64_t filler;
} park_value;

#define PARK_DROP_LOG_CAPACITY 16

static int park_drop_calls;
static uint64_t park_drop_log[PARK_DROP_LOG_CAPACITY];

static void park_reset_drops(void) {
    park_drop_calls = 0;
    memset(park_drop_log, 0, sizeof(park_drop_log));
}

static void park_move(void* destination, void* source) {
    memcpy(destination, source, sizeof(park_value));
    ((park_value*)source)->marker = 0;
}

static void park_drop(void* value) {
    park_value* typed = (park_value*)value;
    if (park_drop_calls < PARK_DROP_LOG_CAPACITY) {
        park_drop_log[park_drop_calls] = typed->marker;
    }
    park_drop_calls++;
    typed->marker = 0;
}

static const rt_value_ops park_ops = {
    .layout = {.size = sizeof(park_value),
               .align = _Alignof(park_value),
               .stride = sizeof(park_value),
               .flags = RT_VALUE_FLAG_DROPPABLE},
    .move_init = park_move,
    .copy_init = NULL,
    .clone_init = NULL,
    .drop_in_place = park_drop,
    .trace = NULL,
    .plan_cross = harness_noop_plan,
    .cross_move_init = NULL,
    .cross_clone_init = NULL,
};

static void* park_alloc_storage(uint64_t capacity, size_t* size) {
    *size = rt_park_pool_alloc_size(&park_ops, capacity);
    if (*size == 0) {
        return NULL;
    }
    size_t align = _Alignof(park_value) < sizeof(void*) ? sizeof(void*) : _Alignof(park_value);
    return aligned_alloc(align, (*size + align - 1) / align * align);
}

static int park_deliver(rt_park_pool* pool, const rt_park_token* token, uint64_t marker) {
    void* address = NULL;
    if (rt_park_pool_reserve_deliver_locked(pool, token, &address) != RT_SLOT_CONTROL_OK) {
        return 0;
    }
    park_value staged = {.marker = marker, .filler = marker + 1};
    park_ops.move_init(address, &staged);
    return rt_park_pool_commit_deliver_locked(pool, token) == RT_SLOT_CONTROL_OK;
}

static int park_take(rt_park_pool* pool, const rt_park_token* token, park_value* out) {
    void* address = NULL;
    if (rt_park_pool_reserve_take_locked(pool, token, &address) != RT_SLOT_CONTROL_OK) {
        return 0;
    }
    park_ops.move_init(out, address);
    return rt_park_pool_commit_take_locked(pool, token) == RT_SLOT_CONTROL_OK;
}

// Slots are handed out, exhausted and returned.
static int park_case_acquire_and_release(void) {
    size_t size = 0;
    void* storage = park_alloc_storage(2, &size);
    REQUIRE(storage != NULL);
    rt_park_pool pool;
    REQUIRE(rt_park_pool_init(&pool, &park_ops, 2, storage, size) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_park_pool_live(&pool) == 0);

    rt_park_token first;
    rt_park_token second;
    REQUIRE(rt_park_pool_acquire_locked(&pool, &first) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_park_pool_acquire_locked(&pool, &second) == RT_SLOT_CONTROL_OK);
    REQUIRE(first.index != second.index);
    REQUIRE(first.generation != second.generation);
    REQUIRE(rt_park_pool_live(&pool) == 2);

    rt_park_token overflow;
    REQUIRE(rt_park_pool_acquire_locked(&pool, &overflow) == RT_SLOT_CONTROL_INVALID_STATE);
    REQUIRE(overflow.generation == 0 && overflow.owner == NULL);

    REQUIRE(rt_park_pool_release(&pool, &first) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_park_pool_live(&pool) == 1);
    // Releasing twice would return a slot to the free list that is not this
    // caller's any more.
    REQUIRE(rt_park_pool_release(&pool, &first) == RT_SLOT_CONTROL_STALE);
    REQUIRE(rt_park_pool_live(&pool) == 1);

    rt_park_token reused;
    REQUIRE(rt_park_pool_acquire_locked(&pool, &reused) == RT_SLOT_CONTROL_OK);
    REQUIRE(reused.index == first.index);
    REQUIRE(rt_park_pool_live(&pool) == 2);

    // A token from another pool names a slot that is not this pool's to act on.
    rt_park_token foreign = reused;
    foreign.owner = NULL;
    REQUIRE(rt_park_pool_token_is_live(&pool, &foreign) == 0);
    REQUIRE(rt_park_pool_release(&pool, &foreign) == RT_SLOT_CONTROL_STALE);

    rt_park_pool_drain(&pool);
    free(storage);
    return 0;
}

// A value is delivered into a parked task's slot and taken out of it intact.
static int park_case_deliver_and_take(void) {
    size_t size = 0;
    void* storage = park_alloc_storage(2, &size);
    REQUIRE(storage != NULL);
    rt_park_pool pool;
    REQUIRE(rt_park_pool_init(&pool, &park_ops, 2, storage, size) == RT_SLOT_CONTROL_OK);
    park_reset_drops();

    rt_park_token token;
    REQUIRE(rt_park_pool_acquire_locked(&pool, &token) == RT_SLOT_CONTROL_OK);
    // Nothing has been delivered, so there is nothing to take yet.
    void* address = NULL;
    REQUIRE(rt_park_pool_reserve_take_locked(&pool, &token, &address) ==
            RT_SLOT_CONTROL_INVALID_STATE);
    REQUIRE(park_deliver(&pool, &token, 4242));
    // And a second delivery has nowhere to go until the first is taken.
    REQUIRE(rt_park_pool_reserve_deliver_locked(&pool, &token, &address) ==
            RT_SLOT_CONTROL_INVALID_STATE);

    park_value taken = {0};
    REQUIRE(park_take(&pool, &token, &taken));
    REQUIRE(taken.marker == 4242 && taken.filler == 4243);
    // Taking a value moved it out; the slot is empty and nothing was dropped.
    REQUIRE(park_drop_calls == 0);
    REQUIRE(rt_park_pool_release(&pool, &token) == RT_SLOT_CONTROL_OK);
    REQUIRE(park_drop_calls == 0);
    rt_park_pool_drain(&pool);
    free(storage);
    return 0;
}

// The case the generation exists for: the slot has already been handed to the
// next task, so index and occupancy both say yes and only the generation says
// this token belongs to a park that is over.
static int park_case_stale_token_is_inert(void) {
    size_t size = 0;
    void* storage = park_alloc_storage(1, &size);
    REQUIRE(storage != NULL);
    rt_park_pool pool;
    REQUIRE(rt_park_pool_init(&pool, &park_ops, 1, storage, size) == RT_SLOT_CONTROL_OK);
    park_reset_drops();

    rt_park_token departed;
    REQUIRE(rt_park_pool_acquire_locked(&pool, &departed) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_park_pool_release(&pool, &departed) == RT_SLOT_CONTROL_OK);

    rt_park_token successor;
    REQUIRE(rt_park_pool_acquire_locked(&pool, &successor) == RT_SLOT_CONTROL_OK);
    REQUIRE(successor.index == departed.index);
    REQUIRE(successor.generation != departed.generation);
    REQUIRE(rt_park_pool_token_is_live(&pool, &successor) == 1);
    REQUIRE(rt_park_pool_token_is_live(&pool, &departed) == 0);

    // A wake that was already in flight when the first task left. It must not
    // deliver into the slot the successor is parked on.
    void* address = NULL;
    REQUIRE(rt_park_pool_reserve_deliver_locked(&pool, &departed, &address) ==
            RT_SLOT_CONTROL_STALE);
    REQUIRE(address == NULL);

    REQUIRE(park_deliver(&pool, &successor, 77));

    // Nor may the late token take the successor's value, destroy it by ending a
    // park that is not its own, or commit against a transfer it never started.
    REQUIRE(rt_park_pool_reserve_take_locked(&pool, &departed, &address) == RT_SLOT_CONTROL_STALE);
    REQUIRE(rt_park_pool_release(&pool, &departed) == RT_SLOT_CONTROL_STALE);
    REQUIRE(rt_park_pool_commit_deliver_locked(&pool, &departed) == RT_SLOT_CONTROL_STALE);
    REQUIRE(rt_park_pool_commit_take_locked(&pool, &departed) == RT_SLOT_CONTROL_STALE);
    REQUIRE(park_drop_calls == 0);

    // The successor's value is untouched and still its own.
    park_value taken = {0};
    REQUIRE(park_take(&pool, &successor, &taken));
    REQUIRE(taken.marker == 77);
    REQUIRE(rt_park_pool_release(&pool, &successor) == RT_SLOT_CONTROL_OK);
    rt_park_pool_drain(&pool);
    free(storage);
    return 0;
}

// A park that ends with a value still in the slot destroys it, exactly once.
// This is the cancel-after-delivery race: the value has an owner either way.
static int park_case_release_frees_an_undelivered_value_once(void) {
    size_t size = 0;
    void* storage = park_alloc_storage(2, &size);
    REQUIRE(storage != NULL);
    rt_park_pool pool;
    REQUIRE(rt_park_pool_init(&pool, &park_ops, 2, storage, size) == RT_SLOT_CONTROL_OK);
    park_reset_drops();

    rt_park_token token;
    REQUIRE(rt_park_pool_acquire_locked(&pool, &token) == RT_SLOT_CONTROL_OK);
    REQUIRE(park_deliver(&pool, &token, 9001));
    // The task is cancelled before it ever wakes.
    REQUIRE(rt_park_pool_release(&pool, &token) == RT_SLOT_CONTROL_OK);
    REQUIRE(park_drop_calls == 1);
    REQUIRE(park_drop_log[0] == 9001);
    REQUIRE(rt_park_pool_live(&pool) == 0);

    // Draining afterwards finds nothing: exactly once means the sweep is silent.
    rt_park_pool_drain(&pool);
    REQUIRE(park_drop_calls == 1);

    // The slot is reusable, and what the next park gets is not the old value.
    rt_park_token next;
    REQUIRE(rt_park_pool_acquire_locked(&pool, &next) == RT_SLOT_CONTROL_OK);
    REQUIRE(park_deliver(&pool, &next, 5));
    park_value taken = {0};
    REQUIRE(park_take(&pool, &next, &taken));
    REQUIRE(taken.marker == 5);
    REQUIRE(park_drop_calls == 1);
    rt_park_pool_drain(&pool);
    free(storage);
    return 0;
}

// Tearing the channel down destroys every value still parked, exactly once each.
static int park_case_drain_frees_every_value_once(void) {
    size_t size = 0;
    void* storage = park_alloc_storage(4, &size);
    REQUIRE(storage != NULL);
    rt_park_pool pool;
    REQUIRE(rt_park_pool_init(&pool, &park_ops, 4, storage, size) == RT_SLOT_CONTROL_OK);
    park_reset_drops();

    rt_park_token tokens[4];
    for (uint64_t i = 0; i < 4; i++) {
        REQUIRE(rt_park_pool_acquire_locked(&pool, &tokens[i]) == RT_SLOT_CONTROL_OK);
    }
    // Two slots hold a value, one was already taken, one never received one.
    REQUIRE(park_deliver(&pool, &tokens[0], 11));
    REQUIRE(park_deliver(&pool, &tokens[2], 33));
    REQUIRE(park_deliver(&pool, &tokens[3], 44));
    park_value taken = {0};
    REQUIRE(park_take(&pool, &tokens[3], &taken));
    REQUIRE(taken.marker == 44);

    rt_park_pool_drain(&pool);
    REQUIRE(park_drop_calls == 2);
    REQUIRE(park_drop_log[0] == 11 && park_drop_log[1] == 33);
    REQUIRE(rt_park_pool_live(&pool) == 0);
    // Every token is dead now: the pool ended all of the parks.
    for (uint64_t i = 0; i < 4; i++) {
        REQUIRE(rt_park_pool_token_is_live(&pool, &tokens[i]) == 0);
    }
    rt_park_pool_drain(&pool);
    REQUIRE(park_drop_calls == 2);
    free(storage);
    return 0;
}

// One typed transfer at a time, and a park cannot end underneath one.
static int park_case_one_transfer_at_a_time(void) {
    size_t size = 0;
    void* storage = park_alloc_storage(2, &size);
    REQUIRE(storage != NULL);
    rt_park_pool pool;
    REQUIRE(rt_park_pool_init(&pool, &park_ops, 2, storage, size) == RT_SLOT_CONTROL_OK);
    park_reset_drops();

    rt_park_token first;
    rt_park_token second;
    REQUIRE(rt_park_pool_acquire_locked(&pool, &first) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_park_pool_acquire_locked(&pool, &second) == RT_SLOT_CONTROL_OK);

    void* address = NULL;
    REQUIRE(rt_park_pool_reserve_deliver_locked(&pool, &first, &address) == RT_SLOT_CONTROL_OK);
    REQUIRE(address != NULL);
    // A different slot's transfer waits its turn -- BUSY is a lost race, and the
    // caller retries.
    void* blocked = NULL;
    REQUIRE(rt_park_pool_reserve_deliver_locked(&pool, &second, &blocked) == RT_SLOT_CONTROL_BUSY);
    REQUIRE(blocked == NULL);
    // Ending the park mid-transfer would free bytes the deliverer is writing.
    REQUIRE(rt_park_pool_release(&pool, &first) == RT_SLOT_CONTROL_BUSY);
    // A commit for a slot that is not the one in flight is refused too.
    REQUIRE(rt_park_pool_commit_deliver_locked(&pool, &second) == RT_SLOT_CONTROL_INVALID_STATE);

    // Abandoning frees the transfer and leaves the slot empty.
    REQUIRE(rt_park_pool_abandon_deliver_locked(&pool, &first) == RT_SLOT_CONTROL_OK);
    REQUIRE(park_deliver(&pool, &second, 8));
    REQUIRE(rt_park_pool_release(&pool, &first) == RT_SLOT_CONTROL_OK);
    REQUIRE(park_drop_calls == 0);

    rt_park_pool_drain(&pool);
    REQUIRE(park_drop_calls == 1);
    REQUIRE(park_drop_log[0] == 8);
    free(storage);
    return 0;
}

// Storage the caller did not size or align correctly is refused.
static int park_case_storage_is_checked(void) {
    size_t size = rt_park_pool_alloc_size(&park_ops, 4);
    REQUIRE(size > 0);
    REQUIRE(rt_park_pool_alloc_size(&park_ops, 0) == 0);
    void* storage = aligned_alloc(16, (size + 16 + 15) / 16 * 16);
    REQUIRE(storage != NULL);
    rt_park_pool pool;
    REQUIRE(rt_park_pool_init(&pool, &park_ops, 4, storage, size - 1) ==
            RT_SLOT_CONTROL_STORAGE_OVERFLOW);
    REQUIRE(rt_park_pool_init(&pool, &park_ops, 4, (uint8_t*)storage + 1, size) ==
            RT_SLOT_CONTROL_STORAGE_MISALIGNED);
    REQUIRE(rt_park_pool_init(&pool, NULL, 4, storage, size) == RT_SLOT_CONTROL_INVALID_ARGUMENT);
    REQUIRE(rt_park_pool_init(&pool, &park_ops, 0, storage, size) ==
            RT_SLOT_CONTROL_STORAGE_OVERFLOW);
    REQUIRE(rt_park_pool_init(&pool, &park_ops, 4, storage, size) == RT_SLOT_CONTROL_OK);
    free(storage);
    return 0;
}

int harness_case_park(void) {
    int status = park_case_acquire_and_release();
    if (status != 0) {
        return status;
    }
    status = park_case_deliver_and_take();
    if (status != 0) {
        return status;
    }
    status = park_case_stale_token_is_inert();
    if (status != 0) {
        return status;
    }
    status = park_case_release_frees_an_undelivered_value_once();
    if (status != 0) {
        return status;
    }
    status = park_case_drain_frees_every_value_once();
    if (status != 0) {
        return status;
    }
    status = park_case_one_transfer_at_a_time();
    if (status != 0) {
        return status;
    }
    return park_case_storage_is_checked();
}
