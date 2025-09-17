#include "ast.h"
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

static void free_ident(SurgeAstIdent *id) {
    if (id && id->name) free(id->name);
}

static void free_expr(SurgeAstExpr *e);
static void free_stmt(SurgeAstStmt *s);

static void free_expr_list(SurgeAstExpr **xs, size_t n) {
    if (!xs) return;
    for (size_t i=0; i < n; i++) free_expr(xs[i]);
    free(xs);
}

static void free_stmt_list(SurgeAstStmt **xs, size_t n) {
    if (!xs) return;
    for (size_t i=0; i < n; i++) free_stmt(xs[i]);
    free(xs);
}

static void free_expr(SurgeAstExpr *e) {
    if (!e) return;
    switch (e->base.kind) {
        case AST_STRING_LIT: if (e->as.string_lit.v) free(e->as.string_lit.v); break;
        case AST_IDENT: free_ident(&e->as.ident.ident); break;
        case AST_ARRAY_LIT: free_expr_list(e->as.array_lit.items, e->as.array_lit.count); break;
        case AST_INDEX: free_expr(e->as.index.base); free_expr(e->as.index.index); break;
        case AST_CALL: free_expr(e->as.call.callee); free_expr_list(e->as.call.args, e->as.call.argc); break;
        case AST_UNARY: free_expr(e->as.unary.expr); break;
        case AST_BINARY: free_expr(e->as.binary.lhs); free_expr(e->as.binary.rhs); break;
        case AST_PAREN: free_expr(e->as.paren.inner); break;
        case AST_BIND_EXPR: free_expr(e->as.bind_expr.lhs); free_expr(e->as.bind_expr.rhs); break;
        default: break;
    }
    free(e);
}

static void free_stmt(SurgeAstStmt *s) {
    if (!s) return;
    switch (s->base.kind) {
        case AST_LET_DECL:
            free_ident(&s->as.let_decl.name);
            if (s->as.let_decl.has_type) free_ident(&s->as.let_decl.type_name);
            free_expr(s->as.let_decl.init);
            break;
        case AST_SIGNAL_DECL:
            free_ident(&s->as.signal_decl.name);
            free_expr(s->as.signal_decl.init);
            break;
        case AST_ASSIGN_STMT:
            free_ident(&s->as.assign_stmt.name);
            free_expr(s->as.signal_decl.init);
            break;
        case AST_EXPR_STMT:
            free_expr(s->as.expr_stmt.expr);
            break;
        case AST_BLOCK:
            free_stmt_list(s->as.block.stmts, s->as.block.count);
            break;
        case AST_IF:
            free_expr(s->as.if_stmt.cond);
            free_stmt(s->as.if_stmt.then_blk);
            if (s->as.if_stmt.has_else) free_stmt(s->as.if_stmt.else_blk);
            break;
        case AST_WHILE:
            free_expr(s->as.while_stmt.cond);
            free_stmt(s->as.while_stmt.body);
            break;
        case AST_RETURN:
            if (s->as.return_stmt.has_value) free_expr(s->as.return_stmt.value);
            break;
        case AST_FN_DECL:
            free_ident(&s->as.fn_decl.name);
            for (size_t i=0; i < s->as.fn_decl.paramc; i++) {
                free_ident(&s->as.fn_decl.params[i].name);
                free_ident(&s->as.fn_decl.params[i].type_name);
            }
            free(s->as.fn_decl.params);
            if (s->as.fn_decl.has_ret) free_ident(&s->as.fn_decl.ret_type);
            free_stmt(s->as.fn_decl.body);
            break;
        case AST_PAR_MAP:
            free_expr(s->as.par_map.fn_or_ident);
            free_expr(s->as.par_map.seq);
            break;
        case AST_PAR_REDUCE:
            free_expr(s->as.par_reduce.seq);
            free_ident(&s->as.par_reduce.acc.name);
            free_ident(&s->as.par_reduce.acc.type_name);
            free_ident(&s->as.par_reduce.v.name);
            free_ident(&s->as.par_reduce.v.type_name);
            free_expr(s->as.par_reduce.body);
            break;
        default: break;
    }
    free(s);
}

void as_free_unit(SurgeAstUnit *u) {
    if (!u) return;
    free_stmt_list(u->decls, u->count);
    free(u);
}

// --- pretty print (snapshot) ---

