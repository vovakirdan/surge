#ifndef SURGE_RUNTIME_NATIVE_RT_HEAP_ACCOUNTING_H
#define SURGE_RUNTIME_NATIVE_RT_HEAP_ACCOUNTING_H

#include <stdatomic.h>
#include <stdint.h>

typedef enum {
    RT_HEAP_ACCOUNTING_OK = 0,
    RT_HEAP_ACCOUNTING_INVALID_ARGUMENT = 1,
    RT_HEAP_ACCOUNTING_ALLOCATION_FAILED = 2,
    RT_HEAP_ACCOUNTING_CAPACITY_OVERFLOW = 3,
    RT_HEAP_ACCOUNTING_INVARIANT_VIOLATION = 4,
} rt_heap_accounting_status;

typedef struct rt_heap_accounting_cell {
    _Atomic uint64_t alloc_count;
    _Atomic uint64_t free_count;
    _Atomic uint64_t alloc_bytes;
    _Atomic uint64_t free_bytes;
} rt_heap_accounting_cell;

struct rt_heap_accounting_snapshot {
    uint64_t alloc_count;
    uint64_t free_count;
    uint64_t live_blocks;
    uint64_t live_bytes;
};

typedef struct rt_heap_accounting {
    rt_heap_accounting_cell main_cell;
    rt_heap_accounting_cell io_cell;
    rt_heap_accounting_cell* worker_cells;
    rt_heap_accounting_cell* blocking_cells;
    rt_heap_accounting_cell* compensation_cells;
    uint32_t worker_cell_count;
    uint32_t blocking_cell_count;
    uint32_t compensation_cell_count;
    uint8_t initialized;
} rt_heap_accounting;

rt_heap_accounting_status rt_heap_accounting_init(rt_heap_accounting* accounting);
rt_heap_accounting_status rt_heap_accounting_prepare_cells(rt_heap_accounting* accounting,
                                                           uint32_t workers,
                                                           uint32_t blocking_workers);
void rt_heap_accounting_destroy(rt_heap_accounting* accounting);

rt_heap_accounting_cell* rt_heap_accounting_cold_cell(void);
rt_heap_accounting_cell* rt_heap_accounting_main_cell(rt_heap_accounting* accounting);
rt_heap_accounting_cell* rt_heap_accounting_io_cell(rt_heap_accounting* accounting);
rt_heap_accounting_cell* rt_heap_accounting_worker_cell(rt_heap_accounting* accounting,
                                                        uint32_t worker_id);
rt_heap_accounting_cell* rt_heap_accounting_blocking_cell(rt_heap_accounting* accounting,
                                                          uint32_t worker_id);
rt_heap_accounting_cell* rt_heap_accounting_compensation_cell(rt_heap_accounting* accounting,
                                                              uint32_t compensation_id);

void rt_heap_accounting_set_current_cell(rt_heap_accounting_cell* cell);
rt_heap_accounting_cell* rt_heap_accounting_current_cell(void);

void rt_heap_accounting_record_alloc(rt_heap_accounting_cell* cell, uint64_t size);
void rt_heap_accounting_record_free(rt_heap_accounting_cell* cell, uint64_t size);
void rt_heap_accounting_record_realloc(rt_heap_accounting_cell* cell,
                                       uint64_t old_size,
                                       uint64_t new_size);
rt_heap_accounting_status rt_heap_accounting_snapshot(rt_heap_accounting* accounting,
                                                      struct rt_heap_accounting_snapshot* out);

#endif
