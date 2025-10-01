pub mod ast;
pub mod error;

mod attributes;
mod expressions;
mod lexer_api;
mod parser;
mod precedence;
mod statements;
mod sync;
mod types;

pub use ast::{
    AliasDef, AssignOp, Ast, Attr, BinaryOp, Block, CompareArm, Expr, Func, FuncSig, Import, Item,
    LiteralDef, Module, Param, Pattern, PatternKind, Stmt, StmtOrBlock, TypeDef, TypeNode, UnaryOp,
    Using,
};
pub use error::{ParseCode, ParseDiag};
pub use parser::{ParseResult, parse_source, parse_source_with_options, parse_tokens};

#[cfg(test)]
mod tests;
