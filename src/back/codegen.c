#include "codegen.h"
#include "opcodes.h"

#include <limits.h>
#include <stdlib.h>
#include <string.h>

static bool grow(CgBuf *b, uint32_t need) {
    if (b->len + need <= b->cap) {
        return true;
    }
    uint32_t cap = b->cap ? b->cap : 64u;
    while (cap < b->len + need) {
        cap <<= 1;
    }
    uint8_t *nb = (uint8_t*)realloc(b->data, cap);
    if (!nb) {
        return false;
    }
    b->data = nb;
    b->cap = cap;
    return true;
}

void cg_buf_init(CgBuf *b) { memset(b, 0, sizeof(*b)); }

void cg_buf_free(CgBuf *b) {
    if (!b) {
        return;
    }
    free(b->data);
    memset(b, 0, sizeof(*b));
}

bool cg_emit_u8(CgBuf *b, uint8_t v) {
    if (!grow(b, 1)) {
        return false;
    }
    b->data[b->len++] = v;
    return true;
}

bool cg_emit_u16(CgBuf *b, uint16_t v) {
    if (!grow(b, 2)) {
        return false;
    }
    b->data[b->len++] = (uint8_t)(v & 0xFFu);
    b->data[b->len++] = (uint8_t)((v >> 8) & 0xFFu);
    return true;
}

bool cg_emit_u32(CgBuf *b, uint32_t v) {
    if (!grow(b, 4)) {
        return false;
    }
    for (int i = 0; i < 4; ++i) {
        b->data[b->len++] = (uint8_t)((v >> (i * 8)) & 0xFFu);
    }
    return true;
}

bool cg_emit_u64(CgBuf *b, uint64_t v) {
    if (!grow(b, 8)) {
        return false;
    }
    for (int i = 0; i < 8; ++i) {
        b->data[b->len++] = (uint8_t)((v >> (i * 8)) & 0xFFu);
    }
    return true;
}

bool cg_emit_f64(CgBuf *b, double d) {
    uint64_t bits;
    memcpy(&bits, &d, sizeof(bits));
    return cg_emit_u64(b, bits);
}

bool cg_patch_i32(CgBuf *b, uint32_t offset, int32_t value) {
    if (!b || offset + 4 > b->len) {
        return false;
    }
    b->data[offset + 0] = (uint8_t)(value & 0xFF);
    b->data[offset + 1] = (uint8_t)((value >> 8) & 0xFF);
    b->data[offset + 2] = (uint8_t)((value >> 16) & 0xFF);
    b->data[offset + 3] = (uint8_t)((value >> 24) & 0xFF);
    return true;
}

bool cg_locals_init(CgLocals *ls) {
    memset(ls, 0, sizeof(*ls));
    return true;
}

void cg_locals_free(CgLocals *ls) {
    if (!ls) {
        return;
    }
    for (uint16_t i = 0; i < ls->nlocals; ++i) {
        free(ls->locals[i].name);
    }
    free(ls->locals);
    memset(ls, 0, sizeof(*ls));
}

static bool locals_grow(CgLocals *ls) {
    uint16_t cap = ls->caplocals ? (uint16_t)(ls->caplocals * 2) : 8u;
    void *tmp = realloc(ls->locals, cap * sizeof(*ls->locals));
    if (!tmp) {
        return false;
    }
    ls->locals = tmp;
    ls->caplocals = cap;
    return true;
}

bool cg_locals_put(CgLocals *ls, const char *name, uint16_t *out_slot) {
    if (ls->nlocals == ls->caplocals) {
        if (!locals_grow(ls)) {
            return false;
        }
    }
    uint16_t slot = ls->nlocals++;
    ls->locals[slot].name = name ? strdup(name) : NULL;
    ls->locals[slot].slot = slot;
    if (out_slot) {
        *out_slot = slot;
    }
    return true;
}

bool cg_locals_get(const CgLocals *ls, const char *name, uint16_t *out_slot) {
    if (!ls || !name) {
        return false;
    }
    for (uint16_t i = 0; i < ls->nlocals; ++i) {
        if (ls->locals[i].name && strcmp(ls->locals[i].name, name) == 0) {
            if (out_slot) {
                *out_slot = ls->locals[i].slot;
            }
            return true;
        }
    }
    return false;
}

