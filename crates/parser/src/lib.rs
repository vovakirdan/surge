pub mod ast;
pub mod error;
pub mod render;

mod attributes;
mod expressions;
mod lexer_api;
mod parser;
mod precedence;
mod statements;
mod sync;
mod types;

pub use ast::{
    AliasDef, AssignOp, Ast, Attr, BinaryOp, Block, CompareArm, Expr, ExternBlock, Func, FuncSig,
    Import, Item, LiteralDef, Module, Param, Pattern, PatternKind, Stmt, StmtOrBlock, TypeDef,
    TypeNode, UnaryOp,
};
pub use error::{ParseCode, ParseDiag};
pub use parser::{ParseResult, parse_source, parse_source_with_options, parse_tokens};

#[cfg(test)]
mod tests;
