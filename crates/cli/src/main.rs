use anyhow::{Context, Result};
use clap::{Parser, Subcommand, ValueEnum};
use std::{
    fs,
    io::{self, Read, Write},
    path::{Path, PathBuf},
};
use walkdir::WalkDir;

use surge_diagnostics::{
    Format, InMemorySourceText, ReportOptions, Reporter, SourceMap, from_lexer_diags,
    from_parser_diags,
};
use surge_lexer::{LexOptions, lex};
use surge_parser::{
    Ast, Attr, Block, Expr, Func, FuncSig, Item, Module, Param, Stmt, TypeNode,
    parse_source_with_options, parse_tokens,
};
use surge_token::SourceId;

/// Источник входных данных
pub struct InputSource {
    pub id: SourceId,
    pub label: String,
    pub content: String,
}

/// Результат обработки входных данных
pub struct ProcessedInput {
    pub sources: Vec<InputSource>,
    pub source_map: SourceMap,
    pub text_provider: InMemorySourceText,
}

/// Общий интерфейс для обработки входных данных
pub fn collect_inputs(path: Option<String>) -> Result<ProcessedInput> {
    let mut sources = Vec::new();
    let mut source_map = SourceMap::new();
    let mut text_provider = InMemorySourceText::new();
    let mut sid_counter: u32 = 0;

    match path.as_deref() {
        None | Some("-") => {
            let mut buf = String::new();
            io::stdin()
                .read_to_string(&mut buf)
                .context("failed to read stdin")?;
            let source = InputSource {
                id: SourceId(sid_counter),
                label: "<stdin>".to_string(),
                content: buf,
            };
            source_map.insert(source.id, source.label.clone());
            text_provider.insert(source.id, source.content.clone());
            sources.push(source);
        }
        Some(p) => {
            let p = Path::new(p);
            if p.is_dir() {
                for entry in WalkDir::new(p).into_iter().filter_map(Result::ok) {
                    let path = entry.path();
                    if path.is_file() && path.extension().and_then(|s| s.to_str()) == Some("sg") {
                        let content = fs::read_to_string(path)
                            .with_context(|| format!("failed to read {}", path.display()))?;
                        let source = InputSource {
                            id: SourceId(sid_counter),
                            label: path.display().to_string(),
                            content,
                        };
                        source_map.insert(source.id, source.label.clone());
                        text_provider.insert(source.id, source.content.clone());
                        sources.push(source);
                        sid_counter += 1;
                    }
                }
            } else {
                let content = fs::read_to_string(p)
                    .with_context(|| format!("failed to read {}", p.display()))?;
                let source = InputSource {
                    id: SourceId(sid_counter),
                    label: p.display().to_string(),
                    content,
                };
                source_map.insert(source.id, source.label.clone());
                text_provider.insert(source.id, source.content.clone());
                sources.push(source);
            }
        }
    }

    Ok(ProcessedInput {
        sources,
        source_map,
        text_provider,
    })
}

#[derive(Parser)]
#[command(name = "surge", version, about = "Surge toolchain")]
struct Cli {
    #[command(subcommand)]
    cmd: Cmd,
}

#[derive(Subcommand)]
enum Cmd {
    /// Run diagnostics on .sg file/dir or stdin and output formatted results
    Diag {
        /// Path to .sg file/dir or '-' for stdin. Omit to use stdin.
        path: Option<String>,

        /// Diagnostics format
        #[arg(long, value_enum, default_value = "pretty")]
        format: Fmt,

        /// Write diagnostics to file instead of stdout
        #[arg(long)]
        out: Option<PathBuf>,

        /// Keep trivia in lexer
        #[arg(long)]
        keep_trivia: bool,

        /// Enable /// directives
        #[arg(long)]
        enable_directives: bool,
    },

    /// Tokenize .sg file/dir or stdin and print tokens to stdout
    Tokenize {
        /// Path to .sg file/dir or '-' for stdin. Omit to use stdin.
        path: Option<String>,

        /// Keep trivia in lexer
        #[arg(long)]
        keep_trivia: bool,

        /// Enable /// directives
        #[arg(long)]
        enable_directives: bool,
    },

