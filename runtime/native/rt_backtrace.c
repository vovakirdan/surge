/* Recovering a Surge backtrace from a machine stack.
 *
 * The VM can name its frames because it has frames — an interpreter stack it
 * walks. A compiled binary has a machine stack instead, and nothing on it says
 * which Surge function an address belongs to or what line it is running.
 *
 * Two things supply that here, and neither costs anything while the program is
 * healthy. The stack is walked with the unwinder's own CFI, which LLVM already
 * emits as `.eh_frame` — so no frame pointer is required, which matters because
 * clang keeps none for these functions even without optimisation. And the
 * addresses are named by two tables the ASSEMBLER built: the emitter drops a
 * label in the instruction stream and a row in a section of its own, so the
 * table is exact and the code carries not one extra instruction. See
 * internal/backend/llvm/emit_trace_table.go for the emitting half.
 *
 *   surge_fn_map    one row per function entry:     address -> function name
 *   surge_line_map  one row per change of location: address -> file:line:col
 *
 * Rows are self-relative (`.long 1b - .`) so they need no relocation and work
 * in a position-independent executable.
 *
 * Lookup is a linear scan, deliberately. Sorting would need either an
 * allocation or a mutable copy, and this runs exactly once, on a path that is
 * already ending the process. A scan that cannot fail is worth more here than
 * a search that can. */

#include "rt_async_internal.h"

#include <unwind.h>

typedef struct {
    int32_t rel;
    uint32_t str;
} surge_trace_row;

typedef struct {
    const uint8_t* ptr;
    uint64_t len;
} surge_trace_str;

/* Section bounds the linker synthesises, and the string table the emitter
 * writes. All weak: a module that emitted nothing still has to link, and the
 * answer then is "no backtrace", not a missing symbol. */
/* NOLINTBEGIN(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
 * These names are NOT ours to choose. The linker synthesises __start_SECTION
 * and __stop_SECTION for every section a program defines, so spelling them any
 * other way does not name the bounds of anything. __surge_trace_text_end is
 * ours (emit_trace_table.go writes it), and it stays in the reserved space
 * deliberately: reserved names belong to the IMPLEMENTATION, and a language
 * runtime is the implementation — that is what keeps it from colliding with a
 * symbol a Surge program defines. */
extern const surge_trace_row __start_surge_fn_map[] __attribute__((weak));
extern const surge_trace_row __stop_surge_fn_map[] __attribute__((weak));
extern const surge_trace_row __start_surge_line_map[] __attribute__((weak));
extern const surge_trace_row __stop_surge_line_map[] __attribute__((weak));
extern const surge_trace_str surge_trace_strings[] __attribute__((weak));
extern const uint64_t surge_trace_string_count __attribute__((weak));
/* Where this program's Surge code stops. The map records only where functions
 * BEGIN, so without an upper bound every address in this runtime — which links
 * after the emitted object and therefore sits above it — resolves to the last
 * Surge function. Three frames of the panic path printed under a Surge name
 * before this bound existed. */
extern void __surge_trace_text_end(void) __attribute__((weak));
/* NOLINTEND(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp) */

static uintptr_t trace_row_addr(const surge_trace_row* row) {
    /* The offset is signed and the base is not, so the add is done in the
     * unsigned type deliberately: wrap-around is the defined behaviour that
     * makes a negative offset walk backwards. */
    return (uintptr_t)&row->rel + (uintptr_t)(intptr_t)row->rel;
}

/* The last row at or before `pc`, or NULL when the tables place `pc` outside
 * every Surge function. `floor` bounds the answer so a function's own rows
 * cannot be attributed to the next function along. */
static const surge_trace_row* trace_lookup(const surge_trace_row* start,
                                           const surge_trace_row* stop,
                                           uintptr_t pc,
                                           uintptr_t floor_addr) {
    if (start == NULL || stop == NULL) {
        return NULL;
    }
    const surge_trace_row* best = NULL;
    uintptr_t best_addr = 0;
    for (const surge_trace_row* row = start; row < stop; row++) {
        uintptr_t addr = trace_row_addr(row);
        if (addr > pc || addr < floor_addr) {
            continue;
        }
        if (best == NULL || addr > best_addr) {
            best = row;
            best_addr = addr;
        }
    }
    return best;
}

static int trace_string(uint32_t index, const uint8_t** out_ptr, uint64_t* out_len) {
    /* Both are WEAK: when the emitter wrote no string table the symbols are
     * absent and their addresses are genuinely 0. cppcheck reads these as an
     * array and an address-of, which "can never be null" for ordinary symbols —
     * true, and not true of weak ones, which is the whole point of declaring
     * them weak. Removing the checks would fault on any module that emitted
     * nothing. */
    /* cppcheck-suppress[knownConditionTrueFalse,knownConditionTrueFalse] */
    if (surge_trace_strings == NULL || &surge_trace_string_count == NULL) {
        return 0;
    }
    if ((uint64_t)index >= surge_trace_string_count) {
        return 0;
    }
    *out_ptr = surge_trace_strings[index].ptr;
    *out_len = surge_trace_strings[index].len;
    return *out_ptr != NULL;
}

typedef struct {
    uintptr_t pcs[64];
    unsigned len;
} trace_frames;

