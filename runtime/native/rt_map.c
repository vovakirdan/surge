#include "rt.h"

#include "rt_value_ops.h"

#include <limits.h>
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <string.h>

#ifndef alignof
#define alignof(t) __alignof__(t)
#endif

// A map stores each key and each value AT ITS OWN TYPE.
//
// WHAT THIS REPLACES. An entry used to be `{ uint64_t key; uint64_t value }` --
// two machine words, whatever the map was declared over. A composite value
// could not live in such an entry at all: it was copied into a separate
// transport allocation and the entry held that allocation's address, so the
// entry buffer said `uint64_t` while the language said `Payload64`.
//
// TWO RUNS, NOT ONE RUN OF PAIRS. Entry `i` is key slot `i` beside value slot
// `i`. A run of pairs would need a struct type per key/value combination, and
// the runtime has no type interner to make one in. Two runs also keep each side
// addressed at the type its own descriptor describes, which is the property the
// flip exists for: a key is read as the map's key type and a value as the map's
// value type, never as whatever the slot happens to hold.
//
// ONE ALLOCATION. Both runs live in a single block -- keys first, then values
// at the value type's alignment. Growth allocates the next block and MOVES
// every live key and value into it through the descriptors. It is never
// rt_realloc: relocating bytes is a decision only the type's own move may make,
// and rt_realloc never asks.
//
// WHO OWNS WHAT. The map owns every key and value it was handed, from the
// insert that moved them in until a removal moves one out or a replacement
// destroys one. Lookup is the exception and says so in its own signature: it
// hands back an interior ADDRESS and transfers nothing. That address is
// invalidated by this map's own growth and by its swap-with-last removal, which
// is why sema refuses a container mutation across a live element borrow
// (RV2-DEBT-158/172, owner ruling 2026-08-09) rather than the runtime trying to
// keep a moved-from address alive.
//
// NO LOCK, AND THE LANE RULE STILL APPLIES. A map has no lock of its own, but
// every descriptor operation still goes through the detached helpers, because
// those refuse to run generated code while a scheduler or shard lock is held. A
// map operation reached from inside such a section is a defect; this is where
// it is refused rather than where it deadlocks.

typedef struct SurgeMap {
    const rt_value_ops* key_ops;
    const rt_value_ops* value_ops;
    uint64_t key_kind;
    uint64_t len;
    uint64_t cap;
    // The single block both runs live in, plus the size and alignment it was
    // allocated at -- kept because rt_free is told what it is being given back.
    uint8_t* storage;
    uint64_t storage_size;
    uint64_t storage_align;
    uint64_t values_offset;
} SurgeMap;

typedef struct SurgeArrayHeader {
    uint64_t len;
    uint64_t cap;
    void* data;
} SurgeArrayHeader;

// The one layout calculation, so the size the map allocates and the offsets it
// addresses through cannot drift apart.
typedef struct SurgeMapStoragePlan {
    uint64_t values_offset;
    uint64_t total;
    uint64_t align;
    bool valid;
} SurgeMapStoragePlan;

enum {
    MAP_KEY_STRING = 1,
    MAP_KEY_INT = 2,
    MAP_KEY_UINT = 3,
    MAP_KEY_BIGINT = 4,
    MAP_KEY_BIGUINT = 5,
};

static void map_panic(const char* msg) {
    rt_panic_numeric((const uint8_t*)msg, (uint64_t)strlen(msg), NULL, 0);
}

static uint64_t map_round_up(uint64_t size, uint64_t align) {
    if (align <= 1) {
        return size;
    }
    uint64_t rem = size % align;
    if (rem == 0) {
        return size;
    }
    uint64_t add = align - rem;
    if (size > UINT64_MAX - add) {
        return UINT64_MAX;
    }
    return size + add;
}

static uint64_t map_align_of(const rt_value_ops* operations) {
    uint64_t align = (uint64_t)operations->layout.align;
    return align == 0 ? 1 : align;
}

static SurgeMapStoragePlan map_plan(const SurgeMap* map, uint64_t capacity) {
    SurgeMapStoragePlan plan;
    memset(&plan, 0, sizeof(plan));
    uint64_t key_align = map_align_of(map->key_ops);
    uint64_t value_align = map_align_of(map->value_ops);
    uint64_t key_stride = (uint64_t)map->key_ops->layout.stride;
    uint64_t value_stride = (uint64_t)map->value_ops->layout.stride;
    plan.align = key_align > value_align ? key_align : value_align;
    if (capacity > 0) {
        if (key_stride > 0 && capacity > UINT64_MAX / key_stride) {
            return plan;
        }
        if (value_stride > 0 && capacity > UINT64_MAX / value_stride) {
            return plan;
        }
    }
    uint64_t offset = map_round_up(capacity * key_stride, value_align);
    if (offset == UINT64_MAX) {
        return plan;
    }
    uint64_t value_bytes = capacity * value_stride;
    // A zero-sized value stores no bytes, but a move still needs a real,
    // correctly aligned address to land on, so the run keeps one element's
    // worth of alignment rather than one word per entry.
    if (capacity > 0 && value_bytes == 0) {
        value_bytes = value_align;
    }
    if (offset > UINT64_MAX - value_bytes) {
        return plan;
    }
    plan.values_offset = offset;
    plan.total = offset + value_bytes;
    plan.valid = true;
    return plan;
}

