// Package diag defines the core diagnostic model shared by all pipeline phases.
//
// # Purpose
//
//   - Provide deterministic, serialisable data structures that capture findings
//     produced by lexer / parser / semantic passes.
//   - Offer light-weight utilities (Reporter, Bag) that let producers emit
//     diagnostics without coupling to concrete storage or formatting layers.
//   - Model fix suggestions as structured edits that the driver or CLI can
//     materialise and optionally apply.
//
// # Scope
//
// Package diag does not perform any formatting, IO, CLI integration, or
// interactive behaviour. Rendering responsibilities live in internal/diagfmt,
// whereas orchestration and application of fixes lives in internal/fix and the
// driver layer.
//
// # Data model
//
// Diagnostic is the central record. It contains:
//
//   - Severity – tri-level enum (Info, Warning, Error) defined in severity.go.
//   - Code – compact numeric identifier (see codes.go) with stable string form.
//   - Message – human oriented text; keep it short and actionable.
//   - Primary span – the canonical source.Span pointing to the issue.
//   - Notes – optional secondary spans/messages for additional context.
//   - Fixes – optional Fix records describing how to address the problem.
//
// Notes should be used sparingly: each note must add new context (e.g. “value
// declared here”) rather than repeating the diagnostic message.
//
// # Fix suggestions
//
// Fix represents a possible automated correction. Each fix carries:
//
//   - Title – short label used in UI listings.
//   - Kind – coarse classification (quick fix, refactor, rewrite, source action).
//   - Applicability – confidence level: AlwaysSafe, SafeWithHeuristics,
//     ManualReview.
//   - IsPreferred – optionally mark the most relevant fix when several exist.
//   - Edits – concrete text edits (Span + new/old text) to apply.
//   - Thunk – optional lazy builder used when edits are expensive to construct.
//
// Fixes are intentionally data-only. Producers can attach thunks to defer heavy
// computation; formatters and the fix engine call Resolve/MaterializeFixes to
// expand them deterministically.
//
// TextEdit enforces spans in source coordinates; OldText acts as an optional
// guard that the fix engine uses to validate the context before applying edits.
//
// # Emitting diagnostics
//
// Phases should use a diag.Reporter to decouple emission from storage. The
// parser, for example, constructs a ReportBuilder via NewReportBuilder (or the
// helper functions ReportError/ReportWarning/ReportInfo) and chains WithNote /
// WithFixSuggestion before calling Emit.
//
// When no additional metadata is needed, phases may call Reporter.Report(...)
// directly. For convenience, diag.BagReporter aggregates diagnostics into a Bag,
// which supports sorting, deduplication, filtering, and transformation.
//
// # Consumers
//
//   - internal/diagfmt: renders Diagnostics into pretty/json/sarif formats.
//   - internal/fix: materialises Fix records and applies edits to source files.
//   - internal/driver: coordinates bag collection per file/module and transports
//     diagnostic data to CLI commands.
//
// Keep the data model deterministic: any new fields should honour the package’s
// layering constraints and avoid side effects, so the CLI and future tooling can
// safely serialise diagnostics for caching and testing.
package diag
