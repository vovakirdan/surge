#ifndef SURGE_OPCODES_H
#define SURGE_OPCODES_H

#include <stddef.h>
#include <stdint.h>

// Maximum operands per opcode (ISA v0)
#define SURGE_OPCODE_MAX_OPERANDS 2

typedef enum SurgeOpcode {
    SURGE_OP_INVALID = -1,
    SURGE_OP_PUSH_I64 = 0,
    SURGE_OP_PUSH_F64,
    SURGE_OP_PUSH_BOOL,
    SURGE_OP_PUSH_STR,
    SURGE_OP_PUSH_NULL,
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
    SURGE_OP_JMP,
    SURGE_OP_JMP_IF_TRUE,
    SURGE_OP_JMP_IF_FALSE,
    SURGE_OP_CALL,
    SURGE_OP_RET,
    SURGE_OP_ARR_NEW,
    SURGE_OP_ARR_LEN,
    SURGE_OP_ARR_GET,
    SURGE_OP_ARR_SET,
    SURGE_OP_TRAP,
    SURGE_OP_HALT,
    SURGE_OP_COUNT
} SurgeOpcode;

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

#endif // SURGE_OPCODES_H
