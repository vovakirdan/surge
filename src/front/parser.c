#include "parser.h"
#include "lexer.h"
#include "token.h"
#include "diagnostics.h"
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <assert.h>

static SurgeAstExpr *parse_expr(SurgeParser *ps);
static SurgeAstStmt *parse_stmt(SurgeParser *ps);
static SurgeAstType *parse_type(SurgeParser *ps);

// --- helpers ---

static void parser_advance(SurgeParser *ps) {
    surge_token_free(&ps->cur);
    ps->cur = surge_lexer_next(ps->lx);
}

static bool parser_expect(SurgeParser *ps, SurgeTokenKind k, const char *what) {
    if (ps->cur.kind != k) {
        ps->had_error = true;
        surge_diag_errorf(ps->cur.pos, "Expected %s, got %s", what, surge_token_kind_cstr(ps->cur.kind));
        return false;
    }
    parser_advance(ps);
    return true;
}

static bool is_token(SurgeParser *ps, SurgeTokenKind k) { return ps->cur.kind == k; }

static char *dup_lex(const char *s) {
    size_t n = s ? strlen(s) : 0;
    char *p = (char*)malloc(n + 1);
    if (!p) return NULL;
    memcpy(p, s, n);
    p[n] = '\0';
    return p;
}

static SurgeAstIdent make_ident_from_tok(SurgeToken t) {
    SurgeAstIdent id = {0};
    id.name = dup_lex(t.lexeme ? t.lexeme : "");
    return id;
}

static SurgeAstExpr *mk_expr(SurgeAstKind k, SurgeSrcPos pos) {
    SurgeAstExpr *e = (SurgeAstExpr*)calloc(1,sizeof(SurgeAstExpr));
    e->base.kind = k; e->base.pos = pos; return e;
}
static SurgeAstStmt *mk_stmt(SurgeAstKind k, SurgeSrcPos pos) {
    SurgeAstStmt *s = (SurgeAstStmt*)calloc(1,sizeof(SurgeAstStmt));
    s->base.kind = k; s->base.pos = pos; return s;
}
static SurgeAstType *mk_type(SurgeTypeAstKind k, SurgeSrcPos pos){
    SurgeAstType *t = (SurgeAstType*)calloc(1,sizeof(*t));
    t->base.kind = (SurgeAstKind)0; // not used for type nodes
    t->base.pos = pos;
    t->kind = k;
    return t;
}

// precedence
static int precedence_of(SurgeAstOp op) {
    switch (op){
        case AST_OP_OR:  return 1;
        case AST_OP_AND: return 2;
        case AST_OP_EQ: case AST_OP_NE: return 3;
        case AST_OP_LT: case AST_OP_LE: case AST_OP_GT: case AST_OP_GE: return 4;
        case AST_OP_ADD: case AST_OP_SUB: return 5;
        case AST_OP_MUL: case AST_OP_DIV: case AST_OP_REM: return 6;
        default: return 0;
    }
}

static SurgeAstOp binop_from_token(SurgeTokenKind k) {
    switch (k) {
        case TOK_PLUS: return AST_OP_ADD;
        case TOK_MINUS: return AST_OP_SUB;
        case TOK_STAR: return AST_OP_MUL;
        case TOK_SLASH: return AST_OP_DIV;
        case TOK_PERCENT: return AST_OP_REM;
        case TOK_EQ: return AST_OP_EQ;
        case TOK_NE: return AST_OP_NE;
        case TOK_LT: return AST_OP_LT;
        case TOK_LE: return AST_OP_LE;
        case TOK_GT: return AST_OP_GT;
        case TOK_GE: return AST_OP_GE;
        case TOK_AND_AND: return AST_OP_AND;
        case TOK_OR_OR: return AST_OP_OR;
        default: return AST_OP_NONE;
    }
}

// Parse array literal ONLY when current token is '[' (without treating it as postfix).
static SurgeAstExpr *parse_array_literal_only(SurgeParser *ps){
    SurgeSrcPos pos = ps->cur.pos;
    (void)parser_expect(ps, TOK_LBRACKET, "'['");
    SurgeAstExpr **items = NULL; size_t cap=0, cnt=0;
    if (!is_token(ps, TOK_RBRACKET)) {
        for (;;) {
            SurgeAstExpr *it = parse_expr(ps);
            if (cnt==cap){ cap=cap?cap*2:4; items=(SurgeAstExpr**)realloc(items, cap*sizeof(*items)); }
            items[cnt++] = it;
            if (is_token(ps, TOK_COMMA)) { parser_advance(ps); continue; }
            break;
        }
    }
    (void)parser_expect(ps, TOK_RBRACKET, "']'");
    SurgeAstExpr *e = mk_expr(AST_ARRAY_LIT, pos);
    e->as.array_lit.items = items;
    e->as.array_lit.count = cnt;
    return e;
}

