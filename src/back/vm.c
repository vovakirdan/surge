#include "vm.h"
#include "config.h"

#include <stdarg.h>
#include <float.h>
#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define VM_DEFAULT_STACK_CAP 1024u
#define VM_DEFAULT_FRAME_CAP 64u

typedef struct RtString {
    uint32_t refcnt;
    uint32_t len;
    uint32_t from_arena;
    char data[];
} RtString;

static const char *kGlobalInitName = "__global_init_auto__";

typedef struct VmArenaChunk {
    struct VmArenaChunk *next;
    size_t used;
    size_t cap;
    uint8_t data[];
} VmArenaChunk;

static void vm_arena_init(VmArena *arena, size_t chunk_size) {
    if (!arena) {
        return;
    }
    arena->chunks = NULL;
    arena->chunk_size = chunk_size ? chunk_size : 64u * 1024u;
}

static void vm_arena_destroy(VmArena *arena) {
    if (!arena) {
        return;
    }
    VmArenaChunk *chunk = arena->chunks;
    while (chunk) {
        VmArenaChunk *next = chunk->next;
        free(chunk);
        chunk = next;
    }
    arena->chunks = NULL;
}

static void vm_arena_reset(VmArena *arena) {
    if (!arena) {
        return;
    }
    for (VmArenaChunk *chunk = arena->chunks; chunk; chunk = chunk->next) {
        chunk->used = 0;
    }
}

static void *vm_arena_alloc(VmArena *arena, size_t size) {
    if (!arena || size == 0) {
        return NULL;
    }
    size = (size + 7u) & ~7u;
    VmArenaChunk *chunk = arena->chunks;
    if (!chunk || chunk->used + size > chunk->cap) {
        size_t cap = size > arena->chunk_size ? size : arena->chunk_size;
        VmArenaChunk *new_chunk = (VmArenaChunk*)malloc(sizeof(VmArenaChunk) + cap);
        if (!new_chunk) {
            return NULL;
        }
        new_chunk->next = chunk;
        new_chunk->used = 0;
        new_chunk->cap = cap;
        arena->chunks = new_chunk;
        chunk = new_chunk;
    }
    void *ptr = chunk->data + chunk->used;
    chunk->used += size;
    return ptr;
}

struct VmArray {
    uint32_t refcnt;
    uint32_t len;
    VmValue *data;
};

typedef struct VmFrame {
    const SbcFuncDesc *func;
    const uint8_t *code_start;
    const uint8_t *ip;
    const uint8_t *code_end;
    uint32_t base;
    uint32_t locals_end;
} VmFrame;

static RtString *rt_string_new(Vm *vm, const uint8_t *bytes, uint32_t len) {
    size_t total = sizeof(RtString) + (size_t)len + 1u;
    RtString *str = vm ? (RtString*)vm_arena_alloc(&vm->arena, total) : NULL;
    if (str) {
        str->from_arena = 1u;
    } else {
        str = (RtString*)malloc(total);
        if (!str) {
            return NULL;
        }
        str->from_arena = 0u;
    }
    str->refcnt = 1u;
    str->len = len;
    if (len) {
        memcpy(str->data, bytes, len);
    }
    str->data[len] = '\0';
    return str;
}

static void rt_string_retain(RtString *str) {
    if (str) {
        ++str->refcnt;
    }
}

static void rt_string_release(RtString *str) {
    if (!str) {
        return;
    }
    if (str->refcnt > 0) {
        --str->refcnt;
    }
    if (str->refcnt == 0) {
        if (str->from_arena) {
            return;
        }
        free(str);
    }
}

static VmValue vm_value_null(void) {
    VmValue v;
    memset(&v, 0, sizeof(v));
    v.tag = VM_VT_NULL;
    return v;
}

static VmValue vm_value_bool(bool b) {
    VmValue v = vm_value_null();
    v.tag = VM_VT_BOOL;
    v.as.b = (uint8_t)(b ? 1 : 0);
    return v;
}

static VmValue vm_value_i64(int64_t x) {
    VmValue v = vm_value_null();
    v.tag = VM_VT_I64;
    v.as.i64 = x;
    return v;
}

static VmValue vm_value_f64(double x) {
    VmValue v = vm_value_null();
    v.tag = VM_VT_F64;
    v.as.f64 = x;
    return v;
}

static void vm_clear_error(Vm *vm) {
    if (!vm) {
        return;
    }
    free(vm->last_error);
    vm->last_error = NULL;
}

static void vm_set_errorf(Vm *vm, const char *fmt, ...) {
    if (!vm) {
        return;
    }
    vm_clear_error(vm);
    if (!fmt) {
        return;
    }
    va_list ap;
    va_start(ap, fmt);
    int needed = vsnprintf(NULL, 0, fmt, ap);
    va_end(ap);
    if (needed < 0) {
        return;
    }
    char *buf = (char*)malloc((size_t)needed + 1u);
    if (!buf) {
        return;
    }
    va_start(ap, fmt);
    vsnprintf(buf, (size_t)needed + 1u, fmt, ap);
    va_end(ap);
    vm->last_error = buf;
}

static void vm_array_retain(struct VmArray *arr) {
    if (arr) {
        ++arr->refcnt;
    }
}

static void vm_value_retain(VmValue *value) {
    if (!value) {
        return;
    }
    if (value->tag == VM_VT_ARR && value->as.arr) {
        vm_array_retain(value->as.arr);
    }
    if (value->tag == VM_VT_STR && value->as.str.obj) {
        rt_string_retain(value->as.str.obj);
    }
}

static bool vm_fetch_func_name(const Vm *vm, const SbcFuncDesc *func, VmString *out);

void vm_value_release(Vm *vm, VmValue *value) {
    (void)vm;
    if (!value) {
        return;
    }
    if (value->tag == VM_VT_ARR && value->as.arr) {
        struct VmArray *arr = value->as.arr;
        if (arr->refcnt > 0) {
            --arr->refcnt;
        }
        if (arr->refcnt == 0) {
            if (arr->data) {
                for (uint32_t i = 0; i < arr->len; ++i) {
                    vm_value_release(vm, &arr->data[i]);
                }
                free(arr->data);
            }
            free(arr);
        }
    }
    if (value->tag == VM_VT_STR && value->as.str.obj) {
        rt_string_release(value->as.str.obj);
    }
    if (value->tag == VM_VT_STR) {
        value->as.str.obj = NULL;
        value->as.str.data = NULL;
        value->as.str.len = 0;
    }
    memset(value, 0, sizeof(*value));
    value->tag = VM_VT_NULL;
}

void vm_config_defaults(VmConfig *cfg) {
    if (!cfg) {
        return;
    }
    cfg->stack_capacity = VM_DEFAULT_STACK_CAP;
    cfg->frame_capacity = VM_DEFAULT_FRAME_CAP;
    cfg->trace = false;
}

static void vm_clear_stack(Vm *vm) {
    if (!vm) {
        return;
    }
    for (uint32_t i = 0; i < vm->sp; ++i) {
        vm_value_release(vm, &vm->stack[i]);
    }
    vm->sp = 0;
}

