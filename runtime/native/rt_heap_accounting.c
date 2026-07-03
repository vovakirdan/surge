#include "rt_heap_accounting.h"

#include <stddef.h>
#include <stdlib.h>
#include <string.h>

static rt_heap_accounting_cell cold_cell;
static _Thread_local rt_heap_accounting_cell* tls_heap_accounting_cell;

static uint64_t heap_event_size(uint64_t size) {
    return size == 0 ? 1 : size;
}

static rt_heap_accounting_status allocate_cells(uint32_t count, rt_heap_accounting_cell** out) {
    if (out == NULL) {
        return RT_HEAP_ACCOUNTING_INVALID_ARGUMENT;
    }
    *out = NULL;
    if (count == 0) {
        return RT_HEAP_ACCOUNTING_OK;
    }
    rt_heap_accounting_cell* cells =
        (rt_heap_accounting_cell*)calloc((size_t)count, sizeof(rt_heap_accounting_cell));
    if (cells == NULL) {
        return RT_HEAP_ACCOUNTING_ALLOCATION_FAILED;
    }
    *out = cells;
    return RT_HEAP_ACCOUNTING_OK;
}

static void add_cell_snapshot(rt_heap_accounting_cell* cell,
                              uint64_t* alloc_count,
                              uint64_t* free_count,
                              uint64_t* alloc_bytes,
                              uint64_t* free_bytes) {
    if (cell == NULL) {
        return;
    }
    *alloc_count += atomic_load_explicit(&cell->alloc_count, memory_order_relaxed);
    *free_count += atomic_load_explicit(&cell->free_count, memory_order_relaxed);
    *alloc_bytes += atomic_load_explicit(&cell->alloc_bytes, memory_order_relaxed);
    *free_bytes += atomic_load_explicit(&cell->free_bytes, memory_order_relaxed);
}

static void add_cell_range_snapshot(rt_heap_accounting_cell* cells,
                                    uint32_t count,
                                    uint64_t* alloc_count,
                                    uint64_t* free_count,
                                    uint64_t* alloc_bytes,
                                    uint64_t* free_bytes) {
    for (uint32_t i = 0; i < count; i++) {
        add_cell_snapshot(&cells[i], alloc_count, free_count, alloc_bytes, free_bytes);
    }
}

rt_heap_accounting_status rt_heap_accounting_init(rt_heap_accounting* accounting) {
    if (accounting == NULL) {
        return RT_HEAP_ACCOUNTING_INVALID_ARGUMENT;
    }
    memset(accounting, 0, sizeof(*accounting));
    accounting->initialized = 1;
    return RT_HEAP_ACCOUNTING_OK;
}

rt_heap_accounting_status rt_heap_accounting_prepare_cells(rt_heap_accounting* accounting,
                                                           uint32_t workers,
                                                           uint32_t blocking_workers) {
    if (accounting == NULL || accounting->initialized == 0) {
        return RT_HEAP_ACCOUNTING_INVALID_ARGUMENT;
    }
    if (workers > UINT32_MAX / 32U) {
        return RT_HEAP_ACCOUNTING_CAPACITY_OVERFLOW;
    }
    uint32_t compensation_workers = workers * 32U;
    rt_heap_accounting_cell* worker_cells = NULL;
    rt_heap_accounting_cell* blocking_cells = NULL;
    rt_heap_accounting_cell* compensation_cells = NULL;
    rt_heap_accounting_status status = allocate_cells(workers, &worker_cells);
    if (status != RT_HEAP_ACCOUNTING_OK) {
        return status;
    }
    status = allocate_cells(blocking_workers, &blocking_cells);
    if (status != RT_HEAP_ACCOUNTING_OK) {
        free(worker_cells);
        return status;
    }
    status = allocate_cells(compensation_workers, &compensation_cells);
    if (status != RT_HEAP_ACCOUNTING_OK) {
        free(blocking_cells);
        free(worker_cells);
        return status;
    }
    accounting->worker_cells = worker_cells;
    accounting->blocking_cells = blocking_cells;
    accounting->compensation_cells = compensation_cells;
    accounting->worker_cell_count = workers;
    accounting->blocking_cell_count = blocking_workers;
    accounting->compensation_cell_count = compensation_workers;
    return RT_HEAP_ACCOUNTING_OK;
}

void rt_heap_accounting_destroy(rt_heap_accounting* accounting) {
    if (accounting == NULL) {
        return;
    }
    free(accounting->compensation_cells);
    free(accounting->blocking_cells);
    free(accounting->worker_cells);
    accounting->compensation_cells = NULL;
    accounting->blocking_cells = NULL;
    accounting->worker_cells = NULL;
    accounting->worker_cell_count = 0;
    accounting->blocking_cell_count = 0;
    accounting->compensation_cell_count = 0;
}