static void* map_key_slot(const SurgeMap* map, uint64_t index) {
    return map->storage + index * (uint64_t)map->key_ops->layout.stride;
}

static void* map_value_slot(const SurgeMap* map, uint64_t index) {
    return map->storage + map->values_offset + index * (uint64_t)map->value_ops->layout.stride;
}

// Compares a probe key against a stored one. Both are ADDRESSES of key storage,
// so the two sides are read the same way whichever of them the caller owns.
static bool map_key_eq(const SurgeMap* map, const void* probe, const void* stored) {
    switch (map->key_kind) {
        case MAP_KEY_STRING: {
            void* left = *(void* const*)probe;
            void* right = *(void* const*)stored;
            return rt_string_eq((void*)&left, (void*)&right);
        }
        case MAP_KEY_INT:
        case MAP_KEY_UINT:
            // An integer key occupies exactly its own width with no padding, so
            // bitwise equality over that width IS numeric equality, whatever
            // the width is.
            return memcmp(probe, stored, (size_t)map->key_ops->layout.size) == 0;
        case MAP_KEY_BIGINT:
            return rt_bigint_cmp(*(void* const*)probe, *(void* const*)stored) == 0;
        case MAP_KEY_BIGUINT:
            return rt_biguint_cmp(*(void* const*)probe, *(void* const*)stored) == 0;
        default:
            map_panic("map: unsupported key kind");
            return false;
    }
}

static bool map_find(const SurgeMap* map, const void* key, uint64_t* out_idx) {
    if (map == NULL || key == NULL) {
        return false;
    }
    for (uint64_t i = 0; i < map->len; i++) {
        if (map_key_eq(map, key, map_key_slot(map, i))) {
            if (out_idx != NULL) {
                *out_idx = i;
            }
            return true;
        }
    }
    return false;
}

// Grows the entry storage to hold at least `needed` entries, MOVING every live
// key and value into the new block one at a time.
//
// Until a destination slot is initialized the source stays responsible for it,
// and each move initializes exactly one slot before the next begins, so a
// refused allocation leaves the old block whole rather than half emptied.
static void map_reserve(SurgeMap* map, uint64_t needed) {
    if (map == NULL) {
        map_panic("map: null handle");
        return;
    }
    if (needed <= map->cap) {
        return;
    }
    uint64_t new_cap = map->cap;
    if (new_cap == 0) {
        new_cap = 4;
    } else if (new_cap > UINT64_MAX / 2) {
        new_cap = needed;
    } else {
        new_cap *= 2;
    }
    if (new_cap < needed) {
        new_cap = needed;
    }
    SurgeMapStoragePlan plan = map_plan(map, new_cap);
    if (!plan.valid) {
        map_panic("map capacity overflow");
        return;
    }
    uint8_t* next = (uint8_t*)rt_alloc(plan.total, plan.align);
    if (next == NULL) {
        static const uint8_t msg[] = "map allocation failed";
        rt_fatal_static(RT_OOM, msg, sizeof(msg) - 1);
        return;
    }
    uint64_t key_stride = (uint64_t)map->key_ops->layout.stride;
    uint64_t value_stride = (uint64_t)map->value_ops->layout.stride;
    for (uint64_t i = 0; i < map->len; i++) {
        rt_value_move_init_detached(map->key_ops, next + i * key_stride, map_key_slot(map, i));
        rt_value_move_init_detached(
            map->value_ops, next + plan.values_offset + i * value_stride, map_value_slot(map, i));
    }
    if (map->storage != NULL) {
        rt_free(map->storage, map->storage_size, map->storage_align);
    }
    map->storage = next;
    map->storage_size = plan.total;
    map->storage_align = plan.align;
    map->values_offset = plan.values_offset;
    map->cap = new_cap;
}

