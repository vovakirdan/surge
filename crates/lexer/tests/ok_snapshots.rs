mod util;

use std::{fs, path::Path};
use surge_lexer::{LexOptions, lex};
use surge_token::SourceId;
use util::dump_tokens;
use walkdir::WalkDir;

fn collect_ok_files(root: &Path) -> Vec<String> {
    let mut out = Vec::new();
    if root.is_file() {
        out.push(root.to_string_lossy().to_string());
    } else {
        for e in WalkDir::new(root).into_iter().filter_map(Result::ok) {
            let p = e.path();
            if p.is_file() && p.extension().and_then(|s| s.to_str()) == Some("sg") {
                // Берём только позитивные кейсы
                if p.to_string_lossy().contains("/ok/") || p.to_string_lossy().ends_with("ok.sg") {
                    out.push(p.to_string_lossy().to_string());
                }
            }
        }
    }
    out.sort();
    out
}

#[test]
fn ok_files_tokenize_snapshot() {
    let root = Path::new(env!("CARGO_MANIFEST_DIR")).join("../../examples/lexer"); // подстрой если у тебя другой путь
    let files = collect_ok_files(&root);

    let opts = LexOptions {
        keep_trivia: false,
        enable_directives: true,
    };

    for file in files {
        let src =
            fs::read_to_string(&file).unwrap_or_else(|e| panic!("failed to read {file}: {e}"));

        let res = lex(&src, SourceId(0), &opts);

        // Убедимся, что diagnostics пустые для позитивных кейсов
        if !res.diags.is_empty() {
            let msgs = res
                .diags
                .iter()
                .map(|d| format!("{:?}: {}", d.code, d.message))
                .collect::<Vec<_>>()
                .join("\n");
            panic!("unexpected diagnostics for {}:\n{}", file, msgs);
        }

        let dump = dump_tokens(&res.tokens, &src);

        // Ключ снепшота: нормализуем путь, чтобы он красиво назывался
        let name = file
            .replace('\\', "/")
            .split("/examples/lexer/")
            .last()
            .unwrap()
            .replace('/', "__");
        insta::with_settings!({snapshot_suffix => name}, {
            insta::assert_snapshot!(dump);
        });
    }
}
