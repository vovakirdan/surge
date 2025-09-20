#include "sema.h"
#include "diagnostics.h"
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

// ---------- scope helpers ----------

static SemaScope *scope_push(Sema *s){
    SemaScope *sc = (SemaScope*)calloc(1,sizeof(*sc));
    sc->parent = s->scope;
    s->scope = sc;
    return sc;
}
static void scope_free(SemaScope *sc){
    if (!sc) return;
    for (size_t i=0;i<sc->n;i++) free(sc->entries[i].name);
    free(sc->entries);
    free(sc);
}
static void scope_pop(Sema *s){
    SemaScope *old = s->scope;
    s->scope = old? old->parent : NULL;
    scope_free(old);
}
static Symbol *scope_lookup(SemaScope *sc, const char *name){
    for (SemaScope *p=sc; p; p=p->parent){
        for (size_t i=0;i<p->n;i++) if (strcmp(p->entries[i].name, name)==0) return &p->entries[i].sym;
    }
    return NULL;
}
static bool scope_insert_current(SemaScope *sc, const char *name, Symbol sym){
    if (sc->n == sc->cap){
        sc->cap = sc->cap? sc->cap*2 : 8;
        struct Entry *new_entries = realloc(sc->entries, sc->cap*sizeof(*sc->entries));
        if (!new_entries) return false;
        sc->entries = new_entries;
    }
    sc->entries[sc->n].name = strdup(name);
    sc->entries[sc->n].sym = sym;
    sc->n++;
    return true;
}

static Symbol *scope_lookup_current(SemaScope *sc, const char *name){
    for (size_t i=0;i<sc->n;i++) if (strcmp(sc->entries[i].name, name)==0) return &sc->entries[i].sym;
    return NULL;
}

static bool sema_insert_symbol(Sema *s, const char *name, Symbol sym){
    // ALLOW: запрещаем только дубликаты в текущем скоупе
    if (s->shadow == SHADOW_ALLOW) {
        if (scope_lookup_current(s->scope, name)) return false;
        return scope_insert_current(s->scope, name, sym);
    }
    // DENY: нельзя, если имя встречается в любом предке
    if (s->shadow == SHADOW_DENY) {
        if (scope_lookup(s->scope, name)) return false;
        return scope_insert_current(s->scope, name, sym);
    }
    // CONTROLLED: можно перекрывать, если тип совпадает
    if (s->shadow == SHADOW_CONTROLLED) {
        Symbol *prev = scope_lookup(s->scope, name);
        if (prev && !ty_equal(prev->type, sym.type)) {
            return false; // тип другой — запрещаем
        }
        // если нет или тип совпадает — ок
        return scope_insert_current(s->scope, name, sym);
    }
    // fallback
    return scope_insert_current(s->scope, name, sym);
}

static bool scope_insert_raw(SemaScope *sc, const char *name, Symbol sym, SurgeSrcPos pos){
    for (size_t i=0;i<sc->n;i++) if (strcmp(sc->entries[i].name, name)==0) return false;
    if (sc->n == sc->cap){
        sc->cap = sc->cap ? sc->cap*2 : 8;
        sc->entries = realloc(sc->entries, sc->cap * sizeof(*sc->entries));
        if (!sc->entries) return false;
    }
    sc->entries[sc->n].name = strdup(name);
    sc->entries[sc->n].sym  = sym;
    sc->entries[sc->n].pos  = pos;
    sc->n++;
    return true;
}

static struct Entry *scope_find_any(SemaScope *sc, const char *name){
    for (SemaScope *p=sc; p; p=p->parent)
        for (size_t i=0;i<p->n;i++)
            if (strcmp(p->entries[i].name, name)==0) return &p->entries[i];
    return NULL;
}

