#ifndef SURGE_OPCODES_H
#define SURGE_OPCODES_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

// Maximum operands per opcode (ISA v0)
#define SURGE_OPCODE_MAX_OPERANDS 2

// Encoding note:
// - All multi-byte operands in .sbc are written little-endian.
// - Instruction layout: [1 byte opcode][operands packed without padding].

typedef enum SurgeOpcode {
    SURGE_OP_INVALID = -1,
    SURGE_OP_PUSH_I64 = 0,
    SURGE_OP_PUSH_F64,
    SURGE_OP_PUSH_BOOL,
    SURGE_OP_PUSH_STR,
    SURGE_OP_PUSH_NULL, // null sentinel for reference-like values
    SURGE_OP_LOAD,
    SURGE_OP_STORE,
    SURGE_OP_GLOAD,
    SURGE_OP_GSTORE,
    SURGE_OP_ADD,
    SURGE_OP_SUB,
    SURGE_OP_MUL,
    SURGE_OP_DIV,
    SURGE_OP_REM,
    SURGE_OP_CMP_EQ,
    SURGE_OP_CMP_NE,
    SURGE_OP_CMP_LT,
    SURGE_OP_CMP_LE,
    SURGE_OP_CMP_GT,
    SURGE_OP_CMP_GE,
    SURGE_OP_NEG_I64,
    SURGE_OP_NEG_F64,
    SURGE_OP_NOT_BOOL,
    SURGE_OP_I64_TO_F64,
    SURGE_OP_F64_TO_I64,
    SURGE_OP_ADD_F64,
    SURGE_OP_SUB_F64,
    SURGE_OP_MUL_F64,
    SURGE_OP_DIV_F64,
    SURGE_OP_REM_F64,
    SURGE_OP_CMP_EQ_F64,
    SURGE_OP_CMP_NE_F64,
    SURGE_OP_CMP_LT_F64,
    SURGE_OP_CMP_LE_F64,
    SURGE_OP_CMP_GT_F64,
    SURGE_OP_CMP_GE_F64,
    SURGE_OP_CMP_EQ_STR,
    SURGE_OP_AND_I64,
    SURGE_OP_OR_I64,
    SURGE_OP_XOR_I64,
    SURGE_OP_SHL_I64,
    SURGE_OP_SHR_I64,
    SURGE_OP_JMP,
    SURGE_OP_JMP_IF_TRUE,
    SURGE_OP_JMP_IF_FALSE,
    SURGE_OP_CALL,
    SURGE_OP_RET,
    SURGE_OP_NOP,
    SURGE_OP_POP,
    SURGE_OP_ARR_NEW,
    SURGE_OP_ARR_LEN,
    SURGE_OP_ARR_GET,
    // ARR_GET: stack [..., arr, idx] -> [..., value]
    SURGE_OP_ARR_SET,
    // ARR_SET: stack [..., arr, idx, value] -> [...]
    SURGE_OP_TRAP,
    SURGE_OP_HALT,
    SURGE_OP_COUNT
} SurgeOpcode;

typedef enum SurgeTrapCode {
    SURGE_TRAP_UNREACHABLE = 1,
    SURGE_TRAP_DIV_BY_ZERO = 2,
    SURGE_TRAP_OUT_OF_BOUNDS = 3,
    SURGE_TRAP_BAD_CALL = 4,
    SURGE_TRAP_TYPE_ERROR = 5,
    SURGE_TRAP_STACK_OVERFLOW = 6
} SurgeTrapCode;

typedef enum SurgeOperandKind {
    SURGE_OPERAND_NONE = 0,
    SURGE_OPERAND_I64,
    SURGE_OPERAND_F64,
    SURGE_OPERAND_BOOL,       // encoded as u8
    SURGE_OPERAND_CONST_IDX,  // u32 index into const pool
    SURGE_OPERAND_LOCAL_SLOT, // u16 local slot id
    SURGE_OPERAND_GLOBAL_SLOT,// u16 global slot id
    SURGE_OPERAND_JUMP_OFFSET,// s32 relative offset
    SURGE_OPERAND_FUNC_INDEX, // u16 function table index
    SURGE_OPERAND_ARG_COUNT,  // u8 argument count
    SURGE_OPERAND_ARRAY_COUNT,// u32 element count (ARR_NEW)
    SURGE_OPERAND_TRAP_CODE   // u16 trap identifier
} SurgeOperandKind;

typedef struct SurgeOpcodeInfo {
    const char *mnemonic;
    uint8_t operand_count;
    SurgeOperandKind operands[SURGE_OPCODE_MAX_OPERANDS];
} SurgeOpcodeInfo;

const SurgeOpcodeInfo *surge_opcode_info(SurgeOpcode opcode);
const char *surge_opcode_name(SurgeOpcode opcode);
const char *surge_operand_kind_name(SurgeOperandKind kind);
size_t surge_operand_kind_size(SurgeOperandKind kind);
bool surge_operand_kind_is_signed(SurgeOperandKind kind);
bool surge_opcode_from_name(const char *mnemonic, SurgeOpcode *out_opcode);
void surge_opcode_table_selfcheck(void);

#endif // SURGE_OPCODES_H
