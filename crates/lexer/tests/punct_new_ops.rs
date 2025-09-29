use surge_lexer::{LexOptions, lex};
use surge_token::{SourceId, TokenKind};

fn kinds(src: &str) -> Vec<TokenKind> {
    let res = lex(
        src,
        SourceId(0),
        &LexOptions {
            keep_trivia: false,
            enable_directives: true,
        },
    );
    res.tokens.into_iter().map(|t| t.kind).collect()
}

#[test]
fn compound_assignment_tokens() {
    let ks = kinds("x += 1; y -= 2; z *= 3; w /= 4; r %= 5;");
    assert!(ks.contains(&TokenKind::PlusEq));
    assert!(ks.contains(&TokenKind::MinusEq));
    assert!(ks.contains(&TokenKind::StarEq));
    assert!(ks.contains(&TokenKind::SlashEq));
    assert!(ks.contains(&TokenKind::PercentEq));
}

#[test]
fn question_token() {
    let ks = kinds("let v = f()?;");
    assert!(ks.contains(&TokenKind::Question));
}