bool sema_insert(Sema *s, const char *name, Symbol sym, SurgeSrcPos pos){
    struct Entry *prev = scope_find_any(s->scope, name);

    bool violate = false;
    if (s->shadow == SHADOW_DENY) {
        violate = (prev != NULL);
    } else if (s->shadow == SHADOW_CONTROLLED) {
        // пример: разрешаем только перекрывать обычные локальные переменные из внешних блоков,
        // но не параметры / функции / глобальные сигналы
        if (prev) {
            SymKind k = prev->sym.kind;
            if (k == SYM_FN || k == SYM_SIGNAL) violate = true;
            // можно запретить и параметры (распознаются по месту вставки — например, помечать флагом)
        }
    } // SHADOW_ALLOW — всегда ок

    if (violate) {
        s->had_error = true;
        surge_diag_errorf(pos,
            "redeclaration of '%s' (previous at %s:%d:%d)",
            name,
            prev->pos.file ? prev->pos.file : "<unknown>",
            prev->pos.line, prev->pos.col);
        return false;
    }

    // запрет дублей в текущем блоке:
    for (size_t i=0;i<s->scope->n;i++){
        if (strcmp(s->scope->entries[i].name, name)==0){
            s->had_error = true;
            surge_diag_errorf(pos, "redeclaration of '%s' in the same scope", name);
            return false;
        }
    }
    return scope_insert_raw(s->scope, name, sym, pos);
}

static const SurgeType *alias_lookup(Sema *s, const char *name){
    for (size_t i=0;i<s->alias_n;i++) if (strcmp(s->aliases[i].name,name)==0) return s->aliases[i].type;
    return NULL;
}

// View type as rvalue: own T -> T, otherwise itself.
// NOTE: move/consume rules will be implemented later.
static const SurgeType *rview(const SurgeType *t) {
    if (!t) return &TY_Invalid;
    return (t->kind == TY_OWN && t->elem) ? t->elem : t;
}

static bool is_lvalue_expr(SurgeAstExpr *e) {
    if (!e) return false;
    switch (e->base.kind) {
        case AST_IDENT: return true;
        case AST_INDEX: return true;
        case AST_PAREN: return is_lvalue_expr(e->as.paren.inner);
        case AST_UNARY: // *expr — результат lvalue
            return e->as.unary.op == AST_OP_DEREF;
        default: return false;
    }
}

// check if we are inside a pure context
static bool in_pure_ctx(Sema *s) { return s->pure_depth > 0; }

// ---------- type resolution ----------

static const SurgeType *resolve_type_ast(Sema *s, const SurgeAstType *t){
    switch (t->kind){
        case TYPE_IDENT: {
            const char *nm = t->as.ident.name.name;
            // builtins
            if (strcmp(nm,"int")==0) return &TY_Int;
            if (strcmp(nm,"float")==0) return &TY_Float;
            if (strcmp(nm,"bool")==0) return &TY_Bool;
            if (strcmp(nm,"string")==0) return &TY_String;
            // user alias?
            const SurgeType *al = alias_lookup(s, nm);
            return al ? al : &TY_Invalid;
        }
        case TYPE_ARRAY:   return ty_array_of(resolve_type_ast(s, t->as.array.elem));
        case TYPE_REF:     return ty_ref_of(resolve_type_ast(s, t->as.ref_ty.elem));
        case TYPE_OWN:     return ty_own_of(resolve_type_ast(s, t->as.own_ty.elem));
        case TYPE_APPLY: {
            const char *nm = t->as.apply.name.name;
            if (strcmp(nm,"channel")==0) {
                if (t->as.apply.argc != 1) { s->had_error=true; surge_diag_errorf(t->base.pos, "channel<T> expects 1 type arg"); return &TY_Invalid; }
                const SurgeType *inner = resolve_type_ast(s, t->as.apply.args[0]);
                return ty_channel_of(inner);
            }
            // generic user types — пока не поддерживаем
            s->had_error=true; surge_diag_errorf(t->base.pos, "unknown generic type '%s'", nm);
            return &TY_Invalid;
        }
    }
    return &TY_Invalid;
}

// ---------- expr typing ----------

typedef struct {
    const SurgeType *type;
    bool is_lvalue;
} TExpr;

static TExpr check_expr(Sema *s, SurgeAstExpr *e);
static bool stmt_guarantees_return(const SurgeAstStmt *st);

static void type_mismatch(Sema *s, SurgeSrcPos pos, const char *ctx, const SurgeType *a, const SurgeType *b){
    s->had_error = true;
    surge_diag_errorf(pos, "%s: type mismatch (%s vs %s)", ctx, ty_name(a), ty_name(b));
}

static TExpr mk(const SurgeType *t, bool is_lvalue) { TExpr x; x.type=t; x.is_lvalue=is_lvalue; return x; }