static void vm_globals_clear(Vm *vm) {
    if (!vm || !vm->globals) {
        return;
    }
    for (uint32_t i = 0; i < vm->global_count; ++i) {
        vm_value_release(vm, &vm->globals[i]);
    }
    free(vm->globals);
    vm->globals = NULL;
    vm->global_count = 0;
}

static void vm_string_cache_clear(Vm *vm) {
    if (!vm || !vm->str_cache) {
        return;
    }
    for (uint32_t i = 0; i < vm->str_cache_len; ++i) {
        if (vm->str_cache[i]) {
            rt_string_release(vm->str_cache[i]);
            vm->str_cache[i] = NULL;
        }
    }
    free(vm->str_cache);
    vm->str_cache = NULL;
    vm->str_cache_len = 0;
}

static bool vm_prepare_globals(Vm *vm, uint32_t count) {
    vm_globals_clear(vm);
    if (count == 0) {
        return true;
    }
    vm->globals = (VmValue*)calloc(count, sizeof(VmValue));
    if (!vm->globals) {
        vm_set_errorf(vm, "vm: failed to allocate globals (%u)", count);
        return false;
    }
    vm->global_count = count;
    return true;
}

static bool vm_prepare_string_cache(Vm *vm, uint32_t count) {
    vm_string_cache_clear(vm);
    if (count == 0) {
        return true;
    }
    vm->str_cache = (RtString**)calloc(count, sizeof(RtString*));
    if (!vm->str_cache) {
        vm_set_errorf(vm, "vm: failed to allocate string cache (%u)", count);
        return false;
    }
    vm->str_cache_len = count;
    return true;
}

bool vm_init(Vm *vm, const VmConfig *cfg) {
    if (!vm) {
        return false;
    }
    memset(vm, 0, sizeof(*vm));

    VmConfig local_cfg;
    if (cfg) {
        local_cfg = *cfg;
    } else {
        vm_config_defaults(&local_cfg);
    }
    if (local_cfg.stack_capacity == 0) {
        local_cfg.stack_capacity = VM_DEFAULT_STACK_CAP;
    }
    if (local_cfg.frame_capacity == 0) {
        local_cfg.frame_capacity = VM_DEFAULT_FRAME_CAP;
    }

    vm->cfg = local_cfg;
    vm->trace = local_cfg.trace;

    vm->stack = (VmValue*)calloc(vm->cfg.stack_capacity, sizeof(VmValue));
    if (!vm->stack) {
        return false;
    }
    vm->stack_cap = vm->cfg.stack_capacity;

    vm->frames = (VmFrame*)calloc(vm->cfg.frame_capacity, sizeof(VmFrame));
    if (!vm->frames) {
        free(vm->stack);
        vm->stack = NULL;
        vm->stack_cap = 0;
        return false;
    }
    vm->frame_cap = vm->cfg.frame_capacity;
    vm_arena_init(&vm->arena, 64u * 1024u);
    return true;
}

void vm_reset(Vm *vm) {
    if (!vm) {
        return;
    }
    vm_clear_stack(vm);
    vm_globals_clear(vm);
    vm_string_cache_clear(vm);
    vm_arena_reset(&vm->arena);
    vm->fp = 0;
    vm->img = NULL;
    vm->trace = vm->cfg.trace;
    vm_clear_error(vm);
}

void vm_destroy(Vm *vm) {
    if (!vm) {
        return;
    }
    vm_clear_stack(vm);
    vm_globals_clear(vm);
    vm_string_cache_clear(vm);
    vm_arena_destroy(&vm->arena);
    free(vm->stack);
    free(vm->frames);
    vm->stack = NULL;
    vm->frames = NULL;
    vm->stack_cap = 0;
    vm->frame_cap = 0;
    vm->fp = 0;
    vm_clear_error(vm);
}

const char *vm_last_error(const Vm *vm) {
    return vm ? vm->last_error : NULL;
}

static const char *vm_trap_name(SurgeTrapCode code) {
    switch (code) {
        case SURGE_TRAP_UNREACHABLE:    return "UNREACHABLE";
        case SURGE_TRAP_DIV_BY_ZERO:    return "DIV_BY_ZERO";
        case SURGE_TRAP_OUT_OF_BOUNDS:  return "OUT_OF_BOUNDS";
        case SURGE_TRAP_BAD_CALL:       return "BAD_CALL";
        case SURGE_TRAP_TYPE_ERROR:     return "TYPE_ERROR";
        case SURGE_TRAP_STACK_OVERFLOW: return "STACK_OVERFLOW";
        default:                        return "UNKNOWN";
    }
}

static uint32_t read_u32_le(const uint8_t *p) {
    return (uint32_t)p[0] |
           ((uint32_t)p[1] << 8) |
           ((uint32_t)p[2] << 16) |
           ((uint32_t)p[3] << 24);
}

static uint16_t read_u16_le(const uint8_t *p) {
    return (uint16_t)p[0] |
           ((uint16_t)p[1] << 8);
}

static int32_t read_i32_le(const uint8_t *p) {
    return (int32_t)read_u32_le(p);
}

static uint64_t read_u64_le(const uint8_t *p) {
    return (uint64_t)p[0] |
           ((uint64_t)p[1] << 8) |
           ((uint64_t)p[2] << 16) |
           ((uint64_t)p[3] << 24) |
           ((uint64_t)p[4] << 32) |
           ((uint64_t)p[5] << 40) |
           ((uint64_t)p[6] << 48) |
           ((uint64_t)p[7] << 56);
}

static int64_t read_i64_le(const uint8_t *p) {
    uint64_t raw = read_u64_le(p);
    int64_t v;
    memcpy(&v, &raw, sizeof(v));
    return v;
}

static double read_f64_le(const uint8_t *p) {
    uint64_t raw = read_u64_le(p);
    double d;
    memcpy(&d, &raw, sizeof(d));
    return d;
}

static void vm_format_location(const Vm *vm, const VmFrame *frame, const uint8_t *op_start,
                               char *buf, size_t buf_sz) {
    if (!buf || buf_sz == 0) {
        return;
    }
    buf[0] = '\0';
    const char *name = "<unknown>";
    uint32_t name_len = (uint32_t)strlen(name);
    if (vm && frame && frame->func) {
        VmString str = {0};
        if (vm_fetch_func_name(vm, frame->func, &str) && str.data) {
            name = str.data;
            name_len = str.len;
        } else {
            name = "<anon>";
            name_len = (uint32_t)strlen(name);
        }
    }
    uint32_t offset = 0;
    if (frame && op_start) {
        offset = (uint32_t)(op_start - frame->code_start);
    }
    if (snprintf(buf, buf_sz, "%.*s+%04u", (int)name_len, name, offset) < 0) {
        /* fallback ensure string ends */
        if (buf_sz > 0) {
            buf[0] = '\0';
        }
    }
}

