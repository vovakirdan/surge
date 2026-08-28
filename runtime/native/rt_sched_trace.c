#include "rt_sched_trace.h"

#include "rt_async_internal.h"
#include "rt_async_trace.h"

#include <signal.h>
#include <stdlib.h>
#include <unistd.h>

static volatile sig_atomic_t sched_trace_enabled_flag = 0;

// One cell per carrier, indexed by the carrier's flat index, one for the
// runtime's control lane, and one for the runtime itself. The array is sized
// once, before any carrier exists, and never resized or freed: a cell that
// could move or vanish under its owner is not a cell the owner can write
// without synchronizing with whoever moved it, and a carrier records right up
// to the moment the process ends. So it lasts as long as the process, exactly
// as the file-scope words it replaced did, and is reported by a leak checker as
// still reachable rather than lost.
//
// Nothing else in this file holds a counter. Every number the dump prints is
// read out of one of these cells, which is what lets the dump name the owner of
// every number it prints -- and what stops the next counter from arriving as a
// word at file scope that nobody can attribute.
static rt_sched_trace_cell* sched_trace_carrier_cells;
static uint32_t sched_trace_carrier_count;
static rt_sched_trace_cell sched_trace_control_cell;
static rt_sched_trace_runtime_cell sched_trace_runtime_cell;

static int trace_sched_enabled(void) {
    return sched_trace_enabled_flag != 0;
}

// The calling thread's own cell, or NULL when this thread's record belongs to
// no owner the dump could name.
static rt_sched_trace_cell* sched_trace_owner_cell(void) {
    if (tls_worker_ctx == NULL) {
        // Not a carrier: this is the control lane draining shard 0 from inside
        // ready_pop, which holds shard 0's lock across the record.
        return &sched_trace_control_cell;
    }
    if (sched_trace_carrier_cells == NULL ||
        tls_worker_ctx->worker_index >= sched_trace_carrier_count) {
        return NULL;
    }
    return &sched_trace_carrier_cells[tls_worker_ctx->worker_index];
}

// The owner reads back only what it wrote itself, so a relaxed load is the
// whole of the read side; the store is release because a reader elsewhere has
// no lock to acquire and must not see the count without the pops behind it.
static void sched_trace_publish(_Atomic uint64_t* field, uint64_t next) {
    atomic_store_explicit(field, next, memory_order_release);
}

static uint64_t sched_trace_own(const _Atomic uint64_t* field) {
    return atomic_load_explicit(field, memory_order_relaxed);
}

static void sched_trace_bump_owned(_Atomic uint64_t* field) {
    sched_trace_publish(field, sched_trace_own(field) + 1);
}

// The runtime's fields have many writers and no lock between them, so the
// increment has to be the atomic one; it is still released, because the reader
// of the runtime's cell holds no lock either.
static void sched_trace_bump_runtime(_Atomic uint64_t* field) {
    (void)atomic_fetch_add_explicit(field, 1, memory_order_release);
}

static uint64_t sched_trace_read(const _Atomic uint64_t* field) {
    return atomic_load_explicit(field, memory_order_acquire);
}