// Парсит только базовое выражение БЕЗ постфиксных операций (вызовов, индексации, bind)
static SurgeAstExpr *parse_base_expr(SurgeParser *ps) {
    SurgeAstExpr *e = NULL;
    SurgeSrcPos pos = ps->cur.pos;
    if (is_token(ps, TOK_INT)) {
        e = mk_expr(AST_INT_LIT, pos);
        e->as.int_lit.v = ps->cur.int_value;
        parser_advance(ps);
    } else if (is_token(ps, TOK_FLOAT)) {
        e = mk_expr(AST_FLOAT_LIT, pos);
        e->as.float_lit.v = ps->cur.float_value;
        parser_advance(ps);
    } else if (is_token(ps, TOK_KW_TRUE) || is_token(ps, TOK_KW_FALSE)) {
        e = mk_expr(AST_BOOL_LIT, pos);
        e->as.bool_lit.v = is_token(ps, TOK_KW_TRUE);
        parser_advance(ps);
    } else if (is_token(ps, TOK_STRING)) {
        e = mk_expr(AST_STRING_LIT, pos);
        e->as.string_lit.v = dup_lex(ps->cur.lexeme);
        parser_advance(ps);
    } else if (is_token(ps, TOK_IDENTIFIER)) {
        e = mk_expr(AST_IDENT, pos);
        e->as.ident.ident = make_ident_from_tok(ps->cur);
        parser_advance(ps);
    } else if (is_token(ps, TOK_LPAREN)) {
        parser_advance(ps);
        SurgeAstExpr *inner = parse_expr(ps);
        (void)parser_expect(ps, TOK_RPAREN, "')'");
        e = mk_expr(AST_PAREN, pos);
        e->as.paren.inner = inner;
    } else {
        surge_diag_errorf(ps->cur.pos, "Unexpected token in base expression: %s", surge_token_kind_cstr(ps->cur.kind));
        ps->had_error = true;
        // error recovery: create dummy int literal
        e = mk_expr(AST_INT_LIT, pos); e->as.int_lit.v = 0;
        parser_advance(ps);
    }
    // НЕ обрабатываем постфиксные операции!
    return e;
}

// --- Primary / postfix ---

