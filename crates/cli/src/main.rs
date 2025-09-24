use surge_lexer::{LexOptions, SourceId, lex};

fn main() {
    let source = "fn main() { let x = 42; }";
    let file = SourceId(0);
    let opts = LexOptions::default();

    let result = lex(source, file, &opts);

    println!("Tokens:");
    for (i, token) in result.tokens.iter().enumerate() {
        let text = &source[token.span.start as usize..token.span.end as usize];
        println!("  {}: {:?} = {:?}", i, token.kind, text);
    }

    if !result.diags.is_empty() {
        println!("\nDiagnostics:");
        for diag in &result.diags {
            let text = &source[diag.span.start as usize..diag.span.end as usize];
            println!("  {:?}: {} at {:?}", diag.code, diag.message, text);
        }
    }
}