static bool cg_funcs_reserve(Codegen *cg, size_t need) {
    if (need <= cg->func_cap) {
        return true;
    }
    size_t cap = cg->func_cap ? cg->func_cap * 2 : 4;
    if (cap < need) {
        cap = need;
    }
    CgFunc *tmp = (CgFunc*)realloc(cg->funcs, cap * sizeof(*cg->funcs));
    if (!tmp) {
        return false;
    }
    cg->funcs = tmp;
    cg->func_cap = cap;
    return true;
}

static void cg_funcs_free(Codegen *cg) {
    if (!cg) {
        return;
    }
    for (size_t i = 0; i < cg->func_count; ++i) {
        free(cg->funcs[i].name);
    }
    free(cg->funcs);
    cg->funcs = NULL;
    cg->func_count = 0;
    cg->func_cap = 0;
}

static bool cg_register_function(Codegen *cg, SurgeAstStmt *fn) {
    if (!cg || !fn || fn->base.kind != AST_FN_DECL) {
        return true;
    }
    if (fn->as.fn_decl.paramc > UINT16_MAX) {
        return false;
    }
    if (cg->func_count >= UINT16_MAX) {
        return false;
    }
    if (!cg_funcs_reserve(cg, cg->func_count + 1)) {
        return false;
    }

    CgFunc *slot = &cg->funcs[cg->func_count];
    memset(slot, 0, sizeof(*slot));
    slot->name = strdup(fn->as.fn_decl.name.name ? fn->as.fn_decl.name.name : "");
    if (!slot->name) {
        return false;
    }
    slot->arity = (uint16_t)fn->as.fn_decl.paramc;
    slot->decl = fn;
    slot->name_idx = sbc_intern_string(cg->w, slot->name);
    if (slot->name_idx == UINT32_MAX) {
        free(slot->name);
        slot->name = NULL;
        return false;
    }

    cg->func_count++;
    return true;
}

static bool cg_collect_functions(Codegen *cg, SurgeAstUnit *unit) {
    for (size_t i = 0; i < unit->count; ++i) {
        SurgeAstStmt *st = unit->decls[i];
        if (st->base.kind == AST_FN_DECL) {
            if (!cg_register_function(cg, st)) {
                return false;
            }
        }
    }
    return true;
}

static bool cg_lookup_func_index(const Codegen *cg, const char *name, uint16_t *out_idx) {
    if (!cg || !name) {
        return false;
    }
    for (size_t i = 0; i < cg->func_count; ++i) {
        if (strcmp(cg->funcs[i].name, name) == 0) {
            if (out_idx) {
                *out_idx = (uint16_t)i;
            }
            return true;
        }
    }
    return false;
}

static bool op0(CgBuf *b, SurgeOpcode op) {
    return cg_emit_u8(b, (uint8_t)op);
}

static bool op_u16(CgBuf *b, SurgeOpcode op, uint16_t v) {
    return cg_emit_u8(b, (uint8_t)op) && cg_emit_u16(b, v);
}

static bool op_u32(CgBuf *b, SurgeOpcode op, uint32_t v) {
    return cg_emit_u8(b, (uint8_t)op) && cg_emit_u32(b, v);
}

static bool op_i64(CgBuf *b, int64_t v) {
    return cg_emit_u8(b, (uint8_t)SURGE_OP_PUSH_I64) && cg_emit_u64(b, (uint64_t)v);
}

static bool op_f64(CgBuf *b, double d) {
    return cg_emit_u8(b, (uint8_t)SURGE_OP_PUSH_F64) && cg_emit_f64(b, d);
}

static bool op_call(CgBuf *b, uint16_t func_idx, uint8_t argc) {
    if (!cg_emit_u8(b, (uint8_t)SURGE_OP_CALL)) {
        return false;
    }
    if (!cg_emit_u16(b, func_idx)) {
        return false;
    }
    return cg_emit_u8(b, argc);
}