static bool is_numeric_type(const SurgeType *t) {
    const SurgeType *rt = rview(t);
    return rt->kind == TY_INT || rt->kind == TY_FLOAT;
}

static const SurgeType *numeric_join(const SurgeType *a, const SurgeType *b) {
    const SurgeType *ra = rview(a);
    const SurgeType *rb = rview(b);
    if (ra->kind == TY_INVALID || rb->kind == TY_INVALID) return &TY_Invalid;
    if (ra->kind == TY_INT && rb->kind == TY_INT) return &TY_Int;
    if (ra->kind == TY_FLOAT && rb->kind == TY_FLOAT) return &TY_Float;
    if ((ra->kind == TY_FLOAT && rb->kind == TY_INT) || (ra->kind == TY_INT && rb->kind == TY_FLOAT)) return &TY_Float;
    return &TY_Invalid;
}

static bool can_assign_to(const SurgeType *dst, const SurgeType *src) {
    if (ty_equal(dst, src)) return true;
    const SurgeType *rd = rview(dst);
    const SurgeType *rs = rview(src);
    if (rd->kind == TY_FLOAT && rs->kind == TY_INT) return true;
    return false;
}

static TExpr check_call(Sema *s, SurgeAstExpr *call){
    // MVP: callee должен быть идентификатором функции; аргументы не проверяем по сигнатуре (позже)
    SurgeAstExpr *callee = call->as.call.callee;
    if (callee->base.kind != AST_IDENT){
        s->had_error=true;
        surge_diag_errorf(callee->base.pos, "callee is not an identifier");
        return mk(&TY_Invalid, false);
    }
    const char *name = callee->as.ident.ident.name;
    Symbol *sym = scope_lookup(s->scope, name);
    if (!sym || sym->kind != SYM_FN){
        s->had_error=true;
        surge_diag_errorf(callee->base.pos, "unknown function '%s'", name);
        return mk(&TY_Invalid, false);
    }
    // If inside pure context, callee must be pure
    if (in_pure_ctx(s) && !sym->is_pure) {
        s->had_error=true;
        surge_diag_errorf(callee->base.pos, "calling impure function '%s' in a pure context", name);
    }
    // пока считаем, что fn возвращает sym->type
    for (size_t i=0;i<call->as.call.argc;i++){
        (void)check_expr(s, call->as.call.args[i]); // просто посетим для побочных ошибок
    }
    return mk(sym->type ? sym->type : &TY_Invalid, false);
}

static TExpr check_index(Sema *s, SurgeAstExpr *ix){
    TExpr base = check_expr(s, ix->as.index.base);
    TExpr idx  = check_expr(s, ix->as.index.index);
    if (base.type->kind != TY_ARRAY){
        s->had_error = true;
        surge_diag_errorf(ix->base.pos, "indexing non-array value of type %s", ty_name(base.type));
        return mk(&TY_Invalid, false);
    }
    if (rview(idx.type)->kind != TY_INT) {
        s->had_error = true;
        surge_diag_errorf(ix->as.index.index->base.pos,
            "array index must be int, got %s", ty_name(rview(idx.type)));
        return mk(&TY_Invalid, false);
    }
    return mk(base.type->elem, true); // элемент массива — lvalue
}

static SurgeAstOp arith_ops[] = { AST_OP_ADD, AST_OP_SUB, AST_OP_MUL, AST_OP_DIV, AST_OP_REM };
static bool is_arith(SurgeAstOp op){
    for (size_t i=0;i<sizeof(arith_ops)/sizeof(arith_ops[0]);i++)
        if (arith_ops[i]==op) return true;
    return false;
}

