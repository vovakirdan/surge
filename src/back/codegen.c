#include "codegen.h"
#include "opcodes.h"
#include <string.h>
#include <stdlib.h>

static bool grow(CgBuf *b, uint32_t need){
    if (b->len + need <= b->cap) return true;
    uint32_t ncap = b->cap ? b->cap*2 : 64;
    while (ncap < b->len + need) ncap *= 2;
    uint8_t *nb = (uint8_t*)realloc(b->data, ncap);
    if (!nb) return false;
    b->data = nb; b->cap = ncap; return true;
}
void cg_buf_init(CgBuf *b){ memset(b,0,sizeof(*b)); }
void cg_buf_free(CgBuf *b){ free(b->data); memset(b,0,sizeof(*b)); }
bool cg_emit_u8(CgBuf *b, uint8_t v){ if(!grow(b,1))return false; b->data[b->len++]=v; return true; }
bool cg_emit_u16(CgBuf *b, uint16_t v){ if(!grow(b,2))return false; b->data[b->len++]=v&0xFF; b->data[b->len++]=v>>8; return true; }
bool cg_emit_u32(CgBuf *b, uint32_t v){ if(!grow(b,4))return false; for(int i=0;i<4;i++) b->data[b->len++]=(v>>(8*i))&0xFF; return true; }
bool cg_emit_u64(CgBuf *b, uint64_t v){ if(!grow(b,8))return false; for(int i=0;i<8;i++) b->data[b->len++]=(v>>(8*i))&0xFF; return true; }
bool cg_emit_f64(CgBuf *b, double d){ uint64_t u; memcpy(&u,&d,sizeof u); return cg_emit_u64(b,u); }

bool cg_locals_init(CgLocals *ls){ memset(ls,0,sizeof(*ls)); return true; }
void cg_locals_free(CgLocals *ls){
    for (uint16_t i=0;i<ls->nlocals;i++) free(ls->locals[i].name);
    free(ls->locals);
    memset(ls,0,sizeof(*ls));
}
static bool locals_grow(CgLocals *ls){
    uint16_t nc = ls->caplocals? ls->caplocals*2 : 8;
    void *nb = realloc(ls->locals, nc * sizeof(*ls->locals));
    if (!nb) return false;
    ls->locals = nb; ls->caplocals = nc; return true;
}
bool cg_locals_put(CgLocals *ls, const char *name, uint16_t *out_slot){
    // no shadowing in this MVP backend — assume Sema handled it
    if (ls->nlocals == ls->caplocals && !locals_grow(ls)) return false;
    uint16_t slot = ls->nlocals++;
    ls->locals[slot].name = strdup(name);
    ls->locals[slot].slot = slot;
    if (out_slot) *out_slot = slot;
    return true;
}
bool cg_locals_get(const CgLocals *ls, const char *name, uint16_t *out_slot){
    for (uint16_t i=0;i<ls->nlocals;i++){
        if (strcmp(ls->locals[i].name,name)==0){ if(out_slot)*out_slot=ls->locals[i].slot; return true; }
    }
    return false;
}

/* ---- tiny helpers to emit opcodes ---- */
static bool op0(CgBuf *b, SurgeOpcode op){ return cg_emit_u8(b,(uint8_t)op); }
// static bool op_u8(CgBuf *b, SurgeOpcode op, uint8_t a){ return cg_emit_u8(b,(uint8_t)op) && cg_emit_u8(b,a); }
static bool op_u16(CgBuf *b, SurgeOpcode op, uint16_t a){ return cg_emit_u8(b,(uint8_t)op) && cg_emit_u16(b,a); }
static bool op_u32(CgBuf *b, SurgeOpcode op, uint32_t a){ return cg_emit_u8(b,(uint8_t)op) && cg_emit_u32(b,a); }
static bool op_i64(CgBuf *b, int64_t v){ return cg_emit_u8(b,(uint8_t)SURGE_OP_PUSH_I64) && cg_emit_u64(b,(uint64_t)v); }
static bool op_f64(CgBuf *b, double d){ return cg_emit_u8(b,(uint8_t)SURGE_OP_PUSH_F64) && cg_emit_f64(b,d); }

/* ---- expr codegen (узкий MVP) ---- */

typedef struct {
    Codegen *cg;
    SbcWriter *w;
    CgBuf *code;
    CgLocals *locals;
} Ctx;

