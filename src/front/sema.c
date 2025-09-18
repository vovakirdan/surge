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
static bool scope_insert(SemaScope *sc, const char *name, Symbol sym){
    // запрет шадовинга в пределах одного scope (разрешён — в родителях? пока запретим шадовинг вообще)
    for (SemaScope *p=sc; p; p=p->parent){
        for (size_t i=0;i<p->n;i++){
            if (strcmp(p->entries[i].name, name)==0){
                return false;
            }
        }
        break; // запрет только в текущем скоупе
    }
    if (sc->n == sc->cap){
        sc->cap = sc->cap? sc->cap*2 : 8;
        sc->entries = (__typeof__(sc->entries))realloc(sc->entries, sc->cap*sizeof(*sc->entries));
    }
    sc->entries[sc->n].name = strdup(name);
    sc->entries[sc->n].sym = sym;
    sc->n++;
    return true;
}

static const SurgeType *alias_lookup(Sema *s, const char *name){
    for (size_t i=0;i<s->alias_n;i++) if (strcmp(s->aliases[i].name,name)==0) return s->aliases[i].type;
    return NULL;
}

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
} TExpr;

static TExpr check_expr(Sema *s, SurgeAstExpr *e);

static void type_mismatch(Sema *s, SurgeSrcPos pos, const char *ctx, const SurgeType *a, const SurgeType *b){
    s->had_error = true;
    surge_diag_errorf(pos, "%s: type mismatch (%s vs %s)", ctx, ty_name(a), ty_name(b));
}

static TExpr mk(const SurgeType *t){ TExpr x; x.type=t; return x; }

static TExpr check_call(Sema *s, SurgeAstExpr *call){
    // MVP: callee должен быть идентификатором функции; аргументы не проверяем по сигнатуре (позже)
    SurgeAstExpr *callee = call->as.call.callee;
    if (callee->base.kind != AST_IDENT){
        s->had_error=true;
        surge_diag_errorf(callee->base.pos, "callee is not an identifier");
        return mk(&TY_Invalid);
    }
    const char *name = callee->as.ident.ident.name;
    Symbol *sym = scope_lookup(s->scope, name);
    if (!sym || sym->kind != SYM_FN){
        s->had_error=true;
        surge_diag_errorf(callee->base.pos, "unknown function '%s'", name);
        return mk(&TY_Invalid);
    }
    // пока считаем, что fn возвращает sym->type
    for (size_t i=0;i<call->as.call.argc;i++){
        (void)check_expr(s, call->as.call.args[i]); // просто посетим для побочных ошибок
    }
    return mk(sym->type ? sym->type : &TY_Invalid);
}

static TExpr check_index(Sema *s, SurgeAstExpr *ix){
    TExpr base = check_expr(s, ix->as.index.base);
    (void)check_expr(s, ix->as.index.index);
    if (base.type->kind != TY_ARRAY){
        s->had_error=true;
        surge_diag_errorf(ix->base.pos, "indexing non-array value of type %s", ty_name(base.type));
        return mk(&TY_Invalid);
    }
    return mk(base.type->elem);
}

static SurgeAstOp arith_ops[] = { AST_OP_ADD, AST_OP_SUB, AST_OP_MUL, AST_OP_DIV, AST_OP_REM };
static bool is_arith(SurgeAstOp op){
    for (size_t i=0;i<sizeof(arith_ops)/sizeof(arith_ops[0]);i++)
        if (arith_ops[i]==op) return true;
    return false;
}

// fast check: expression - is it a just lvalue-variable?
static bool is_plain_lvalue_ident(SurgeAstExpr *e) {
    return e && e->base.kind == AST_IDENT;
}