static TExpr check_expr(Sema *s, SurgeAstExpr *e){
    switch (e->base.kind){
        case AST_INT_LIT:   return mk(&TY_Int, false);
        case AST_FLOAT_LIT: return mk(&TY_Float, false);
        case AST_BOOL_LIT:  return mk(&TY_Bool, false);
        case AST_STRING_LIT:return mk(&TY_String, false);
        case AST_IDENT: {
            const char *name = e->as.ident.ident.name;
            Symbol *sym = scope_lookup(s->scope, name);
            if (!sym){
                s->had_error=true;
                surge_diag_errorf(e->base.pos, "unknown identifier '%s'", name);
                return mk(&TY_Invalid, false);
            }
            // pure context: forbid reading signals
            if (in_pure_ctx(s) && sym->kind == SYM_SIGNAL) {
                s->had_error=true;
                surge_diag_errorf(e->base.pos, "using signal '%s' is not allowed in a pure context", name);
            }
            bool lv = (sym->kind == SYM_VAR); // signals/fn are not addressable (MVP)
            return mk(sym->type, lv);
        }
        case AST_ARRAY_LIT: {
            // правило: все элементы одного типа (строгий)
            const SurgeType *elem_t = NULL;
            for (size_t i=0;i<e->as.array_lit.count;i++){
                TExpr it = check_expr(s, e->as.array_lit.items[i]);
                if (it.type->kind == TY_INVALID) continue;
                if (!elem_t) elem_t = it.type;
                else if (!ty_equal(elem_t, it.type)){
                    s->had_error=true;
                    surge_diag_errorf(e->base.pos, "array elements must have same type (%s vs %s)", ty_name(elem_t), ty_name(it.type));
                }
            }
            if (!elem_t) elem_t = &TY_Invalid;
            return mk(ty_array_of(elem_t), false);
        }
        case AST_INDEX:  return check_index(s, e);
        case AST_CALL:   return check_call(s, e);
        case AST_PAREN:  { TExpr in = check_expr(s, e->as.paren.inner); return mk(in.type, in.is_lvalue); } // скобки сохраняют lvalue
        case AST_UNARY: {
            TExpr x = check_expr(s, e->as.unary.expr);
            if (e->as.unary.op == AST_OP_NEG || e->as.unary.op == AST_OP_POS){
                const SurgeType *ux = rview(x.type);
                if (ux->kind==TY_INT || ux->kind==TY_FLOAT) return mk(ux, false);
                char opch = (e->as.unary.op == AST_OP_NEG) ? '-' : '+';
                s->had_error=true; surge_diag_errorf(e->base.pos, "unary '%c' expects int or float, got %s", opch, ty_name(x.type));
                return mk(&TY_Invalid, false);
            } else if (e->as.unary.op == AST_OP_NOT){
                const SurgeType *ux = rview(x.type);
                if (ux->kind==TY_BOOL) return mk(ux, false);
                s->had_error=true; surge_diag_errorf(e->base.pos, "unary '!' expects bool, got %s", ty_name(x.type));
                return mk(&TY_Invalid, false);
            } else if (e->as.unary.op == AST_OP_ADDR){
                // MVP: we can take address only from var/par (simple lvalue)
                if (!is_lvalue_expr(e->as.unary.expr)) {
                    s->had_error=true;
                    surge_diag_errorf(e->base.pos, "address-of '&' requires an lvalue variable/parameter");
                    return mk(&TY_Invalid, false);
                }
                // deprecate reference from signal
                if (e->as.unary.expr->base.kind == AST_IDENT) {
                    const char *nm = e->as.unary.expr->as.ident.ident.name;
                    Symbol *sym = scope_lookup(s->scope, nm);
                    if (sym && sym->kind == SYM_SIGNAL) {
                        s->had_error = true;
                        surge_diag_errorf(e->base.pos, "cannot take address of signal '%s'", nm);
                        return mk(&TY_Invalid, false);
                    }
                }
                return mk(ty_ref_of(x.type), false);
            } else if (e->as.unary.op == AST_OP_DEREF){
                // *expr — expr must be &T
                const SurgeType *ux = rview(x.type);
                if (ux->kind != TY_REF || !ux->elem) {
                    s->had_error = true;
                    surge_diag_errorf(e->base.pos, "dereference '*' expects '&T', got %s", ty_name(x.type));
                    return mk(&TY_Invalid, false);
                }
                // deref result behaves like an lvalue (so later we can assign to *p)
                return mk(ux->elem, true);
            }
            return mk(&TY_Invalid, false);
        }
        case AST_BINARY: {
            TExpr L = check_expr(s, e->as.binary.lhs);
            TExpr R = check_expr(s, e->as.binary.rhs);
            const SurgeType *Lt = rview(L.type);
            const SurgeType *Rt = rview(R.type);
            if (is_arith(e->as.binary.op)){
                const SurgeType *res = numeric_join(Lt, Rt);
                if (res != &TY_Invalid) {
                    return mk(res, false);
                }
                if (!is_numeric_type(Lt) || !is_numeric_type(Rt)) {
                    s->had_error = true;
                    surge_diag_errorf(e->base.pos, "arithmetic on non-numeric operands (%s vs %s)", ty_name(Lt), ty_name(Rt));
                    return mk(&TY_Invalid, false);
                }
                type_mismatch(s, e->base.pos, "arithmetic operands", Lt, Rt);
                return mk(&TY_Invalid, false);
            }
            switch (e->as.binary.op){
                case AST_OP_EQ:
                case AST_OP_NE: {
                    if (is_numeric_type(Lt) && is_numeric_type(Rt)) {
                        if (numeric_join(Lt, Rt) == &TY_Invalid) {
                            type_mismatch(s, e->base.pos, "numeric comparison", Lt, Rt);
                            return mk(&TY_Invalid, false);
                        }
                        return mk(&TY_Bool, false);
                    }
                    if (!ty_equal(Lt, Rt)) {
                        type_mismatch(s, e->base.pos, "comparison operands", Lt, Rt);
                        return mk(&TY_Invalid, false);
                    }
                    return mk(&TY_Bool, false);
                }
                case AST_OP_LT:
                case AST_OP_LE:
                case AST_OP_GT:
                case AST_OP_GE: {
                    const SurgeType *res = numeric_join(Lt, Rt);
                    if (res == &TY_Invalid) {
                        s->had_error = true;
                        surge_diag_errorf(e->base.pos, "ordered comparison requires numeric operands (%s vs %s)", ty_name(Lt), ty_name(Rt));
                        return mk(&TY_Invalid, false);
                    }
                    return mk(&TY_Bool, false);
                }
                case AST_OP_AND:
                case AST_OP_OR:
                    if (rview(L.type)->kind!=TY_BOOL || rview(R.type)->kind!=TY_BOOL){
                        s->had_error=true; surge_diag_errorf(e->base.pos, "logical operator requires bool operands");
                        return mk(&TY_Invalid, false);
                    }
                    return mk(&TY_Bool, false);
                default:
                    break;
            }
            return mk(&TY_Invalid, false);
        }
        case AST_BIND_EXPR: {
            // реактивная привязка — тип RHS возвращаем как тип выражения (на случай последующих проверок)
            TExpr L = check_expr(s, e->as.bind_expr.lhs);
            s->pure_depth++;
            TExpr R = check_expr(s, e->as.bind_expr.rhs);
            s->pure_depth--;
            if (!ty_equal(rview(L.type), rview(R.type))){
                type_mismatch(s, e->base.pos, "reactive bind (:=)", L.type, R.type);
            }
            return mk(rview(R.type), false);
        }
        default:
            return mk(&TY_Invalid, false);
    }
}