static const char* op_str(SurgeAstOp op) {
    switch (op) {
        case AST_OP_ADD: return "+";
        case AST_OP_SUB: return "-";
        case AST_OP_MUL: return "*";
        case AST_OP_DIV: return "/";
        case AST_OP_REM: return "%";
        case AST_OP_EQ:  return "==";
        case AST_OP_NE:  return "!=";
        case AST_OP_LT:  return "<";
        case AST_OP_LE:  return "<=";
        case AST_OP_GT:  return ">";
        case AST_OP_GE:  return ">=";
        case AST_OP_AND: return "&&";
        case AST_OP_OR:  return "||";
        case AST_OP_NEG: return "neg";
        case AST_OP_NOT: return "not";
        default: return "?";
    }
}

static void print_indent(FILE *out, int n) { for (int i = 0; i < n; i++) fputc(' ', out); }

static void print_expr(const SurgeAstExpr *e, FILE *out, int ind);
static void print_stmt(const SurgeAstStmt *s, FILE *out, int ind);

static void print_ident(const SurgeAstIdent *id, FILE *out) {
    fprintf(out, "%s", id->name ? id->name : "<null>");
}

static void print_expr(const SurgeAstExpr *e, FILE *out, int ind) {
    if (!e) { print_indent(out, ind); fprintf(out, "(null-expr)\n"); return; }
    switch (e->base.kind) {
        case AST_INT_LIT:    print_indent(out, ind); fprintf(out, "Int(%lld)\n",(long long)e->as.int_lit.v); break;
        case AST_FLOAT_LIT:  print_indent(out, ind); fprintf(out, "Float(%g)\n", e->as.float_lit.v); break;
        case AST_BOOL_LIT:   print_indent(out, ind); fprintf(out, "Bool(%s)\n", e->as.bool_lit.v?"true":"false"); break;
        case AST_STRING_LIT: print_indent(out, ind); fprintf(out, "String(\"%s\")\n", e->as.string_lit.v?e->as.string_lit.v:""); break;
        case AST_IDENT:      print_indent(out, ind); fprintf(out, "Ident("); print_ident(&e->as.ident.ident,out); fprintf(out,")\n"); break;
        case AST_ARRAY_LIT:
            print_indent(out, ind); fprintf(out, "Array[\n");
            for (size_t i=0;i<e->as.array_lit.count;i++) print_expr(e->as.array_lit.items[i], out, ind+2);
            print_indent(out, ind); fprintf(out, "]\n");
            break;
        case AST_INDEX:
            print_indent(out, ind); fprintf(out, "Index{\n");
            print_expr(e->as.index.base, out, ind+2);
            print_expr(e->as.index.index, out, ind+2);
            print_indent(out, ind); fprintf(out, "}\n");
            break;
        case AST_CALL:
            print_indent(out, ind); fprintf(out, "Call{\n");
            print_expr(e->as.call.callee, out, ind+2);
            for (size_t i=0;i<e->as.call.argc;i++) print_expr(e->as.call.args[i], out, ind+2);
            print_indent(out, ind); fprintf(out, "}\n");
            break;
        case AST_UNARY:
            print_indent(out, ind); fprintf(out, "Unary(%s)\n", op_str(e->as.unary.op));
            print_expr(e->as.unary.expr, out, ind+2);
            break;
        case AST_BINARY:
            print_indent(out, ind); fprintf(out, "Binary(%s)\n", op_str(e->as.binary.op));
            print_expr(e->as.binary.lhs, out, ind+2);
            print_expr(e->as.binary.rhs, out, ind+2);
            break;
        case AST_PAREN:
            print_indent(out, ind); fprintf(out, "Paren\n");
            print_expr(e->as.paren.inner, out, ind+2);
            break;
        case AST_BIND_EXPR:
            print_indent(out, ind); fprintf(out, "BindExpr(:=)\n");
            print_expr(e->as.bind_expr.lhs, out, ind+2);
            print_expr(e->as.bind_expr.rhs, out, ind+2);
            break;
        default:
            print_indent(out, ind); fprintf(out, "(expr-kind=%d)\n", (int)e->base.kind);
    }
}

static void print_params(const SurgeAstParam *ps, size_t n, FILE *out, int ind) {
    for (size_t i=0;i<n;i++){
        print_indent(out, ind);
        fprintf(out, "Param ");
        print_ident(&ps[i].name,out);
        fprintf(out, ":");
        print_ident(&ps[i].type_name,out);
        fprintf(out, "\n");
    }
}