static TExpr check_expr(Sema *s, SurgeAstExpr *e){
    switch (e->base.kind){
        case AST_INT_LIT:   return mk(&TY_Int);
        case AST_FLOAT_LIT: return mk(&TY_Float);
        case AST_BOOL_LIT:  return mk(&TY_Bool);
        case AST_STRING_LIT:return mk(&TY_String);
        case AST_IDENT: {
            const char *name = e->as.ident.ident.name;
            Symbol *sym = scope_lookup(s->scope, name);
            if (!sym){
                s->had_error=true;
                surge_diag_errorf(e->base.pos, "unknown identifier '%s'", name);
                return mk(&TY_Invalid);
            }
            return mk(sym->type);
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
            return mk(ty_array_of(elem_t));
        }
        case AST_INDEX:  return check_index(s, e);
        case AST_CALL:   return check_call(s, e);
        case AST_PAREN:  return check_expr(s, e->as.paren.inner);
        case AST_UNARY: {
            TExpr x = check_expr(s, e->as.unary.expr);
            if (e->as.unary.op == AST_OP_NEG){
                if (x.type->kind==TY_INT || x.type->kind==TY_FLOAT) return x;
                s->had_error=true; surge_diag_errorf(e->base.pos, "unary '-' expects int or float, got %s", ty_name(x.type));
                return mk(&TY_Invalid);
            } else if (e->as.unary.op == AST_OP_NOT){
                if (x.type->kind==TY_BOOL) return x;
                s->had_error=true; surge_diag_errorf(e->base.pos, "unary '!' expects bool, got %s", ty_name(x.type));
                return mk(&TY_Invalid);
            } else if (e->as.unary.op == AST_OP_ADDR){
                // MVP: we can take address only from var/par (simple lvalue)
                if (!is_plain_lvalue_ident(e->as.unary.expr)) {
                    s->had_error=true;
                    surge_diag_errorf(e->base.pos, "address-of '&' requires an lvalue variable/parameter");
                    return mk(&TY_Invalid);
                }
                // deprecate reference from signal
                const char *nm = e->as.unary.expr->as.ident.ident.name;
                Symbol *sym = scope_lookup(s->scope, nm);
                if (!sym) {
                    s->had_error = true;
                    surge_diag_errorf(e->base.pos, "unknown identifier '%s' in address-of", nm);
                    return mk(&TY_Invalid);
                }
                if (sym->kind == SYM_SIGNAL) {
                    s->had_error = true;
                    surge_diag_errorf(e->base.pos, "cannot take address of signal '%s'", nm);
                    return mk(&TY_Invalid);
                }
                return mk(ty_ref_of(sym->type));
            } else if (e->as.unary.op == AST_OP_DEREF){
                if (x.type->kind != TY_REF) {
                    s->had_error = true;
                    surge_diag_errorf(e->base.pos, "dereference '*' expects '&T', got %s", ty_name(x.type));
                    return mk(&TY_Invalid);
                }
                return mk(x.type->elem);
            }
            return mk(&TY_Invalid);
        }
        case AST_BINARY: {
            TExpr L = check_expr(s, e->as.binary.lhs);
            TExpr R = check_expr(s, e->as.binary.rhs);
            // НЕТ автоприведения (MVP)
            if (is_arith(e->as.binary.op)){
                if (!ty_equal(L.type, R.type)){
                    type_mismatch(s, e->base.pos, "arithmetic operands", L.type, R.type);
                    return mk(&TY_Invalid);
                }
                if (L.type->kind==TY_INT || L.type->kind==TY_FLOAT) return L;
                s->had_error=true; surge_diag_errorf(e->base.pos, "arithmetic on non-numeric type %s", ty_name(L.type));
                return mk(&TY_Invalid);
            }
            // сравнения -> bool, операнды одного типа
            switch (e->as.binary.op){
                case AST_OP_EQ: case AST_OP_NE: case AST_OP_LT: case AST_OP_LE: case AST_OP_GT: case AST_OP_GE:
                    if (!ty_equal(L.type, R.type)){
                        type_mismatch(s, e->base.pos, "comparison operands", L.type, R.type);
                        return mk(&TY_Invalid);
                    }
                    return mk(&TY_Bool);
                case AST_OP_AND: case AST_OP_OR:
                    if (L.type->kind!=TY_BOOL || R.type->kind!=TY_BOOL){
                        s->had_error=true; surge_diag_errorf(e->base.pos, "logical operator requires bool operands");
                        return mk(&TY_Invalid);
                    }
                    return mk(&TY_Bool);
                default: break;
            }
            return mk(&TY_Invalid);
        }
        case AST_BIND_EXPR: {
            // реактивная привязка — тип RHS возвращаем как тип выражения (на случай последующих проверок)
            TExpr L = check_expr(s, e->as.bind_expr.lhs);
            TExpr R = check_expr(s, e->as.bind_expr.rhs);
            if (!ty_equal(L.type, R.type)){
                type_mismatch(s, e->base.pos, "reactive bind (:=)", L.type, R.type);
            }
            return R;
        }
        default:
            return mk(&TY_Invalid);
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

    Symbol sym = { .kind=SYM_FN, .type=ret };
    if (!scope_insert(s->scope, name, sym)){
        s->had_error=true;
        surge_diag_errorf(fn->base.pos, "redeclaration of '%s'", name);
    }
}

static void check_fn(Sema *s, SurgeAstStmt *fn){
    scope_push(s);
    // параметры
    for (size_t i=0;i<fn->as.fn_decl.paramc;i++){
        const char *pname = fn->as.fn_decl.params[i].name.name;
        const SurgeType *pt = resolve_type_ast(s, fn->as.fn_decl.params[i].type_ast);
        if (pt->kind == TY_INVALID){
            s->had_error=true;
            surge_diag_errorf(fn->base.pos, "unknown parameter type for '%s'", pname);
        }
        Symbol sym = { .kind=SYM_VAR, .type=pt };
        if (!scope_insert(s->scope, pname, sym)){
            s->had_error=true;
            surge_diag_errorf(fn->base.pos, "duplicate parameter '%s'", pname);
        }
    }
    check_stmt(s, fn->as.fn_decl.body);
    scope_pop(s);
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
            } else if (!ty_equal(decl, init.type)){
                type_mismatch(s, st->base.pos, "let initializer", decl, init.type);
            }
            Symbol sym = { .kind=SYM_VAR, .type=decl };
            if (!scope_insert(s->scope, st->as.let_decl.name.name, sym)){
                s->had_error=true; surge_diag_errorf(st->base.pos, "redeclaration of '%s'", st->as.let_decl.name.name);
            }
            break;
        }
        case AST_SIGNAL_DECL: {
            TExpr init = check_expr(s, st->as.signal_decl.init);
            Symbol sym = { .kind=SYM_SIGNAL, .type=init.type };
            if (!scope_insert(s->scope, st->as.signal_decl.name.name, sym)){
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
            if (!ty_equal(sym->type, rhs.type)){
                type_mismatch(s, st->base.pos, "assignment", sym->type, rhs.type);
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
            // минимально: разрешаем везде; проверка соответствия ret-типа будет позже, когда добавим контроль текущей fn
            if (st->as.return_stmt.has_value) (void)check_expr(s, st->as.return_stmt.value);
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
            (void)scope_insert(s->scope, st->as.par_reduce.acc.name.name, (Symbol){ .kind=SYM_VAR, .type=acc_t });
            (void)scope_insert(s->scope, st->as.par_reduce.v.name.name,   (Symbol){ .kind=SYM_VAR, .type=v_t });

            TExpr body = check_expr(s, st->as.par_reduce.body);

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

void sema_init(Sema *sema){ memset(sema, 0, sizeof(*sema)); }
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