// ---------- stmt typing ----------

static void check_stmt(Sema *s, SurgeAstStmt *st);

static void check_block(Sema *s, SurgeAstStmt *blk){
    scope_push(s);
    for (size_t i=0;i<blk->as.block.count;i++) check_stmt(s, blk->as.block.stmts[i]);
    scope_pop(s);
}

static void declare_fn_signature(Sema *s, SurgeAstStmt *fn){
    // регистрируем функцию, чтобы она была видна до тела (single-pass достаточно)
    const char *name = fn->as.fn_decl.name.name;
    const SurgeType *ret = fn->as.fn_decl.has_ret
        ? resolve_type_ast(s, fn->as.fn_decl.ret_type_ast)
        : &TY_Invalid; // void будет задан позже, пока оставим invalid => нельзя использовать как значение

    Symbol sym = { .kind=SYM_FN, .type=ret, .is_pure=fn->as.fn_decl.is_pure, .is_global=true };
    if (!sema_insert_symbol(s, name, sym)){
        s->had_error=true;
        surge_diag_errorf(fn->base.pos, "redeclaration of '%s'", name);
    }
}

static void check_fn(Sema *s, SurgeAstStmt *fn){
    Symbol *fn_sym = scope_lookup(s->scope, fn->as.fn_decl.name.name);
    const SurgeType *ret_type = fn_sym ? fn_sym->type : &TY_Invalid;
    bool prev_has_ret = s->current_fn_has_ret;
    const SurgeType *prev_ret = s->current_ret;
    int prev_pure_depth = s->pure_depth;

    s->current_fn_has_ret = fn->as.fn_decl.has_ret;
    s->current_ret = ret_type;
    if (fn->as.fn_decl.is_pure) {
        s->pure_depth++;
    }

    scope_push(s);
    // параметры
    for (size_t i=0;i<fn->as.fn_decl.paramc;i++){
        const char *pname = fn->as.fn_decl.params[i].name.name;
        const SurgeType *pt = resolve_type_ast(s, fn->as.fn_decl.params[i].type_ast);
        if (pt->kind == TY_INVALID){
            s->had_error=true;
            surge_diag_errorf(fn->base.pos, "unknown parameter type for '%s'", pname);
        }
        Symbol sym = { .kind=SYM_VAR, .type=pt, .is_pure=false, .is_global=false };
        if (!sema_insert_symbol(s, pname, sym)){
            s->had_error=true;
            surge_diag_errorf(fn->base.pos, "duplicate parameter '%s'", pname);
        }
    }
    check_stmt(s, fn->as.fn_decl.body);

    if (fn->as.fn_decl.has_ret) {
        bool ensured = fn->as.fn_decl.body ? stmt_guarantees_return(fn->as.fn_decl.body) : false;
        if (!ensured) {
            s->had_error = true;
            surge_diag_errorf(fn->base.pos, "not all paths return a value in function '%s'", fn->as.fn_decl.name.name);
        }
    }
    scope_pop(s);

    if (fn->as.fn_decl.is_pure) {
        s->pure_depth = prev_pure_depth;
    }
    s->current_fn_has_ret = prev_has_ret;
    s->current_ret = prev_ret;
}

