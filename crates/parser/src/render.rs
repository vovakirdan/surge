//! AST and token rendering helpers for developer tooling.

use std::fmt::Write as _;

use surge_token::{SourceId, Token};

use crate::{
    AliasDef, Ast, Attr, Block, Expr, ExternBlock, Func, FuncSig, Import, Item, LiteralDef, Module, Param, Pattern, PatternKind, Stmt,
    StmtOrBlock, TypeDef, TypeNode,
};

/// Rendering context passed to AST nodes.
pub struct RenderCtx<'a> {
    output: &'a mut String,
}

impl<'a> RenderCtx<'a> {
    pub fn new(output: &'a mut String) -> Self {
        Self { output }
    }

    fn push_str(&mut self, text: &str) {
        self.output.push_str(text);
    }
}

/// Trait implemented by AST nodes that can render themselves into a textual tree.
pub trait AstRender {
    fn render(&self, ctx: &mut RenderCtx<'_>, indent: usize);
}

/// Render AST tree with indentation.
pub fn render_ast_tree(ast: &Ast, _src: &str, _source_id: SourceId) -> String {
    let mut output = String::new();
    output.push_str("AST Tree:\n");
    {
        let mut ctx = RenderCtx::new(&mut output);
        ast.module.render(&mut ctx, 0);
    }
    output
}

impl AstRender for Module {
    fn render(&self, ctx: &mut RenderCtx<'_>, indent: usize) {
        let indent_str = "  ".repeat(indent);
        ctx.push_str(&format!("{}Module {{\n", indent_str));
        ctx.push_str(&format!("{}  items: [\n", indent_str));

        for (i, item) in self.items.iter().enumerate() {
            if i > 0 {
                ctx.push_str(",\n");
            }
            item.render(ctx, indent + 2);
        }

        ctx.push_str(&format!("\n{}  ]\n", indent_str));
        ctx.push_str(&format!("{}}}\n", indent_str));
    }
}

impl AstRender for Item {
    fn render(&self, ctx: &mut RenderCtx<'_>, indent: usize) {
        let indent_str = "  ".repeat(indent);
        match self {
            Item::Fn(func) => {
                ctx.push_str(&format!("{}Fn(\n", indent_str));
                func.render(ctx, indent + 1);
                ctx.push_str(&format!("{})", indent_str));
            }
            Item::Let(stmt) => {
                ctx.push_str(&format!("{}Let(\n", indent_str));
                stmt.render(ctx, indent + 1);
                ctx.push_str(&format!("\n{})", indent_str));
            }
            Item::Type(type_def) => {
                ctx.push_str(&format!("{}Type(\n", indent_str));
                type_def.render(ctx, indent + 1);
                ctx.push_str(&format!("\n{})", indent_str));
            }
            Item::Literal(literal_def) => {
                ctx.push_str(&format!("{}Literal(\n", indent_str));
                literal_def.render(ctx, indent + 1);
                ctx.push_str(&format!("\n{})", indent_str));
            }
            Item::Alias(alias_def) => {
                ctx.push_str(&format!("{}Alias(\n", indent_str));
                alias_def.render(ctx, indent + 1);
                ctx.push_str(&format!("\n{})", indent_str));
            }
            Item::Extern(extern_block) => {
                ctx.push_str(&format!("{}Extern(\n", indent_str));
                extern_block.render(ctx, indent + 1);
                ctx.push_str(&format!("\n{})", indent_str));
            }
            Item::Import(import) => {
                ctx.push_str(&format!("{}Import(\n", indent_str));
                import.render(ctx, indent + 1);
                ctx.push_str(&format!("\n{})", indent_str));
            }
            // Все варианты Item теперь поддерживаются
        }
    }
}

impl AstRender for Func {
    fn render(&self, ctx: &mut RenderCtx<'_>, indent: usize) {
        let indent_str = "  ".repeat(indent);
        ctx.push_str(&format!("{}sig: (\n", indent_str));
        self.sig.render(ctx, indent + 1);
        ctx.push_str(&format!("\n{})", indent_str));

        if let Some(body) = &self.body {
            ctx.push_str(&format!(",\n{}body: (\n", indent_str));
            body.render(ctx, indent + 1);
            ctx.push_str(&format!("\n{})", indent_str));
        } else {
            ctx.push_str(", body: None");
        }

        ctx.push_str(&format!(", span: {:?}", self.span));
    }
}

impl AstRender for FuncSig {
    fn render(&self, ctx: &mut RenderCtx<'_>, indent: usize) {
        let indent_str = "  ".repeat(indent);
        ctx.push_str(&format!("{}name: \"{}\",\n", indent_str, self.name));
        ctx.push_str(&format!("{}params: [\n", indent_str));

        for (i, param) in self.params.iter().enumerate() {
            if i > 0 {
                ctx.push_str(",\n");
            }
            param.render(ctx, indent + 1);
        }

        ctx.push_str(&format!(
            "\n{}]{}",
            indent_str,
            if self.params.is_empty() { "" } else { "," }
        ));
        ctx.push_str(&format!("\n{}ret: ", indent_str));

        if let Some(ret) = &self.ret {
            ctx.push_str("Some(");
            ret.render(ctx, indent + 1);
            ctx.push_str(")");
        } else {
            ctx.push_str("None");
        }

        ctx.push_str(&format!(",\n{}span: {:?}", indent_str, self.span));
        ctx.push_str(&format!(",\n{}attrs: [", indent_str));

        for (i, attr) in self.attrs.iter().enumerate() {
            if i > 0 {
                ctx.push_str(", ");
            }
            attr.render(ctx, indent + 1);
        }

        ctx.push_str("]");
    }
}

impl AstRender for Param {
    fn render(&self, ctx: &mut RenderCtx<'_>, indent: usize) {
        let indent_str = "  ".repeat(indent);
        ctx.push_str(&format!("{}Param {{\n", indent_str));
        ctx.push_str(&format!("{}  name: \"{}\",\n", indent_str, self.name));

        if let Some(ty) = &self.ty {
            ctx.push_str(&format!("{}  ty: ", indent_str));
            ty.render(ctx, indent + 1);
            ctx.push_str(",\n");
        } else {
            ctx.push_str(&format!("{}  ty: None,\n", indent_str));
        }

        ctx.push_str(&format!("{}  span: {:?}\n", indent_str, self.span));
        ctx.push_str(&format!("{}}}", indent_str));
    }
}

impl AstRender for Block {
    fn render(&self, ctx: &mut RenderCtx<'_>, indent: usize) {
        let indent_str = "  ".repeat(indent);
        ctx.push_str(&format!("{}Block {{\n", indent_str));
        ctx.push_str(&format!("{}  stmts: [\n", indent_str));

        for (i, stmt) in self.stmts.iter().enumerate() {
            if i > 0 {
                ctx.push_str(",\n");
            }
            stmt.render(ctx, indent + 2);
        }

        ctx.push_str(&format!("\n{}  ],\n", indent_str));
        ctx.push_str(&format!("{}  span: {:?}\n", indent_str, self.span));
        ctx.push_str(&format!("{}}}", indent_str));
    }
}

impl AstRender for Stmt {
    fn render(&self, ctx: &mut RenderCtx<'_>, indent: usize) {
        let indent_str = "  ".repeat(indent);
        match self {
            Stmt::Let {
                name,
                ty,
                init,
                mutable,
                span,
                semi,
            } => {
                ctx.push_str(&format!("{}Let {{\n", indent_str));
                ctx.push_str(&format!("{}  name: \"{}\",\n", indent_str, name));
                ctx.push_str(&format!("{}  mutable: {},\n", indent_str, mutable));

                if let Some(ty) = ty {
                    ctx.push_str(&format!("{}  ty: ", indent_str));
                    ty.render(ctx, indent + 1);
                    ctx.push_str(",\n");
                } else {
                    ctx.push_str(&format!("{}  ty: None,\n", indent_str));
                }

                if let Some(init) = init {
                    ctx.push_str(&format!("{}  init: ", indent_str));
                    init.render(ctx, indent + 1);
                    ctx.push_str(",\n");
                } else {
                    ctx.push_str(&format!("{}  init: None,\n", indent_str));
                }

                ctx.push_str(&format!("{}  span: {:?},\n", indent_str, span));
                ctx.push_str(&format!("{}  semi: {:?}\n", indent_str, semi));
                ctx.push_str(&format!("{}}}", indent_str));
            }
            Stmt::While { cond, body, span } => {
                ctx.push_str(&format!("{}While {{\n", indent_str));
                ctx.push_str(&format!("{}  cond: ", indent_str));
                cond.render(ctx, indent + 1);
                ctx.push_str(&format!(",\n{}  body: ", indent_str));
                body.render(ctx, indent + 1);
                ctx.push_str(&format!(",\n{}  span: {:?}\n", indent_str, span));
                ctx.push_str(&format!("{}}}", indent_str));
            }
            Stmt::ForC {
                init,
                cond,
                step,
                body,
                span,
            } => {
                ctx.push_str(&format!("{}ForC {{\n", indent_str));
                ctx.push_str(&format!("{}  init: ", indent_str));
                if let Some(expr) = init {
                    expr.render(ctx, indent + 1);
                } else {
                    ctx.push_str("None");
                }
                ctx.push_str(&format!(",\n{}  cond: ", indent_str));
                if let Some(expr) = cond {
                    expr.render(ctx, indent + 1);
                } else {
                    ctx.push_str("None");
                }
                ctx.push_str(&format!(",\n{}  step: ", indent_str));
                if let Some(expr) = step {
                    expr.render(ctx, indent + 1);
                } else {
                    ctx.push_str("None");
                }
                ctx.push_str(&format!(",\n{}  body: ", indent_str));
                body.render(ctx, indent + 1);
                ctx.push_str(&format!(",\n{}  span: {:?}\n", indent_str, span));
                ctx.push_str(&format!("{}}}", indent_str));
            }
            Stmt::ForIn {
                pat,
                ty,
                iter,
                body,
                span,
            } => {
                ctx.push_str(&format!("{}ForIn {{\n", indent_str));
                ctx.push_str(&format!("{}  pat: \"{}\",\n", indent_str, pat));
                ctx.push_str(&format!("{}  ty: ", indent_str));
                if let Some(ty) = ty {
                    ty.render(ctx, indent + 1);
                } else {
                    ctx.push_str("None");
                }
                ctx.push_str(&format!(",\n{}  iter: ", indent_str));
                iter.render(ctx, indent + 1);
                ctx.push_str(&format!(",\n{}  body: ", indent_str));
                body.render(ctx, indent + 1);
                ctx.push_str(&format!(",\n{}  span: {:?}\n", indent_str, span));
                ctx.push_str(&format!("{}}}", indent_str));
            }
            Stmt::If {
                cond,
                then_b,
                else_b,
                span,
            } => {
                ctx.push_str(&format!("{}If {{\n", indent_str));
                ctx.push_str(&format!("{}  cond: ", indent_str));
                cond.render(ctx, indent + 1);
                ctx.push_str(&format!(",\n{}  then: ", indent_str));
                then_b.render(ctx, indent + 1);
                ctx.push_str(&format!(",\n{}  else: ", indent_str));
                if let Some(else_b) = else_b {
                    else_b.render(ctx, indent + 1);
                } else {
                    ctx.push_str("None");
                }
                ctx.push_str(&format!(",\n{}  span: {:?}\n", indent_str, span));
                ctx.push_str(&format!("{}}}", indent_str));
            }
            Stmt::Return { expr, span, semi } => {
                ctx.push_str(&format!("{}Return {{\n", indent_str));
                ctx.push_str(&format!("{}  expr: ", indent_str));
                if let Some(expr) = expr {
                    expr.render(ctx, indent + 1);
                } else {
                    ctx.push_str("None");
                }
                ctx.push_str(&format!(",\n{}  span: {:?},\n", indent_str, span));
                ctx.push_str(&format!("{}  semi: {:?}\n", indent_str, semi));
                ctx.push_str(&format!("{}}}", indent_str));
            }
            Stmt::ExprStmt { expr, span, semi } => {
                ctx.push_str(&format!("{}ExprStmt {{\n", indent_str));
                ctx.push_str(&format!("{}  expr: ", indent_str));
                expr.render(ctx, indent + 1);
                ctx.push_str(&format!(",\n{}  span: {:?},\n", indent_str, span));
                ctx.push_str(&format!("{}  semi: {:?}\n", indent_str, semi));
                ctx.push_str(&format!("{}}}", indent_str));
            }
            Stmt::Signal {
                name,
                expr,
                span,
                semi,
            } => {
                ctx.push_str(&format!("{}Signal {{\n", indent_str));
                ctx.push_str(&format!("{}  name: \"{}\",\n", indent_str, name));
                ctx.push_str(&format!("{}  expr: ", indent_str));
                expr.render(ctx, indent + 1);
                ctx.push_str(&format!(",\n{}  span: {:?},\n", indent_str, span));
                ctx.push_str(&format!("{}  semi: {:?}\n", indent_str, semi));
                ctx.push_str(&format!("{}}}", indent_str));
            }
            Stmt::Break { span, semi } => {
                ctx.push_str(&format!(
                    "{}Break {{ span: {:?}, semi: {:?} }}",
                    indent_str, span, semi
                ));
            }
            Stmt::Continue { span, semi } => {
                ctx.push_str(&format!(
                    "{}Continue {{ span: {:?}, semi: {:?} }}",
                    indent_str, span, semi
                ));
            }
        }
    }
}

impl AstRender for StmtOrBlock {
    fn render(&self, ctx: &mut RenderCtx<'_>, indent: usize) {
        match self {
            StmtOrBlock::Stmt(stmt) => stmt.render(ctx, indent),
            StmtOrBlock::Block(block) => block.render(ctx, indent),
        }
    }
}

impl AstRender for Expr {
    fn render(&self, ctx: &mut RenderCtx<'_>, indent: usize) {
        let indent_str = "  ".repeat(indent);
        match self {
            Expr::LitInt(value, span) => {
                ctx.push_str(&format!("LitInt(\"{}\", span={:?})", value, span));
            }
            Expr::LitFloat(value, span) => {
                ctx.push_str(&format!("LitFloat(\"{}\", span={:?})", value, span));
            }
            Expr::LitString(value, span) => {
                ctx.push_str(&format!("LitString(\"{}\", span={:?})", value, span));
            }
            Expr::Ident(name, span) => {
                ctx.push_str(&format!("Ident(\"{}\", span={:?})", name, span));
            }
            Expr::Call { callee, args, span } => {
                ctx.push_str(&format!("Call {{\n"));
                ctx.push_str(&format!("{}  callee: ", indent_str));
                callee.render(ctx, indent + 1);
                ctx.push_str(&format!(",\n{}  args: [\n", indent_str));
                for (i, arg) in args.iter().enumerate() {
                    if i > 0 {
                        ctx.push_str(",\n");
                    }
                    ctx.push_str(&format!("{}    ", indent_str));
                    arg.render(ctx, indent + 2);
                }
                ctx.push_str(&format!(
                    "\n{}  ],\n{}  span: {:?}\n",
                    indent_str, indent_str, span
                ));
                ctx.push_str(&format!("{}}}", indent_str));
            }
            Expr::Index { base, index, span } => {
                ctx.push_str(&format!("Index {{\n"));
                ctx.push_str(&format!("{}  base: ", indent_str));
                base.render(ctx, indent + 1);
                ctx.push_str(&format!(",\n{}  index: ", indent_str));
                index.render(ctx, indent + 1);
                ctx.push_str(&format!(",\n{}  span: {:?}\n", indent_str, span));
                ctx.push_str(&format!("{}}}", indent_str));
            }
            Expr::Array { elems, span } => {
                ctx.push_str(&format!("Array {{\n"));
                ctx.push_str(&format!("{}  elems: [\n", indent_str));
                for (i, elem) in elems.iter().enumerate() {
                    if i > 0 {
                        ctx.push_str(",\n");
                    }
                    ctx.push_str(&format!("{}    ", indent_str));
                    elem.render(ctx, indent + 2);
                }
                ctx.push_str(&format!("\n{}  ],\n", indent_str));
                ctx.push_str(&format!("{}  span: {:?}\n", indent_str, span));
                ctx.push_str(&format!("{}}}", indent_str));
            }
            Expr::Unary { op, rhs, span } => {
                ctx.push_str(&format!("Unary {{\n"));
                ctx.push_str(&format!("{}  op: {:?},\n", indent_str, op));
                ctx.push_str(&format!("{}  rhs: ", indent_str));
                rhs.render(ctx, indent + 1);
                ctx.push_str(&format!(",\n{}  span: {:?}\n", indent_str, span));
                ctx.push_str(&format!("{}}}", indent_str));
            }
            Expr::Binary { lhs, op, rhs, span } => {
                ctx.push_str(&format!("Binary {{\n"));
                ctx.push_str(&format!("{}  lhs: ", indent_str));
                lhs.render(ctx, indent + 1);
                ctx.push_str(&format!(",\n{}  op: {:?},\n", indent_str, op));
                ctx.push_str(&format!("{}  rhs: ", indent_str));
                rhs.render(ctx, indent + 1);
                ctx.push_str(&format!(",\n{}  span: {:?}\n", indent_str, span));
                ctx.push_str(&format!("{}}}", indent_str));
            }
            Expr::Assign { lhs, rhs, op, span } => {
                ctx.push_str(&format!("Assign {{\n"));
                ctx.push_str(&format!("{}  lhs: ", indent_str));
                lhs.render(ctx, indent + 1);
                ctx.push_str(&format!(",\n{}  op: {:?},\n", indent_str, op));
                ctx.push_str(&format!("{}  rhs: ", indent_str));
                rhs.render(ctx, indent + 1);
                ctx.push_str(&format!(",\n{}  span: {:?}\n", indent_str, span));
                ctx.push_str(&format!("{}}}", indent_str));
            }
            Expr::Compare {
                scrutinee,
                arms,
                span,
            } => {
                ctx.push_str(&format!("Compare {{\n"));
                ctx.push_str(&format!("{}  scrutinee: ", indent_str));
                scrutinee.render(ctx, indent + 1);
                ctx.push_str(&format!(",\n{}  arms: [\n", indent_str));
                for (i, arm) in arms.iter().enumerate() {
                    if i > 0 {
                        ctx.push_str(",\n");
                    }
                    ctx.push_str(&format!("{}    Arm {{\n", indent_str));
                    ctx.push_str(&format!("{}      pattern: ", indent_str));
                    arm.pattern.render(ctx, indent + 3);
                    if let Some(guard) = &arm.guard {
                        ctx.push_str(&format!(",\n{}      guard: ", indent_str));
                        guard.render(ctx, indent + 3);
                    } else {
                        ctx.push_str(&format!(",\n{}      guard: None", indent_str));
                    }
                    ctx.push_str(&format!(",\n{}      expr: ", indent_str));
                    arm.expr.render(ctx, indent + 3);
                    ctx.push_str(&format!(",\n{}      span: {:?}\n", indent_str, arm.span));
                    ctx.push_str(&format!("{}    }}", indent_str));
                }
                ctx.push_str(&format!("\n{}  ],\n", indent_str));
                ctx.push_str(&format!("{}  span: {:?}\n", indent_str, span));
                ctx.push_str(&format!("{}}}", indent_str));
            }
            Expr::Ternary {
                cond,
                then_branch,
                else_branch,
                span,
            } => {
                ctx.push_str(&format!("Ternary {{\n"));
                ctx.push_str(&format!("{}  cond: ", indent_str));
                cond.render(ctx, indent + 1);
                ctx.push_str(&format!(",\n{}  then: ", indent_str));
                then_branch.render(ctx, indent + 1);
                ctx.push_str(&format!(",\n{}  else: ", indent_str));
                else_branch.render(ctx, indent + 1);
                ctx.push_str(&format!(",\n{}  span: {:?}\n", indent_str, span));
                ctx.push_str(&format!("{}}}", indent_str));
            }
            Expr::Let {
                name,
                ty,
                init,
                mutable,
                span,
            } => {
                ctx.push_str(&format!("LetExpr {{\n"));
                ctx.push_str(&format!("{}  name: \"{}\",\n", indent_str, name));
                ctx.push_str(&format!("{}  mutable: {},\n", indent_str, mutable));
                ctx.push_str(&format!("{}  ty: ", indent_str));
                if let Some(ty) = ty {
                    ty.render(ctx, indent + 1);
                    ctx.push_str(",\n");
                } else {
                    ctx.push_str("None,\n");
                }
                ctx.push_str(&format!("{}  init: ", indent_str));
                if let Some(expr) = init {
                    expr.render(ctx, indent + 1);
                    ctx.push_str(",\n");
                } else {
                    ctx.push_str("None,\n");
                }
                ctx.push_str(&format!("{}  span: {:?}\n", indent_str, span));
                ctx.push_str(&format!("{}}}", indent_str));
            }
            Expr::ParallelMap {
                seq,
                args,
                func,
                span,
            } => {
                ctx.push_str(&format!("ParallelMap {{\n"));
                ctx.push_str(&format!("{}  seq: ", indent_str));
                seq.render(ctx, indent + 1);
                ctx.push_str(&format!(",\n{}  args: [\n", indent_str));
                for (i, arg) in args.iter().enumerate() {
                    if i > 0 {
                        ctx.push_str(",\n");
                    }
                    ctx.push_str(&format!("{}    ", indent_str));
                    arg.render(ctx, indent + 2);
                }
                ctx.push_str(&format!("\n{}  ],\n{}  func: ", indent_str, indent_str));
                func.render(ctx, indent + 1);
                ctx.push_str(&format!("\n{}  span: {:?}\n", indent_str, span));
                ctx.push_str(&format!("{}}}", indent_str));
            }
            Expr::ParallelReduce {
                seq,
                init,
                args,
                func,
                span,
            } => {
                ctx.push_str(&format!("ParallelReduce {{\n"));
                ctx.push_str(&format!("{}  seq: ", indent_str));
                seq.render(ctx, indent + 1);
                ctx.push_str(&format!(",\n{}  init: ", indent_str));
                init.render(ctx, indent + 1);
                ctx.push_str(&format!(",\n{}  args: [\n", indent_str));
                for (i, arg) in args.iter().enumerate() {
                    if i > 0 {
                        ctx.push_str(",\n");
                    }
                    ctx.push_str(&format!("{}    ", indent_str));
                    arg.render(ctx, indent + 2);
                }
                ctx.push_str(&format!("\n{}  ],\n{}  func: ", indent_str, indent_str));
                func.render(ctx, indent + 1);
                ctx.push_str(&format!(",\n{}  span: {:?}\n", indent_str, span));
                ctx.push_str(&format!("{}}}", indent_str));
            }
        }
    }
}

impl AstRender for Pattern {
    fn render(&self, ctx: &mut RenderCtx<'_>, indent: usize) {
        match &self.kind {
            PatternKind::Finally => {
                ctx.push_str(&format!("Finally(span={:?})", self.span));
            }
            PatternKind::Nothing => {
                ctx.push_str(&format!("Nothing(span={:?})", self.span));
            }
            PatternKind::Binding(name) => {
                ctx.push_str(&format!("Binding(\"{}\", span={:?})", name, self.span));
            }
            PatternKind::Literal(expr) => {
                ctx.push_str("Literal(");
                expr.render(ctx, indent + 1);
                ctx.push_str(&format!(", span={:?})", self.span));
            }
            PatternKind::Tag { name, args } => {
                ctx.push_str(&format!("Tag {{ name: \"{}\", args: [", name));
                for (i, arg) in args.iter().enumerate() {
                    if i > 0 {
                        ctx.push_str(", ");
                    }
                    arg.render(ctx, indent + 1);
                }
                ctx.push_str(&format!("], span: {:?} }}", self.span));
            }
        }
    }
}

impl AstRender for TypeNode {
    fn render(&self, ctx: &mut RenderCtx<'_>, indent: usize) {
        let indent_str = "  ".repeat(indent);
        ctx.push_str("TypeNode {\n");
        ctx.push_str(&format!("{}  text: \"{}\",\n", indent_str, self.repr));
        ctx.push_str(&format!("{}  span: {:?}\n", indent_str, self.span));
        ctx.push_str(&format!("{}}}", indent_str));
    }
}

impl AstRender for Attr {
    fn render(&self, ctx: &mut RenderCtx<'_>, _indent: usize) {
        match self {
            Attr::Pure { span } => ctx.push_str(&format!("Pure({:?})", span)),
            Attr::Overload { span } => ctx.push_str(&format!("Overload({:?})", span)),
            Attr::Override { span } => ctx.push_str(&format!("Override({:?})", span)),
            Attr::Intrinsic { span } => ctx.push_str(&format!("Intrinsic({:?})", span)),
            Attr::Backend {
                span,
                value,
                value_span,
            } => ctx.push_str(&format!(
                "Backend({:?}, \"{}\", {:?})",
                span, value, value_span
            )),
            Attr::Deprecated {
                span,
                message,
                message_span,
            } => ctx.push_str(&format!(
                "Deprecated({:?}, \"{}\", {:?})",
                span, message, message_span
            )),
            Attr::Packed { span } => ctx.push_str(&format!("Packed({:?})", span)),
            Attr::Align {
                span,
                value,
                value_span,
            } => ctx.push_str(&format!("Align({:?}, {}, {:?})", span, value, value_span)),
            Attr::Shared { span } => ctx.push_str(&format!("Shared({:?})", span)),
            Attr::Atomic { span } => ctx.push_str(&format!("Atomic({:?})", span)),
            Attr::Raii { span } => ctx.push_str(&format!("Raii({:?})", span)),
            Attr::Arena { span } => ctx.push_str(&format!("Arena({:?})", span)),
            Attr::Weak { span } => ctx.push_str(&format!("Weak({:?})", span)),
            Attr::Readonly { span } => ctx.push_str(&format!("Readonly({:?})", span)),
            Attr::Hidden { span } => ctx.push_str(&format!("Hidden({:?})", span)),
            Attr::NoInherit { span } => ctx.push_str(&format!("NoInherit({:?})", span)),
            Attr::Sealed { span } => ctx.push_str(&format!("Sealed({:?})", span)),
            Attr::GuardedBy {
                span,
                lock,
                lock_span,
            } => ctx.push_str(&format!(
                "GuardedBy({:?}, \"{}\", {:?})",
                span, lock, lock_span
            )),
            Attr::RequiresLock {
                span,
                lock,
                lock_span,
            } => ctx.push_str(&format!(
                "RequiresLock({:?}, \"{}\", {:?})",
                span, lock, lock_span
            )),
            Attr::AcquiresLock {
                span,
                lock,
                lock_span,
            } => ctx.push_str(&format!(
                "AcquiresLock({:?}, \"{}\", {:?})",
                span, lock, lock_span
            )),
            Attr::ReleasesLock {
                span,
                lock,
                lock_span,
            } => ctx.push_str(&format!(
                "ReleasesLock({:?}, \"{}\", {:?})",
                span, lock, lock_span
            )),
            Attr::WaitsOn {
                span,
                cond,
                cond_span,
            } => ctx.push_str(&format!(
                "WaitsOn({:?}, \"{}\", {:?})",
                span, cond, cond_span
            )),
            Attr::Send { span } => ctx.push_str(&format!("Send({:?})", span)),
            Attr::NoSend { span } => ctx.push_str(&format!("NoSend({:?})", span)),
            Attr::NonBlocking { span } => ctx.push_str(&format!("NonBlocking({:?})", span)),
        }
    }
}

impl AstRender for TypeDef {
    fn render(&self, ctx: &mut RenderCtx<'_>, indent: usize) {
        let indent_str = "  ".repeat(indent);
        ctx.push_str(&format!("{}TypeDef {{\n", indent_str));
        ctx.push_str(&format!("{}  name: \"{}\",\n", indent_str, self.name));
        ctx.push_str(&format!("{}  span: {:?}\n", indent_str, self.span));
        ctx.push_str(&format!("{}}}", indent_str));
    }
}

impl AstRender for LiteralDef {
    fn render(&self, ctx: &mut RenderCtx<'_>, indent: usize) {
        let indent_str = "  ".repeat(indent);
        ctx.push_str(&format!("{}LiteralDef {{\n", indent_str));
        ctx.push_str(&format!("{}  name: \"{}\",\n", indent_str, self.name));
        ctx.push_str(&format!("{}  span: {:?}\n", indent_str, self.span));
        ctx.push_str(&format!("{}}}", indent_str));
    }
}

impl AstRender for AliasDef {
    fn render(&self, ctx: &mut RenderCtx<'_>, indent: usize) {
        let indent_str = "  ".repeat(indent);
        ctx.push_str(&format!("{}AliasDef {{\n", indent_str));
        ctx.push_str(&format!("{}  name: \"{}\",\n", indent_str, self.name));
        ctx.push_str(&format!("{}  span: {:?}\n", indent_str, self.span));
        ctx.push_str(&format!("{}}}", indent_str));
    }
}

impl AstRender for ExternBlock {
    fn render(&self, ctx: &mut RenderCtx<'_>, indent: usize) {
        let indent_str = "  ".repeat(indent);
        ctx.push_str(&format!("{}ExternBlock {{\n", indent_str));
        ctx.push_str(&format!("{}  name: ", indent_str));
        if let Some(name) = &self.name {
            ctx.push_str(&format!("Some(\"{}\")", name));
        } else {
            ctx.push_str("None");
        }
        ctx.push_str(&format!(",\n{}  span: {:?}\n", indent_str, self.span));
        ctx.push_str(&format!("{}}}", indent_str));
    }
}

impl AstRender for Import {
    fn render(&self, ctx: &mut RenderCtx<'_>, indent: usize) {
        let indent_str = "  ".repeat(indent);
        ctx.push_str(&format!("{}Import {{\n", indent_str));
        ctx.push_str(&format!("{}  path: \"{}\",\n", indent_str, self.path));
        ctx.push_str(&format!("{}  alias: ", indent_str));
        if let Some(alias) = &self.alias {
            ctx.push_str(&format!("Some(\"{}\")", alias));
        } else {
            ctx.push_str("None");
        }
        ctx.push_str(&format!(",\n{}  span: {:?}\n", indent_str, self.span));
        ctx.push_str(&format!("{}}}", indent_str));
    }
}

/// Render tokens in a human-readable table.
pub fn render_tokens_table(src: &str, tokens: &[Token]) -> String {
    let mut out = String::new();
    let _ = writeln!(out, " IDX   START..END    KIND                 LEXEME");
    for (i, t) in tokens.iter().enumerate() {
        let s = t.span.start as usize;
        let e = t.span.end as usize;
        let lexeme = &src.get(s..e).unwrap_or("");
        let lexeme = escape_and_truncate(lexeme, 80);
        let _ = writeln!(
            out,
            "{:>4}  {:>5}..{:<5}  {:<20}  \"{}\"",
            i,
            t.span.start,
            t.span.end,
            format!("{:?}", t.kind),
            lexeme
        );
    }
    out
}

fn escape_and_truncate(s: &str, max: usize) -> String {
    let mut out = String::with_capacity(s.len());
    for ch in s.chars() {
        match ch {
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            _ => out.push(ch),
        }
        if out.len() >= max {
            out.push('…');
            break;
        }
    }
    out
}