    /// Parse .sg file/dir or stdin and print AST tree to stdout
    Parse {
        /// Path to .sg file/dir or '-' for stdin. Omit to use stdin.
        path: Option<String>,

        /// Keep trivia in lexer
        #[arg(long)]
        keep_trivia: bool,

        /// Enable /// directives
        #[arg(long)]
        enable_directives: bool,
    },
}

#[derive(Clone, Copy, ValueEnum)]
enum Fmt {
    Pretty,
    Json,
    Csv,
}

impl From<Fmt> for Format {
    fn from(f: Fmt) -> Self {
        match f {
            Fmt::Pretty => Format::Pretty,
            Fmt::Json => Format::Json,
            Fmt::Csv => Format::Csv,
        }
    }
}

fn main() -> Result<()> {
    let cli = Cli::parse();
    match cli.cmd {
        Cmd::Diag {
            path,
            format,
            out,
            keep_trivia,
            enable_directives,
        } => run_diag(path, format.into(), out, keep_trivia, enable_directives),
        Cmd::Tokenize {
            path,
            keep_trivia,
            enable_directives,
        } => run_tokenize(path, keep_trivia, enable_directives),
        Cmd::Parse {
            path,
            keep_trivia,
            enable_directives,
        } => run_parse(path, keep_trivia, enable_directives),
    }
}

/// Команда diag: запуск диагностики с форматированным выводом
fn run_diag(
    path: Option<String>,
    format: Format,
    out: Option<PathBuf>,
    keep_trivia: bool,
    enable_directives: bool,
) -> Result<()> {
    // Собрать входные данные через общий интерфейс
    let input = collect_inputs(path)?;

    // Настроить опции лексера
    let lex_opts = LexOptions {
        keep_trivia,
        enable_directives,
    };

    // Прогнать лексер, собрать диагностики
    let mut all_diags = Vec::new();

    for source in &input.sources {
        let res = lex(&source.content, source.id, &lex_opts);
        let parse_res = parse_tokens(source.id, &res.tokens);

        let mut lex_diags = from_lexer_diags(source.id, &res.diags);
        let mut parse_diags = from_parser_diags(source.id, &parse_res.diags);

        all_diags.append(&mut lex_diags);
        all_diags.append(&mut parse_diags);
    }

    // Вывести диагностику выбранным форматтером
    let reporter = Reporter::new(
        input.source_map,
        Box::new(input.text_provider),
        ReportOptions { format },
    );
    let rendered = reporter.render(&all_diags)?;

    if let Some(out_path) = out {
        fs::write(&out_path, rendered.as_bytes())
            .with_context(|| format!("failed to write {}", out_path.display()))?;
    } else {
        let mut stdout = io::stdout().lock();
        stdout.write_all(rendered.as_bytes())?;
    }

    // Код возврата: 1 если есть диагностики, 0 если нет
    if !all_diags.is_empty() {
        std::process::exit(1);
    } else {
        Ok(())
    }
}

/// Команда parse: парсинг с выводом AST дерева в stdout
fn run_parse(path: Option<String>, keep_trivia: bool, enable_directives: bool) -> Result<()> {
    // Собрать входные данные через общий интерфейс
    let input = collect_inputs(path)?;

    // Настроить опции лексера
    let lex_opts = LexOptions {
        keep_trivia,
        enable_directives,
    };

    let mut stdout = io::stdout().lock();

    // Обработать каждый источник
    for source in &input.sources {
        // Парсим с пользовательскими опциями лексера
        let (parse_res, _lex_res) =
            parse_source_with_options(source.id, &source.content, &lex_opts);

        // Вывести заголовок для источника (если их больше одного)
        if input.sources.len() > 1 {
            writeln!(stdout, "== AST: {} ==", source.label)?;
        }

        // Вывести AST дерево
        let ast_tree = render_ast_tree(&parse_res.ast, &source.content, source.id);
        stdout.write_all(ast_tree.as_bytes())?;

        if input.sources.len() > 1 {
            writeln!(stdout)?;
        }

        // Вывести диагностики парсера, если есть
        if !parse_res.diags.is_empty() {
            writeln!(stdout, "\nParser diagnostics:")?;
            for diag in &parse_res.diags {
                writeln!(stdout, "  {:?}", diag)?;
            }
        }
    }

    Ok(())
}