void* rt_map_new(uint64_t key_kind, const rt_value_ops* key_ops, const rt_value_ops* value_ops) {
    switch (key_kind) {
        case MAP_KEY_STRING:
        case MAP_KEY_INT:
        case MAP_KEY_UINT:
        case MAP_KEY_BIGINT:
        case MAP_KEY_BIGUINT:
            break;
        default:
            map_panic("map: unsupported key kind");
            return NULL;
    }
    if (key_ops == NULL || value_ops == NULL) {
        // Without both descriptors the map knows neither how wide an entry is
        // nor how to move one, so there is no degraded mode to fall back to.
        map_panic("map: entry storage needs key and value operations");
        return NULL;
    }
    SurgeMap* map = (SurgeMap*)rt_alloc((uint64_t)sizeof(SurgeMap), (uint64_t)alignof(SurgeMap));
    if (map == NULL) {
        static const uint8_t msg[] = "map allocation failed";
        rt_fatal_static(RT_OOM, msg, sizeof(msg) - 1);
        return NULL;
    }
    memset(map, 0, sizeof(*map));
    map->key_ops = key_ops;
    map->value_ops = value_ops;
    map->key_kind = key_kind;
    return (void*)map;
}

uint64_t rt_map_len(const void* map_ptr) {
    if (map_ptr == NULL) {
        map_panic("map: null handle");
        return 0;
    }
    const SurgeMap* map = (const SurgeMap*)map_ptr;
    return map->len;
}

bool rt_map_contains(const void* map_ptr, const void* key) {
    if (map_ptr == NULL) {
        map_panic("map: null handle");
        return false;
    }
    const SurgeMap* map = (const SurgeMap*)map_ptr;
    return map_find(map, key, NULL);
}

// Answers with the ADDRESS of the value's storage and transfers nothing: the
// map still owns what lives there. `out_value` is a slot for one pointer --
// the destination Option's payload when the caller wants the answer, and NULL
// when it only wants to know whether there is one.
bool rt_map_get_ref(void* map_ptr, const void* key, void** out_value) {
    if (map_ptr == NULL) {
        map_panic("map: null handle");
        return false;
    }
    // Const here, and the entry point still takes a mutable handle: the map is
    // not modified by a lookup, but the ADDRESS it hands back is writable, and
    // a caller holding only a shared borrow may not ask for one.
    const SurgeMap* map = (const SurgeMap*)map_ptr;
    uint64_t idx = 0;
    if (!map_find(map, key, &idx)) {
        return false;
    }
    if (out_value != NULL) {
        *out_value = map_value_slot(map, idx);
    }
    return true;
}

// The same address, and deliberately the same body. A shared borrow and a
// mutable borrow of a map element differ in what the LANGUAGE permits through
// them, not in where the element lives, and the ownership question they used to
// raise -- an address this map's own insert and remove invalidate -- is refused
// in sema instead (RV2-DEBT-158/172).
bool rt_map_get_mut(void* map_ptr, const void* key, void** out_value) {
    return rt_map_get_ref(map_ptr, key, out_value);
}

// Takes ownership of the key and the value it is handed, and reports whether it
// displaced an entry.
//
// On a hit the map keeps the key it already holds, so the incoming equal key's
// obligation ends here. The displaced value goes to `previous` when the caller
// wants it and is destroyed when it does not: an insert whose result is
// discarded still took ownership of what it displaced.
bool rt_map_insert(void* map_ptr, void* key, void* value, void* previous) {
    if (map_ptr == NULL) {
        map_panic("map: null handle");
        return false;
    }
    if (key == NULL || value == NULL) {
        map_panic("map: insert needs key and value storage");
        return false;
    }
    SurgeMap* map = (SurgeMap*)map_ptr;
    uint64_t idx = 0;
    if (map_find(map, key, &idx)) {
        rt_value_drop_in_place_detached(map->key_ops, key);
        void* slot = map_value_slot(map, idx);
        if (previous != NULL) {
            rt_value_move_init_detached(map->value_ops, previous, slot);
        } else {
            rt_value_drop_in_place_detached(map->value_ops, slot);
        }
        // The slot is empty between these two statements and nothing observes
        // it: no lock is released here, and a generated drop body never
        // reenters the map it was called from.
        rt_value_move_init_detached(map->value_ops, slot, value);
        return true;
    }
    if (map->len == UINT64_MAX) {
        map_panic("map length overflow");
    }
    map_reserve(map, map->len + 1);
    rt_value_move_init_detached(map->key_ops, map_key_slot(map, map->len), key);
    rt_value_move_init_detached(map->value_ops, map_value_slot(map, map->len), value);
    map->len += 1;
    return false;
}