static bool emit_jump_placeholder(CgBuf *code, SurgeOpcode op, uint32_t *operand_off) {
    if (!cg_emit_u8(code, (uint8_t)op)) {
        return false;
    }
    uint32_t operand = code->len;
    if (!cg_emit_u32(code, 0)) {
        return false;
    }
    if (operand_off) {
        *operand_off = operand;
    }
    return true;
}

static bool patch_jump(CgBuf *code, uint32_t operand_off, uint32_t target) {
    uint32_t insn_end = operand_off + 4u;
    int64_t delta = (int64_t)target - (int64_t)insn_end;
    if (delta < (int64_t)INT32_MIN || delta > (int64_t)INT32_MAX) {
        return false;
    }
    return cg_patch_i32(code, operand_off, (int32_t)delta);
}

typedef struct {
    Codegen *cg;
    SbcWriter *w;
    CgBuf *code;
    CgLocals *locals;
} Ctx;

static bool gen_expr(Ctx *cx, SurgeAstExpr *e);
static bool gen_stmt(Ctx *cx, SurgeAstStmt *st);

static bool emit_trap(Ctx *cx, SurgeTrapCode code) {
    return op_u16(cx->code, SURGE_OP_TRAP, (uint16_t)code);
}

static bool gen_expr(Ctx *cx, SurgeAstExpr *e) {
    switch (e->base.kind) {
        case AST_INT_LIT:
            return op_i64(cx->code, (int64_t)e->as.int_lit.v);
        case AST_FLOAT_LIT:
            return op_f64(cx->code, e->as.float_lit.v);
        case AST_BOOL_LIT:
            return cg_emit_u8(cx->code, (uint8_t)SURGE_OP_PUSH_BOOL) &&
                   cg_emit_u8(cx->code, e->as.bool_lit.v ? 1u : 0u);
        case AST_STRING_LIT: {
            uint32_t idx = sbc_intern_string(cx->w, e->as.string_lit.v);
            if (idx == UINT32_MAX) {
                return false;
            }
            return op_u32(cx->code, SURGE_OP_PUSH_STR, idx);
        }
        case AST_IDENT: {
            uint16_t slot;
            if (cg_locals_get(cx->locals, e->as.ident.ident.name, &slot)) {
                return op_u16(cx->code, SURGE_OP_LOAD, slot);
            }
            return emit_trap(cx, SURGE_TRAP_BAD_CALL);
        }
        case AST_PAREN:
            return gen_expr(cx, e->as.paren.inner);
        case AST_ARRAY_LIT: {
            if (e->as.array_lit.count > UINT32_MAX) {
                return emit_trap(cx, SURGE_TRAP_TYPE_ERROR);
            }
            for (size_t i = 0; i < e->as.array_lit.count; ++i) {
                if (!gen_expr(cx, e->as.array_lit.items[i])) {
                    return false;
                }
            }
            return op_u32(cx->code, SURGE_OP_ARR_NEW, (uint32_t)e->as.array_lit.count);
        }
        case AST_INDEX: {
            if (!gen_expr(cx, e->as.index.base)) {
                return false;
            }
            if (!gen_expr(cx, e->as.index.index)) {
                return false;
            }
            return op0(cx->code, SURGE_OP_ARR_GET);
        }
        case AST_CALL: {
            SurgeAstExpr *callee = e->as.call.callee;
            if (callee->base.kind != AST_IDENT) {
                return emit_trap(cx, SURGE_TRAP_BAD_CALL);
            }
            uint16_t fn_idx;
            if (!cg_lookup_func_index(cx->cg, callee->as.ident.ident.name, &fn_idx)) {
                return emit_trap(cx, SURGE_TRAP_BAD_CALL);
            }
            if (e->as.call.argc > UINT8_MAX) {
                return emit_trap(cx, SURGE_TRAP_BAD_CALL);
            }
            for (size_t i = 0; i < e->as.call.argc; ++i) {
                if (!gen_expr(cx, e->as.call.args[i])) {
                    return false;
                }
            }
            return op_call(cx->code, fn_idx, (uint8_t)e->as.call.argc);
        }
        case AST_BINARY: {
            if (!gen_expr(cx, e->as.binary.lhs)) {
                return false;
            }
            if (!gen_expr(cx, e->as.binary.rhs)) {
                return false;
            }
            switch (e->as.binary.op) {
                case AST_OP_ADD: return op0(cx->code, SURGE_OP_ADD);
                case AST_OP_SUB: return op0(cx->code, SURGE_OP_SUB);
                case AST_OP_MUL: return op0(cx->code, SURGE_OP_MUL);
                case AST_OP_DIV: return op0(cx->code, SURGE_OP_DIV);
                case AST_OP_REM: return op0(cx->code, SURGE_OP_REM);
                case AST_OP_EQ:  return op0(cx->code, SURGE_OP_CMP_EQ);
                case AST_OP_NE:  return op0(cx->code, SURGE_OP_CMP_NE);
                case AST_OP_LT:  return op0(cx->code, SURGE_OP_CMP_LT);
                case AST_OP_LE:  return op0(cx->code, SURGE_OP_CMP_LE);
                case AST_OP_GT:  return op0(cx->code, SURGE_OP_CMP_GT);
                case AST_OP_GE:  return op0(cx->code, SURGE_OP_CMP_GE);
                default: break;
            }
            return emit_trap(cx, SURGE_TRAP_TYPE_ERROR);
        }
        case AST_BIND_EXPR: {
            SurgeAstExpr *lhs = e->as.bind_expr.lhs;
            if (lhs->base.kind == AST_INDEX) {
                if (!gen_expr(cx, lhs->as.index.base)) {
                    return false;
                }
                if (!gen_expr(cx, lhs->as.index.index)) {
                    return false;
                }
                if (!gen_expr(cx, e->as.bind_expr.rhs)) {
                    return false;
                }
                return op0(cx->code, SURGE_OP_ARR_SET);
            }
            return emit_trap(cx, SURGE_TRAP_TYPE_ERROR);
        }
        default:
            return emit_trap(cx, SURGE_TRAP_TYPE_ERROR);
    }
}