static void check_stmt(Sema *s, SurgeAstStmt *st){
    switch (st->base.kind){
        case AST_IMPORT:
            // skip for now
            break;
        case AST_LET_DECL: {
            if (!st->as.let_decl.has_type){
                s->had_error=true;
                surge_diag_errorf(st->base.pos, "let requires explicit type (no inference yet)");
            }
            const SurgeType *decl = st->as.let_decl.has_type
                ? resolve_type_ast(s, st->as.let_decl.type_ast)
                : &TY_Invalid;
            TExpr init = check_expr(s, st->as.let_decl.init);
            if (decl->kind == TY_INVALID){
                s->had_error=true; surge_diag_errorf(st->base.pos, "unknown declared type");
            } else if (init.type->kind != TY_INVALID && !can_assign_to(decl, init.type)){
                type_mismatch(s, st->base.pos, "let initializer", decl, init.type);
            }
            bool is_global = (s->scope && s->scope->parent == NULL);
            Symbol sym = { .kind=SYM_VAR, .type=decl, .is_pure=false, .is_global=is_global };
            if (!sema_insert_symbol(s, st->as.let_decl.name.name, sym)){
                s->had_error=true; surge_diag_errorf(st->base.pos, "redeclaration of '%s'", st->as.let_decl.name.name);
            }
            break;
        }
        case AST_SIGNAL_DECL: {
            TExpr init = check_expr(s, st->as.signal_decl.init);
            bool is_global = (s->scope && s->scope->parent == NULL);
            Symbol sym = { .kind=SYM_SIGNAL, .type=init.type, .is_pure=false, .is_global=is_global };
            if (!sema_insert_symbol(s, st->as.signal_decl.name.name, sym)){
                s->had_error=true; surge_diag_errorf(st->base.pos, "redeclaration of '%s'", st->as.signal_decl.name.name);
            }
            break;
        }
        case AST_ASSIGN_STMT: {
            Symbol *sym = scope_lookup(s->scope, st->as.assign_stmt.name.name);
            if (!sym){ s->had_error=true; surge_diag_errorf(st->base.pos, "unknown variable '%s'", st->as.assign_stmt.name.name); break; }
            if (sym->kind == SYM_SIGNAL){
                s->had_error=true; surge_diag_errorf(st->base.pos, "cannot assign to signal '%s' (use ':=' for reactive bind)", st->as.assign_stmt.name.name);
                break;
            }
            TExpr rhs = check_expr(s, st->as.assign_stmt.expr);
            if (rhs.type->kind != TY_INVALID && !can_assign_to(sym->type, rhs.type)){
                type_mismatch(s, st->base.pos, "assignment", sym->type, rhs.type);
            }
            if (in_pure_ctx(s) && sym->is_global) {
                s->had_error = true;
                surge_diag_errorf(st->base.pos, "cannot assign to global '%s' in a pure context", st->as.assign_stmt.name.name);
            }
            break;
        }
        case AST_EXPR_STMT:
            (void)check_expr(s, st->as.expr_stmt.expr);
            break;
        case AST_BLOCK:
            check_block(s, st);
            break;
        case AST_IF:
            if (check_expr(s, st->as.if_stmt.cond).type->kind != TY_BOOL){
                s->had_error=true; surge_diag_errorf(st->base.pos, "if condition must be bool");
            }
            check_stmt(s, st->as.if_stmt.then_blk);
            if (st->as.if_stmt.has_else) check_stmt(s, st->as.if_stmt.else_blk);
            break;
        case AST_WHILE:
            if (check_expr(s, st->as.while_stmt.cond).type->kind != TY_BOOL){
                s->had_error=true; surge_diag_errorf(st->base.pos, "while condition must be bool");
            }
            check_stmt(s, st->as.while_stmt.body);
            break;
        case AST_RETURN:
            if (s->current_fn_has_ret) {
                if (!st->as.return_stmt.has_value) {
                    s->had_error = true;
                    surge_diag_errorf(st->base.pos, "missing return value");
                } else {
                    TExpr val = check_expr(s, st->as.return_stmt.value);
                    if (val.type->kind != TY_INVALID && !can_assign_to(s->current_ret, val.type)) {
                        type_mismatch(s, st->base.pos, "return", s->current_ret, val.type);
                    }
                }
            } else {
                if (st->as.return_stmt.has_value) {
                    TExpr val = check_expr(s, st->as.return_stmt.value);
                    (void)val;
                    s->had_error = true;
                    surge_diag_errorf(st->base.pos, "return with a value in function returning void");
                }
            }
            break;
        case AST_FN_DECL:
            // сигнатуры объявлены заранее (см. prepass)
            check_fn(s, st);
            break;
        case AST_PAR_MAP: {
            // callee expr должен быть идентификатором функции
            if (st->as.par_map.fn_or_ident->base.kind != AST_IDENT){
                s->had_error=true; surge_diag_errorf(st->base.pos, "parallel map: callee must be function identifier");
            } else {
                const char *name = st->as.par_map.fn_or_ident->as.ident.ident.name;
                Symbol *sym = scope_lookup(s->scope, name);
                if (!sym || sym->kind != SYM_FN){
                    s->had_error=true; surge_diag_errorf(st->base.pos, "parallel map: unknown function '%s'", name);
                }
            }
            // callee must be pure even outside a pure context
            if (st->as.par_map.fn_or_ident->base.kind == AST_IDENT){
                const char *name = st->as.par_map.fn_or_ident->as.ident.ident.name;
                Symbol *sym = scope_lookup(s->scope, name);
                if (sym && sym->kind == SYM_FN && !sym->is_pure) {
                    s->had_error = true;
                    surge_diag_errorf(st->base.pos, "parallel map requires a pure function, '%s' is impure", name);
                }
            }
            (void)check_expr(s, st->as.par_map.seq);
            break;
        }
        case AST_PAR_REDUCE: {
            // 1) Тип последовательности должен быть массивом
            TExpr seq = check_expr(s, st->as.par_reduce.seq);
            if (seq.type->kind != TY_ARRAY) {
                s->had_error = true;
                surge_diag_errorf(st->base.pos, "reduce: sequence must be an array, got %s", ty_name(seq.type));
                break;
            }
            const SurgeType *elem_t = seq.type->elem;

            // 2) Типы параметров (acc:int, v:int)
            const SurgeType *acc_t = resolve_type_ast(s, st->as.par_reduce.acc.type_ast);
            const SurgeType *v_t   = resolve_type_ast(s, st->as.par_reduce.v.type_ast);

            if (acc_t->kind == TY_INVALID) {
                s->had_error = true;
                surge_diag_errorf(st->base.pos, "reduce: invalid type for 'acc'");
            }
            if (v_t->kind == TY_INVALID) {
                s->had_error = true;
                surge_diag_errorf(st->base.pos, "reduce: invalid type for 'v'");
            }

            // 3) v должен совпадать с элементом массива
            if (!ty_equal(v_t, elem_t)) {
                s->had_error = true;
                surge_diag_errorf(st->base.pos, "reduce: 'v' type must match element type (%s vs %s)", ty_name(v_t), ty_name(elem_t));
            }

            // 4) Лексическая область параметров тела: (acc, v) видимы только в лямбде
            scope_push(s);
            (void)scope_insert_current(s->scope, st->as.par_reduce.acc.name.name, (Symbol){ .kind=SYM_VAR, .type=acc_t, .is_pure=false, .is_global=false });
            (void)scope_insert_current(s->scope, st->as.par_reduce.v.name.name,   (Symbol){ .kind=SYM_VAR, .type=v_t, .is_pure=false, .is_global=false });
            s->pure_depth++;
            TExpr body = check_expr(s, st->as.par_reduce.body);
            s->pure_depth--;
            scope_pop(s);

            // 5) Результат тела должен совпадать с типом аккумулятора
            if (!ty_equal(body.type, acc_t)) {
                s->had_error = true;
                surge_diag_errorf(st->as.par_reduce.body->base.pos,
                    "reduce: body result type must equal accumulator type (%s vs %s)",
                    ty_name(body.type), ty_name(acc_t));
            }
            break;
        }
        case AST_TYPEDEF: {
            const char *nm = st->as.typedef_decl.name.name;
            const SurgeType *aliased = resolve_type_ast(s, st->as.typedef_decl.aliased);
            if (aliased->kind==TY_INVALID){ s->had_error=true; surge_diag_errorf(st->base.pos,"invalid aliased type"); break; }
            // insert alias
            if (s->alias_n==s->alias_cap){ s->alias_cap=s->alias_cap?2*s->alias_cap:8; s->aliases=realloc(s->aliases, s->alias_cap*sizeof(*s->aliases)); }
            s->aliases[s->alias_n].name=strdup(nm);
            s->aliases[s->alias_n].type=aliased;
            s->alias_n++;
            break;
        }        
        default:
            break;
    }
}