void rt_sched_trace_init(uint32_t carriers) {
    const char* value = getenv("SURGE_SCHED_TRACE");
    if (value == NULL || value[0] == '\0' || (value[0] == '0' && value[1] == '\0')) {
        return;
    }
    atomic_store_explicit(
        &sched_trace_control_cell.shard_id, RT_SCHED_TRACE_SHARD_NONE, memory_order_relaxed);
    if (carriers > 0) {
        size_t bytes = (size_t)carriers * sizeof(rt_sched_trace_cell);
        rt_sched_trace_cell* cells =
            (rt_sched_trace_cell*)aligned_alloc(RT_SCHED_TRACE_CELL_BYTES, bytes);
        if (cells == NULL) {
            // Reporting that cannot name an owner does not report. Arming it
            // anyway would put carrier pops on the control lane's cell and
            // publish them under a name that did not earn them.
            return;
        }
        for (uint32_t i = 0; i < carriers; i++) {
            atomic_store_explicit(&cells[i].local_pops, 0, memory_order_relaxed);
            atomic_store_explicit(&cells[i].inject_pops, 0, memory_order_relaxed);
            atomic_store_explicit(&cells[i].steal_pops, 0, memory_order_relaxed);
            atomic_store_explicit(&cells[i].pop_mix, 0, memory_order_relaxed);
            atomic_store_explicit(&cells[i].steal_denied, 0, memory_order_relaxed);
            atomic_store_explicit(&cells[i].conn_run_local, 0, memory_order_relaxed);
            atomic_store_explicit(&cells[i].conn_run_mismatch, 0, memory_order_relaxed);
            atomic_store_explicit(
                &cells[i].shard_id, RT_SCHED_TRACE_SHARD_NONE, memory_order_relaxed);
        }
        sched_trace_carrier_cells = cells;
        sched_trace_carrier_count = carriers;
    }
    // Armed last: no thread may find the switch on before the cells behind it
    // exist.
    sched_trace_enabled_flag = 1;
}

// One pop, finalized so that the sum of many is sensitive to WHICH pops were
// made and not merely to how many: a plain sum of ids would report the same
// number for any set with the same total.
static uint64_t sched_trace_mix(uint64_t id, rt_trace_sched_source source) {
    uint64_t x = id ^ ((uint64_t)source << 56);
    x ^= x >> 30;
    x *= UINT64_C(0xbf58476d1ce4e5b9);
    x ^= x >> 27;
    x *= UINT64_C(0x94d049bb133111eb);
    x ^= x >> 31;
    return x;
}

// The owner's cell for a record taken inside a pop, with the shard filled in on
// the first record that owner makes, or NULL when the dump could not name the
// owner. Only the pop itself counts that NULL, on `unowned_pops`, which is what
// its name says; a refusal or a connection run from the same unnameable thread
// adds nothing, because that thread's pops already report it and counting it
// twice would report one unattributable owner as two.
static rt_sched_trace_cell* sched_trace_pop_path_cell(void) {
    rt_sched_trace_cell* cell = sched_trace_owner_cell();
    if (cell == NULL) {
        return NULL;
    }
    if (atomic_load_explicit(&cell->shard_id, memory_order_relaxed) == RT_SCHED_TRACE_SHARD_NONE) {
        // The control lane's pops all come out of shard 0's queue under shard
        // 0's lock, which is the shard it is naming here.
        uint32_t shard_id = tls_worker_ctx != NULL ? tls_worker_ctx->shard_id : 0;
        atomic_store_explicit(&cell->shard_id, shard_id, memory_order_release);
    }
    return cell;
}

void rt_trace_sched_record(rt_trace_sched_source source, uint64_t id) {
    if (!trace_sched_enabled()) {
        return;
    }
    rt_sched_trace_cell* cell = sched_trace_pop_path_cell();
    if (cell == NULL) {
        sched_trace_bump_runtime(&sched_trace_runtime_cell.unowned_pops);
        return;
    }
    _Atomic uint64_t* bucket = NULL;
    if (source == RT_TRACE_SCHED_SRC_LOCAL) {
        bucket = &cell->local_pops;
    } else if (source == RT_TRACE_SCHED_SRC_INJECT) {
        bucket = &cell->inject_pops;
    } else if (source == RT_TRACE_SCHED_SRC_STEAL) {
        bucket = &cell->steal_pops;
    }
    sched_trace_publish(&cell->pop_mix,
                        sched_trace_own(&cell->pop_mix) + sched_trace_mix(id, source));
    if (bucket != NULL) {
        sched_trace_bump_owned(bucket);
    }
}

