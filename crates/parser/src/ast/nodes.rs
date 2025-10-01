//! Abstract syntax tree nodes for the Surge language parser.

use surge_token::Span;

/// Root AST node that wraps a [`Module`].
#[derive(Debug, Default)]
pub struct Ast {
    pub module: Module,
}

/// A compilation unit containing top-level items.
#[derive(Debug, Default)]
pub struct Module {
    pub items: Vec<Item>,
}

/// All supported top-level declarations.
#[derive(Debug)]
pub enum Item {
    Fn(Func),
    Type(TypeDef),
    Literal(LiteralDef),
    Alias(AliasDef),
    Extern(ExternBlock),
    Import(Import),
    Let(Stmt),
}

/// Function declaration.
#[derive(Debug)]
pub struct Func {
    pub sig: FuncSig,
    pub body: Option<Block>,
    pub span: Span,
}

/// Function signature with attributes.
#[derive(Debug)]
pub struct FuncSig {
    pub name: String,
    pub params: Vec<Param>,
    pub ret: Option<TypeNode>,
    pub span: Span,
    pub attrs: Vec<Attr>,
}

/// Function attribute.
#[derive(Debug, Clone)]
pub enum Attr {
    Pure {
        span: Span,
    },
    Overload {
        span: Span,
    },
    Override {
        span: Span,
    },
    Intrinsic {
        span: Span,
    },
    Backend {
        span: Span,
        value: String,
        value_span: Span,
    },
    Deprecated {
        span: Span,
        message: String,
        message_span: Span,
    },
    Packed {
        span: Span,
    },
    Align {
        span: Span,
        value: String,
        value_span: Span,
    },
    Shared {
        span: Span,
    },
    Atomic {
        span: Span,
    },
    Raii {
        span: Span,
    },
    Arena {
        span: Span,
    },
    Weak {
        span: Span,
    },
    Readonly {
        span: Span,
    },
    Hidden {
        span: Span,
    },
    NoInherit {
        span: Span,
    },
    Sealed {
        span: Span,
    },
    GuardedBy {
        span: Span,
        lock: String,
        lock_span: Span,
    },
    RequiresLock {
        span: Span,
        lock: String,
        lock_span: Span,
    },
    AcquiresLock {
        span: Span,
        lock: String,
        lock_span: Span,
    },
    ReleasesLock {
        span: Span,
        lock: String,
        lock_span: Span,
    },
    WaitsOn {
        span: Span,
        cond: String,
        cond_span: Span,
    },
    Send {
        span: Span,
    },
    NoSend {
        span: Span,
    },
    NonBlocking {
        span: Span,
    },
}

/// Function parameter.
#[derive(Debug)]
pub struct Param {
    pub name: String,
    pub ty: Option<TypeNode>,
    pub span: Span,
}

/// Code block `{ ... }` containing statements.
#[derive(Debug)]
pub struct Block {
    pub stmts: Vec<Stmt>,
    pub span: Span,
}

/// Statement node.
#[derive(Debug)]
pub enum Stmt {
    Let {
        name: String,
        ty: Option<TypeNode>,
        init: Option<Expr>,
        mutable: bool,
        span: Span,
        semi: Option<Span>,
    },
    While {
        cond: Expr,
        body: Block,
        span: Span,
    },
    ForC {
        init: Option<Expr>,
        cond: Option<Expr>,
        step: Option<Expr>,
        body: Block,
        span: Span,
    },
    ForIn {
        pat: String,
        ty: Option<TypeNode>,
        iter: Expr,
        body: Block,
        span: Span,
    },
    If {
        cond: Expr,
        then_b: Block,
        else_b: Option<Box<StmtOrBlock>>,
        span: Span,
    },
    Return {
        expr: Option<Expr>,
        span: Span,
        semi: Option<Span>,
    },
    ExprStmt {
        expr: Expr,
        span: Span,
        semi: Option<Span>,
    },
    Signal {
        name: String,
        expr: Expr,
        span: Span,
        semi: Option<Span>,
    },
    Break {
        span: Span,
        semi: Option<Span>,
    },
    Continue {
        span: Span,
        semi: Option<Span>,
    },
}