static SurgeAstExpr *parse_primary(SurgeParser *ps) {
    SurgeAstExpr *e = NULL;
    SurgeSrcPos pos = ps->cur.pos;
    if (is_token(ps, TOK_INT)) {
        e = mk_expr(AST_INT_LIT, pos);
        e->as.int_lit.v = ps->cur.int_value;
        parser_advance(ps);
    } else if (is_token(ps, TOK_FLOAT)) {
        e = mk_expr(AST_FLOAT_LIT, pos);
        e->as.float_lit.v = ps->cur.float_value;
        parser_advance(ps);
    } else if (is_token(ps, TOK_KW_TRUE) || is_token(ps, TOK_KW_FALSE)) {
        e = mk_expr(AST_BOOL_LIT, pos);
        e->as.bool_lit.v = is_token(ps, TOK_KW_TRUE);
        parser_advance(ps);
    } else if (is_token(ps, TOK_STRING)) {
        e = mk_expr(AST_STRING_LIT, pos);
        e->as.string_lit.v = dup_lex(ps->cur.lexeme);
        parser_advance(ps);
    } else if (is_token(ps, TOK_IDENTIFIER)) {
        e = mk_expr(AST_IDENT, pos);
        e->as.ident.ident = make_ident_from_tok(ps->cur);
        parser_advance(ps);
    } else if (is_token(ps, TOK_LPAREN)) {
        parser_advance(ps);
        SurgeAstExpr *inner = parse_expr(ps);
        (void)parser_expect(ps, TOK_RPAREN, "')'");
        e = mk_expr(AST_PAREN, pos);
        e->as.paren.inner = inner;
    } else if (is_token(ps, TOK_LBRACKET)) {
        // array literal: [e1, e2, ...]
        parser_advance(ps);
        SurgeAstExpr **items = NULL; size_t cap=0, cnt=0;
        if (!is_token(ps, TOK_RBRACKET)) {
            for (;;) {
                SurgeAstExpr *it = parse_expr(ps);
                if (cnt==cap){ cap=cap?cap*2:4; items=(SurgeAstExpr**)realloc(items, cap*sizeof(*items)); }
                items[cnt++]=it;
                if (is_token(ps, TOK_COMMA)){ parser_advance(ps); continue; }
                break;
            }
        }
        (void)parser_expect(ps, TOK_RBRACKET, "']'");
        e = mk_expr(AST_ARRAY_LIT, pos);
        e->as.array_lit.items = items;
        e->as.array_lit.count = cnt;
    } else {
        surge_diag_errorf(ps->cur.pos, "Unexpected token in primary: %s", surge_token_kind_cstr(ps->cur.kind));
        ps->had_error = true;
        // error recovery: create dummy int literal
        e = mk_expr(AST_INT_LIT, pos); e->as.int_lit.v = 0;
        parser_advance(ps);
    }
    // Postfix: calls, indexing, bind-expr (lhs := rhs) only when lhs is ident/expr
    for (;;) {
        if (is_token(ps, TOK_LPAREN)) {
            // call
            SurgeAstExpr *call = mk_expr(AST_CALL, e->base.pos);
            call->as.call.callee = e;
            parser_advance(ps); // (
            // args
            SurgeAstExpr **args=NULL; size_t cap=0, cnt=0;
            if (!is_token(ps, TOK_RPAREN)) {
                for (;;) {
                    SurgeAstExpr *a = parse_expr(ps);
                    if (cnt==cap) { cap=cap?cap*2:4; args=(SurgeAstExpr**)realloc(args, cap*sizeof(*args)); }
                    args[cnt++]=a;
                    if (is_token(ps, TOK_COMMA)) { parser_advance(ps); continue; }
                    break;
                }
            }
            (void) parser_expect(ps, TOK_RPAREN, "')'");
            call->as.call.args=args; call->as.call.argc=cnt;
            e = call;
            continue;
        }
        if (is_token(ps, TOK_LBRACKET)) {
            parser_advance(ps);
            SurgeAstExpr *idx = parse_expr(ps);
            (void)parser_expect(ps, TOK_RBRACKET, "']'");
            SurgeAstExpr *ix = mk_expr(AST_INDEX, e->base.pos);
            ix->as.index.base = e;
            ix->as.index.index = idx;
            e = ix;
            continue;
        }
        if (is_token(ps, TOK_BIND)) {
            // lhs := rhs
            parser_advance(ps);
            SurgeAstExpr *rhs = parse_expr(ps);
            SurgeAstExpr *be = mk_expr(AST_BIND_EXPR, e->base.pos);
            be->as.bind_expr.lhs = e;
            be->as.bind_expr.rhs = rhs;
            e = be;
            continue;
        }
        break;
    }
    return e;
}

// --- Unary ---

static SurgeAstExpr *parse_unary(SurgeParser *ps){
    if (is_token(ps, TOK_MINUS)) {
        SurgeSrcPos pos = ps->cur.pos; parser_advance(ps);
        SurgeAstExpr *e = parse_unary(ps);
        SurgeAstExpr *u = mk_expr(AST_UNARY, pos);
        u->as.unary.op = AST_OP_NEG; u->as.unary.expr = e; return u;
    }
    if (is_token(ps, TOK_BANG)) {
        SurgeSrcPos pos = ps->cur.pos; parser_advance(ps);
        SurgeAstExpr *e = parse_unary(ps);
        SurgeAstExpr *u = mk_expr(AST_UNARY, pos);
        u->as.unary.op = AST_OP_NOT; u->as.unary.expr = e; return u;
    }
    return parse_primary(ps);
}

// --- Binary (precedence climbing) ---

static SurgeAstExpr *parse_bin_rhs(SurgeParser *ps, int min_prec, SurgeAstExpr *lhs) {
    for (;;) {
        SurgeAstOp op = binop_from_token(ps->cur.kind);
        int prec = precedence_of(op);
        if (prec < min_prec) break;
        SurgeSrcPos pos = ps->cur.pos;
        parser_advance(ps);
        SurgeAstExpr *rhs = parse_unary(ps);
        // handle right-associative if needed (none here)
        SurgeAstOp next_op = binop_from_token(ps->cur.kind);
        int next_prec = precedence_of(next_op);
        if (next_prec > prec) {
            rhs = parse_bin_rhs(ps, prec+1, rhs);
        }
        SurgeAstExpr *bin = mk_expr(AST_BINARY, pos);
        bin->as.binary.op = op; bin->as.binary.lhs = lhs; bin->as.binary.rhs = rhs;
        lhs = bin;
    }
    return lhs;
}