static void print_stmt(const SurgeAstStmt *s, FILE *out, int ind) {
    if (!s) { print_indent(out, ind); fprintf(out, "(null-stmt)\n"); return; }
    switch (s->base.kind) {
        case AST_LET_DECL:
            print_indent(out, ind); fprintf(out, "Let "); print_ident(&s->as.let_decl.name, out);
            if (s->as.let_decl.has_type){ fprintf(out, ":"); print_ident(&s->as.let_decl.type_name,out); }
            fprintf(out, " =\n"); print_expr(s->as.let_decl.init, out, ind+2); break;
        case AST_SIGNAL_DECL:
            print_indent(out, ind); fprintf(out, "Signal "); print_ident(&s->as.signal_decl.name, out); fprintf(out, " =\n");
            print_expr(s->as.signal_decl.init, out, ind+2); break;
        case AST_ASSIGN_STMT:
            print_indent(out, ind); fprintf(out, "Assign "); print_ident(&s->as.assign_stmt.name,out); fprintf(out," =\n");
            print_expr(s->as.assign_stmt.expr,out,ind+2); break;
        case AST_EXPR_STMT:
            print_indent(out, ind); fprintf(out, "ExprStmt\n"); print_expr(s->as.expr_stmt.expr,out,ind+2); break;
        case AST_BLOCK:
            print_indent(out, ind); fprintf(out, "Block{\n");
            for (size_t i=0;i<s->as.block.count;i++) print_stmt(s->as.block.stmts[i], out, ind+2);
            print_indent(out, ind); fprintf(out, "}\n");
            break;
        case AST_IF:
            print_indent(out, ind); fprintf(out, "If\n");
            print_expr(s->as.if_stmt.cond,out,ind+2);
            print_stmt(s->as.if_stmt.then_blk,out,ind+2);
            if (s->as.if_stmt.has_else){ print_indent(out, ind); fprintf(out,"Else\n"); print_stmt(s->as.if_stmt.else_blk,out,ind+2); }
            break;
        case AST_WHILE:
            print_indent(out, ind); fprintf(out, "While\n");
            print_expr(s->as.while_stmt.cond,out,ind+2);
            print_stmt(s->as.while_stmt.body,out,ind+2);
            break;
        case AST_RETURN:
            print_indent(out, ind); fprintf(out, "Return\n");
            if (s->as.return_stmt.has_value) print_expr(s->as.return_stmt.value,out,ind+2);
            break;
        case AST_FN_DECL:
            print_indent(out, ind); fprintf(out, "Fn "); print_ident(&s->as.fn_decl.name,out); fprintf(out,"\n");
            print_params(s->as.fn_decl.params, s->as.fn_decl.paramc, out, ind+2);
            if (s->as.fn_decl.has_ret){ print_indent(out, ind); fprintf(out, "Ret: "); print_ident(&s->as.fn_decl.ret_type,out); fprintf(out,"\n"); }
            print_stmt(s->as.fn_decl.body,out,ind+2);
            break;
        case AST_PAR_MAP:
            print_indent(out, ind); fprintf(out, "ParallelMap\n");
            print_expr(s->as.par_map.fn_or_ident,out,ind+2);
            print_expr(s->as.par_map.seq,out,ind+2);
            break;
        case AST_PAR_REDUCE:
            print_indent(out, ind); fprintf(out, "ParallelReduce\n");
            print_expr(s->as.par_reduce.seq,out,ind+2);
            print_indent(out, ind+2); fprintf(out,"Acc "); print_ident(&s->as.par_reduce.acc.name,out); fprintf(out,":"); print_ident(&s->as.par_reduce.acc.type_name,out); fprintf(out,"\n");
            print_indent(out, ind+2); fprintf(out,"Val "); print_ident(&s->as.par_reduce.v.name,out); fprintf(out,":"); print_ident(&s->as.par_reduce.v.type_name,out); fprintf(out,"\n");
            print_expr(s->as.par_reduce.body,out,ind+2);
            break;
        default:
            print_indent(out, ind); fprintf(out, "(stmt-kind=%d)\n",(int)s->base.kind);
    }
}

void ast_print_unit(const SurgeAstUnit *u, FILE *out) {
    if (!u){ fprintf(out,"(null-unit)\n"); return; }
    fprintf(out, "Unit{\n");
    for (size_t i=0;i<u->count;i++) print_stmt(u->decls[i], out, 2);
    fprintf(out, "}\n");
}

void ast_free_unit(SurgeAstUnit *u) {
    if (!u) return;
    // Освобождаем все объявления в unit
    free_stmt_list(u->decls, u->count);
    // Освобождаем сам unit
    free(u);
}