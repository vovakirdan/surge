use std::{fs, io::{self, Read, Write}, path::{Path, PathBuf}};
use anyhow::{Result, Context};
use clap::{Parser, Subcommand, ValueEnum};
use walkdir::WalkDir;

use surge_token::SourceId;
use surge_lexer::{lex, LexOptions};
use surge_diagnostics::{
    SourceMap, InMemorySourceText, from_lexer_diags,
    Reporter, ReportOptions, Format,
};

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
            io::stdin().read_to_string(&mut buf).context("failed to read stdin")?;
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
#[command(name="surge", version, about="Surge toolchain")]
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
        #[arg(long, value_enum, default_value="pretty")]
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
}

#[derive(Clone, Copy, ValueEnum)]
enum Fmt { Pretty, Json, Csv }

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
        Cmd::Diag { path, format, out, keep_trivia, enable_directives } => {
            run_diag(path, format.into(), out, keep_trivia, enable_directives)
        }
        Cmd::Tokenize { path, keep_trivia, enable_directives } => {
            run_tokenize(path, keep_trivia, enable_directives)
        }
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
    let lex_opts = LexOptions { keep_trivia, enable_directives };
    
    // Прогнать лексер, собрать диагностики
    let mut all_diags = Vec::new();
    
    for source in &input.sources {
        let res = lex(&source.content, source.id, &lex_opts);
        let mut diags = from_lexer_diags(source.id, &res.diags);
        all_diags.append(&mut diags);
    }
    
    // Вывести диагностику выбранным форматтером
    let reporter = Reporter::new(
        input.source_map,
        Box::new(input.text_provider),
        ReportOptions { format }
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

/// Команда tokenize: токенизация с выводом токенов в stdout
fn run_tokenize(
    path: Option<String>,
    keep_trivia: bool,
    enable_directives: bool,
) -> Result<()> {
    // Собрать входные данные через общий интерфейс
    let input = collect_inputs(path)?;
    
    // Настроить опции лексера
    let lex_opts = LexOptions { keep_trivia, enable_directives };
    
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
            i, t.span.start, t.span.end, format!("{:?}", t.kind), lexeme
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