static SurgeAstExpr *parse_expr(SurgeParser *ps){
    SurgeAstExpr *lhs = parse_unary(ps);
    return parse_bin_rhs(ps, 1, lhs);
}

// --- Statements & Declarations ---

static SurgeAstStmt *parse_let(SurgeParser *ps) {
    SurgeSrcPos pos = ps->cur.pos;
    parser_advance(ps); // let
    if (!is_token(ps, TOK_IDENTIFIER)) { surge_diag_errorf(ps->cur.pos, "Expected identifier after 'let'"); ps->had_error=true; }
    SurgeAstIdent name = make_ident_from_tok(ps->cur); parser_advance(ps);

    bool has_type=false; SurgeAstType *type_ast=NULL;
    if (is_token(ps, TOK_COLON)) {
        parser_advance(ps);
        type_ast = parse_type(ps);
        has_type = true;
    }
    (void)parser_expect(ps, TOK_ASSIGN, "'='");
    SurgeAstExpr *init = parse_expr(ps);
    (void)parser_expect(ps, TOK_SEMICOLON, "';'");
    SurgeAstStmt *s = mk_stmt(AST_LET_DECL, pos);
    s->as.let_decl.name = name; s->as.let_decl.has_type=has_type; s->as.let_decl.type_ast=type_ast; s->as.let_decl.init=init;
    return s;
}

static SurgeAstStmt *parse_signal(SurgeParser *ps){
    SurgeSrcPos pos = ps->cur.pos;
    parser_advance(ps); // signal
    if (!is_token(ps, TOK_IDENTIFIER)) { surge_diag_errorf(ps->cur.pos, "Expected identifier after 'signal'"); ps->had_error=true; }
    SurgeAstIdent name = make_ident_from_tok(ps->cur); parser_advance(ps);
    (void)parser_expect(ps, TOK_ASSIGN, "'='");
    SurgeAstExpr *init = parse_expr(ps);
    (void)parser_expect(ps, TOK_SEMICOLON, "';'");
    SurgeAstStmt *s = mk_stmt(AST_SIGNAL_DECL, pos);
    s->as.signal_decl.name = name; s->as.signal_decl.init = init; return s;
}

static SurgeAstStmt *parse_return(SurgeParser *ps){
    SurgeSrcPos pos = ps->cur.pos;
    parser_advance(ps); // return
    SurgeAstStmt *s = mk_stmt(AST_RETURN, pos);
    if (!is_token(ps, TOK_SEMICOLON)) {
        s->as.return_stmt.has_value = true;
        s->as.return_stmt.value = parse_expr(ps);
    }
    (void)parser_expect(ps, TOK_SEMICOLON, "';'");
    return s;
}

static SurgeAstStmt *parse_block(SurgeParser *ps){
    SurgeSrcPos pos = ps->cur.pos;
    (void)parser_expect(ps, TOK_LBRACE, "'{'");
    SurgeAstStmt **list=NULL; size_t cap=0,cnt=0;
    while (!is_token(ps, TOK_RBRACE) && !is_token(ps, TOK_EOF)) {
        SurgeAstStmt *st = parse_stmt(ps);
        if (cnt==cap){ cap=cap?cap*2:8; list=(SurgeAstStmt**)realloc(list, cap*sizeof(*list)); }
        list[cnt++]=st;
    }
    (void)parser_expect(ps, TOK_RBRACE, "'}'");
    SurgeAstStmt *blk = mk_stmt(AST_BLOCK, pos);
    blk->as.block.stmts=list; blk->as.block.count=cnt; return blk;
}

