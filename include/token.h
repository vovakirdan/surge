#ifndef SURGE_TOKEN_H
#define SURGE_TOKEN_H

#include <stddef.h>
#include <stdint.h>
#include <stdbool.h>
#include "diagnostics.h"

// Token kinds for Surge
typedef enum SurgeTokenKind {
    // Special
    TOK_EOF = 0,
    TOK_ERROR,

    // Identifiers / keywords
    TOK_IDENTIFIER,
    TOK_KW_LET,
    TOK_KW_SIGNAL,
    TOK_KW_FN,
    TOK_KW_IF,
    TOK_KW_ELSE,
    TOK_KW_WHILE,
    TOK_KW_RETURN,
    TOK_KW_IMPORT,
    TOK_KW_TRUE,
    TOK_KW_FALSE,
    TOK_KW_PARALLEL,
    TOK_KW_MAP,
    TOK_KW_REDUCE,
    TOK_KW_OWN, // own
    TOK_KW_TYPE, // type

    // Literals
    TOK_INT, // 64-BIT SIGNED INT (LATER WILL BE DYNAMICALLY SIZED)
    TOK_FLOAT, // 64-BIT DOUBLE
    TOK_STRING, // JUST STRING
    TOK_BOOL,

    // Punctuation / operators
    TOK_LPAREN,     // (
    TOK_RPAREN,     // )
    TOK_LBRACE,     // {
    TOK_RBRACE,     // }
    TOK_LBRACKET,   // [
    TOK_RBRACKET,   // ]
    TOK_COMMA,      // ,
    TOK_SEMICOLON,  // ;
    TOK_DOT,        // .
    TOK_COLON,      // :

    // Arithmetic
    TOK_PLUS,       // +
    TOK_MINUS,      // -
    TOK_STAR,       // *
    TOK_SLASH,      // /
    TOK_PERCENT,    // %

    // Logical / unary
    TOK_BANG,       // !
    TOK_AND_AND,    // &&
    TOK_OR_OR,      // ||

    // Assignment / comparison / special
    TOK_ASSIGN,     // =
    TOK_AMP,        // &
    TOK_BIND,       // :=   (reactive bind)
    TOK_EQ,         // ==
    TOK_NE,         // !=
    TOK_LT,         // <
    TOK_LE,         // <=
    TOK_GT,         // >
    TOK_GE,         // >=
    TOK_ARROW,      // ->   (fn return type)
    TOK_REDUCE_EXP, // =>   (reduce expression)
    TOK_AT          // @    (pure function)
} SurgeTokenKind;

// Token structure
typedef struct SurgeToken {
    SurgeTokenKind kind;
    SurgeSrcPos pos; // position of first character
    char *lexeme; // heap-allocated (may be NULL for EOF)
    size_t length; // bytes in lexeme
    bool has_int; // true if lexeme is a valid integer
    bool has_float; // true if lexeme is a valid float
    int64_t int_value; // valid if has_int
    double float_value; // valid if has_float
} SurgeToken;

const char *surge_token_kind_cstr(SurgeTokenKind k);
void surge_token_free(SurgeToken *t);

#endif // SURGE_TOKEN_H