static bool gen_if_stmt(Ctx *cx, SurgeAstStmt *st) {
    if (!gen_expr(cx, st->as.if_stmt.cond)) {
        return false;
    }
    uint32_t jfalse_operand;
    if (!emit_jump_placeholder(cx->code, SURGE_OP_JMP_IF_FALSE, &jfalse_operand)) {
        return false;
    }

    if (!gen_stmt(cx, st->as.if_stmt.then_blk)) {
        return false;
    }

    if (st->as.if_stmt.has_else) {
        uint32_t jend_operand;
        if (!emit_jump_placeholder(cx->code, SURGE_OP_JMP, &jend_operand)) {
            return false;
        }
        if (!patch_jump(cx->code, jfalse_operand, cx->code->len)) {
            return false;
        }
        if (!gen_stmt(cx, st->as.if_stmt.else_blk)) {
            return false;
        }
        if (!patch_jump(cx->code, jend_operand, cx->code->len)) {
            return false;
        }
    } else {
        if (!patch_jump(cx->code, jfalse_operand, cx->code->len)) {
            return false;
        }
    }

    return true;
}

static bool gen_while_stmt(Ctx *cx, SurgeAstStmt *st) {
    uint32_t loop_start = cx->code->len;
    if (!gen_expr(cx, st->as.while_stmt.cond)) {
        return false;
    }
    uint32_t jexit_operand;
    if (!emit_jump_placeholder(cx->code, SURGE_OP_JMP_IF_FALSE, &jexit_operand)) {
        return false;
    }
    if (!gen_stmt(cx, st->as.while_stmt.body)) {
        return false;
    }
    uint32_t jloop_operand;
    if (!emit_jump_placeholder(cx->code, SURGE_OP_JMP, &jloop_operand)) {
        return false;
    }
    if (!patch_jump(cx->code, jloop_operand, loop_start)) {
        return false;
    }
    if (!patch_jump(cx->code, jexit_operand, cx->code->len)) {
        return false;
    }
    return true;
}