static SurgeAstStmt *parse_if(SurgeParser *ps){
    SurgeSrcPos pos = ps->cur.pos;
    parser_advance(ps); // if
    (void)parser_expect(ps, TOK_LPAREN, "'('");
    SurgeAstExpr *cond = parse_expr(ps);
    (void)parser_expect(ps, TOK_RPAREN, "')'");
    SurgeAstStmt *then_blk = parse_block(ps);
    bool has_else=false; SurgeAstStmt *else_blk=NULL;
    if (is_token(ps, TOK_KW_ELSE)) { parser_advance(ps); has_else=true; else_blk = parse_block(ps); }
    SurgeAstStmt *s = mk_stmt(AST_IF, pos);
    s->as.if_stmt.cond=cond; s->as.if_stmt.then_blk=then_blk; s->as.if_stmt.has_else=has_else; s->as.if_stmt.else_blk=else_blk;
    return s;
}

static SurgeAstStmt *parse_while(SurgeParser *ps){
    SurgeSrcPos pos = ps->cur.pos;
    parser_advance(ps); // while
    (void)parser_expect(ps, TOK_LPAREN, "'('");
    SurgeAstExpr *cond = parse_expr(ps);
    (void)parser_expect(ps, TOK_RPAREN, "')'");
    SurgeAstStmt *body = parse_block(ps);
    SurgeAstStmt *s = mk_stmt(AST_WHILE, pos);
    s->as.while_stmt.cond=cond; s->as.while_stmt.body=body; return s;
}

static SurgeAstParam parse_param(SurgeParser *ps){
    SurgeAstParam p = {0};
    if (!is_token(ps, TOK_IDENTIFIER)) { surge_diag_errorf(ps->cur.pos,"Expected param name"); ps->had_error=true; return p; }
    p.name = make_ident_from_tok(ps->cur); parser_advance(ps);
    (void)parser_expect(ps, TOK_COLON, "':'");
    SurgeAstType *ty = parse_type(ps);
    p.type_ast = ty;
    // Удаляем использование несуществующего поля type_name
    if (is_token(ps, TOK_LBRACKET)) {
        parser_advance(ps);
        (void)parser_expect(ps, TOK_RBRACKET, "']'");
        // Создаем новый тип массива вместо модификации строки
        SurgeAstType *arr_ty = (SurgeAstType*)malloc(sizeof(SurgeAstType));
        if (arr_ty) {
            arr_ty->base = ty->base;
            arr_ty->kind = TYPE_ARRAY;
            arr_ty->as.array.elem = ty;
            p.type_ast = arr_ty;
        }
    }    
    return p;
}

static SurgeAstStmt *parse_fn(SurgeParser *ps){
    SurgeSrcPos pos = ps->cur.pos;
    parser_advance(ps); // fn
    if (!is_token(ps, TOK_IDENTIFIER)) { surge_diag_errorf(ps->cur.pos,"Expected function name"); ps->had_error=true; }
    SurgeAstIdent name = make_ident_from_tok(ps->cur); parser_advance(ps);
    (void)parser_expect(ps, TOK_LPAREN, "'('");
    SurgeAstParam *params=NULL; size_t cap=0,cnt=0;
    if (!is_token(ps, TOK_RPAREN)) {
        for (;;) {
            SurgeAstParam p = parse_param(ps);
            if (cnt==cap){ cap=cap?cap*2:4; params=(SurgeAstParam*)realloc(params, cap*sizeof(*params)); }
            params[cnt++]=p;
            if (is_token(ps, TOK_COMMA)){ parser_advance(ps); continue; }
            break;
        }
    }
    (void)parser_expect(ps, TOK_RPAREN, "')'");
    bool has_ret=false; SurgeAstType *ret=NULL;
    if (is_token(ps, TOK_ARROW)) {
        parser_advance(ps);
        ret = parse_type(ps);
        has_ret = true;
    }
    SurgeAstStmt *body = parse_block(ps);
    SurgeAstStmt *fn = mk_stmt(AST_FN_DECL, pos);
    fn->as.fn_decl.name=name; fn->as.fn_decl.params=params; fn->as.fn_decl.paramc=cnt; fn->as.fn_decl.has_ret=has_ret; fn->as.fn_decl.ret_type_ast=ret; fn->as.fn_decl.body=body;
    return fn;
}