/// Команда tokenize: токенизация с выводом токенов в stdout
fn run_tokenize(path: Option<String>, keep_trivia: bool, enable_directives: bool) -> Result<()> {
    // Собрать входные данные через общий интерфейс
    let input = collect_inputs(path)?;

    // Настроить опции лексера
    let lex_opts = LexOptions {
        keep_trivia,
        enable_directives,
    };

    let mut stdout = io::stdout().lock();

    // Обработать каждый источник
    for source in &input.sources {
        let res = lex(&source.content, source.id, &lex_opts);

        // Вывести заголовок для источника (если их больше одного)
        if input.sources.len() > 1 {
            writeln!(stdout, "== TOKENS: {} ==", source.label)?;
        }

        // Вывести таблицу токенов
        let table = render_tokens_table(&source.content, &res.tokens);
        stdout.write_all(table.as_bytes())?;

        if input.sources.len() > 1 {
            writeln!(stdout)?;
        }
    }

    Ok(())
}

/// Извлечь текст из исходного кода по span
fn get_text_from_span(src: &str, span: &surge_token::Span) -> String {
    let start = span.start as usize;
    let end = span.end as usize;
    src.get(start..end).unwrap_or("").to_string()
}

/// Напечатать AST дерево с красивой индентацией
fn render_ast_tree(ast: &Ast, src: &str, source_id: SourceId) -> String {
    let mut output = String::new();
    output.push_str("AST Tree:\n");
    render_module(&mut output, &ast.module, src, source_id, 0);
    output
}

fn render_module(
    output: &mut String,
    module: &Module,
    src: &str,
    source_id: SourceId,
    indent: usize,
) {
    let indent_str = "  ".repeat(indent);
    output.push_str(&format!("{}Module {{\n", indent_str));
    output.push_str(&format!("{}  items: [\n", indent_str));

    for (i, item) in module.items.iter().enumerate() {
        if i > 0 {
            output.push_str(",\n");
        }
        render_item(output, item, src, source_id, indent + 2);
    }

    output.push_str(&format!("\n{}  ]\n", indent_str));
    output.push_str(&format!("{}}}\n", indent_str));
}

fn render_item(output: &mut String, item: &Item, src: &str, source_id: SourceId, indent: usize) {
    let indent_str = "  ".repeat(indent);
    match item {
        Item::Fn(func) => {
            output.push_str(&format!("{}Fn(\n", indent_str));
            render_func(output, func, src, source_id, indent + 1);
            output.push_str(&format!("{})", indent_str));
        }
        Item::Let(stmt) => {
            output.push_str(&format!("{}Let(\n", indent_str));
            render_stmt(output, stmt, src, source_id, indent + 1);
            output.push_str(&format!("\n{})", indent_str));
        }
        _ => {
            output.push_str(&format!("{}<unimplemented item>", indent_str));
        }
    }
}

fn render_func(output: &mut String, func: &Func, src: &str, source_id: SourceId, indent: usize) {
    let indent_str = "  ".repeat(indent);
    output.push_str(&format!("{}sig: (\n", indent_str));
    render_func_sig(output, &func.sig, src, source_id, indent + 1);
    output.push_str(&format!("\n{})", indent_str));

    if let Some(body) = &func.body {
        output.push_str(&format!(",\n{}body: (\n", indent_str));
        render_block(output, body, src, source_id, indent + 1);
        output.push_str(&format!("\n{})", indent_str));
    } else {
        output.push_str(&format!(", body: None"));
    }

    output.push_str(&format!(", span: {:?}", func.span));
}

