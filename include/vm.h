#ifndef SURGE_VM_H
#define SURGE_VM_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "opcodes.h"
#include "sbc.h"

typedef enum VmValueTag {
    VM_VT_NULL = 0,
    VM_VT_BOOL,
    VM_VT_I64,
    VM_VT_F64,
    VM_VT_STR,
    VM_VT_ARR
} VmValueTag;

struct RtString;

typedef struct VmString {
    struct RtString *obj;
    const char *data;
    uint32_t len;
} VmString;

struct VmArray;
struct VmFrame;
struct VmArenaChunk;

typedef struct VmArena {
    struct VmArenaChunk *chunks;
    size_t chunk_size;
} VmArena;

typedef struct VmValue {
    VmValueTag tag;
    union {
        int64_t i64;
        double f64;
        uint8_t b;
        VmString str;
        struct VmArray *arr;
    } as;
} VmValue;

typedef struct VmConfig {
    uint32_t stack_capacity;
    uint32_t frame_capacity;
    bool trace;
} VmConfig;

void vm_config_defaults(VmConfig *cfg);

typedef struct Vm {
    VmConfig cfg;
    const SbcImage *img;

    VmValue *stack;
    uint32_t stack_cap;
    uint32_t sp;

    struct VmFrame *frames;
    uint32_t frame_cap;
    uint32_t fp;

    VmValue *globals;
    uint32_t global_count;

    struct RtString **str_cache;
    uint32_t str_cache_len;

    VmArena arena;

    char *last_error;
    bool trace;
} Vm;

typedef enum VmRunStatus {
    VM_RUN_OK = 0,
    VM_RUN_TRAP,
    VM_RUN_ERROR
} VmRunStatus;

typedef struct VmRunResult {
    VmRunStatus status;
    VmValue return_value;
    SurgeTrapCode trap_code;
} VmRunResult;

bool vm_init(Vm *vm, const VmConfig *cfg);
void vm_reset(Vm *vm);
void vm_destroy(Vm *vm);

VmRunStatus vm_run_main(Vm *vm, const SbcImage *img, VmRunResult *out_result);
const char *vm_last_error(const Vm *vm);
void vm_value_release(Vm *vm, VmValue *value);

#endif /* SURGE_VM_H */
