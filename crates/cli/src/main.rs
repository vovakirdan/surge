use anyhow::Context;
use clap::{Parser, Subcommand};
use surge_lexer::lex;
use surge_token::{SourceId, TokenKind};

#[derive(Parser)]
#[command(name = "surge", version, about = "Surge toolchain")]
struct Cli {
    #[command(subcommand)]
    cmd: Cmd,
}

#[derive(Subcommand)]
enum Cmd {
    /// Tokenize a .sg file and print tokens
    Tokenize { file: String },
}

fn main() -> anyhow::Result<()> {
    let cli = Cli::parse();
    match cli.cmd {
        Cmd::Tokenize { file } => tokenize(&file),
    }
}

fn tokenize(file: &str) -> anyhow::Result<()> {
    let src = std::fs::read_to_string(file)
        .with_context(|| format!("failed to read {}", file))?;
    let res = lex(&src, SourceId(0));

    for t in res.tokens {
        match t.kind {
            TokenKind::Eof => println!("{:?} @ {:?}", t.kind, t.span),
            _ => println!("{:?} @ {:?}", t.kind, t.span),
        }
    }
    for d in res.diags {
        eprintln!("diag: {d:?}");
    }
    Ok(())
}
