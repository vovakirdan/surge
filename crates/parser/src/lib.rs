#![deny(warnings)]

pub mod ast;
pub mod error;

mod lexer_api;
mod parser;
mod precedence;
mod sync;

pub use ast::{
    AliasDef, AssignOp, Ast, Attr, BinaryOp, Block, Expr, Func, FuncSig, Import, Item, LiteralDef,
    Module, Param, Stmt, StmtOrBlock, TypeDef, TypeNode, UnaryOp, Using,
};
pub use error::{ParseCode, ParseDiag};
pub use parser::{ParseResult, parse_source, parse_source_with_options, parse_tokens};

#[cfg(test)]
mod tests;