static bool gen_stmt(Ctx *cx, SurgeAstStmt *st) {
    switch (st->base.kind) {
        case AST_LET_DECL: {
            uint16_t slot;
            if (!cg_locals_put(cx->locals, st->as.let_decl.name.name, &slot)) {
                return false;
            }
            if (!gen_expr(cx, st->as.let_decl.init)) {
                return false;
            }
            return op_u16(cx->code, SURGE_OP_STORE, slot);
        }
        case AST_ASSIGN_STMT: {
            uint16_t slot;
            if (!cg_locals_get(cx->locals, st->as.assign_stmt.name.name, &slot)) {
                return emit_trap(cx, SURGE_TRAP_BAD_CALL);
            }
            if (!gen_expr(cx, st->as.assign_stmt.expr)) {
                return false;
            }
            return op_u16(cx->code, SURGE_OP_STORE, slot);
        }
        case AST_EXPR_STMT: {
            if (!gen_expr(cx, st->as.expr_stmt.expr)) {
                return false;
            }
            if (st->as.expr_stmt.expr->base.kind == AST_BIND_EXPR) {
                return true;
            }
            return op0(cx->code, SURGE_OP_POP);
        }
        case AST_RETURN: {
            if (st->as.return_stmt.has_value) {
                if (!gen_expr(cx, st->as.return_stmt.value)) {
                    return false;
                }
            } else {
                if (!op0(cx->code, SURGE_OP_PUSH_NULL)) {
                    return false;
                }
            }
            return op0(cx->code, SURGE_OP_RET);
        }
        case AST_BLOCK:
            for (size_t i = 0; i < st->as.block.count; ++i) {
                if (!gen_stmt(cx, st->as.block.stmts[i])) {
                    return false;
                }
            }
            return true;
        case AST_IF:
            return gen_if_stmt(cx, st);
        case AST_WHILE:
            return gen_while_stmt(cx, st);
        default:
            return emit_trap(cx, SURGE_TRAP_UNREACHABLE);
    }
}

static bool gen_function(Codegen *cg, size_t index) {
    CgFunc *fn = &cg->funcs[index];
    SurgeAstStmt *node = fn->decl;

    cg_buf_init(&cg->code);
    cg_locals_init(&cg->locals);

    for (size_t i = 0; i < node->as.fn_decl.paramc; ++i) {
        if (!cg_locals_put(&cg->locals, node->as.fn_decl.params[i].name.name, NULL)) {
            cg_locals_free(&cg->locals);
            cg_buf_free(&cg->code);
            return false;
        }
    }

    Ctx cx = { .cg = cg, .w = cg->w, .code = &cg->code, .locals = &cg->locals };
    if (!gen_stmt(&cx, node->as.fn_decl.body)) {
        cg_locals_free(&cg->locals);
        cg_buf_free(&cg->code);
        return false;
    }

    if (cg->code.len == 0 || cg->code.data[cg->code.len - 1] != (uint8_t)SURGE_OP_RET) {
        if (!op0(&cg->code, SURGE_OP_PUSH_NULL) || !op0(&cg->code, SURGE_OP_RET)) {
            cg_locals_free(&cg->locals);
            cg_buf_free(&cg->code);
            return false;
        }
    }

    SbcFuncInput input = {
        .name_idx = fn->name_idx,
        .arity = fn->arity,
        .nlocals = cg->locals.nlocals,
        .code = cg->code.data,
        .code_len = cg->code.len,
        .flags = 0
    };

    bool ok = sbc_add_function(cg->w, &input);
    cg_locals_free(&cg->locals);
    cg_buf_free(&cg->code);
    return ok;
}

static bool gen_unit(Codegen *cg, SurgeAstUnit *unit) {
    if (!cg_collect_functions(cg, unit)) {
        return false;
    }
    for (size_t i = 0; i < cg->func_count; ++i) {
        if (!gen_function(cg, i)) {
            return false;
        }
    }
    return true;
}

CgResult surge_codegen_unit(SurgeAstUnit *unit, const char *out_path) {
    if (!unit || !out_path) {
        return CG_ERR;
    }

    Codegen cg = {0};
    cg.w = sbc_writer_new();
    if (!cg.w) {
        return CG_ERR;
    }

    bool ok = gen_unit(&cg, unit);
    if (ok) {
        ok = sbc_write_to_file(cg.w, out_path);
    }

    cg_funcs_free(&cg);
    sbc_writer_free(cg.w);
    return ok ? CG_OK : CG_ERR;
}