// A steal this thread attempted and had refused. Its own action, on its own
// cell -- the same cell, the same path and the same lock discipline as the pop
// the refusal happened inside.
void rt_trace_sched_tier1_steal_denied(void) {
    if (!trace_sched_enabled()) {
        return;
    }
    rt_sched_trace_cell* cell = sched_trace_pop_path_cell();
    if (cell == NULL) {
        return;
    }
    sched_trace_bump_owned(&cell->steal_denied);
}

// The placement, unlike the run below, is made by whoever created the task --
// a carrier, the io thread, or a thread with no cell at all -- so it is not a
// record of one owner's own action and it is counted as the runtime's.
void rt_trace_sched_connection_owner_placed(void) {
    if (!trace_sched_enabled()) {
        return;
    }
    sched_trace_bump_runtime(&sched_trace_runtime_cell.conn_placed);
}

void rt_trace_sched_connection_owner_run(uint32_t owner_shard_id, uint32_t worker_shard_id) {
    if (!trace_sched_enabled()) {
        return;
    }
    rt_sched_trace_cell* cell = sched_trace_pop_path_cell();
    if (cell == NULL) {
        return;
    }
    if (owner_shard_id == worker_shard_id) {
        sched_trace_bump_owned(&cell->conn_run_local);
        return;
    }
    sched_trace_bump_owned(&cell->conn_run_mismatch);
}

// One record, one write, and a short or refused write is counted rather than
// dropped on the floor: a reader that summed four owner records out of five has
// to be told, or it will report the sum as the whole.
static void sched_trace_emit(const char* buf, size_t pos) {
    if (buf == NULL || pos == 0) {
        return;
    }
    ssize_t written = write(STDERR_FILENO, buf, pos);
    if (written < 0 || (size_t)written != pos) {
        sched_trace_bump_runtime(&sched_trace_runtime_cell.dropped_records);
    }
}

static void sched_trace_emit_cell(const rt_sched_trace_cell* cell, const char* owner, uint32_t id) {
    uint64_t local = sched_trace_read(&cell->local_pops);
    uint64_t inject = sched_trace_read(&cell->inject_pops);
    uint64_t steal = sched_trace_read(&cell->steal_pops);
    uint64_t pop_mix = sched_trace_read(&cell->pop_mix);
    uint64_t steal_denied = sched_trace_read(&cell->steal_denied);
    uint64_t conn_run_local = sched_trace_read(&cell->conn_run_local);
    uint64_t conn_run_mismatch = sched_trace_read(&cell->conn_run_mismatch);
    uint32_t shard_id = atomic_load_explicit(&cell->shard_id, memory_order_acquire);

    char buf[512];
    size_t pos = 0;
    pos = trace_append_literal(buf, pos, sizeof(buf), "SCHED_TRACE owner=");
    pos = trace_append_literal(buf, pos, sizeof(buf), owner);
    pos = trace_append_literal(buf, pos, sizeof(buf), " id=");
    pos = trace_append_u64(buf, pos, sizeof(buf), id);
    pos = trace_append_literal(buf, pos, sizeof(buf), " shard=");
    if (shard_id == RT_SCHED_TRACE_SHARD_NONE) {
        pos = trace_append_literal(buf, pos, sizeof(buf), "none");
    } else {
        pos = trace_append_u64(buf, pos, sizeof(buf), shard_id);
    }
    trace_append_kv_u64(buf, &pos, sizeof(buf), "local", local);
    trace_append_kv_u64(buf, &pos, sizeof(buf), "inject", inject);
    trace_append_kv_u64(buf, &pos, sizeof(buf), "steal", steal);
    // Derived here rather than kept as a sixth word, so this owner's total can
    // never disagree with the three buckets that make it up.
    trace_append_kv_u64(buf, &pos, sizeof(buf), "events", local + inject + steal);
    trace_append_kv_u64(buf, &pos, sizeof(buf), "pop_mix", pop_mix);
    trace_append_kv_u64(buf, &pos, sizeof(buf), "tier1_steal_denied", steal_denied);
    trace_append_kv_u64(buf, &pos, sizeof(buf), "conn_owner_local", conn_run_local);
    trace_append_kv_u64(buf, &pos, sizeof(buf), "conn_owner_mismatch", conn_run_mismatch);
    if (pos + 1 < sizeof(buf)) {
        buf[pos++] = '\n';
    }
    sched_trace_emit(buf, pos);
}