static SurgeAstStmt *parse_parallel(SurgeParser *ps) {
    SurgeSrcPos pos = ps->cur.pos;
    parser_advance(ps); // parallel
    if (is_token(ps, TOK_KW_MAP)) {
        parser_advance(ps);
        // grammar (MVP): parallel map <callee_expr> <array_expr> ;
        // Парсим только базовое выражение для функции (без постфиксов)
        SurgeAstExpr *fn = parse_base_expr(ps);
        SurgeAstExpr *seq = NULL;
        if (is_token(ps, TOK_LBRACKET)) {
            // Disambiguate: treat following '[' as array literal, not as fn[index]
            seq = parse_array_literal_only(ps);
        } else {
            seq = parse_expr(ps);
        }
        (void)parser_expect(ps, TOK_SEMICOLON, "';'");
        SurgeAstStmt *s = mk_stmt(AST_PAR_MAP, pos);
        s->as.par_map.fn_or_ident = fn; s->as.par_map.seq = seq; return s;
    }
    if (is_token(ps, TOK_KW_REDUCE)) {
        parser_advance(ps);
        // grammar (MVP): parallel reduce <array_expr> with (acc:T, v:T) => <expr> ;
        SurgeAstExpr *seq = parse_expr(ps);
        if (!is_token(ps, TOK_IDENTIFIER) || !ps->cur.lexeme || strcmp(ps->cur.lexeme,"with") != 0) {
            surge_diag_errorf(ps->cur.pos, "Expected 'with'");
            ps->had_error = true;
        } else {
            parser_advance(ps); // consume 'with'
        }
        (void)parser_expect(ps, TOK_LPAREN, "'('");
        SurgeAstParam acc = parse_param(ps);
        (void)parser_expect(ps, TOK_COMMA, "','");
        SurgeAstParam v = parse_param(ps);
        (void)parser_expect(ps, TOK_RPAREN, "')'");
        // expect => as token sequence: TOK_ASSIGN? No, we have TOK_ARROW only '->'. For lambda use '=>'
        (void)parser_expect(ps, TOK_REDUCE_EXP, "'=>'");
        SurgeAstExpr *body = parse_expr(ps);
        (void)parser_expect(ps, TOK_SEMICOLON, "';'");
        SurgeAstStmt *s = mk_stmt(AST_PAR_REDUCE, pos);
        s->as.par_reduce.seq=seq; s->as.par_reduce.acc=acc; s->as.par_reduce.v=v; s->as.par_reduce.body=body; return s;
    }
    surge_diag_errorf(pos, "Expected 'map' or 'reduce' after 'parallel'");
    ps->had_error=true;
    // recover: skip to ';'
    while (!is_token(ps, TOK_SEMICOLON) && !is_token(ps, TOK_EOF)) parser_advance(ps);
    if (is_token(ps, TOK_SEMICOLON)) parser_advance(ps);
    return mk_stmt(AST_EXPR_STMT, pos);
}

static SurgeAstStmt *parse_import(SurgeParser *ps){
    SurgeSrcPos pos = ps->cur.pos;
    parser_advance(ps); // import
    if (!is_token(ps, TOK_IDENTIFIER)) {
        surge_diag_errorf(ps->cur.pos, "Expected module name after 'import'");
        ps->had_error = true;
        // попытка восстановления
        while (!is_token(ps, TOK_SEMICOLON) && !is_token(ps, TOK_EOF)) parser_advance(ps);
        if (is_token(ps, TOK_SEMICOLON)) parser_advance(ps);
        return mk_stmt(AST_IMPORT, pos);
    }
    SurgeAstIdent mod = (SurgeAstIdent){ .name = dup_lex(ps->cur.lexeme ? ps->cur.lexeme : "") };
    parser_advance(ps);
    (void)parser_expect(ps, TOK_SEMICOLON, "';'");
    SurgeAstStmt *s = mk_stmt(AST_IMPORT, pos);
    s->as.import_stmt.name = mod;
    return s;
}

static SurgeAstStmt *parse_typedef(SurgeParser *ps){
    SurgeSrcPos pos = ps->cur.pos;
    parser_advance(ps); // type
    if (!is_token(ps, TOK_IDENTIFIER)) {
        surge_diag_errorf(ps->cur.pos, "Expected type name after 'type'");
        ps->had_error = true;
        // попытка восстановления
        while (!is_token(ps, TOK_SEMICOLON) && !is_token(ps, TOK_EOF)) parser_advance(ps);
        if (is_token(ps, TOK_SEMICOLON)) parser_advance(ps);
        return mk_stmt(AST_TYPEDEF, pos);
    }
    SurgeAstIdent name = (SurgeAstIdent){ .name = dup_lex(ps->cur.lexeme ? ps->cur.lexeme : "") };
    parser_advance(ps);
    (void)parser_expect(ps, TOK_ASSIGN, "'='");
    SurgeAstType *aliased = parse_type(ps);
    (void)parser_expect(ps, TOK_SEMICOLON, "';'");
    SurgeAstStmt *s = mk_stmt(AST_TYPEDEF, pos);
    s->as.typedef_decl.name = name;
    s->as.typedef_decl.aliased = aliased;
    return s;
}