rt_heap_accounting_cell* rt_heap_accounting_cold_cell(void) {
    return &cold_cell;
}

rt_heap_accounting_cell* rt_heap_accounting_main_cell(rt_heap_accounting* accounting) {
    return accounting != NULL ? &accounting->main_cell : &cold_cell;
}

rt_heap_accounting_cell* rt_heap_accounting_io_cell(rt_heap_accounting* accounting) {
    return accounting != NULL ? &accounting->io_cell : &cold_cell;
}

rt_heap_accounting_cell* rt_heap_accounting_worker_cell(rt_heap_accounting* accounting,
                                                        uint32_t worker_id) {
    if (accounting == NULL || worker_id >= accounting->worker_cell_count) {
        return &cold_cell;
    }
    return &accounting->worker_cells[worker_id];
}

rt_heap_accounting_cell* rt_heap_accounting_blocking_cell(rt_heap_accounting* accounting,
                                                          uint32_t worker_id) {
    if (accounting == NULL || worker_id >= accounting->blocking_cell_count) {
        return &cold_cell;
    }
    return &accounting->blocking_cells[worker_id];
}

rt_heap_accounting_cell* rt_heap_accounting_compensation_cell(rt_heap_accounting* accounting,
                                                              uint32_t compensation_id) {
    if (accounting == NULL || compensation_id >= accounting->compensation_cell_count) {
        return &cold_cell;
    }
    return &accounting->compensation_cells[compensation_id];
}

void rt_heap_accounting_set_current_cell(rt_heap_accounting_cell* cell) {
    tls_heap_accounting_cell = cell;
}

rt_heap_accounting_cell* rt_heap_accounting_current_cell(void) {
    return tls_heap_accounting_cell != NULL ? tls_heap_accounting_cell : &cold_cell;
}

void rt_heap_accounting_record_alloc(rt_heap_accounting_cell* cell, uint64_t size) {
    rt_heap_accounting_cell* target = cell != NULL ? cell : &cold_cell;
    (void)atomic_fetch_add_explicit(&target->alloc_count, 1, memory_order_relaxed);
    (void)atomic_fetch_add_explicit(
        &target->alloc_bytes, heap_event_size(size), memory_order_relaxed);
}

void rt_heap_accounting_record_free(rt_heap_accounting_cell* cell, uint64_t size) {
    rt_heap_accounting_cell* target = cell != NULL ? cell : &cold_cell;
    (void)atomic_fetch_add_explicit(&target->free_count, 1, memory_order_relaxed);
    (void)atomic_fetch_add_explicit(
        &target->free_bytes, heap_event_size(size), memory_order_relaxed);
}

void rt_heap_accounting_record_realloc(rt_heap_accounting_cell* cell,
                                       uint64_t old_size,
                                       uint64_t new_size) {
    rt_heap_accounting_record_alloc(cell, new_size);
    rt_heap_accounting_record_free(cell, old_size);
}

rt_heap_accounting_status rt_heap_accounting_snapshot(rt_heap_accounting* accounting,
                                                      struct rt_heap_accounting_snapshot* out) {
    if (out == NULL) {
        return RT_HEAP_ACCOUNTING_INVALID_ARGUMENT;
    }
    uint64_t alloc_count = 0;
    uint64_t free_count = 0;
    uint64_t alloc_bytes = 0;
    uint64_t free_bytes = 0;
    add_cell_snapshot(&cold_cell, &alloc_count, &free_count, &alloc_bytes, &free_bytes);
    if (accounting != NULL) {
        add_cell_snapshot(
            &accounting->main_cell, &alloc_count, &free_count, &alloc_bytes, &free_bytes);
        add_cell_snapshot(
            &accounting->io_cell, &alloc_count, &free_count, &alloc_bytes, &free_bytes);
        add_cell_range_snapshot(accounting->worker_cells,
                                accounting->worker_cell_count,
                                &alloc_count,
                                &free_count,
                                &alloc_bytes,
                                &free_bytes);
        add_cell_range_snapshot(accounting->blocking_cells,
                                accounting->blocking_cell_count,
                                &alloc_count,
                                &free_count,
                                &alloc_bytes,
                                &free_bytes);
        add_cell_range_snapshot(accounting->compensation_cells,
                                accounting->compensation_cell_count,
                                &alloc_count,
                                &free_count,
                                &alloc_bytes,
                                &free_bytes);
    }
    out->alloc_count = alloc_count;
    out->free_count = free_count;
    // Snapshot reads are not a global cut across all relaxed per-lane counters.
    // Counts stay raw, while transient live underflow is saturated for public stats.
    out->live_blocks = alloc_count >= free_count ? alloc_count - free_count : 0;
    out->live_bytes = alloc_bytes >= free_bytes ? alloc_bytes - free_bytes : 0;
    return RT_HEAP_ACCOUNTING_OK;
}