static void vm_set_errorf_loc(Vm *vm, VmFrame *frame, const uint8_t *op_start,
                              const char *fmt, ...) {
    if (!vm) {
        return;
    }
    char loc[128];
    vm_format_location(vm, frame, op_start, loc, sizeof(loc));

    va_list ap;
    va_start(ap, fmt);
    int needed = vsnprintf(NULL, 0, fmt, ap);
    va_end(ap);

    if (needed < 0) {
        vm_set_errorf(vm, "%s: <format-error>", loc);
        return;
    }

    char *msg = (char*)malloc((size_t)needed + 1u);
    if (!msg) {
        vm_set_errorf(vm, "%s: <oom>", loc);
        return;
    }

    va_start(ap, fmt);
    vsnprintf(msg, (size_t)needed + 1u, fmt, ap);
    va_end(ap);

    vm_set_errorf(vm, "%s: %s", loc, msg);
    free(msg);
}

static void vm_trace_warn(Vm *vm, VmFrame *frame, const uint8_t *op_start, const char *msg) {
    if (!vm || !vm->trace || !msg) {
        return;
    }
    char loc[128];
    vm_format_location(vm, frame, op_start, loc, sizeof(loc));
    fprintf(stderr, "[vm] warning %s: %s\n", loc, msg);
}

static bool vm_stack_reserve(Vm *vm, uint32_t needed_extra) {
    if (vm->sp + needed_extra <= vm->stack_cap) {
        return true;
    }
    uint32_t new_cap = vm->stack_cap ? vm->stack_cap : VM_DEFAULT_STACK_CAP;
    while (vm->sp + needed_extra > new_cap) {
        new_cap <<= 1;
        if (new_cap < vm->stack_cap) {
            new_cap = vm->stack_cap + needed_extra;
            break;
        }
    }
    VmValue *tmp = (VmValue*)realloc(vm->stack, new_cap * sizeof(VmValue));
    if (!tmp) {
        vm_set_errorf(vm, "vm: failed to grow stack to %u slots", new_cap);
        return false;
    }
    for (uint32_t i = vm->stack_cap; i < new_cap; ++i) {
        tmp[i] = vm_value_null();
    }
    vm->stack = tmp;
    vm->stack_cap = new_cap;
    return true;
}

static bool vm_frames_reserve(Vm *vm, uint32_t needed_extra) {
    if (vm->fp + needed_extra <= vm->frame_cap) {
        return true;
    }
    uint32_t new_cap = vm->frame_cap ? vm->frame_cap : VM_DEFAULT_FRAME_CAP;
    while (vm->fp + needed_extra > new_cap) {
        new_cap <<= 1;
        if (new_cap < vm->frame_cap) {
            new_cap = vm->frame_cap + needed_extra;
            break;
        }
    }
    VmFrame *tmp = (VmFrame*)realloc(vm->frames, new_cap * sizeof(VmFrame));
    if (!tmp) {
        vm_set_errorf(vm, "vm: failed to grow frame stack to %u entries", new_cap);
        return false;
    }
    memset(tmp + vm->frame_cap, 0, (new_cap - vm->frame_cap) * sizeof(VmFrame));
    vm->frames = tmp;
    vm->frame_cap = new_cap;
    return true;
}

static bool vm_stack_push(Vm *vm, VmValue value) {
    if (!vm_stack_reserve(vm, 1)) {
        return false;
    }
    vm_value_retain(&value);
    vm->stack[vm->sp++] = value;
    return true;
}

static bool vm_stack_pop(Vm *vm, VmValue *out) {
    if (vm->sp == 0) {
        vm_set_errorf(vm, "vm: stack underflow");
        return false;
    }
    vm->sp--;
    if (out) {
        *out = vm->stack[vm->sp];
    }
    vm->stack[vm->sp] = vm_value_null();
    return true;
}

static bool vm_fetch_func_name(const Vm *vm, const SbcFuncDesc *func, VmString *out) {
    if (!vm || !vm->img || !func || !out) {
        return false;
    }
    const void *ptr = NULL;
    uint32_t len = 0;
    SbcConstKind kind;
    if (!sbc_const_at(vm->img, func->name_idx, &kind, &ptr, &len)) {
        return false;
    }
    if (kind != SBC_CONST_STR) {
        return false;
    }
    out->data = (const char*)ptr;
    out->len = len;
    return true;
}

static void vm_trace_op(Vm *vm, VmFrame *frame, const uint8_t *op_start, SurgeOpcode opcode) {
    if (!vm || !vm->trace) {
        return;
    }
    uint32_t offs = (uint32_t)(op_start - frame->code_start);
    VmString name = {0};
    if (!vm_fetch_func_name(vm, frame->func, &name)) {
        name.data = "<anon>";
        name.len = (uint32_t)strlen(name.data);
    }
    fprintf(stderr, "[vm] %.*s+%04u %-12s sp=%u\n",
            (int)name.len, name.data,
            offs,
            surge_opcode_name(opcode),
            vm->sp);
}

static int vm_find_function_by_name(const SbcImage *img, const char *name) {
    if (!img || !name) {
        return -1;
    }
    size_t name_len = strlen(name);
    for (uint32_t i = 0; i < img->func_count; ++i) {
        const SbcFuncDesc *fn = &img->funcs[i];
        const void *ptr = NULL;
        uint32_t len = 0;
        SbcConstKind kind;
        if (!sbc_const_at(img, fn->name_idx, &kind, &ptr, &len)) {
            continue;
        }
        if (kind != SBC_CONST_STR) {
            continue;
        }
        if (len == name_len && memcmp(ptr, name, len) == 0) {
            return (int)i;
        }
    }
    return -1;
}

static bool vm_push_frame(Vm *vm, const SbcFuncDesc *func, uint32_t base) {
    if (!vm_frames_reserve(vm, 1)) {
        return false;
    }
    if (func->nlocals < func->arity) {
        vm_set_errorf(vm, "vm: function metadata invalid (locals < arity)");
        return false;
    }
    const uint8_t *code_start = vm->img->code_sec + func->code_off;
    VmFrame *frame = &vm->frames[vm->fp++];
    frame->func = func;
    frame->code_start = code_start;
    frame->ip = code_start;
    frame->code_end = code_start + func->code_len;
    frame->base = base;
    frame->locals_end = base + func->nlocals;
    return true;
}

static bool vm_prepare_frame_locals(Vm *vm, VmFrame *frame, uint16_t argc) {
    if (argc != frame->func->arity) {
        vm_set_errorf(vm, "vm: bad call arity: expected %u got %u",
                      (unsigned)frame->func->arity, (unsigned)argc);
        return false;
    }
    if (frame->locals_end > vm->stack_cap) {
        if (!vm_stack_reserve(vm, frame->locals_end - vm->sp)) {
            return false;
        }
    }
    if (vm->sp < frame->base + argc) {
        vm_set_errorf(vm, "vm: stack underflow entering frame");
        return false;
    }
    if (frame->locals_end > vm->sp) {
        for (uint32_t slot = vm->sp; slot < frame->locals_end; ++slot) {
            vm->stack[slot] = vm_value_null();
        }
        vm->sp = frame->locals_end;
    }
    return true;
}

