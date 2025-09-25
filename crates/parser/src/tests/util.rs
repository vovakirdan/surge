use crate::{ParseResult, parse_source};
use surge_token::SourceId;

pub fn parse(src: &str) -> ParseResult {
    let (res, _) = parse_source(SourceId(0), src);
    res
}

pub fn assert_no_parse_errors(res: &ParseResult) {
    if !res.diags.is_empty() {
        let details: Vec<_> = res
            .diags
            .iter()
            .map(|d| (d.code, d.message.as_str()))
            .collect();
        panic!("expected no parser diagnostics, got: {:?}", details);
    }
}