// Destroys the stored key and hands the value to `removed`, or destroys that
// too when nobody asked for it. The vacated entry is filled by the last one, so
// an index into this map does not survive a removal -- see the header note.
bool rt_map_remove(void* map_ptr, const void* key, void* removed) {
    if (map_ptr == NULL) {
        map_panic("map: null handle");
        return false;
    }
    SurgeMap* map = (SurgeMap*)map_ptr;
    uint64_t idx = 0;
    if (!map_find(map, key, &idx)) {
        return false;
    }
    rt_value_drop_in_place_detached(map->key_ops, map_key_slot(map, idx));
    void* slot = map_value_slot(map, idx);
    if (removed != NULL) {
        rt_value_move_init_detached(map->value_ops, removed, slot);
    } else {
        rt_value_drop_in_place_detached(map->value_ops, slot);
    }
    uint64_t last = map->len - 1;
    if (idx != last) {
        rt_value_move_init_detached(map->key_ops, map_key_slot(map, idx), map_key_slot(map, last));
        rt_value_move_init_detached(
            map->value_ops, map_value_slot(map, idx), map_value_slot(map, last));
    }
    map->len = last;
    return true;
}

// Builds an independent owning array of this map's keys.
//
// WHY A RECIPE AND NOT THE DESCRIPTOR'S CLONE. The array does not become a
// second owner of the map's keys; it becomes the owner of its own. Which body
// makes that copy is decided by the operation, not by the key's type, which is
// the same distinction rt_task_clone draws for a result. A NULL recipe says the
// key's bytes ARE the whole value -- an integer key -- so copying them owns
// nothing new.
//
// Without this the array and the map shared one block per heap key, and the
// map's own teardown freed what the array still held: `keys()` then `remove` in
// a loop, which is the shape stdlib/json/stringify.sg takes.
void* rt_map_keys(const void* map_ptr,
                  uint64_t elem_size,
                  uint64_t elem_align,
                  rt_value_clone_init_fn duplicate) {
    if (map_ptr == NULL) {
        map_panic("map: null handle");
        return NULL;
    }
    const SurgeMap* map = (const SurgeMap*)map_ptr;
    if (elem_size == 0) {
        elem_size = 1;
    }
    if (elem_align == 0) {
        elem_align = 1;
    }
    if (map->len > 0 && elem_size != (uint64_t)map->key_ops->layout.size) {
        // The array the caller is about to read and the run this map keeps keys
        // in would disagree about how wide one key is.
        map_panic("map keys element width disagrees with the key type");
    }
    uint64_t stride = map_round_up(elem_size, elem_align);
    if (stride == 0 || stride == UINT64_MAX) {
        map_panic("map keys stride overflow");
    }
    if (map->len > 0 && stride > UINT64_MAX / map->len) {
        map_panic("map keys size overflow");
    }
    uint64_t data_size = stride * map->len;
    void* data = NULL;
    if (data_size > 0) {
        if (data_size > (uint64_t)SIZE_MAX) {
            map_panic("map keys size overflow");
        }
        data = rt_alloc(data_size, elem_align);
        if (data == NULL) {
            static const uint8_t msg[] = "map keys allocation failed";
            rt_fatal_static(RT_OOM, msg, sizeof(msg) - 1);
        }
    }
    SurgeArrayHeader* header = (SurgeArrayHeader*)rt_alloc((uint64_t)sizeof(SurgeArrayHeader),
                                                           (uint64_t)alignof(SurgeArrayHeader));
    if (header == NULL) {
        static const uint8_t msg[] = "map keys allocation failed";
        rt_fatal_static(RT_OOM, msg, sizeof(msg) - 1);
        return NULL;
    }
    header->len = map->len;
    header->cap = map->len;
    header->data = data;
    if (data != NULL && map->len > 0) {
        uint8_t* bytes = (uint8_t*)data;
        for (uint64_t i = 0; i < map->len; i++) {
            void* slot = bytes + (size_t)i * (size_t)stride;
            if (duplicate != NULL) {
                rt_value_duplicate_detached(duplicate, slot, map_key_slot(map, i));
            } else {
                memcpy(slot, map_key_slot(map, i), (size_t)elem_size);
            }
        }
    }
    return (void*)header;
}

void rt_map_free(void* map_ptr) {
    if (map_ptr == NULL) {
        return;
    }
    SurgeMap* map = (SurgeMap*)map_ptr;
    // Every live entry, exactly once each, and outside any owner lock -- which
    // for a map means outside a scheduler or shard lock, since a map has none
    // of its own. The length is cleared first so a drop body that somehow
    // reached this map again would find it empty rather than find the entry it
    // is in the middle of destroying.
    uint64_t live = map->len;
    map->len = 0;
    for (uint64_t i = 0; i < live; i++) {
        rt_value_drop_in_place_detached(map->key_ops, map_key_slot(map, i));
        rt_value_drop_in_place_detached(map->value_ops, map_value_slot(map, i));
    }
    if (map->storage != NULL) {
        rt_free(map->storage, map->storage_size, map->storage_align);
        map->storage = NULL;
        map->storage_size = 0;
        map->cap = 0;
    }
    rt_free((uint8_t*)map, (uint64_t)sizeof(SurgeMap), (uint64_t)alignof(SurgeMap));
}