static _Unwind_Reason_Code trace_collect(struct _Unwind_Context* ctx, void* arg) {
    trace_frames* frames = (trace_frames*)arg;
    if (frames->len >= sizeof(frames->pcs) / sizeof(frames->pcs[0])) {
        return _URC_END_OF_STACK;
    }
    uintptr_t ip = (uintptr_t)_Unwind_GetIP(ctx);
    if (ip == 0) {
        return _URC_END_OF_STACK;
    }
    frames->pcs[frames->len++] = ip;
    return _URC_NO_REASON;
}

static void trace_write_decimal(unsigned value) {
    uint8_t buf[12];
    unsigned n = 0;
    do {
        buf[n++] = (uint8_t)('0' + (value % 10));
        value /= 10;
    } while (value != 0 && n < sizeof(buf));
    while (n > 0) {
        rt_write_stderr(&buf[--n], 1);
    }
}

/* Prints the frames in the shape the VM prints them (internal/vm/panic.go,
 * FormatWithFiles), because two backends reporting one panic two ways is worse
 * than one of them reporting nothing.
 *
 * `site` is the location the emitter already knew for the innermost frame — a
 * panic it raised itself carries one. A panic raised inside this runtime does
 * not, and then the innermost SURGE frame's own row answers instead, which is
 * how a bignum divide-by-zero gets a location it cannot supply for itself. */
void rt_panic_write_where(const uint8_t* site, uint64_t site_length) {
    trace_frames frames;
    frames.len = 0;
    _Unwind_Backtrace(trace_collect, &frames);

    /* The `at` line and the frames are one report and are written together, so
     * a panic cannot end up with one and not the other. When the caller has no
     * location of its own — every panic raised inside this runtime — the
     * innermost Surge frame supplies it, which is what gives a bignum
     * divide-by-zero the line the emitter never knew. */
    static const uint8_t header[] = "backtrace:\n";
    unsigned printed = 0;
    int seen_surge = 0;
    for (unsigned i = 0; i < frames.len; i++) {
        /* For every frame but the innermost the unwinder reports the RETURN
         * address, which is the instruction after the call; stepping back one
         * byte puts the lookup inside the call it came from. */
        uintptr_t pc = frames.pcs[i];
        uintptr_t lookup = (i == 0 || pc == 0) ? pc : pc - 1;

        const surge_trace_row* fn = NULL;
        /* Weak again: a module with no Surge text has no end marker, and then
         * every pc is inside the (empty) range rather than outside it. */
        /* cppcheck-suppress knownConditionTrueFalse */
        if (&__surge_trace_text_end == NULL || lookup < (uintptr_t)&__surge_trace_text_end) {
            fn = trace_lookup(__start_surge_fn_map, __stop_surge_fn_map, lookup, 0);
        }
        if (fn == NULL) {
            /* Leading frames are this runtime walking to here, so they are
             * skipped; once Surge code has been seen, the first frame that is
             * not Surge is the edge of what the program can be said to be
             * doing — a task's poll entry, or the C main below __surge_start.
             * The VM stops in the same place, which is why it prints no `main`
             * for a panic inside a spawned task. */
            if (seen_surge) {
                break;
            }
            continue;
        }
        /* Glue rather than a function the program wrote — a task's poll
         * dispatcher. The VM has no frame for its own dispatcher either, so the
         * walk ends here and the trace ends where the VM's does. */
        if (fn->str == 0xFFFFFFFFu) {
            if (seen_surge) {
                break;
            }
            continue;
        }
        seen_surge = 1;

        const uint8_t* name = NULL;
        uint64_t name_len = 0;
        if (!trace_string(fn->str, &name, &name_len)) {
            continue;
        }

        const uint8_t* loc = NULL;
        uint64_t loc_len = 0;
        if (printed == 0 && site != NULL && site_length > 0) {
            loc = site;
            loc_len = site_length;
        } else {
            const surge_trace_row* line = trace_lookup(
                __start_surge_line_map, __stop_surge_line_map, lookup, trace_row_addr(fn));
            if (line != NULL) {
                (void)trace_string(line->str, &loc, &loc_len);
            }
        }

        if (printed == 0) {
            if (loc != NULL && loc_len > 0) {
                static const uint8_t at_prefix[] = "at ";
                rt_write_stderr(at_prefix, (uint64_t)(sizeof(at_prefix) - 1));
                rt_write_stderr(loc, loc_len);
                if (loc[loc_len - 1] != '\n') {
                    rt_write_stderr((const uint8_t*)"\n", 1);
                }
            }
            rt_write_stderr(header, (uint64_t)(sizeof(header) - 1));
        }
        rt_write_stderr((const uint8_t*)"  ", 2);
        trace_write_decimal(printed);
        rt_write_stderr((const uint8_t*)": ", 2);
        rt_write_stderr(name, name_len);
        rt_write_stderr((const uint8_t*)" at ", 4);
        if (loc != NULL && loc_len > 0) {
            rt_write_stderr(loc, loc_len);
        } else {
            static const uint8_t nospan[] = "<no-span>";
            rt_write_stderr(nospan, (uint64_t)(sizeof(nospan) - 1));
        }
        rt_write_stderr((const uint8_t*)"\n", 1);
        printed++;
    }
}