fn render_func_sig(
    output: &mut String,
    sig: &FuncSig,
    src: &str,
    source_id: SourceId,
    indent: usize,
) {
    let indent_str = "  ".repeat(indent);
    let name = get_text_from_span(src, &sig.span);
    output.push_str(&format!("{}name: \"{}\",\n", indent_str, name));
    output.push_str(&format!("{}params: [\n", indent_str));

    for (i, param) in sig.params.iter().enumerate() {
        if i > 0 {
            output.push_str(",\n");
        }
        render_param(output, param, src, source_id, indent + 1);
    }

    output.push_str(&format!(
        "\n{}]{}",
        indent_str,
        if sig.params.is_empty() { "" } else { "," }
    ));
    output.push_str(&format!("\n{}ret: ", indent_str));

    if let Some(ret) = &sig.ret {
        output.push_str(&format!("Some("));
        render_type_node(output, ret, src, source_id, indent + 1);
        output.push_str(")");
    } else {
        output.push_str("None");
    }

    output.push_str(&format!(",\n{}span: {:?}", indent_str, sig.span));
    output.push_str(&format!(",\n{}attrs: [", indent_str));

    for (i, attr) in sig.attrs.iter().enumerate() {
        if i > 0 {
            output.push_str(", ");
        }
        render_attr(output, attr);
    }

    output.push_str("]");
}

fn render_param(output: &mut String, param: &Param, src: &str, source_id: SourceId, indent: usize) {
    let indent_str = "  ".repeat(indent);
    let name = get_text_from_span(src, &param.span);
    output.push_str(&format!("{}Param {{\n", indent_str));
    output.push_str(&format!("{}  name: \"{}\",\n", indent_str, name));

    if let Some(ty) = &param.ty {
        output.push_str(&format!("{}  ty: ", indent_str));
        render_type_node(output, ty, src, source_id, indent + 1);
        output.push_str(",\n");
    } else {
        output.push_str(&format!("{}  ty: None,\n", indent_str));
    }

    output.push_str(&format!("{}  span: {:?}\n", indent_str, param.span));
    output.push_str(&format!("{}}}", indent_str));
}

fn render_block(output: &mut String, block: &Block, src: &str, source_id: SourceId, indent: usize) {
    let indent_str = "  ".repeat(indent);
    output.push_str(&format!("{}Block {{\n", indent_str));
    output.push_str(&format!("{}  stmts: [\n", indent_str));

    for (i, stmt) in block.stmts.iter().enumerate() {
        if i > 0 {
            output.push_str(",\n");
        }
        render_stmt(output, stmt, src, source_id, indent + 2);
    }

    output.push_str(&format!("\n{}  ],\n", indent_str));
    output.push_str(&format!("{}  span: {:?}\n", indent_str, block.span));
    output.push_str(&format!("{}}}", indent_str));
}