static SurgeAstStmt *parse_simple_or_assign_or_bind_stmt(SurgeParser *ps){
    // Lookahead: parse an expression, then if ';' => expr stmt
    // If pattern IDENT '=' expr ';' => assign
    // If pattern <expr ':=' expr> ';' => expr with bind already represented as AST_BIND_EXPR inside ExprStmt
    SurgeSrcPos pos = ps->cur.pos;
    if (is_token(ps, TOK_IDENTIFIER)) {
        // Copy identifier lexeme BEFORE advancing
        char *name_lex = dup_lex(ps->cur.lexeme ? ps->cur.lexeme : "");
        parser_advance(ps);
    
        if (is_token(ps, TOK_ASSIGN)) {
            parser_advance(ps);
            SurgeAstExpr *rhs = parse_expr(ps);
            (void)parser_expect(ps, TOK_SEMICOLON, "';'");
            SurgeAstStmt *st = mk_stmt(AST_ASSIGN_STMT, pos);
            st->as.assign_stmt.name.name = name_lex;  // take ownership
            st->as.assign_stmt.expr = rhs;
            return st;
        } else {
            // build Ident expr from copied name
            SurgeAstExpr *id = mk_expr(AST_IDENT, pos);
            id->as.ident.ident.name = name_lex;  // take ownership
    
            // postfix: calls/index/bind
            for (;;) {
                if (is_token(ps, TOK_LPAREN)) {
                    SurgeAstExpr *call = mk_expr(AST_CALL, id->base.pos);
                    call->as.call.callee = id;
                    parser_advance(ps);
                    SurgeAstExpr **args=NULL; size_t cap=0,cnt=0;
                    if (!is_token(ps, TOK_RPAREN)) {
                        for (;;) {
                            SurgeAstExpr *a = parse_expr(ps);
                            if (cnt==cap){ cap=cap?cap*2:4; args=(SurgeAstExpr**)realloc(args, cap*sizeof(*args)); }
                            args[cnt++]=a;
                            if (is_token(ps, TOK_COMMA)){ parser_advance(ps); continue; }
                            break;
                        }
                    }
                    (void)parser_expect(ps, TOK_RPAREN, "')'");
                    call->as.call.args=args; call->as.call.argc=cnt;
                    id = call; continue;
                }
                if (is_token(ps, TOK_LBRACKET)) {
                    parser_advance(ps);
                    SurgeAstExpr *idx = parse_expr(ps);
                    (void)parser_expect(ps, TOK_RBRACKET, "']'");
                    SurgeAstExpr *ix = mk_expr(AST_INDEX, id->base.pos);
                    ix->as.index.base=id; ix->as.index.index=idx; id=ix; continue;
                }
                if (is_token(ps, TOK_BIND)) {
                    parser_advance(ps);
                    SurgeAstExpr *rhs = parse_expr(ps);
                    SurgeAstExpr *be = mk_expr(AST_BIND_EXPR, id->base.pos);
                    be->as.bind_expr.lhs=id; be->as.bind_expr.rhs=rhs; id=be; continue;
                }
                break;
            }
            // binops tail
            id = parse_bin_rhs(ps, 1, id);
            (void)parser_expect(ps, TOK_SEMICOLON, "';'");
            SurgeAstStmt *st = mk_stmt(AST_EXPR_STMT, pos);
            st->as.expr_stmt.expr = id;
            return st;
        }
    }    
    // General expression stmt
    SurgeAstExpr *e = parse_expr(ps);
    (void)parser_expect(ps, TOK_SEMICOLON, "';'");
    SurgeAstStmt *st = mk_stmt(AST_EXPR_STMT, pos);
    st->as.expr_stmt.expr = e; return st;
}