// Last, so it can report the drops of everything before it, and so a reader has
// the owner set in hand by the time it has finished reading. Its three counts
// are read from the runtime's own cell, exactly as the owner records above are
// read from theirs -- there is no other place in this file for a number to come
// from.
static void sched_trace_emit_runtime(const rt_sched_trace_runtime_cell* runtime,
                                     uint8_t mode,
                                     uint64_t seed,
                                     uint32_t carriers) {
    char buf[512];
    size_t pos = 0;
    pos = trace_append_literal(buf, pos, sizeof(buf), "SCHED_TRACE owner=runtime mode=");
    pos = trace_append_literal(buf, pos, sizeof(buf), mode == SCHED_SEEDED ? "seeded" : "parallel");
    pos = trace_append_literal(buf, pos, sizeof(buf), " seed=");
    pos = trace_append_u64(buf, pos, sizeof(buf), seed);
    // The owner set the records above cover: `carriers` carrier records plus
    // the control lane's. A reader that counted fewer knows one went missing
    // even when the write that lost it could not report itself.
    trace_append_kv_u64(buf, &pos, sizeof(buf), "carriers", (uint64_t)carriers);
    trace_append_kv_u64(buf, &pos, sizeof(buf), "owners", (uint64_t)carriers + 1U);
    trace_append_kv_u64(
        buf, &pos, sizeof(buf), "conn_owner_placed", sched_trace_read(&runtime->conn_placed));
    trace_append_kv_u64(
        buf, &pos, sizeof(buf), "unowned_pops", sched_trace_read(&runtime->unowned_pops));
    trace_append_kv_u64(
        buf, &pos, sizeof(buf), "dropped_records", sched_trace_read(&runtime->dropped_records));
    if (pos + 1 < sizeof(buf)) {
        buf[pos++] = '\n';
    }
    sched_trace_emit(buf, pos);
}

void rt_sched_trace_dump(void) {
    if (!trace_sched_enabled()) {
        return;
    }
    // Only the mode and the seed live on the executor. The owner records do
    // not, so a dump taken before the executor exists still reports the owner
    // set instead of reporting nothing at all.
    uint64_t seed = 0;
    uint8_t mode = SCHED_PARALLEL;
    if (exec_state.initialized) {
        rt_control_lock(&exec_state);
        const rt_scheduler* scheduler = rt_executor_scheduler_const(&exec_state);
        seed = scheduler != NULL ? scheduler->sched_seed : 0;
        mode = scheduler != NULL ? scheduler->sched_mode : SCHED_PARALLEL;
        rt_control_unlock(&exec_state);
    }

    // Every number below is read from exactly one owner's cell and printed
    // under that owner's name. No row sums across owners: a sum over owners has
    // writers that share neither a lock nor an owner, so the runtime does not
    // publish one; a reader that wants a total adds up the records below and
    // knows, from `owners`, exactly which owner set it added up. The runtime's
    // last row is not such a sum -- it is one owner's own three counts, of
    // events no carrier and no lane performed.
    for (uint32_t i = 0; i < sched_trace_carrier_count; i++) {
        sched_trace_emit_cell(&sched_trace_carrier_cells[i], "carrier", i);
    }
    sched_trace_emit_cell(&sched_trace_control_cell, "control", 0);
    sched_trace_emit_runtime(&sched_trace_runtime_cell, mode, seed, sched_trace_carrier_count);
}