fn render_stmt(output: &mut String, stmt: &Stmt, src: &str, source_id: SourceId, indent: usize) {
    let indent_str = "  ".repeat(indent);
    match stmt {
        Stmt::Let {
            ty,
            init,
            mutable,
            span,
            semi,
            ..
        } => {
            let name_text = get_text_from_span(src, span);
            output.push_str(&format!("{}Let {{\n", indent_str));
            output.push_str(&format!("{}  name: \"{}\",\n", indent_str, name_text));
            output.push_str(&format!("{}  mutable: {},\n", indent_str, mutable));

            if let Some(ty) = ty {
                output.push_str(&format!("{}  ty: ", indent_str));
                render_type_node(output, ty, src, source_id, indent + 1);
                output.push_str(",\n");
            } else {
                output.push_str(&format!("{}  ty: None,\n", indent_str));
            }

            if let Some(init) = init {
                output.push_str(&format!("{}  init: ", indent_str));
                render_expr(output, init, src, source_id, indent + 1);
                output.push_str(",\n");
            } else {
                output.push_str(&format!("{}  init: None,\n", indent_str));
            }

            output.push_str(&format!("{}  span: {:?},\n", indent_str, span));
            output.push_str(&format!("{}  semi: {:?}\n", indent_str, semi));
            output.push_str(&format!("{}}}", indent_str));
        }
        Stmt::Signal { name, expr, span, semi } => {
            output.push_str(&format!("{}Signal {{\n", indent_str));
            output.push_str(&format!("{}  name: \"{}\",\n", indent_str, name));
            output.push_str(&format!("{}  expr: ", indent_str));
            render_expr(output, expr, src, source_id, indent + 1);
            output.push_str(&format!(",\n{}  span: {:?},\n", indent_str, span));
            output.push_str(&format!("{}  semi: {:?}\n", indent_str, semi));
            output.push_str(&format!("{}}}", indent_str));
        }
        Stmt::ExprStmt { expr, span, semi } => {
            output.push_str(&format!("{}ExprStmt {{\n", indent_str));
            output.push_str(&format!("{}  expr: ", indent_str));
            render_expr(output, expr, src, source_id, indent + 1);
            output.push_str(&format!(",\n{}  span: {:?},\n", indent_str, span));
            output.push_str(&format!("{}  semi: {:?}\n", indent_str, semi));
            output.push_str(&format!("{}}}", indent_str));
        }
        Stmt::Return { expr, span, semi } => {
            output.push_str(&format!("{}Return {{\n", indent_str));
            output.push_str(&format!("{}  expr: ", indent_str));
            if let Some(expr) = expr {
                render_expr(output, expr, src, source_id, indent + 1);
            } else {
                output.push_str("None");
            }
            output.push_str(&format!(",\n{}  span: {:?},\n", indent_str, span));
            output.push_str(&format!("{}  semi: {:?}\n", indent_str, semi));
            output.push_str(&format!("{}}}", indent_str));
        }
        Stmt::Break { span, semi } => {
            output.push_str(&format!("{}Break {{\n", indent_str));
            output.push_str(&format!("{}  span: {:?},\n", indent_str, span));
            output.push_str(&format!("{}  semi: {:?}\n", indent_str, semi));
            output.push_str(&format!("{}}}", indent_str));
        }
        Stmt::Continue { span, semi } => {
            output.push_str(&format!("{}Continue {{\n", indent_str));
            output.push_str(&format!("{}  span: {:?},\n", indent_str, span));
            output.push_str(&format!("{}  semi: {:?}\n", indent_str, semi));
            output.push_str(&format!("{}}}", indent_str));
        }
        _ => {
            output.push_str(&format!("{}<unimplemented stmt>", indent_str));
        }
    }
}