static SurgeAstStmt *parse_stmt(SurgeParser *ps){
    if (is_token(ps, TOK_KW_LET))    return parse_let(ps);
    if (is_token(ps, TOK_KW_SIGNAL)) return parse_signal(ps);
    if (is_token(ps, TOK_KW_RETURN)) return parse_return(ps);
    if (is_token(ps, TOK_KW_IF))     return parse_if(ps);
    if (is_token(ps, TOK_KW_WHILE))  return parse_while(ps);
    if (is_token(ps, TOK_KW_FN))     return parse_fn(ps);
    if (is_token(ps, TOK_LBRACE))    return parse_block(ps);
    if (is_token(ps, TOK_KW_PARALLEL)) return parse_parallel(ps);
    if (is_token(ps, TOK_KW_IMPORT)) return parse_import(ps);
    if (is_token(ps, TOK_KW_TYPE)) return parse_typedef(ps);
    return parse_simple_or_assign_or_bind_stmt(ps);
}

static SurgeAstType *parse_type_atom(SurgeParser *ps){
    SurgeSrcPos pos = ps->cur.pos;
    if (is_token(ps, TOK_AMP)) {
        parser_advance(ps);
        SurgeAstType *inner = parse_type(ps);
        SurgeAstType *t = mk_type(TYPE_REF, pos);
        t->as.ref_ty.elem = inner; return t;
    }
    if (is_token(ps, TOK_KW_OWN)) {
        parser_advance(ps);
        SurgeAstType *inner = parse_type(ps);
        SurgeAstType *t = mk_type(TYPE_OWN, pos);
        t->as.own_ty.elem = inner; return t;
    }
    if (is_token(ps, TOK_IDENTIFIER)) {
        // Ident [ '<' Type (',' Type)* '>' ]?
        SurgeAstIdent name = make_ident_from_tok(ps->cur);
        parser_advance(ps);
        if (is_token(ps, TOK_LT)) {
            parser_advance(ps);
            SurgeAstType **args=NULL; size_t cap=0,cnt=0;
            if (!is_token(ps, TOK_GT)) {
                for (;;) {
                    SurgeAstType *a = parse_type(ps);
                    if (cnt==cap){ cap=cap?cap*2:4; args=(SurgeAstType**)realloc(args, cap*sizeof(*args)); }
                    args[cnt++]=a;
                    if (is_token(ps, TOK_COMMA)){ parser_advance(ps); continue; }
                    break;
                }
            }
            (void)parser_expect(ps, TOK_GT, "'>'");
            SurgeAstType *t = mk_type(TYPE_APPLY, pos);
            t->as.apply.name = name; t->as.apply.args=args; t->as.apply.argc=cnt; return t;
        } else {
            SurgeAstType *t = mk_type(TYPE_IDENT, pos);
            t->as.ident.name = name; return t;
        }
    }
    surge_diag_errorf(ps->cur.pos, "Expected type");
    ps->had_error = true;
    // fallback
    SurgeAstType *t = mk_type(TYPE_IDENT, pos);
    t->as.ident.name.name = dup_lex("int");
    return t;
}

static SurgeAstType *parse_type(SurgeParser *ps){
    SurgeAstType *t = parse_type_atom(ps);
    // postfix array suffix: '[]' (многократно)
    while (is_token(ps, TOK_LBRACKET)) {
        // only exact '[]'
        SurgeSrcPos pos = ps->cur.pos;
        parser_advance(ps);
        (void)parser_expect(ps, TOK_RBRACKET, "']'");
        SurgeAstType *arr = mk_type(TYPE_ARRAY, pos);
        arr->as.array.elem = t;
        t = arr;
    }
    return t;
}


// --- Unit ---

bool parser_init(SurgeParser *ps, SurgeLexer *lx){
    memset(ps, 0, sizeof(*ps));
    ps->lx = lx;
    ps->cur = surge_lexer_next(lx);
    ps->had_error = false;
    return true;
}

SurgeAstUnit *parser_parse_unit(SurgeParser *ps){
    SurgeAstUnit *u = (SurgeAstUnit*)calloc(1,sizeof(SurgeAstUnit));
    u->base.kind = AST_UNIT; u->base.pos = (SurgeSrcPos){ .file=NULL, .line=1, .col=1 };
    SurgeAstStmt **decls=NULL; size_t cap=0,cnt=0;
    while (!is_token(ps, TOK_EOF)) {
        SurgeAstStmt *d = parse_stmt(ps);
        if (cnt==cap){ cap=cap?cap*2:8; decls=(SurgeAstStmt**)realloc(decls, cap*sizeof(*decls)); }
        decls[cnt++]=d;
    }
    u->decls=decls; u->count=cnt;
    return u;
}

void parser_destroy(SurgeParser *ps){
    surge_token_free(&ps->cur);
    memset(ps, 0, sizeof(*ps));
}