static VmRunStatus vm_raise_trap(Vm *vm, VmFrame *frame, const uint8_t *op_start,
                                 VmRunResult *result, SurgeTrapCode code, const char *fmt, ...) {
    if (result) {
        result->status = VM_RUN_TRAP;
        result->trap_code = code;
        result->return_value = vm_value_null();
    }
    char loc[128];
    vm_format_location(vm, frame, op_start, loc, sizeof(loc));
    va_list ap;
    va_start(ap, fmt);
    int needed = vsnprintf(NULL, 0, fmt, ap);
    va_end(ap);
    if (needed < 0) {
        vm_set_errorf(vm, "%s: runtime trap: %s (%u)", loc, vm_trap_name(code), (unsigned)code);
        return VM_RUN_TRAP;
    }
    char *buf = (char*)malloc((size_t)needed + 1u);
    if (!buf) {
        vm_set_errorf(vm, "%s: runtime trap: %s (%u)", loc, vm_trap_name(code), (unsigned)code);
        return VM_RUN_TRAP;
    }
    va_start(ap, fmt);
    vsnprintf(buf, (size_t)needed + 1u, fmt, ap);
    va_end(ap);
    vm_set_errorf(vm, "%s: runtime trap: %s (%u): %s", loc, vm_trap_name(code), (unsigned)code, buf);
    free(buf);
    return VM_RUN_TRAP;
}

static VmRunStatus vm_trap_stack_overflow(Vm *vm, VmFrame *frame, const uint8_t *op_start,
                                          VmRunResult *result) {
    return vm_raise_trap(vm, frame, op_start, result, SURGE_TRAP_STACK_OVERFLOW,
                         "call stack overflow (limit=%u)", (unsigned)SURGE_VM_MAX_CALL_DEPTH);
}

static bool vm_check_ip(Vm *vm, VmFrame *frame) {
    if (frame->ip >= frame->code_end) {
        vm_set_errorf_loc(vm, frame, frame ? frame->ip : NULL,
                          "instruction pointer out of range");
        return false;
    }
    return true;
}