fn render_expr(output: &mut String, expr: &Expr, src: &str, source_id: SourceId, indent: usize) {
    let indent_str = "  ".repeat(indent);
    match expr {
        Expr::LitInt(val, span) => {
            output.push_str(&format!("LitInt(\"{}\", {:?})", val, span));
        }
        Expr::LitFloat(val, span) => {
            output.push_str(&format!("LitFloat(\"{}\", {:?})", val, span));
        }
        Expr::LitString(val, span) => {
            output.push_str(&format!("LitString(\"{}\", {:?})", val, span));
        }
        Expr::Ident(name, span) => {
            output.push_str(&format!("Ident(\"{}\", {:?})", name, span));
        }
        Expr::Binary { lhs, op, rhs, span } => {
            output.push_str(&format!("Binary {{\n"));
            output.push_str(&format!("{}  lhs: ", indent_str));
            render_expr(output, lhs, src, source_id, indent + 1);
            output.push_str(&format!(",\n{}  op: {:?},\n", indent_str, op));
            output.push_str(&format!("{}  rhs: ", indent_str));
            render_expr(output, rhs, src, source_id, indent + 1);
            output.push_str(&format!(",\n{}  span: {:?}\n", indent_str, span));
            output.push_str(&format!("{}}}", indent_str));
        }
        Expr::Unary { op, rhs, span } => {
            output.push_str(&format!("Unary {{\n"));
            output.push_str(&format!("{}  op: {:?},\n", indent_str, op));
            output.push_str(&format!("{}  rhs: ", indent_str));
            render_expr(output, rhs, src, source_id, indent + 1);
            output.push_str(&format!(",\n{}  span: {:?}\n", indent_str, span));
            output.push_str(&format!("{}}}", indent_str));
        }
        Expr::Call { callee, args, span } => {
            output.push_str(&format!("Call {{\n"));
            output.push_str(&format!("{}  callee: ", indent_str));
            render_expr(output, callee, src, source_id, indent + 1);
            output.push_str(&format!(",\n{}  args: [\n", indent_str));

            for (i, arg) in args.iter().enumerate() {
                if i > 0 {
                    output.push_str(",\n");
                }
                output.push_str(&format!("{}    ", indent_str));
                render_expr(output, arg, src, source_id, indent + 2);
            }

            output.push_str(&format!("\n{}  ],\n", indent_str));
            output.push_str(&format!("{}  span: {:?}\n", indent_str, span));
            output.push_str(&format!("{}}}", indent_str));
        }
        Expr::Array { elems, span } => {
            output.push_str(&format!("Array {{\n"));
            output.push_str(&format!("{}  elems: [\n", indent_str));
            for (i, elem) in elems.iter().enumerate() {
                if i > 0 {
                    output.push_str(",\n");
                }
                output.push_str(&format!("{}    ", indent_str));
                render_expr(output, elem, src, source_id, indent + 2);
            }
            output.push_str(&format!("\n{}  ],\n", indent_str));
            output.push_str(&format!("{}  span: {:?}\n", indent_str, span));
            output.push_str(&format!("{}}}", indent_str));
        }
        Expr::Index { base, index, span } => {
            output.push_str(&format!("Index {{\n"));
            output.push_str(&format!("{}  base: ", indent_str));
            render_expr(output, base, src, source_id, indent + 1);
            output.push_str(&format!(",\n{}  index: ", indent_str));
            render_expr(output, index, src, source_id, indent + 1);
            output.push_str(&format!(",\n{}  span: {:?}\n", indent_str, span));
            output.push_str(&format!("{}}}", indent_str));
        }
        Expr::Assign { lhs, rhs, span } => {
            output.push_str(&format!("Assign {{\n"));
            output.push_str(&format!("{}  lhs: ", indent_str));
            render_expr(output, lhs, src, source_id, indent + 1);
            output.push_str(&format!(",\n{}  rhs: ", indent_str));
            render_expr(output, rhs, src, source_id, indent + 1);
            output.push_str(&format!(",\n{}  span: {:?}\n", indent_str, span));
            output.push_str(&format!("{}}}", indent_str));
        }
        _ => {
            output.push_str(&format!("<unimplemented expr>"));
        }
    }
}

fn render_type_node(
    output: &mut String,
    type_node: &TypeNode,
    _src: &str,
    _source_id: SourceId,
    indent: usize,
) {
    let indent_str = "  ".repeat(indent);
    output.push_str(&format!("TypeNode {{\n"));
    output.push_str(&format!("{}  text: \"{}\",\n", indent_str, type_node.repr));
    output.push_str(&format!("{}  span: {:?}\n", indent_str, type_node.span));
    output.push_str(&format!("{}}}", indent_str));
}

fn render_attr(output: &mut String, attr: &Attr) {
    match attr {
        Attr::Pure { span } => {
            output.push_str(&format!("Pure({:?})", span));
        }
        Attr::Overload { span } => {
            output.push_str(&format!("Overload({:?})", span));
        }
        Attr::Override { span } => {
            output.push_str(&format!("Override({:?})", span));
        }
        Attr::Backend {
            span,
            value,
            value_span,
        } => {
            output.push_str(&format!(
                "Backend({:?}, \"{}\", {:?})",
                span, value, value_span
            ));
        }
    }
}

/// Напечатать таблицу токенов: IDX  START..END  KIND  "LEXEME"
fn render_tokens_table(src: &str, tokens: &[surge_token::Token]) -> String {
    use std::fmt::Write as _;
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
