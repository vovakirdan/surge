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

#[derive(Parser)]
#[command(name="surge", version, about="Surge toolchain")]
struct Cli {
    #[command(subcommand)]
    cmd: Cmd,
}

#[derive(Subcommand)]
enum Cmd {
    /// Tokenize a .sg file/dir or stdin (-), then print diagnostics
    Tokenize {
        /// Path to .sg file/dir or '-' for stdin. Omit to use stdin.
        path: Option<String>,

        /// Diagnostics format
        #[arg(long, value_enum, default_value="pretty")]
        format: Fmt,

        /// Keep trivia in lexer
        #[arg(long)]
        keep_trivia: bool,

        /// Enable /// directives
        #[arg(long)]
        enable_directives: bool,

        /// Write diagnostics to file instead of stdout
        #[arg(long)]
        out: Option<PathBuf>,

        /// If there are no errors, print tokens (debug)
        #[arg(long)]
        print_tokens: bool,
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
        Cmd::Tokenize { path, format, keep_trivia, enable_directives, out, print_tokens } => {
            run_tokenize(path, format.into(), keep_trivia, enable_directives, out, print_tokens)
        }
    }
}

fn run_tokenize(
    path: Option<String>,
    fmt: Format,
    keep_trivia: bool,
    enable_directives: bool,
    out: Option<PathBuf>,
    print_tokens: bool,
) -> Result<()> {
    // 1) собрать входные «файлы» (включая stdin)
    let mut inputs = Vec::<(SourceId, String, String)>::new(); // (sid, label, source)
    let mut sid_counter: u32 = 0;

    match path.as_deref() {
        None | Some("-") => {
            let mut buf = String::new();
            io::stdin().read_to_string(&mut buf).context("failed to read stdin")?;
            inputs.push((SourceId(sid_counter), "<stdin>".into(), buf));
        }
        Some(p) => {
            let p = Path::new(p);
            if p.is_dir() {
                for entry in WalkDir::new(p).into_iter().filter_map(Result::ok) {
                    let path = entry.path();
                    if path.is_file() && path.extension().and_then(|s| s.to_str()) == Some("sg") {
                        let src = fs::read_to_string(path)
                            .with_context(|| format!("failed to read {}", path.display()))?;
                        inputs.push((SourceId(sid_counter), path.display().to_string(), src));
                        sid_counter += 1;
                    }
                }
            } else {
                let src = fs::read_to_string(p)
                    .with_context(|| format!("failed to read {}", p.display()))?;
                inputs.push((SourceId(sid_counter), p.display().to_string(), src));
            }
        }
    }

    // 2) прогнать лексер, собрать диагностики
    let mut all_diags = Vec::new();
    let mut sm = SourceMap::new();
    let mut texts = InMemorySourceText::new();

    let lex_opts = LexOptions { keep_trivia, enable_directives };

    // Чтобы решить печать токенов: сохраняем токены, если попросили
    let mut saved_tokens: Vec<(String, String)> = Vec::new(); // (label, printable_table)

    for (sid, label, src) in &inputs {
        sm.insert(*sid, label.clone());
        texts.insert(*sid, src.clone());

        let res = lex(src, *sid, &lex_opts);
        let mut diags = from_lexer_diags(*sid, &res.diags);
        all_diags.append(&mut diags);

        if print_tokens {
            // Сгенерировать printable таблицу токенов на случай отсутствия ошибок
            let table = render_tokens_table(src, &res.tokens);
            saved_tokens.push((label.clone(), table));
        }
    }

    // 3) вывести диагностику выбранным форматтером
    let reporter = Reporter::new(sm, Box::new(texts), ReportOptions { format: fmt });
    let rendered = reporter.render(&all_diags)?;

    if let Some(out_path) = out {
        fs::write(&out_path, rendered.as_bytes())
            .with_context(|| format!("failed to write {}", out_path.display()))?;
    } else {
        let mut stdout = io::stdout().lock();
        stdout.write_all(rendered.as_bytes())?;
    }

    // 4) если ошибок нет и просили токены — печатаем таблицы токенов после диагностики
    let has_errors = !all_diags.is_empty();
    if print_tokens && !has_errors {
        let mut stdout = io::stdout().lock();
        for (label, table) in saved_tokens {
            writeln!(stdout, "== TOKENS: {} ==", label)?;
            stdout.write_all(table.as_bytes())?;
            writeln!(stdout)?;
        }
    }

    // 5) код возврата
    if has_errors {
        std::process::exit(1);
    } else {
        Ok(())
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