static VmRunStatus vm_run_frames(Vm *vm, VmRunResult *out_result) {
    if (!vm) {
        if (out_result) {
            memset(out_result, 0, sizeof(*out_result));
            out_result->status = VM_RUN_ERROR;
            out_result->return_value = vm_value_null();
        }
        return VM_RUN_ERROR;
    }

    VmRunResult result;
    memset(&result, 0, sizeof(result));
    result.status = VM_RUN_ERROR;
    result.return_value = vm_value_null();

    VmRunStatus status = VM_RUN_ERROR;

    while (vm->fp > 0) {
        VmFrame *frame = &vm->frames[vm->fp - 1];
        if (!vm_check_ip(vm, frame)) {
            status = VM_RUN_ERROR;
            break;
        }

        const uint8_t *op_start = frame->ip;
        uint8_t opcode_byte = *frame->ip++;
        SurgeOpcode opcode = (SurgeOpcode)opcode_byte;

        vm_trace_op(vm, frame, op_start, opcode);

        switch (opcode) {
            case SURGE_OP_PUSH_I64: {
                if (frame->ip + 8 > frame->code_end) {
                    vm_set_errorf_loc(vm, frame, op_start, "PUSH_I64 truncated");
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                int64_t value = read_i64_le(frame->ip);
                frame->ip += 8;
                if (!vm_stack_push(vm, vm_value_i64(value))) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                break;
            }
            case SURGE_OP_PUSH_F64: {
                if (frame->ip + 8 > frame->code_end) {
                    vm_set_errorf_loc(vm, frame, op_start, "PUSH_F64 truncated");
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                double value = read_f64_le(frame->ip);
                frame->ip += 8;
                if (!vm_stack_push(vm, vm_value_f64(value))) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                break;
            }
            case SURGE_OP_PUSH_BOOL: {
                if (frame->ip + 1 > frame->code_end) {
                    vm_set_errorf_loc(vm, frame, op_start, "PUSH_BOOL truncated");
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                uint8_t raw = *frame->ip++;
                if (!vm_stack_push(vm, vm_value_bool(raw != 0))) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                break;
            }
            case SURGE_OP_PUSH_NULL: {
                if (!vm_stack_push(vm, vm_value_null())) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                break;
            }
            case SURGE_OP_PUSH_STR: {
                if (frame->ip + 4 > frame->code_end) {
                    vm_set_errorf_loc(vm, frame, op_start, "PUSH_STR truncated");
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                uint32_t const_idx = read_u32_le(frame->ip);
                frame->ip += 4;
                if (const_idx >= vm->str_cache_len) {
                    vm_set_errorf_loc(vm, frame, op_start, "PUSH_STR invalid const index %u", const_idx);
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                RtString *obj = vm->str_cache[const_idx];
                if (!obj) {
                    SbcConstKind kind;
                    const void *ptr = NULL;
                    uint32_t len = 0;
                    if (!sbc_const_at(vm->img, const_idx, &kind, &ptr, &len) || kind != SBC_CONST_STR) {
                        vm_set_errorf_loc(vm, frame, op_start, "PUSH_STR invalid const index %u", const_idx);
                        status = VM_RUN_ERROR;
                        goto loop_end;
                    }
                    obj = rt_string_new(vm, (const uint8_t*)ptr, len);
                    if (!obj) {
                        vm_set_errorf_loc(vm, frame, op_start, "OOM interning string");
                        status = VM_RUN_ERROR;
                        goto loop_end;
                    }
                    vm->str_cache[const_idx] = obj;
                }
                VmValue val = vm_value_null();
                val.tag = VM_VT_STR;
                val.as.str.obj = obj;
                val.as.str.data = obj->data;
                val.as.str.len = obj->len;
                if (!vm_stack_push(vm, val)) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                break;
            }
            case SURGE_OP_LOAD: {
                if (frame->ip + 2 > frame->code_end) {
                    vm_set_errorf_loc(vm, frame, op_start, "LOAD truncated");
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                uint16_t slot = read_u16_le(frame->ip);
                frame->ip += 2;
                if (frame->base + slot >= frame->locals_end) {
                    vm_set_errorf_loc(vm, frame, op_start, "LOAD slot %u OOB", (unsigned)slot);
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                VmValue val = vm->stack[frame->base + slot];
                if (!vm_stack_push(vm, val)) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                break;
            }
            case SURGE_OP_STORE: {
                if (frame->ip + 2 > frame->code_end) {
                    vm_set_errorf_loc(vm, frame, op_start, "STORE truncated");
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                uint16_t slot = read_u16_le(frame->ip);
                frame->ip += 2;
                if (frame->base + slot >= frame->locals_end) {
                    vm_set_errorf_loc(vm, frame, op_start, "STORE slot %u OOB", (unsigned)slot);
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                VmValue val;
                if (!vm_stack_pop(vm, &val)) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                VmValue *dst = &vm->stack[frame->base + slot];
                vm_value_release(vm, dst);
                *dst = val;
                /* Retain after assignment so locals keep shared refs alive; the
                 * temporary popped value still owns one count until we drop it.
                 */
                vm_value_retain(dst);
                vm_value_release(vm, &val);
                break;
            }
            case SURGE_OP_GLOAD: {
                if (frame->ip + 2 > frame->code_end) {
                    vm_set_errorf_loc(vm, frame, op_start, "GLOAD truncated");
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                uint16_t slot = read_u16_le(frame->ip);
                frame->ip += 2;
                if (slot >= vm->global_count) {
                    status = vm_raise_trap(vm, frame, op_start, &result, SURGE_TRAP_OUT_OF_BOUNDS,
                                           "GLOAD slot %u OOB", (unsigned)slot);
                    goto loop_end;
                }
                VmValue val = vm->globals[slot];
                if (!vm_stack_push(vm, val)) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                break;
            }
            case SURGE_OP_GSTORE: {
                if (frame->ip + 2 > frame->code_end) {
                    vm_set_errorf_loc(vm, frame, op_start, "GSTORE truncated");
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                uint16_t slot = read_u16_le(frame->ip);
                frame->ip += 2;
                if (slot >= vm->global_count) {
                    status = vm_raise_trap(vm, frame, op_start, &result, SURGE_TRAP_OUT_OF_BOUNDS,
                                           "GSTORE slot %u OOB", (unsigned)slot);
                    goto loop_end;
                }
                VmValue val;
                if (!vm_stack_pop(vm, &val)) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                VmValue *dst = &vm->globals[slot];
                vm_value_release(vm, dst);
                *dst = val;
                vm_value_retain(dst);
                vm_value_release(vm, &val);
                break;
            }
            case SURGE_OP_POP: {
                VmValue tmp;
                if (!vm_stack_pop(vm, &tmp)) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                vm_value_release(vm, &tmp);
                break;
            }
            case SURGE_OP_NOP: {
                break;
            }
            case SURGE_OP_NEG_I64: {
                VmValue val;
                if (!vm_stack_pop(vm, &val)) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                if (val.tag != VM_VT_I64) {
                    vm_value_release(vm, &val);
                    status = vm_raise_trap(vm, frame, op_start, &result, SURGE_TRAP_TYPE_ERROR,
                                           "NEG_I64 expects i64");
                    goto loop_end;
                }
                VmValue out = vm_value_i64(-val.as.i64);
                vm_value_release(vm, &val);
                if (!vm_stack_push(vm, out)) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                break;
            }
            case SURGE_OP_NEG_F64: {
                VmValue val;
                if (!vm_stack_pop(vm, &val)) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                if (val.tag != VM_VT_F64) {
                    vm_value_release(vm, &val);
                    status = vm_raise_trap(vm, frame, op_start, &result, SURGE_TRAP_TYPE_ERROR,
                                           "NEG_F64 expects f64");
                    goto loop_end;
                }
                VmValue out = vm_value_f64(-val.as.f64);
                vm_value_release(vm, &val);
                if (!vm_stack_push(vm, out)) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                break;
            }
            case SURGE_OP_NOT_BOOL: {
                VmValue val;
                if (!vm_stack_pop(vm, &val)) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                if (val.tag != VM_VT_BOOL) {
                    vm_value_release(vm, &val);
                    status = vm_raise_trap(vm, frame, op_start, &result, SURGE_TRAP_TYPE_ERROR,
                                           "NOT_BOOL expects bool");
                    goto loop_end;
                }
                VmValue out = vm_value_bool(val.as.b == 0);
                vm_value_release(vm, &val);
                if (!vm_stack_push(vm, out)) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                break;
            }
            case SURGE_OP_I64_TO_F64: {
                VmValue val;
                if (!vm_stack_pop(vm, &val)) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                if (val.tag != VM_VT_I64) {
                    vm_value_release(vm, &val);
                    status = vm_raise_trap(vm, frame, op_start, &result, SURGE_TRAP_TYPE_ERROR,
                                           "I64_TO_F64 expects i64");
                    goto loop_end;
                }
                VmValue out = vm_value_f64((double)val.as.i64);
                vm_value_release(vm, &val);
                if (!vm_stack_push(vm, out)) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                break;
            }
            case SURGE_OP_F64_TO_I64: {
                VmValue val;
                if (!vm_stack_pop(vm, &val)) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                if (val.tag != VM_VT_F64) {
                    vm_value_release(vm, &val);
                    status = vm_raise_trap(vm, frame, op_start, &result, SURGE_TRAP_TYPE_ERROR,
                                           "F64_TO_I64 expects f64");
                    goto loop_end;
                }
                double d = val.as.f64;
                if (isnan(d)) {
                    vm_value_release(vm, &val);
                    status = vm_raise_trap(vm, frame, op_start, &result, SURGE_TRAP_TYPE_ERROR,
                                           "cannot convert NaN to i64");
                    goto loop_end;
                }
                if (d > (double)INT64_MAX || d < (double)INT64_MIN) {
                    vm_value_release(vm, &val);
                    status = vm_raise_trap(vm, frame, op_start, &result, SURGE_TRAP_TYPE_ERROR,
                                           "f64 to i64 overflow");
                    goto loop_end;
                }
                // trunc toward 0 is the C cast semantics, and now it's defined (range-checked)
                VmValue out = vm_value_i64((int64_t)d);
                vm_value_release(vm, &val);
                if (!vm_stack_push(vm, out)) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                break;
            }
            case SURGE_OP_ADD:
            case SURGE_OP_SUB:
            case SURGE_OP_MUL:
            case SURGE_OP_DIV:
            case SURGE_OP_REM:
            case SURGE_OP_CMP_EQ:
            case SURGE_OP_CMP_NE:
            case SURGE_OP_CMP_LT:
            case SURGE_OP_CMP_LE:
            case SURGE_OP_CMP_GT:
            case SURGE_OP_CMP_GE: {
                VmValue rhs;
                if (!vm_stack_pop(vm, &rhs)) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                VmValue lhs;
                if (!vm_stack_pop(vm, &lhs)) {
                    vm_value_release(vm, &rhs);
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                if (lhs.tag != VM_VT_I64 || rhs.tag != VM_VT_I64) {
                    vm_value_release(vm, &rhs);
                    vm_value_release(vm, &lhs);
                    status = vm_raise_trap(vm, frame, op_start, &result, SURGE_TRAP_TYPE_ERROR,
                                           "arithmetic expects i64 operands");
                    goto loop_end;
                }
                int64_t a = lhs.as.i64;
                int64_t b = rhs.as.i64;
                VmValue out = vm_value_null();
                switch (opcode) {
                    case SURGE_OP_ADD: out = vm_value_i64(a + b); break;
                    case SURGE_OP_SUB: out = vm_value_i64(a - b); break;
                    case SURGE_OP_MUL: out = vm_value_i64(a * b); break;
                    case SURGE_OP_DIV:
                        if (b == 0) {
                            vm_value_release(vm, &rhs);
                            vm_value_release(vm, &lhs);
                            status = vm_raise_trap(vm, frame, op_start, &result, SURGE_TRAP_DIV_BY_ZERO,
                                                   "integer division by zero");
                            goto loop_end;
                        }
                        out = vm_value_i64(a / b);
                        break;
                    case SURGE_OP_REM:
                        if (b == 0) {
                            vm_value_release(vm, &rhs);
                            vm_value_release(vm, &lhs);
                            status = vm_raise_trap(vm, frame, op_start, &result, SURGE_TRAP_DIV_BY_ZERO,
                                                   "integer remainder by zero");
                            goto loop_end;
                        }
                        out = vm_value_i64(a % b);
                        break;
                    case SURGE_OP_CMP_EQ: out = vm_value_bool(a == b); break;
                    case SURGE_OP_CMP_NE: out = vm_value_bool(a != b); break;
                    case SURGE_OP_CMP_LT: out = vm_value_bool(a < b); break;
                    case SURGE_OP_CMP_LE: out = vm_value_bool(a <= b); break;
                    case SURGE_OP_CMP_GT: out = vm_value_bool(a > b); break;
                    case SURGE_OP_CMP_GE: out = vm_value_bool(a >= b); break;
                    default:
                        break;
                }
                vm_value_release(vm, &rhs);
                vm_value_release(vm, &lhs);
                if (!vm_stack_push(vm, out)) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                break;
            }
            case SURGE_OP_ADD_F64:
            case SURGE_OP_SUB_F64:
            case SURGE_OP_MUL_F64:
            case SURGE_OP_DIV_F64:
            case SURGE_OP_REM_F64:
            case SURGE_OP_CMP_EQ_F64:
            case SURGE_OP_CMP_NE_F64:
            case SURGE_OP_CMP_LT_F64:
            case SURGE_OP_CMP_LE_F64:
            case SURGE_OP_CMP_GT_F64:
            case SURGE_OP_CMP_GE_F64: {
                VmValue rhs;
                if (!vm_stack_pop(vm, &rhs)) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                VmValue lhs;
                if (!vm_stack_pop(vm, &lhs)) {
                    vm_value_release(vm, &rhs);
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                if (lhs.tag != VM_VT_F64 || rhs.tag != VM_VT_F64) {
                    vm_value_release(vm, &rhs);
                    vm_value_release(vm, &lhs);
                    status = vm_raise_trap(vm, frame, op_start, &result, SURGE_TRAP_TYPE_ERROR,
                                           "floating-point op expects f64 operands");
                    goto loop_end;
                }
                double a = lhs.as.f64;
                double b = rhs.as.f64;
                VmValue out = vm_value_null();
                switch (opcode) {
                    case SURGE_OP_ADD_F64: out = vm_value_f64(a + b); break;
                    case SURGE_OP_SUB_F64: out = vm_value_f64(a - b); break;
                    case SURGE_OP_MUL_F64: out = vm_value_f64(a * b); break;
                    case SURGE_OP_DIV_F64:
                        out = vm_value_f64(a / b);
                        break;
                    case SURGE_OP_REM_F64:
                        out = vm_value_f64(fmod(a, b));
                        break;
                    case SURGE_OP_CMP_EQ_F64: out = vm_value_bool(a == b); break;
                    case SURGE_OP_CMP_NE_F64: out = vm_value_bool(a != b); break;
                    case SURGE_OP_CMP_LT_F64: out = vm_value_bool(a < b); break;
                    case SURGE_OP_CMP_LE_F64: out = vm_value_bool(a <= b); break;
                    case SURGE_OP_CMP_GT_F64: out = vm_value_bool(a > b); break;
                    case SURGE_OP_CMP_GE_F64: out = vm_value_bool(a >= b); break;
                    default: break;
                }
                vm_value_release(vm, &rhs);
                vm_value_release(vm, &lhs);
                if (!vm_stack_push(vm, out)) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                break;
            }
            case SURGE_OP_AND_I64:
            case SURGE_OP_OR_I64:
            case SURGE_OP_XOR_I64:
            case SURGE_OP_SHL_I64:
            case SURGE_OP_SHR_I64: {
                VmValue rhs;
                if (!vm_stack_pop(vm, &rhs)) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                VmValue lhs;
                if (!vm_stack_pop(vm, &lhs)) {
                    vm_value_release(vm, &rhs);
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                if (lhs.tag != VM_VT_I64 || rhs.tag != VM_VT_I64) {
                    vm_value_release(vm, &rhs);
                    vm_value_release(vm, &lhs);
                    status = vm_raise_trap(vm, frame, op_start, &result, SURGE_TRAP_TYPE_ERROR,
                                           "bitwise op expects i64 operands");
                    goto loop_end;
                }
                uint64_t a = (uint64_t)lhs.as.i64;
                uint64_t b = (uint64_t)rhs.as.i64;
                VmValue out = vm_value_null();
                switch (opcode) {
                    case SURGE_OP_AND_I64: out = vm_value_i64((int64_t)(a & b)); break;
                    case SURGE_OP_OR_I64:  out = vm_value_i64((int64_t)(a | b)); break;
                    case SURGE_OP_XOR_I64: out = vm_value_i64((int64_t)(a ^ b)); break;
                    case SURGE_OP_SHL_I64:
                        if (b >= 64u) {
                            vm_value_release(vm, &rhs);
                            vm_value_release(vm, &lhs);
                            status = vm_raise_trap(vm, frame, op_start, &result, SURGE_TRAP_TYPE_ERROR,
                                                   "shift amount out of range");
                            goto loop_end;
                        }
                        out = vm_value_i64((int64_t)(a << b));
                        break;
                    case SURGE_OP_SHR_I64:
                        if (b >= 64u) {
                            vm_value_release(vm, &rhs);
                            vm_value_release(vm, &lhs);
                            status = vm_raise_trap(vm, frame, op_start, &result, SURGE_TRAP_TYPE_ERROR,
                                                   "shift amount out of range");
                            goto loop_end;
                        }
                        out = vm_value_i64((int64_t)(a >> b));
                        break;
                    default: break;
                }
                vm_value_release(vm, &rhs);
                vm_value_release(vm, &lhs);
                if (!vm_stack_push(vm, out)) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                break;
            }
            case SURGE_OP_JMP: {
                if (frame->ip + 4 > frame->code_end) {
                    vm_set_errorf_loc(vm, frame, op_start, "JMP truncated");
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                int32_t offset = read_i32_le(frame->ip);
                frame->ip = op_start + 1 + 4 + offset;
                if (frame->ip < frame->code_start || frame->ip > frame->code_end) {
                    vm_set_errorf_loc(vm, frame, op_start, "JMP out of range");
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                if (offset == 0) {
                    vm_trace_warn(vm, frame, op_start, "suspicious self-jump");
                }
                break;
            }
            case SURGE_OP_JMP_IF_TRUE:
            case SURGE_OP_JMP_IF_FALSE: {
                if (frame->ip + 4 > frame->code_end) {
                    vm_set_errorf_loc(vm, frame, op_start, "conditional jump truncated");
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                int32_t offset = read_i32_le(frame->ip);
                frame->ip += 4;
                VmValue cond;
                if (!vm_stack_pop(vm, &cond)) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                if (cond.tag != VM_VT_BOOL) {
                    vm_value_release(vm, &cond);
                    status = vm_raise_trap(vm, frame, op_start, &result, SURGE_TRAP_TYPE_ERROR,
                                           "conditional jump expects bool");
                    goto loop_end;
                }
                bool truth = cond.as.b != 0;
                vm_value_release(vm, &cond);
                if ((opcode == SURGE_OP_JMP_IF_TRUE && truth) ||
                    (opcode == SURGE_OP_JMP_IF_FALSE && !truth)) {
                    frame->ip = op_start + 1 + 4 + offset;
                    if (frame->ip < frame->code_start || frame->ip > frame->code_end) {
                        vm_set_errorf_loc(vm, frame, op_start, "jump target out of range");
                        status = VM_RUN_ERROR;
                        goto loop_end;
                    }
                }
                break;
            }
            case SURGE_OP_CALL: {
                if (frame->ip + 3 > frame->code_end) {
                    vm_set_errorf_loc(vm, frame, op_start, "CALL truncated");
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                uint16_t func_index = read_u16_le(frame->ip);
                frame->ip += 2;
                uint8_t argc = *frame->ip++;
                if (func_index >= vm->img->func_count) {
                    status = vm_raise_trap(vm, frame, op_start, &result, SURGE_TRAP_BAD_CALL,
                                           "call to invalid function index %u", (unsigned)func_index);
                    goto loop_end;
                }
                if (vm->sp < argc) {
                    status = vm_raise_trap(vm, frame, op_start, &result, SURGE_TRAP_BAD_CALL,
                                           "stack underflow for call (argc=%u)", (unsigned)argc);
                    goto loop_end;
                }
                VmFrame *caller = frame;
                caller->ip = frame->ip;
                const SbcFuncDesc *callee = &vm->img->funcs[func_index];
                uint32_t base = vm->sp - argc;
                if (vm->fp >= SURGE_VM_MAX_CALL_DEPTH) {
                    status = vm_trap_stack_overflow(vm, frame, op_start, &result);
                    goto loop_end;
                }
                if (!vm_push_frame(vm, callee, base)) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                frame = &vm->frames[vm->fp - 1];
                if (!vm_prepare_frame_locals(vm, frame, argc)) {
                    vm->fp--;
                    frame = caller;
                    status = vm_raise_trap(vm, caller, op_start, &result, SURGE_TRAP_BAD_CALL,
                                           "call to %u expected %u args",
                                           (unsigned)func_index,
                                           (unsigned)callee->arity);
                    goto loop_end;
                }
                continue;
            }
            case SURGE_OP_RET: {
                VmValue ret = vm_value_null();
                bool have_ret = false;
                if (vm->sp > frame->base) {
                    if (!vm_stack_pop(vm, &ret)) {
                        status = VM_RUN_ERROR;
                        goto loop_end;
                    }
                    have_ret = true;
                }
                for (uint32_t i = frame->base; i < vm->sp; ++i) {
                    vm_value_release(vm, &vm->stack[i]);
                }
                vm->sp = frame->base;

                vm->fp--;
                if (vm->fp == 0) {
                    result.status = VM_RUN_OK;
                    result.return_value = have_ret ? ret : vm_value_null();
                    status = VM_RUN_OK;
                    goto loop_end;
                }

                if (have_ret) {
                    if (!vm_stack_push(vm, ret)) {
                        vm_value_release(vm, &ret);
                        status = VM_RUN_ERROR;
                        goto loop_end;
                    }
                    vm_value_release(vm, &ret);
                }
                frame = &vm->frames[vm->fp - 1];
                break;
            }
            case SURGE_OP_ARR_NEW: {
                if (frame->ip + 4 > frame->code_end) {
                    vm_set_errorf_loc(vm, frame, op_start, "ARR_NEW truncated");
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                uint32_t count = read_u32_le(frame->ip);
                frame->ip += 4;
                if (count > vm->sp) {
                    status = vm_raise_trap(vm, frame, op_start, &result, SURGE_TRAP_TYPE_ERROR,
                                           "ARR_NEW requires %u stack values", (unsigned)count);
                    goto loop_end;
                }
                struct VmArray *arr = (struct VmArray*)calloc(1, sizeof(struct VmArray));
                if (!arr) {
                    vm_set_errorf_loc(vm, frame, op_start, "OOM allocating array header");
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                arr->len = count;
                arr->refcnt = 1;
                if (count) {
                    arr->data = (VmValue*)calloc(count, sizeof(VmValue));
                    if (!arr->data) {
                        free(arr);
                        vm_set_errorf_loc(vm, frame, op_start, "OOM allocating array payload");
                        status = VM_RUN_ERROR;
                        goto loop_end;
                    }
                }
                uint32_t start = vm->sp - count;
                for (uint32_t i = 0; i < count; ++i) {
                    VmValue elem = vm->stack[start + i];
                    arr->data[i] = elem;
                    vm_value_retain(&arr->data[i]);
                    vm_value_release(vm, &vm->stack[start + i]);
                }
                vm->sp -= count;
                VmValue arr_val = vm_value_null();
                arr_val.tag = VM_VT_ARR;
                arr_val.as.arr = arr;
                if (!vm_stack_push(vm, arr_val)) {
                    vm_value_release(vm, &arr_val);
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                vm_value_release(vm, &arr_val);
                break;
            }
            case SURGE_OP_ARR_GET: {
                VmValue idx_val;
                if (!vm_stack_pop(vm, &idx_val)) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                VmValue arr_val;
                if (!vm_stack_pop(vm, &arr_val)) {
                    vm_value_release(vm, &idx_val);
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                if (arr_val.tag != VM_VT_ARR || !arr_val.as.arr) {
                    vm_value_release(vm, &idx_val);
                    vm_value_release(vm, &arr_val);
                    status = vm_raise_trap(vm, frame, op_start, &result, SURGE_TRAP_TYPE_ERROR,
                                           "ARR_GET expects array");
                    goto loop_end;
                }
                if (idx_val.tag != VM_VT_I64 || idx_val.as.i64 < 0 ||
                    (uint64_t)idx_val.as.i64 >= arr_val.as.arr->len) {
                    vm_value_release(vm, &idx_val);
                    vm_value_release(vm, &arr_val);
                    status = vm_raise_trap(vm, frame, op_start, &result, SURGE_TRAP_OUT_OF_BOUNDS,
                                           "ARR_GET index out of bounds");
                    goto loop_end;
                }
                VmValue elem = arr_val.as.arr->data[(uint32_t)idx_val.as.i64];
                vm_value_release(vm, &idx_val);
                vm_value_release(vm, &arr_val);
                if (!vm_stack_push(vm, elem)) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                break;
            }
            case SURGE_OP_ARR_SET: {
                VmValue value;
                if (!vm_stack_pop(vm, &value)) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                VmValue idx_val;
                if (!vm_stack_pop(vm, &idx_val)) {
                    vm_value_release(vm, &value);
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                VmValue arr_val;
                if (!vm_stack_pop(vm, &arr_val)) {
                    vm_value_release(vm, &value);
                    vm_value_release(vm, &idx_val);
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                if (arr_val.tag != VM_VT_ARR || !arr_val.as.arr) {
                    vm_value_release(vm, &value);
                    vm_value_release(vm, &idx_val);
                    vm_value_release(vm, &arr_val);
                    status = vm_raise_trap(vm, frame, op_start, &result, SURGE_TRAP_TYPE_ERROR,
                                           "ARR_SET expects array");
                    goto loop_end;
                }
                if (idx_val.tag != VM_VT_I64 || idx_val.as.i64 < 0 ||
                    (uint64_t)idx_val.as.i64 >= arr_val.as.arr->len) {
                    vm_value_release(vm, &value);
                    vm_value_release(vm, &idx_val);
                    vm_value_release(vm, &arr_val);
                    status = vm_raise_trap(vm, frame, op_start, &result, SURGE_TRAP_OUT_OF_BOUNDS,
                                           "ARR_SET index out of bounds");
                    goto loop_end;
                }
                uint32_t slot = (uint32_t)idx_val.as.i64;
                VmValue *dst = &arr_val.as.arr->data[slot];
                vm_value_release(vm, dst);
                *dst = value;
                vm_value_retain(dst);
                vm_value_release(vm, &value);
                vm_value_release(vm, &idx_val);
                vm_value_release(vm, &arr_val);
                break;
            }
            case SURGE_OP_ARR_LEN: {
                VmValue arr_val;
                if (!vm_stack_pop(vm, &arr_val)) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                if (arr_val.tag != VM_VT_ARR || !arr_val.as.arr) {
                    vm_value_release(vm, &arr_val);
                    status = vm_raise_trap(vm, frame, op_start, &result, SURGE_TRAP_TYPE_ERROR,
                                           "ARR_LEN expects array");
                    goto loop_end;
                }
                uint32_t len = arr_val.as.arr->len;
                VmValue out = vm_value_i64((int64_t)len);
                vm_value_release(vm, &arr_val);
                if (!vm_stack_push(vm, out)) {
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                break;
            }
            case SURGE_OP_TRAP: {
                if (frame->ip + 2 > frame->code_end) {
                    vm_set_errorf_loc(vm, frame, op_start, "TRAP truncated");
                    status = VM_RUN_ERROR;
                    goto loop_end;
                }
                uint16_t code = read_u16_le(frame->ip);
                frame->ip += 2;
                status = vm_raise_trap(vm, frame, op_start, &result, (SurgeTrapCode)code, "TRAP instruction");
                goto loop_end;
            }
            case SURGE_OP_HALT: {
                result.status = VM_RUN_OK;
                result.return_value = vm_value_null();
                status = VM_RUN_OK;
                goto loop_end;
            }
            default: {
                vm_set_errorf_loc(vm, frame, op_start, "unknown opcode %u", (unsigned)opcode);
                status = VM_RUN_ERROR;
                goto loop_end;
            }
        }
        continue;

loop_end:
        break;
    }

    vm_clear_stack(vm);
    vm->fp = 0;

    if (out_result) {
        *out_result = result;
    } else {
        vm_value_release(vm, &result.return_value);
    }
    return status;
}

static VmRunStatus vm_call_function(Vm *vm, int func_idx, uint16_t argc, bool keep_return, VmRunResult *out_result) {
    if (func_idx < 0) {
        if (out_result) {
            memset(out_result, 0, sizeof(*out_result));
            out_result->status = VM_RUN_OK;
            out_result->return_value = vm_value_null();
        }
        return VM_RUN_OK;
    }

    const SbcFuncDesc *func = &vm->img->funcs[func_idx];
    if (argc > vm->sp) {
        vm_set_errorf(vm, "vm: stack underflow entering call");
        return VM_RUN_ERROR;
    }

    if (vm->fp >= SURGE_VM_MAX_CALL_DEPTH) {
        VmRunResult trap;
        memset(&trap, 0, sizeof(trap));
        VmFrame *top = (vm->fp > 0) ? &vm->frames[vm->fp - 1] : NULL;
        const uint8_t *ip = top ? top->ip : NULL;
        VmRunStatus status = vm_trap_stack_overflow(vm, top, ip, &trap);
        if (out_result) {
            *out_result = trap;
        } else {
            vm_value_release(vm, &trap.return_value);
        }
        return status;
    }

    uint32_t base = vm->sp - argc;
    if (!vm_push_frame(vm, func, base)) {
        return VM_RUN_ERROR;
    }
    VmFrame *frame = &vm->frames[vm->fp - 1];
    if (!vm_prepare_frame_locals(vm, frame, argc)) {
        vm->fp--;
        vm_clear_stack(vm);
        return VM_RUN_ERROR;
    }

    VmRunResult local;
    VmRunStatus status = vm_run_frames(vm, &local);
    if (status == VM_RUN_OK && !keep_return) {
        vm_value_release(vm, &local.return_value);
        local.return_value = vm_value_null();
    }

    if (out_result) {
        *out_result = local;
    } else {
        vm_value_release(vm, &local.return_value);
    }

    return status;
}

VmRunStatus vm_run_main(Vm *vm, const SbcImage *img, VmRunResult *out_result) {
    if (!vm || !img) {
        if (out_result) {
            memset(out_result, 0, sizeof(*out_result));
            out_result->status = VM_RUN_ERROR;
            out_result->return_value = vm_value_null();
        }
        return VM_RUN_ERROR;
    }

    vm_reset(vm);

    if (!vm_prepare_string_cache(vm, img->const_count)) {
        if (out_result) {
            memset(out_result, 0, sizeof(*out_result));
            out_result->status = VM_RUN_ERROR;
            out_result->return_value = vm_value_null();
        }
        return VM_RUN_ERROR;
    }

    if (!vm_prepare_globals(vm, img->global_count)) {
        if (out_result) {
            memset(out_result, 0, sizeof(*out_result));
            out_result->status = VM_RUN_ERROR;
            out_result->return_value = vm_value_null();
        }
        return VM_RUN_ERROR;
    }

    vm->img = img;
    vm->trace = vm->cfg.trace;

    VmRunResult tmp = {0};
    VmRunStatus status;

    int global_init_idx = vm_find_function_by_name(img, kGlobalInitName);
    status = vm_call_function(vm, global_init_idx, 0, false, &tmp);
    if (status != VM_RUN_OK) {
        if (out_result) {
            *out_result = tmp;
        }
        return status;
    }

    int init_idx = vm_find_function_by_name(img, "__init__");
    if (init_idx < 0) {
        init_idx = vm_find_function_by_name(img, "__init");
    }
    status = vm_call_function(vm, init_idx, 0, false, &tmp);
    if (status != VM_RUN_OK) {
        if (out_result) {
            *out_result = tmp;
        }
        return status;
    }

    int main_idx = vm_find_function_by_name(img, "main");
    if (main_idx < 0) {
        vm_set_errorf(vm, "vm: entry point main() not found");
        VmRunResult err = {
            .status = VM_RUN_ERROR,
            .return_value = vm_value_null(),
            .trap_code = 0
        };
        if (out_result) {
            *out_result = err;
        }
        return VM_RUN_ERROR;
    }

    status = vm_call_function(vm, main_idx, 0, true, &tmp);
    if (out_result) {
        *out_result = tmp;
    } else {
        vm_value_release(vm, &tmp.return_value);
    }
    return status;
}