/// Either a single statement or an inline block.
#[derive(Debug)]
pub enum StmtOrBlock {
    Stmt(Stmt),
    Block(Block),
}

/// Expression node.
#[derive(Debug)]
pub enum Expr {
    LitInt(String, Span),
    LitFloat(String, Span),
    LitString(String, Span),
    Ident(String, Span),
    Call {
        callee: Box<Expr>,
        args: Vec<Expr>,
        span: Span,
    },
    Index {
        base: Box<Expr>,
        index: Box<Expr>,
        span: Span,
    },
    Array {
        elems: Vec<Expr>,
        span: Span,
    },
    Unary {
        op: UnaryOp,
        rhs: Box<Expr>,
        span: Span,
    },
    Binary {
        lhs: Box<Expr>,
        op: BinaryOp,
        rhs: Box<Expr>,
        span: Span,
    },
    Assign {
        lhs: Box<Expr>,
        rhs: Box<Expr>,
        op: AssignOp,
        span: Span,
    },
    Compare {
        scrutinee: Box<Expr>,
        arms: Vec<CompareArm>,
        span: Span,
    },
    Ternary {
        cond: Box<Expr>,
        then_branch: Box<Expr>,
        else_branch: Box<Expr>,
        span: Span,
    },
    Let {
        name: String,
        ty: Option<TypeNode>,
        init: Option<Box<Expr>>,
        mutable: bool,
        span: Span,
    },
    ParallelMap {
        seq: Box<Expr>,
        args: Vec<Expr>,
        func: Box<Expr>,
        span: Span,
    },
    ParallelReduce {
        seq: Box<Expr>,
        init: Box<Expr>,
        args: Vec<Expr>,
        func: Box<Expr>,
        span: Span,
    },
}

/// Unary operator variants.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum UnaryOp {
    Pos,
    Neg,
    Not,
}

/// Binary operator variants.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BinaryOp {
    Add,
    Sub,
    Mul,
    Div,
    Mod,
    Shl,
    Shr,
    BitAnd,
    BitOr,
    BitXor,
    Lt,
    Le,
    Gt,
    Ge,
    EqEq,
    Ne,
    Is,
    AndAnd,
    OrOr,
    Range,
    RangeInclusive,
    NullCoalesce,
}

/// Assignment operators (plain and compound).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AssignOp {
    Assign,
    AddAssign,
    SubAssign,
    MulAssign,
    DivAssign,
    ModAssign,
    BitAndAssign,
    BitOrAssign,
    BitXorAssign,
    ShlAssign,
    ShrAssign,
}

/// Compare expression arm.
#[derive(Debug)]
pub struct CompareArm {
    pub pattern: Pattern,
    pub guard: Option<Expr>,
    pub expr: Expr,
    pub span: Span,
}

/// Compare pattern with its span.
#[derive(Debug)]
pub struct Pattern {
    pub kind: PatternKind,
    pub span: Span,
}

/// Kinds of patterns supported by compare expressions.
#[derive(Debug)]
pub enum PatternKind {
    Finally,
    Binding(String),
    Nothing,
    Literal(Expr),
    Tag { name: String, args: Vec<Pattern> },
}

/// Type node represented by its span and optional textual form.
#[derive(Debug, Clone)]
pub struct TypeNode {
    pub span: Span,
    pub repr: String,
}

/// Type alias declaration.
#[derive(Debug)]
pub struct AliasDef {
    pub name: String,
    pub span: Span,
}

/// Literal definition declaration stub.
#[derive(Debug)]
pub struct LiteralDef {
    pub name: String,
    pub span: Span,
}

/// Type definition declaration stub.
#[derive(Debug)]
pub struct TypeDef {
    pub name: String,
    pub span: Span,
}

/// Extern block declaration stub.
#[derive(Debug)]
pub struct ExternBlock {
    pub name: Option<String>,
    pub span: Span,
}

/// Import declaration stub.
#[derive(Debug)]
pub struct Import {
    pub path: String,
    pub alias: Option<String>,
    pub span: Span,
}

