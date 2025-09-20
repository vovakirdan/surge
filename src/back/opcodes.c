#include "opcodes.h"

#include <assert.h>
#include <string.h>

#define STATIC_ASSERT(COND, MSG) typedef char static_assertion_##MSG[(COND) ? 1 : -1]

static const SurgeOpcodeInfo g_opcode_info[SURGE_OP_COUNT] = { // todo: generate by macro
    [SURGE_OP_PUSH_I64] = {
        .mnemonic = "PUSH_I64",
        .operand_count = 1,
        .operands = { SURGE_OPERAND_I64, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_PUSH_F64] = {
        .mnemonic = "PUSH_F64",
        .operand_count = 1,
        .operands = { SURGE_OPERAND_F64, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_PUSH_BOOL] = {
        .mnemonic = "PUSH_BOOL",
        .operand_count = 1,
        .operands = { SURGE_OPERAND_BOOL, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_PUSH_STR] = {
        .mnemonic = "PUSH_STR",
        .operand_count = 1,
        .operands = { SURGE_OPERAND_CONST_IDX, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_PUSH_NULL] = {
        .mnemonic = "PUSH_NULL",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_LOAD] = {
        .mnemonic = "LOAD",
        .operand_count = 1,
        .operands = { SURGE_OPERAND_LOCAL_SLOT, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_STORE] = {
        .mnemonic = "STORE",
        .operand_count = 1,
        .operands = { SURGE_OPERAND_LOCAL_SLOT, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_GLOAD] = {
        .mnemonic = "GLOAD",
        .operand_count = 1,
        .operands = { SURGE_OPERAND_GLOBAL_SLOT, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_GSTORE] = {
        .mnemonic = "GSTORE",
        .operand_count = 1,
        .operands = { SURGE_OPERAND_GLOBAL_SLOT, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_ADD] = {
        .mnemonic = "ADD",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_SUB] = {
        .mnemonic = "SUB",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_MUL] = {
        .mnemonic = "MUL",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_DIV] = {
        .mnemonic = "DIV",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_REM] = {
        .mnemonic = "REM",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_CMP_EQ] = {
        .mnemonic = "CMP_EQ",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_CMP_NE] = {
        .mnemonic = "CMP_NE",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_CMP_LT] = {
        .mnemonic = "CMP_LT",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_CMP_LE] = {
        .mnemonic = "CMP_LE",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_CMP_GT] = {
        .mnemonic = "CMP_GT",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_CMP_GE] = {
        .mnemonic = "CMP_GE",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_NEG_I64] = {
        .mnemonic = "NEG_I64",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_NEG_F64] = {
        .mnemonic = "NEG_F64",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_NOT_BOOL] = {
        .mnemonic = "NOT_BOOL",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_I64_TO_F64] = {
        .mnemonic = "I64_TO_F64",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_F64_TO_I64] = {
        .mnemonic = "F64_TO_I64",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_ADD_F64] = {
        .mnemonic = "ADD_F64",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_SUB_F64] = {
        .mnemonic = "SUB_F64",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_MUL_F64] = {
        .mnemonic = "MUL_F64",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_DIV_F64] = {
        .mnemonic = "DIV_F64",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_REM_F64] = {
        .mnemonic = "REM_F64",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_CMP_EQ_F64] = {
        .mnemonic = "CMP_EQ_F64",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_CMP_NE_F64] = {
        .mnemonic = "CMP_NE_F64",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_CMP_LT_F64] = {
        .mnemonic = "CMP_LT_F64",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_CMP_LE_F64] = {
        .mnemonic = "CMP_LE_F64",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_CMP_GT_F64] = {
        .mnemonic = "CMP_GT_F64",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_CMP_GE_F64] = {
        .mnemonic = "CMP_GE_F64",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_CMP_EQ_STR] = {
        .mnemonic = "CMP_EQ_STR",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_AND_I64] = {
        .mnemonic = "AND_I64",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_OR_I64] = {
        .mnemonic = "OR_I64",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_XOR_I64] = {
        .mnemonic = "XOR_I64",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_SHL_I64] = {
        .mnemonic = "SHL_I64",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_SHR_I64] = {
        .mnemonic = "SHR_I64",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_JMP] = {
        .mnemonic = "JMP",
        .operand_count = 1,
        .operands = { SURGE_OPERAND_JUMP_OFFSET, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_JMP_IF_TRUE] = {
        .mnemonic = "JMP_IF_TRUE",
        .operand_count = 1,
        .operands = { SURGE_OPERAND_JUMP_OFFSET, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_JMP_IF_FALSE] = {
        .mnemonic = "JMP_IF_FALSE",
        .operand_count = 1,
        .operands = { SURGE_OPERAND_JUMP_OFFSET, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_CALL] = {
        .mnemonic = "CALL",
        .operand_count = 2,
        .operands = { SURGE_OPERAND_FUNC_INDEX, SURGE_OPERAND_ARG_COUNT }
    },
    [SURGE_OP_RET] = {
        .mnemonic = "RET",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_NOP] = {
        .mnemonic = "NOP",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_POP] = {
        .mnemonic = "POP",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_ARR_NEW] = {
        .mnemonic = "ARR_NEW",
        .operand_count = 1,
        .operands = { SURGE_OPERAND_ARRAY_COUNT, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_ARR_LEN] = {
        .mnemonic = "ARR_LEN",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_ARR_GET] = {
        .mnemonic = "ARR_GET",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_ARR_SET] = {
        .mnemonic = "ARR_SET",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_TRAP] = {
        .mnemonic = "TRAP",
        .operand_count = 1,
        .operands = { SURGE_OPERAND_TRAP_CODE, SURGE_OPERAND_NONE }
    },
    [SURGE_OP_HALT] = {
        .mnemonic = "HALT",
        .operand_count = 0,
        .operands = { SURGE_OPERAND_NONE, SURGE_OPERAND_NONE }
    }
};

STATIC_ASSERT(SURGE_OP_COUNT > 0, opcode_count_positive);

static void surge_opcode_table_check_once(void) {
    static bool checked = false;
    if (checked) {
        return;
    }
    surge_opcode_table_selfcheck();
    checked = true;
}

const SurgeOpcodeInfo *surge_opcode_info(SurgeOpcode opcode) {
    surge_opcode_table_check_once();
    if ((int)opcode < 0 || opcode >= SURGE_OP_COUNT) {
        return NULL;
    }
    return &g_opcode_info[opcode];
}

const char *surge_opcode_name(SurgeOpcode opcode) {
    const SurgeOpcodeInfo *info = surge_opcode_info(opcode);
    return info ? info->mnemonic : "<invalid-opcode>";
}

const char *surge_operand_kind_name(SurgeOperandKind kind) {
    switch (kind) {
        case SURGE_OPERAND_NONE: return "NONE";
        case SURGE_OPERAND_I64: return "I64";
        case SURGE_OPERAND_F64: return "F64";
        case SURGE_OPERAND_BOOL: return "BOOL";
        case SURGE_OPERAND_CONST_IDX: return "CONST_IDX";
        case SURGE_OPERAND_LOCAL_SLOT: return "LOCAL_SLOT";
        case SURGE_OPERAND_GLOBAL_SLOT: return "GLOBAL_SLOT";
        case SURGE_OPERAND_JUMP_OFFSET: return "JUMP_OFFSET";
        case SURGE_OPERAND_FUNC_INDEX: return "FUNC_INDEX";
        case SURGE_OPERAND_ARG_COUNT: return "ARG_COUNT";
        case SURGE_OPERAND_ARRAY_COUNT: return "ARRAY_COUNT";
        case SURGE_OPERAND_TRAP_CODE: return "TRAP_CODE";
        default: return "UNKNOWN";
    }
}

size_t surge_operand_kind_size(SurgeOperandKind kind) {
    switch (kind) {
        case SURGE_OPERAND_NONE: return 0;
        case SURGE_OPERAND_BOOL: return sizeof(uint8_t);
        case SURGE_OPERAND_ARG_COUNT: return sizeof(uint8_t);
        case SURGE_OPERAND_LOCAL_SLOT: return sizeof(uint16_t);
        case SURGE_OPERAND_GLOBAL_SLOT: return sizeof(uint16_t);
        case SURGE_OPERAND_FUNC_INDEX: return sizeof(uint16_t);
        case SURGE_OPERAND_TRAP_CODE: return sizeof(uint16_t);
        case SURGE_OPERAND_CONST_IDX: return sizeof(uint32_t);
        case SURGE_OPERAND_ARRAY_COUNT: return sizeof(uint32_t);
        case SURGE_OPERAND_JUMP_OFFSET: return sizeof(int32_t);
        case SURGE_OPERAND_I64: return sizeof(int64_t);
        case SURGE_OPERAND_F64: return sizeof(double);
        default: return 0;
    }
}

bool surge_operand_kind_is_signed(SurgeOperandKind kind) {
    switch (kind) {
        case SURGE_OPERAND_JUMP_OFFSET:
            return true;
        default:
            return false;
    }
}

bool surge_opcode_from_name(const char *mnemonic, SurgeOpcode *out_opcode) {
    if (!mnemonic || !out_opcode) {
        return false;
    }
    surge_opcode_table_check_once();
    for (int i = 0; i < SURGE_OP_COUNT; ++i) {
        if (strcmp(g_opcode_info[i].mnemonic, mnemonic) == 0) {
            *out_opcode = (SurgeOpcode)i;
            return true;
        }
    }
    return false;
}

void surge_opcode_table_selfcheck(void) {
    for (int i = 0; i < SURGE_OP_COUNT; ++i) {
        const SurgeOpcodeInfo *info = &g_opcode_info[i];
        assert(info->mnemonic && info->mnemonic[0] != '\0');
        assert(info->operand_count <= SURGE_OPCODE_MAX_OPERANDS);
        for (uint8_t k = info->operand_count; k < SURGE_OPCODE_MAX_OPERANDS; ++k) {
            assert(info->operands[k] == SURGE_OPERAND_NONE);
        }
    }
}