static bool stmt_guarantees_return(const SurgeAstStmt *st) {
    if (!st) return false;
    switch (st->base.kind) {
        case AST_RETURN:
            return true;
        case AST_BLOCK: {
            for (size_t i = 0; i < st->as.block.count; ++i) {
                if (stmt_guarantees_return(st->as.block.stmts[i])) {
                    return true;
                }
            }
            return false;
        }
        case AST_IF:
            if (!st->as.if_stmt.has_else) return false;
            return stmt_guarantees_return(st->as.if_stmt.then_blk) &&
                   stmt_guarantees_return(st->as.if_stmt.else_blk);
        case AST_WHILE:
            return false; // conservative: loops may not execute
        case AST_EXPR_STMT:
        case AST_LET_DECL:
        case AST_SIGNAL_DECL:
        case AST_ASSIGN_STMT:
        case AST_PAR_MAP:
        case AST_PAR_REDUCE:
        case AST_IMPORT:
        case AST_TYPEDEF:
        case AST_FN_DECL:
            return false;
        default:
            return false;
    }
}

void sema_init(Sema *sema){ memset(sema, 0, sizeof(*sema)); sema->shadow = SHADOW_DENY; }
void sema_destroy(Sema *sema){
    while (sema->scope) scope_pop(sema);

    if (sema->aliases) {
        for (size_t i = 0; i < sema->alias_n; i++) {
            free(sema->aliases[i].name);
            // sema->aliases[i].type points to global or cached types — не освобождаем
        }
        free(sema->aliases);
        sema->aliases = NULL;
        sema->alias_n = sema->alias_cap = 0;
    }
}

bool sema_check_unit(Sema *sema, SurgeAstUnit *unit){
    sema->had_error = false;
    scope_push(sema);

    // prepass: объявим все функции (чтобы были видны до определения)
    for (size_t i=0;i<unit->count;i++){
        SurgeAstStmt *st = unit->decls[i];
        if (st->base.kind == AST_FN_DECL) declare_fn_signature(sema, st);
    }

    // проход: проверки
    for (size_t i=0;i<unit->count;i++){
        check_stmt(sema, unit->decls[i]);
    }

    scope_pop(sema);
    return !sema->had_error;
}
