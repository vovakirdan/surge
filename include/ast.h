#ifndef SURGE_AST_H
#define SURGE_AST_H

#include <stddef.h>
#include <stdint.h>
#include <stdbool.h>
#include <stdio.h>
#include "diagnostics.h"

// Forward decls
struct SurgeAstNode;
struct SurgeAstExpr;
struct SurgeAstStmt;

// Kinds

typedef enum {
    // Expressions
    AST_INT_LIT,
    AST_FLOAT_LIT,
    AST_BOOL_LIT,
    AST_STRING_LIT,
    AST_IDENT,
    AST_ARRAY_LIT,
    AST_INDEX,         // base[index]
    AST_CALL,          // callee(args...)
    AST_UNARY,         // op expr
    AST_BINARY,        // lhs op rhs
    AST_PAREN,         // (expr)
    AST_BIND_EXPR,     // lhs := expr   (reactive bind) — also as stmt form

    // Statements / Decls
    AST_LET_DECL,      // let name [: Type]? = expr ;
    AST_SIGNAL_DECL,   // signal name = expr ;
    AST_EXPR_STMT,     // expr ;
    AST_ASSIGN_STMT,   // name = expr ;
    AST_BLOCK,         // { stmts* }
    AST_IF,            // if (cond) block [else block]
    AST_WHILE,         // while (cond) block
    AST_RETURN,        // return expr? ;
    AST_FN_DECL,       // fn name(params) -> Type block
    AST_PAR_MAP,       // parallel map fn_or_ident expr
    AST_PAR_REDUCE,    // parallel reduce expr with (acc:T, v:T) => expr
    AST_IMPORT,        // import name ;

    // Root
    AST_UNIT           // Program root
} SurgeAstKind;

typedef enum {
    AST_OP_NONE = 0,
    AST_OP_ADD, AST_OP_SUB, AST_OP_MUL, AST_OP_DIV, AST_OP_REM,
    AST_OP_EQ, AST_OP_NE, AST_OP_LT, AST_OP_LE, AST_OP_GT, AST_OP_GE,
    AST_OP_AND, AST_OP_OR,
    AST_OP_NEG, AST_OP_NOT
} SurgeAstOp;

typedef struct SurgeAstNode {
    SurgeAstKind kind;
    SurgeSrcPos  pos;
} SurgeAstNode;

// Ident / Type name (Phase A: keep Type as identifier token)
typedef struct {
    char *name; // heap string
} SurgeAstIdent;

// Expressions
typedef struct SurgeAstExpr {
    SurgeAstNode base;
    union {
        struct { int64_t v; } int_lit;
        struct { double  v; } float_lit;
        struct { bool    v; } bool_lit;
        struct { char   *v; } string_lit;
        struct { SurgeAstIdent ident; } ident;

        struct { struct SurgeAstExpr **items; size_t count; } array_lit;

        struct { struct SurgeAstExpr *base; struct SurgeAstExpr *index; } index;

        struct { struct SurgeAstExpr *callee; struct SurgeAstExpr **args; size_t argc; } call;

        struct { SurgeAstOp op; struct SurgeAstExpr *expr; } unary;
        struct { SurgeAstOp op; struct SurgeAstExpr *lhs; struct SurgeAstExpr *rhs; } binary;

        struct { struct SurgeAstExpr *inner; } paren;

        struct { struct SurgeAstExpr *lhs; struct SurgeAstExpr *rhs; } bind_expr; // ":="
    } as;
} SurgeAstExpr;

// Statements
typedef struct SurgeAstParam {
    SurgeAstIdent name;
    SurgeAstIdent type_name;  // Phase A: just an identifier as type
} SurgeAstParam;

typedef struct SurgeAstStmt {
    SurgeAstNode base;
    union {
        struct { SurgeAstIdent name; bool has_type; SurgeAstIdent type_name; SurgeAstExpr *init; } let_decl;
        struct { SurgeAstIdent name; SurgeAstExpr *init; } signal_decl;
        struct { SurgeAstIdent name; SurgeAstExpr *expr; } assign_stmt;
        struct { SurgeAstExpr *expr; } expr_stmt;
        struct { struct SurgeAstStmt **stmts; size_t count; } block;
        struct { SurgeAstExpr *cond; struct SurgeAstStmt *then_blk; bool has_else; struct SurgeAstStmt *else_blk; } if_stmt;
        struct { SurgeAstExpr *cond; struct SurgeAstStmt *body; } while_stmt;
        struct { bool has_value; SurgeAstExpr *value; } return_stmt;
        struct { SurgeAstIdent name; SurgeAstParam *params; size_t paramc; bool has_ret; SurgeAstIdent ret_type; struct SurgeAstStmt *body; } fn_decl;
        struct { SurgeAstExpr *fn_or_ident; SurgeAstExpr *seq; } par_map;
        struct { SurgeAstExpr *seq; SurgeAstParam acc; SurgeAstParam v; SurgeAstExpr *body; } par_reduce;
        struct { SurgeAstIdent name; } import_stmt;
    } as;
} SurgeAstStmt;

// Root unit
typedef struct SurgeAstUnit {
    SurgeAstNode base;
    SurgeAstStmt **decls;
    size_t count;
} SurgeAstUnit;

// API
void ast_free_unit(SurgeAstUnit *u);
void ast_print_unit(const SurgeAstUnit *u, FILE *out);

#endif // SURGE_AST_H