static bool gen_expr(Ctx *cx, SurgeAstExpr *e){
    switch (e->base.kind){
        case AST_INT_LIT:
            return op_i64(cx->code, (int64_t)e->as.int_lit.v);
        case AST_FLOAT_LIT:
            return op_f64(cx->code, e->as.float_lit.v);
        case AST_BOOL_LIT:
            return cg_emit_u8(cx->code,(uint8_t)SURGE_OP_PUSH_BOOL)
                && cg_emit_u8(cx->code, e->as.bool_lit.v ? 1 : 0);
        case AST_STRING_LIT: {
            uint32_t idx = sbc_intern_string(cx->w, e->as.string_lit.v);
            return op_u32(cx->code, SURGE_OP_PUSH_STR, idx);
        }
        case AST_IDENT: {
            uint16_t slot;
            if (!cg_locals_get(cx->locals, e->as.ident.ident.name, &slot)) {
                // для MVP глобальных нет — если нет в локалах, TRAP
                return op_u16(cx->code, SURGE_OP_TRAP, SURGE_TRAP_BAD_CALL);
            }
            return op_u16(cx->code, SURGE_OP_LOAD, slot);
        }
        case AST_BINARY: {
            if (!gen_expr(cx, e->as.binary.lhs)) return false;
            if (!gen_expr(cx, e->as.binary.rhs)) return false;
            switch (e->as.binary.op){
                case AST_OP_ADD: return op0(cx->code, SURGE_OP_ADD);
                case AST_OP_SUB: return op0(cx->code, SURGE_OP_SUB);
                case AST_OP_MUL: return op0(cx->code, SURGE_OP_MUL);
                case AST_OP_DIV: return op0(cx->code, SURGE_OP_DIV);
                default: return op_u16(cx->code, SURGE_OP_TRAP, SURGE_TRAP_TYPE_ERROR);
            }
        }
        default:
            return op_u16(cx->code, SURGE_OP_TRAP, SURGE_TRAP_TYPE_ERROR);
    }
}

static bool gen_stmt(Ctx *cx, SurgeAstStmt *st){
    switch (st->base.kind){
        case AST_LET_DECL: {
            // поддержим только init != NULL
            uint16_t slot;
            if (!cg_locals_put(cx->locals, st->as.let_decl.name.name, &slot)) return false;
            if (!gen_expr(cx, st->as.let_decl.init)) return false;
            return op_u16(cx->code, SURGE_OP_STORE, slot);
        }
        case AST_EXPR_STMT:
            if (!gen_expr(cx, st->as.expr_stmt.expr)) return false;
            return op0(cx->code, SURGE_OP_POP);
        case AST_RETURN:
            if (st->as.return_stmt.has_value) {
                if (!gen_expr(cx, st->as.return_stmt.value)) return false;
            } else {
                // void -> PUSH_NULL, чтобы RET всегда снимал вершину (в будущем можно отличать)
                if (!op0(cx->code, SURGE_OP_PUSH_NULL)) return false;
            }
            return op0(cx->code, SURGE_OP_RET);
        case AST_BLOCK:
            for (size_t i=0;i<st->as.block.count;i++)
                if (!gen_stmt(cx, st->as.block.stmts[i])) return false;
            return true;
        default:
            // if/while/parallel/etc — теперь можно TRAP'нуть
            return op_u16(cx->code, SURGE_OP_TRAP, SURGE_TRAP_UNREACHABLE);
    }
}

static bool gen_function(Codegen *cg, SurgeAstStmt *fn){
    if (fn->base.kind != AST_FN_DECL) return true;
    // MVP: поддерживаем только main() без параметров
    if (strcmp(fn->as.fn_decl.name.name, "main") != 0) return true;
    if (fn->as.fn_decl.paramc != 0) return false;

    cg_buf_init(&cg->code);
    cg_locals_init(&cg->locals);

    Ctx cx = { .cg=cg, .w=cg->w, .code=&cg->code, .locals=&cg->locals };
    if (!gen_stmt(&cx, fn->as.fn_decl.body)) { cg_locals_free(&cg->locals); cg_buf_free(&cg->code); return false; }

    // гарантируем, что функция завершится (если return отсутствовал)
    if (cg->code.len==0 || cg->code.data[cg->code.len-1] != (uint8_t)SURGE_OP_RET) {
        if (!op0(&cg->code, SURGE_OP_PUSH_NULL)) return false;
        if (!op0(&cg->code, SURGE_OP_RET)) return false;
    }

    SbcFuncInput in = {
        .name_idx = cg->name_main_idx,
        .arity = 0,
        .nlocals = cg->locals.nlocals,
        .code = cg->code.data,
        .code_len = cg->code.len,
        .flags = 0
    };
    bool ok = sbc_add_function(cg->w, &in);
    cg_locals_free(&cg->locals);
    cg_buf_free(&cg->code);
    return ok;
}

static bool gen_unit(Codegen *cg, SurgeAstUnit *u){
    // заранее интерним имя main
    cg->name_main_idx = sbc_intern_string(cg->w, "main");
    if (cg->name_main_idx == UINT32_MAX) return false;

    // пройдёмся по всем декларациям, найдём main и сгенерим
    for (size_t i=0;i<u->count;i++){
        SurgeAstStmt *st = u->decls[i];
        if (st->base.kind == AST_FN_DECL) {
            if (!gen_function(cg, st)) return false;
        }
    }
    return true;
}

CgResult surge_codegen_unit(SurgeAstUnit *unit, const char *out_path){
    Codegen cg = {0};
    cg.w = sbc_writer_new();
    if (!cg.w) return CG_ERR;

    bool ok = gen_unit(&cg, unit);
    if (ok) ok = sbc_write_to_file(cg.w, out_path);

    sbc_writer_free(cg.w);
    return ok ? CG_OK : CG_ERR;
}
